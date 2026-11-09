import Foundation
import Testing
@testable import Persona

@MainActor
struct LarkDraftsTests {
    /// Stands in for StateCache: what was last written.
    final class Disk {
        var saved: [String: LarkDraft]?
        var writes = 0
    }

    private func drafts(_ disk: Disk, delay: Duration = .seconds(60)) -> LarkDrafts {
        LarkDrafts(load: { disk.saved }, store: { disk.saved = $0; disk.writes += 1 }, delay: delay)
    }

    private let quote = LarkReplyTo(id: "m1", senderName: "Tom", text: "周六几点集合？")

    @Test func aDraftComesBackWithItsQuote() {
        let d = drafts(Disk())
        d.set(LarkDraft(text: "八点", replyTo: quote), for: .chat("f3"))
        #expect(d[.chat("f3")] == LarkDraft(text: "八点", replyTo: quote))
        #expect(d[.chat("f2")].isEmpty)
    }

    @Test func aChatAndAThreadKeepTheirOwn() {
        let d = drafts(Disk())
        d.set(LarkDraft(text: "群里"), for: .chat("m1"))
        d.set(LarkDraft(text: "话题里"), for: .thread("m1"))
        #expect(d[.chat("m1")].text == "群里")
        #expect(d[.thread("m1")].text == "话题里")
    }

    @Test func sendingClearsIt() {
        let disk = Disk()
        let d = drafts(disk)
        d.set(LarkDraft(text: "八点", replyTo: quote), for: .chat("f3"))
        d.set(LarkDraft(), for: .chat("f3"))
        d.flush()
        #expect(d[.chat("f3")].isEmpty)
        #expect(disk.saved == [:])
    }

    @Test func blankWordsAreNoDraftButAQuoteIs() {
        #expect(LarkDraft(text: " \n ").isEmpty)
        #expect(!LarkDraft(text: "", replyTo: quote).isEmpty)
        let disk = Disk()
        let d = drafts(disk)
        d.set(LarkDraft(text: "\n"), for: .chat("f3"))
        d.set(LarkDraft(replyTo: quote), for: .chat("f2"))
        d.flush()
        #expect(disk.saved?.keys.sorted() == ["chat:f2"])
    }

    @Test func itOutlivesTheApp() {
        let disk = Disk()
        let first = drafts(disk)
        first.set(LarkDraft(text: "九点"), for: .thread("m9"))
        first.flush()
        let again = drafts(disk)
        #expect(again[.thread("m9")].text == "九点")
    }

    @Test func typingWritesOnceAfterAPause() async throws {
        let disk = Disk()
        let d = drafts(disk, delay: .milliseconds(50))
        for n in 1...5 { d.set(LarkDraft(text: String(repeating: "字", count: n)), for: .chat("f3")) }
        d.set(LarkDraft(text: "字字字字字"), for: .chat("f3")) // unchanged: no new write is scheduled
        #expect(disk.writes == 0)
        for _ in 0..<40 where disk.writes == 0 { try await Task.sleep(for: .milliseconds(25)) }
        #expect(disk.writes == 1)
        #expect(disk.saved?["chat:f3"]?.text == "字字字字字")
    }
}

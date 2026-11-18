import Foundation
import Testing
@testable import Persona
import PersonaWire

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

    /// One chat open through two managers (each signed in as its own user) keeps two drafts.
    @Test func eachManagerKeepsItsOwnDraft() {
        let disk = Disk()
        let d = drafts(disk)
        d.set(LarkDraft(text: "工作"), for: .chat("f3", manager: "m-2"))
        d.set(LarkDraft(text: "家里"), for: .chat("f3"))
        d.set(LarkDraft(text: "话题里"), for: .thread("m1", manager: "m-2"))
        #expect(d[.chat("f3", manager: "m-2")].text == "工作")
        #expect(d[.chat("f3")].text == "家里", "the default's is its own")
        #expect(d[.chat("f3", manager: "m-3")].isEmpty)
        #expect(d[.thread("m1", manager: "m-2")].text == "话题里" && d[.thread("m1")].isEmpty)
        d.flush()
        #expect(disk.saved?.keys.sorted() == ["chat:f3", "chat:f3@m-2", "thread:m1@m-2"])
    }

    /// The default's drafts keep the names they had on disk: one kept before managers comes back in the default's chat only.
    @Test func theDefaultsDraftsKeepTheirNames() {
        let disk = Disk()
        disk.saved = ["chat:f3": LarkDraft(text: "八点"), "thread:m9": LarkDraft(text: "九点")]
        let d = drafts(disk)
        #expect(d[.chat("f3")].text == "八点" && d[.thread("m9")].text == "九点")
        #expect(d[.chat("f3", manager: "m-2")].isEmpty && d[.thread("m9", manager: "m-2")].isEmpty, "never a named manager's")
        #expect(LarkDrafts.Key.chat("f3", manager: nil).name == "chat:f3")
        #expect(LarkDrafts.Key.thread("m9").name == "thread:m9")
    }
}

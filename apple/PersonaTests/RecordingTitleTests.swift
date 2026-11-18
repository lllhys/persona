import Foundation
import Testing
@testable import Persona
import PersonaWire

/// What a recording is called (#8873, docs/apple/recording.md「Titles」): its own title (the owner's, or the calendar
/// event's), else the hub's name (a calendar item it matched, or a title written from what was said), else its kind and
/// when it started. The hub's name is kept without making the recording look changed; a rename is the recording's own.
@MainActor struct RecordingTitleTests {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func entry(title: String = "", hubName: String? = nil, meeting: Bool = true) -> RecordingEntry {
        var e = RecordingEntry(id: UUID(), createdAt: now, source: .phone, title: title, active: false)
        e.meeting = meeting ? RecordingMeeting() : nil
        e.hubName = hubName
        return e
    }

    private func library() -> RecordingLibrary {
        RecordingLibrary(root: FileManager.default.temporaryDirectory.appendingPathComponent("titles-" + UUID().uuidString))
    }

    @Test func ownTitleThenHubNameThenTime() {
        let when = now.formatted(date: .abbreviated, time: .shortened)
        #expect(entry(title: "Design review", hubName: "Export cap").displayTitle == "Design review")
        #expect(RecordingTitle.source(entry(title: "Design review", hubName: "Export cap")) == .own)
        #expect(entry(hubName: "Export cap").displayTitle == "Export cap")
        #expect(RecordingTitle.source(entry(hubName: "Export cap")) == .hub)
        #expect(entry().displayTitle == String(localized: "Meeting · \(when)"))
        #expect(entry(meeting: false).displayTitle == String(localized: "Note · \(when)")) // a note says so (#8731)
        #expect(entry(hubName: "").displayTitle == String(localized: "Meeting · \(when)"))
        // An all-day recording keeps its day's name.
        var day = entry(hubName: "Export cap", meeting: false)
        day.kind = .day
        #expect(RecordingTitle.source(day) == .time)
    }

    @Test func hubNameKeepsTheCalendarsAndTheModels() {
        #expect(RecordingTitle.hubName(name: "Export cap", source: "model") == "Export cap")
        #expect(RecordingTitle.hubName(name: " Weekly sync ", source: "calendar") == "Weekly sync")
        #expect(RecordingTitle.hubName(name: "Two\nlines", source: "model") == "Two lines")
        // The recording's own title (this phone has it), a source this app doesn't know, and none at all.
        #expect(RecordingTitle.hubName(name: "Mine", source: "title") == nil)
        #expect(RecordingTitle.hubName(name: "Later", source: "web") == nil)
        #expect(RecordingTitle.hubName(name: nil, source: nil) == nil)
        #expect(RecordingTitle.hubName(name: "  ", source: "model") == nil)
        #expect(RecordingTitle.hubName(name: String(repeating: "a", count: 300), source: "model")?.count == RecordingTitle.maxLength)
    }

    /// The hub's name is kept without moving the revision or telling sync, so it never uploads the recording again; a
    /// rename is a change of the recording's own, uploaded with it.
    @Test func hubNameIsNotAChangeButARenameIs() throws {
        let lib = library()
        var changed: [UUID] = []
        lib.didChange = { changed.append($0) }
        let made = try lib.create(source: .phone, meeting: RecordingMeeting())
        changed = []
        let revision = try #require(lib.entry(made.id)).revision
        lib.noteHubName(made.id, "Export cap")
        #expect(lib.entry(made.id)?.displayTitle == "Export cap")
        #expect(lib.entry(made.id)?.revision == revision)
        #expect(changed.isEmpty)
        // Kept on disk: a later launch reads it back.
        #expect(RecordingLibrary(root: lib.root).entry(made.id)?.hubName == "Export cap")
        // The same name again writes nothing.
        lib.noteHubName(made.id, "Export cap")
        #expect(changed.isEmpty)

        lib.rename(made.id, to: "  My title\n")
        #expect(lib.entry(made.id)?.title == "My title")
        #expect(lib.entry(made.id)?.displayTitle == "My title")
        #expect(try #require(lib.entry(made.id)).revision > revision)
        #expect(changed == [made.id])
        // A newer generated name never takes its place.
        lib.noteHubName(made.id, "Release date")
        #expect(lib.entry(made.id)?.displayTitle == "My title")
        // Cleared: the hub's name again.
        lib.rename(made.id, to: "")
        #expect(lib.entry(made.id)?.displayTitle == "Release date")
        // Never uploaded.
        let body = try JSONSerialization.jsonObject(with: JSON.encoder.encode(RecordingUpload(try #require(lib.entry(made.id))))) as? [String: Any]
        #expect(body?["title"] as? String == "")
        #expect(body?.keys.contains { $0.contains("hub") } == false)
    }

    /// A name the owner gave on the web (#8877) becomes the recording's own title, unless it already is or a change of
    /// this phone's is still to go up (its newer title wins on the hub); other names are kept beside it.
    @Test func webNameIsTakenAsTheRecordingsOwn() {
        let mine = entry(title: "Design review")
        #expect(RecordingTitle.naming(mine, name: "Export cap", source: "renamed", pending: false) == .adopt("Export cap"))
        #expect(RecordingTitle.naming(mine, name: " Export cap\n", source: "renamed", pending: false) == .adopt("Export cap"))
        #expect(RecordingTitle.naming(mine, name: "Export cap", source: "renamed", pending: true) == .keep(nil))
        #expect(RecordingTitle.naming(mine, name: "Design review", source: "renamed", pending: false) == .keep(nil))
        #expect(RecordingTitle.naming(mine, name: "", source: "renamed", pending: false) == .keep(nil))
        #expect(RecordingTitle.naming(entry(), name: "Export cap", source: "model", pending: false) == .keep("Export cap"))
        #expect(RecordingTitle.naming(mine, name: "Design review", source: "title", pending: false) == .keep(nil))
        #expect(RecordingTitle.naming(entry(), name: nil, source: nil, pending: true) == .keep(nil))
    }

    /// An entry saved before the field existed reads as one with no hub name.
    @Test func entriesFromBeforeReadAsUnnamed() throws {
        let old = #"{"id":"0F8FAD5B-D9CB-469F-A165-70867728950E","createdAt":0,"source":"phone","title":"","locale":"zh-CN","active":false,"segments":[],"marks":[],"endDelivered":false}"#
        let e = try JSONDecoder().decode(RecordingEntry.self, from: Data(old.utf8))
        #expect(e.hubName == nil)
    }
}

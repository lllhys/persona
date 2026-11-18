import Foundation
import Testing
@testable import Persona

/// Persona's echo against the answer still being said (#5905): an upload while Persona speaks marks the lines its stretch
/// so far echoes and sends the stretch with the others, to a hub that lists `meeting_echo`; to one that doesn't, it leaves
/// them out. Someone speaking meanwhile goes as the meeting's either way.
@MainActor struct MeetingEchoTests {
    private func meeting() -> RecordingEntry {
        var e = RecordingEntry(id: UUID(), createdAt: Date(timeIntervalSince1970: 1_800_000_000), source: .phone, title: "Weekly",
                               locale: "en-US", active: true)
        e.meeting = RecordingMeeting(title: "Weekly")
        e.attendance = RecordingAttendance(role: .participant, mainPicked: true)
        e.attendance?.spoken = [RecordingSpan(start: 1, end: 4, text: "Who owns the test?")]
        e.segments = [RecordingSegment(index: 0, start: 0, duration: 60, sentences: [
            RecordingSentence(start: 1, end: 4, text: "Who owns the test?"),
            RecordingSentence(start: 10.2, end: 13, text: "We keep the cap for this release."),
            RecordingSentence(start: 13.2, end: 15, text: "Bo owns the regression test."),
        ])]
        return e
    }

    private let open = RecordingSpan(start: 10, end: 15, text: "We keep the cap for this release, and revisit it next quarter.")

    @Test func theAnswerBeingSaidIsMarkedForAHubThatTakesIt() {
        var body = RecordingUpload(meeting(), hubEcho: true)
        body.speaking(open)
        #expect(body.segments.map(\.text) == ["Who owns the test?", "We keep the cap for this release.", "Bo owns the regression test."])
        #expect(body.segments.map(\.echo) == [true, true, nil])
        #expect(body.spoken?.map(\.text) == ["Who owns the test?", open.text])
    }

    @Test func theAnswerBeingSaidIsLeftOutForAnOlderHub() {
        var body = RecordingUpload(meeting(), hubEcho: false)
        body.speaking(open)
        #expect(body.segments.map(\.text) == ["Bo owns the regression test."])
        #expect(body.segments.allSatisfy { $0.echo == nil })
        #expect(body.spoken == nil)
    }

    /// #5905 verification: whether the hub takes echo lines marked is that hub's own `hello`. Before it arrives, a "yes"
    /// kept from an earlier hub (`MeetingAttendanceDefaults.echoKey`) is not taken: another hub, maybe an older one, would
    /// put Persona's words into its minutes. The echo line is left out until this hub says `meeting_echo`.
    @Test func onlyTheHubsOwnHelloSendsEchoLines() async throws {
        let store = UserDefaults.standard, key = MeetingAttendanceDefaults.echoKey
        let kept = store.object(forKey: key)
        defer { if let kept { store.set(kept, forKey: key) } else { store.removeObject(forKey: key) } }
        store.set(true, forKey: key) // the last hub heard listed meeting_echo
        let lib = RecordingLibrary(root: FileManager.default.temporaryDirectory.appending(path: "meeting-echo-\(UUID())"))
        let e = meeting()
        try lib.save(e)
        let hub = FakeHub()
        let put = "/api/v1/recordings/\(e.id.uuidString.lowercased())"
        hub.on("GET", "/api/v1/recordings", json: #"{"days":14,"recordings":[],"deleted":[]}"#)
        hub.on("PUT", put) { req in
            let revision = ((try? JSONSerialization.jsonObject(with: req.body ?? Data())) as? [String: Any])?["revision"] as? NSNumber
            return Data(#"{"id":"x","revision":\#(revision?.int64Value ?? 0),"status":"stored"}"#.utf8)
        }
        let client = hub.client
        var features: Set<String> = []
        let sync = RecordingSync(library: lib, api: { client }, hubFeatures: { features })
        func sent() throws -> [String: Any] {
            try JSONSerialization.jsonObject(with: try #require(hub.sent("PUT", put).last?.body)) as? [String: Any] ?? [:]
        }

        await sync.syncNow() // no hello from this hub yet
        var body = try sent()
        #expect((body["segments"] as? [[String: Any]])?.compactMap { $0["text"] as? String }
            == ["We keep the cap for this release.", "Bo owns the regression test."])
        #expect(body["spoken"] == nil)

        features = ["meeting_attendance", "meeting_echo"] // its hello says it takes them
        lib.change(e.id) { $0.title = "Weekly, later" }
        await sync.syncNow()
        try #require(hub.sent("PUT", put).count == 2)
        body = try sent()
        #expect((body["segments"] as? [[String: Any]])?.map { $0["echo"] as? Bool } == [true, nil, nil])
        #expect((body["spoken"] as? [[String: Any]])?.count == 1)
    }

    @Test func aStretchSavedBeforeHasNoTextOnTheWire() throws {
        let old = try JSONDecoder().decode(RecordingSpan.self, from: Data(#"{"start":1,"end":2}"#.utf8))
        #expect(old.text == nil && old.echoes(RecordingSentence(start: 1, end: 2, text: "anything")))
        let sent = try JSONSerialization.jsonObject(with: JSONEncoder().encode(RecordingUpload.Spoken(old))) as? [String: Any]
        #expect(sent?.keys.sorted() == ["end", "start"])
    }

    @Test func theRuleIsTheHubs() {
        #expect(MeetingEcho.margin == 0.5 && MeetingEcho.containment == 0.6)
        #expect(String(String.UnicodeScalarView(MeetingEcho.normalized("这一版先保留４ＭＢ的上限。"))) == "这一版先保留4mb的上限")
        #expect(String(String.UnicodeScalarView(MeetingEcho.normalized("Cafe\u{301}\u{301} \u{2460}"))) == "café1")
        #expect(MeetingEcho.normalized("……！").isEmpty)
    }
}

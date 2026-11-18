import Foundation
import Testing
@testable import Persona

/// Persona's echo in a meeting (#5905, docs/assistant/meeting-minutes.md「Persona's echo」): every shared case in
/// spec/fixtures/meeting-echo, which the hub's rule (`server/internal/kb/echo.go`) runs too. A case's line is the
/// meeting's when the transcript shows it (`RecordingEntry.sentences`), Persona's echo when it is hidden there.
@MainActor
struct MeetingEchoFixtureTests {
    private static let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()

    private struct Fixtures: Decodable {
        struct Case: Decodable {
            struct Input: Decodable {
                var line: RecordingSentence
                var spans: [RecordingSpan]
            }
            struct Expected: Decodable { var echo: Bool }
            var name: String
            var input: Input
            var expected: Expected
        }
        var version: Int
        var cases: [Case]
    }

    @Test func readsTheSharedFixturesLikeTheHub() throws {
        let data = try Data(contentsOf: Self.repo.appending(path: "spec/fixtures/meeting-echo/cases.json"))
        let fixtures = try JSONDecoder().decode(Fixtures.self, from: data)
        #expect(fixtures.version == 1)
        #expect(fixtures.cases.count >= 20)
        for c in fixtures.cases {
            var e = RecordingEntry(id: UUID(), createdAt: Date(timeIntervalSince1970: 1_800_000_000), source: .phone, title: "Weekly",
                                   locale: "en-US", active: true)
            e.meeting = RecordingMeeting(title: "Weekly")
            e.attendance = RecordingAttendance(role: .participant, mainPicked: true)
            e.attendance?.spoken = c.input.spans
            e.segments = [RecordingSegment(index: 0, start: 0, duration: 60, sentences: [c.input.line])]
            #expect(e.sentences.isEmpty == c.expected.echo, "\(c.name)")
        }
    }
}

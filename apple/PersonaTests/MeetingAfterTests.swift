import Foundation
import Testing
@testable import Persona

/// After the meeting, in one screen (#8731 V3): the summary card's lines and counts, and the map's topics.
struct MeetingAfterTests {
    private func item(_ text: String) -> RecordingMinutes.Item { RecordingMinutes.Item(text: text) }

    private func line(_ text: String, decision: Bool = false) -> RecordingMinutes.Overview.Line {
        var l = RecordingMinutes.Overview.Line(text: text)
        l.decision = decision
        return l
    }

    /// An overview: its headline and at most three conclusions; the decisions marked on its bullets when the hub sent no
    /// list of them; the to-dos and open questions counted.
    @Test func anOverviewGivesItsHeadlineAndThreeConclusions() {
        var minutes = RecordingMinutes()
        minutes.overview = RecordingMinutes.Overview(
            headline: "  Ship on the 28th; keep the 4 MB cap. ",
            conclusions: ["Ship on the 28th", "Keep the 4 MB cap", "Bo owns the load test", "Cache cause by Thursday"].map { line($0) },
            groups: [.init(title: "", topics: [
                .init(title: "Release", bullets: [line("Ship on the 28th", decision: true), line("Freeze on Friday")]),
                .init(title: "Export", bullets: [line("Keep the 4 MB cap", decision: true)]),
            ])])
        minutes.actions = [item("Load test results"), item("Find the cache cause")]
        minutes.openQuestions = [item("Who owns hiring?")]
        let summary = MeetingAfterSummary.make(minutes)
        #expect(summary?.headline == "Ship on the 28th; keep the 4 MB cap.")
        #expect(summary?.lines == ["Ship on the 28th", "Keep the 4 MB cap", "Bo owns the load test"])
        #expect(summary?.decisions == 2 && summary?.todos == 2 && summary?.open == 1)
        minutes.decisions = [item("Ship on the 28th")]
        #expect(MeetingAfterSummary.make(minutes)?.decisions == 1, "the hub's own list of decisions counts first")
    }

    /// The older shape: the first two points, the lists counted; minutes that say nothing make no card.
    @Test func olderMinutesGiveTheirFirstTwoPoints() {
        var minutes = RecordingMinutes()
        minutes.points = ["We looked at the export flow.", " ", "The cap stays at 4 MB.", "Hiring is open."].map { item($0) }
        minutes.decisions = [item("Keep the cap")]
        let summary = MeetingAfterSummary.make(minutes)
        #expect(summary?.headline == nil)
        #expect(summary?.lines == ["We looked at the export flow.", "The cap stays at 4 MB."])
        #expect(summary?.decisions == 1 && summary?.todos == 0 && summary?.open == 0)
        #expect(MeetingAfterSummary.make(RecordingMinutes()) == nil)
    }

    /// The map: the minutes' topics with their bullets as the note; without an overview, the room's own topics; else none.
    @Test func theMapTakesTheMinutesTopicsThenTheRooms() {
        let live = [MeetingTopic(title: "Release", state: "done"), MeetingTopic(title: "Load test", state: "current")]
        var minutes = RecordingMinutes()
        #expect(MeetingAfterMap.topics(minutes: nil, live: live) == live)
        #expect(MeetingAfterMap.topics(minutes: minutes, live: live) == live)
        #expect(MeetingAfterMap.topics(minutes: nil, live: []).isEmpty)
        minutes.overview = RecordingMinutes.Overview(headline: nil, conclusions: [], groups: [
            .init(title: "", topics: [.init(title: "Release", bullets: [line("Ship on the 28th"), line("Freeze on Friday")])]),
        ])
        let topics = MeetingAfterMap.topics(minutes: minutes, live: live)
        #expect(topics.map(\.title) == ["Release"])
        #expect(topics.first?.note == "• Ship on the 28th\n• Freeze on Friday")
        #expect(topics.first?.known == .done)
    }

    /// The parts under the summary: Minutes and Transcript always; Photos where the room took them; Conversation where the
    /// room or a follow-up has one.
    @MainActor @Test func thePartsAMeetingShows() {
        #expect(MeetingAfterPart.shown(photos: false, conversation: false) == [.minutes, .transcript])
        #expect(MeetingAfterPart.shown(photos: true, conversation: true) == [.minutes, .transcript, .photos, .conversation])
        #expect(MeetingAfterPart.shown(photos: false, conversation: true) == [.minutes, .transcript, .conversation])
    }
}

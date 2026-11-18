import Foundation
import Testing
@testable import Persona

/// The Meetings home (#8731 v2): today's calendar meetings on now or still to come, and the start's name.
struct MeetingHomeTests {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private func event(_ title: String, from: Double, to: Double, allDay: Bool = false, declined: Bool = false) -> MeetingCalendar.Candidate {
        MeetingCalendar.Candidate(id: title, title: title, start: now.addingTimeInterval(from * 3600), end: now.addingTimeInterval(to * 3600),
                                  allDay: allDay, declined: declined)
    }

    @Test func upcomingIsTodaysOnNowOrLaterEarliestFirst() {
        let dayEnd = now.addingTimeInterval(8 * 3600)
        let events = [
            event("later", from: 3, to: 4),
            event("on now", from: -0.5, to: 0.5),
            event("over", from: -2, to: -1),
            event("tomorrow", from: 9, to: 10),
            event("all day", from: -5, to: 19, allDay: true),
            event("declined", from: 1, to: 2, declined: true),
            event("  ", from: 1, to: 2),
            event("same start, shorter", from: 3, to: 3.5),
        ]
        let titles = MeetingCalendar.upcoming(events, at: now, dayEnd: dayEnd, limit: 5).map(\.title)
        #expect(titles == ["on now", "same start, shorter", "later"])
        #expect(MeetingCalendar.upcoming(events, at: now, dayEnd: dayEnd, limit: 1).map(\.title) == ["on now"])
        #expect(MeetingCalendar.upcoming([], at: now, dayEnd: dayEnd, limit: 3).isEmpty)
    }

    @Test func aMeetingNamedAfterItsEventKeepsItsDetails() {
        let e = event("产品周会", from: 0, to: 1)
        let m = RecordingMeeting(e)
        #expect(m.title == "产品周会" && m.eventID == "产品周会" && m.start == e.start && m.end == e.end)
    }

    @Test func aTypedNameIsOneLineAndTrimmed() {
        #expect(RecordingTitle.cleaned("  周会\n复盘 ") == "周会 复盘")
        #expect(RecordingTitle.cleaned("   ").isEmpty)
    }
}

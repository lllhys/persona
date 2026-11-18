import Foundation
import Testing
@testable import Persona

/// The recording's Live Activity once iOS ended the app (#9108): the app confirms a running recording once a minute
/// (`aliveAt`), the activity goes stale a little after the last confirmation, and the widget then draws it stopped, its
/// time standing where it was confirmed, instead of counting on as if it still recorded.
struct RecordingActivityStaleTests {
    typealias Content = RecordingActivityAttributes.ContentState
    private let now = Date(timeIntervalSince1970: 1_800_000_030) // 30 s into a minute

    private func entry() -> RecordingEntry {
        var e = RecordingEntry(id: UUID(), createdAt: now, source: .phone, title: "Weekly")
        e.meeting = RecordingMeeting(title: "Weekly")
        return e
    }

    private func running(elapsed: Double = 100) -> Content {
        RecordingActivity.content(entry: entry(), recording: true, interrupted: false, elapsed: elapsed, previous: nil, now: now)
    }

    @Test func aRunningRecordingIsConfirmedOnTheMinuteAndGoesStaleAfter() throws {
        let alive = RecordingActivity.alive(running(), now: now)
        #expect(alive.aliveAt == 1_800_000_000)
        #expect(RecordingActivity.staleDate(alive) == Date(timeIntervalSince1970: 1_800_000_000 + RecordingActivity.staleAfter))
        // Two missed confirmations and some: a busy minute never makes a running recording's activity stale.
        #expect(RecordingActivity.staleAfter > 2 * RecordingActivity.aliveEvery)
        // Within the minute nothing changes (no update); the next minute is a new confirmation.
        #expect(RecordingActivity.alive(running(), now: now.addingTimeInterval(20)) == alive)
        #expect(RecordingActivity.alive(running(), now: now.addingTimeInterval(40)).aliveAt == 1_800_000_060)
    }

    @Test func aPausedRecordingNeverGoesStale() {
        let paused = RecordingActivity.content(entry: entry(), recording: false, interrupted: false, elapsed: 130,
                                               previous: running(), now: now)
        let call = RecordingActivity.content(entry: entry(), recording: false, interrupted: true, elapsed: 130,
                                             previous: running(), now: now)
        for c in [paused, call] {
            let alive = RecordingActivity.alive(c, now: now)
            #expect(alive.aliveAt == nil)
            #expect(RecordingActivity.staleDate(alive) == nil)
            #expect(alive.drawn(stale: true) == alive)
        }
    }

    @Test func aStaleRunningRecordingIsDrawnStoppedAtItsLastConfirmation() {
        var talk = RecordingActivity.alive(running(elapsed: 100), now: now)
        talk.talking = true
        let drawn = talk.drawn(stale: true)
        #expect(drawn.phase == .stopped)
        #expect(drawn.runningSince == nil)
        // runningSince is now − 100 s; the last confirmation was 30 s before now.
        #expect(drawn.elapsed == 70)
        #expect(drawn.talking == false)
        #expect(drawn.title == talk.title)
        // Not stale: as it is.
        #expect(talk.drawn(stale: false) == talk)
    }

    /// A state from a build before `aliveAt` (or one the app sent while paused) reads as before.
    @Test func aStateWithoutTheConfirmationDecodes() throws {
        let old = #"{"phase":"recording","runningSince":1800000000,"elapsed":0,"title":"Weekly","meeting":true,"marks":0}"#
        let c = try JSONDecoder().decode(Content.self, from: Data(old.utf8))
        #expect(c.aliveAt == nil)
        #expect(c.phase == .recording)
        let encoded = try JSONEncoder().encode(RecordingActivity.alive(running(), now: now))
        #expect(encoded.count < 1024)
        #expect(try JSONDecoder().decode(Content.self, from: encoded).aliveAt == 1_800_000_000)
    }
}

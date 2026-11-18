#if os(iOS)
import Foundation
import Observation
import UserNotifications

/// Talk about this meeting (#3862 phase 6, docs/assistant/meeting-minutes.md「Phase 6: talk about this meeting」): any
/// recording with text or minutes opens the meeting's one thread (`MeetingPage` after mode) and writes into its
/// follow-up through phase 1's `POST …/followup`; no flag of its own. The minutes push names its recording
/// (`recording_id`), and a tap opens that thread when the recording is on this phone. Pure, for the tests.
nonisolated enum MeetingTalk {
    /// Whether a recording's page offers **Talk about this meeting**: a hub with the meeting conversation (phase 1's route),
    /// a recording that has ended (while it records, a room meeting's page is the live sheet and the hub would answer
    /// `409 meeting_running`), not a rolling or all-day one (they have pages of their own), with words in its transcript
    /// (`text`, from `hasText`), minutes ready, or a conversation the hub already holds for it (a follow-up, or a room that
    /// came up). Past retention (`expired`, `RecordingSync.Status.expired`) the transcript here is no material: the hub
    /// has cut it, or deleted the recording, and the first message would be refused (`409 no_material`, `404`); only the
    /// minutes, the follow-up or a room whose conversation the hub still has count then (it deletes the room at the cut).
    static func offered(_ entry: RecordingEntry, conversation: Bool, minutes: Bool, meeting: MeetingConversation.Meeting,
                        text: Bool, expired: Bool) -> Bool {
        guard conversation, !entry.active, !entry.isRolling, !entry.isDay else { return false }
        if minutes || meeting.followup != nil { return true }
        if expired { return meeting.room?.key(group: "") != nil }
        if entry.roomMeeting || meeting.room.map({ $0.known != MeetingRoom.State.none }) == true { return true }
        return text
    }

    /// Whether the transcript has words (what `entry.sentences` would hold that isn't blank), walking the segments and
    /// stopping at the first line rather than sorting and joining them all (docs/apple/performance.md P4).
    static func hasText(_ entry: RecordingEntry) -> Bool {
        let spoken = entry.attendance.flatMap { $0.spoken.isEmpty ? nil : $0 }
        for segment in entry.segments {
            for line in segment.sentences ?? [] where !line.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                if spoken?.echo(line) != true { return true }
            }
        }
        return false
    }

    /// The push's key for the recording it is about (the minutes push; the hub lowercases it). Optional: older hubs leave it out.
    static let pushKey = "recording_id"

    /// The recording a push names (`recording_id`), read off the main actor as the tap arrives; nil without the field or
    /// when it isn't a UUID.
    static func recordingID(_ info: [AnyHashable: Any]) -> UUID? {
        guard let raw = info[pushKey] as? String else { return nil }
        return UUID(uuidString: raw.trimmingCharacters(in: .whitespacesAndNewlines))
    }

    /// The recording whose thread a tapped push opens: one it names (`recordingID`) that is on this phone (`onPhone`),
    /// tapped to open (not dismissed) and with no card to answer (`interaction`). Nil otherwise (recorded elsewhere, an
    /// older hub), and the push opens what it opens today: the session it names.
    static func threadToOpen(_ recording: UUID?, interaction: String?, action: String, onPhone: (UUID) -> Bool) -> UUID? {
        guard let recording, interaction == nil, action != UNNotificationDismissActionIdentifier, onPhone(recording) else { return nil }
        return recording
    }
}

/// A recording's meeting page asked for from outside the Recordings page (the minutes push): Recordings opens
/// (`MeetingRecorder.openMeeting`) and pushes the page over itself once it is on screen (`RecordingsPage`). A request
/// nobody took within `Self.stale` is dropped, so a launch that never reached Recordings doesn't push it later.
@MainActor @Observable final class MeetingPageOpener {
    static let shared = MeetingPageOpener()

    private(set) var serial = 0
    @ObservationIgnored private var pending: (id: UUID, at: Date)?
    nonisolated static let stale: TimeInterval = 30

    func open(_ id: UUID, now: Date = .now) {
        pending = (id, now)
        serial += 1
    }

    /// The recording to push now, once: nil when there is none, or it waited too long.
    func take(now: Date = .now) -> UUID? {
        guard let pending else { return nil }
        self.pending = nil
        return now.timeIntervalSince(pending.at) <= Self.stale ? pending.id : nil
    }
}
#endif

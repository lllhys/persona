import ActivityKit
import Foundation

/// The Live Activity of a recording on this phone (docs/apple/live-activity.md「Recording」): the Lock Screen and the Dynamic
/// Island while it records, with Stop and Bookmark. Local only (`pushType: nil`): the hub never starts or updates it.
/// Its name is the system's identity of the activity: never rename it.
nonisolated struct RecordingActivityAttributes: ActivityAttributes {
    nonisolated struct ContentState: Codable, Hashable, Sendable {
        enum Phase: String, Codable, Hashable, Sendable {
            case recording
            /// Paused by the user.
            case paused
            /// Paused by a call or another app: resumes by itself, or when Persona is opened.
            case interrupted
            /// Drawn only, never sent: a running recording whose activity went stale (`drawn(stale:)`). The app stopped
            /// confirming it (`aliveAt`), so iOS ended the app with it (#9108: killed in the background for CPU).
            case stopped
        }

        var phase: Phase
        /// While recording: when the recorded time would have been zero (Unix seconds; the default decoder counts from
        /// 2001), so the system counts the time without updates. Paused, the time stands at `elapsed`.
        var runningSince: Double?
        var elapsed: Double
        var title: String
        var meeting: Bool
        var marks: Int
        /// Persona attends the meeting (#2981): `listener` or `participant` (`MeetingAttendance`), nil when it doesn't.
        /// Said under the name, so anyone who sees the phone knows, with Ask Persona and a way to turn it off.
        var persona: String? = nil
        /// Persona's latest answer in this meeting (the hub posts no notification for answers), cut short to fit. With the
        /// live view (#3862 phase 2) only for 60 seconds after it came, then `line` takes its place (never both).
        var answer: String? = nil
        /// The meeting's current topic, as the room set it (#3862 phase 2, at most 60 characters): "Now: <topic>" under
        /// the name. nil without the live view, before the room names one, or while Persona doesn't attend.
        var topic: String? = nil
        /// Where the current topic stands among the room's topics (#8731 V4): its place from 1 and how many there are
        /// ("Topic 2 / 4: <topic>"). nil beside no `topic`, and in a state from before.
        var topicNumber: Int? = nil
        var topicCount: Int? = nil
        /// The latest line of the meeting's live log (phase 2, at most 120 characters): what was said while Persona
        /// attended, shown when no answer is new. nil without the live view.
        var line: String? = nil
        /// Talk (#3862 phase 4, docs/assistant/meeting-minutes.md「The meeting card」): nil where the meeting has none
        /// (the first button is Ask Persona, as before); false where it does (the first button is Talk, whose tap opens a
        /// talk window); true while a talk window is open ("Listening to you…", the talk mark in the island).
        var talking: Bool? = nil
        /// Hub transcription (#3862 phase 5): true while a hub-mode meeting's `line` is this iPhone's own because the
        /// hub's transcription fell back ("On iPhone: …" on the third line); nil otherwise, so a state from before reads
        /// as it was. Set only beside a `line`.
        var fellBack: Bool? = nil
        /// While recording: the minute the app last confirmed it still records (Unix seconds). The activity goes stale a
        /// little after it (`RecordingActivity.staleAfter`), so when iOS ends the app in the background (for CPU, #9108)
        /// the island stops counting instead of showing a recording that no longer runs. nil while paused, and in a state
        /// from before.
        var aliveAt: Double? = nil

        var runningSinceDate: Date? { runningSince.map { Date(timeIntervalSince1970: $0) } }

        /// What the widget draws: a recording whose activity went stale (`stale`, the system's `isStale`) as stopped,
        /// its time standing where the app last confirmed it; anything else as it is. Pure, for the tests.
        func drawn(stale: Bool) -> Self {
            guard stale, phase == .recording else { return self }
            var out = self
            out.phase = .stopped
            if let since = runningSince, let alive = aliveAt { out.elapsed = max(0, alive - since) }
            out.runningSince = nil
            out.talking = talking == nil ? nil : false
            return out
        }
    }

    /// The recording's id (`RecordingEntry.id`): what Stop and Bookmark act on.
    var recordingID: String
}

/// The links the recording activity opens: its Recordings page.
nonisolated enum RecordingLink {
    /// persona://record: the Recordings page (the app's own scheme, `HubNaming`).
    static let page = URL(string: "persona://record")!
    /// In a local notification's user info: tapping it opens the Recordings page.
    static let notificationKey = "persona_recording"

    static func matches(_ url: URL) -> Bool {
        HubNaming.accepts(scheme: url.scheme) && url.host() == "record"
    }

    /// A meeting's End on the Lock Screen and in the Dynamic Island (#8731 V4): persona://record?end=<recording id> opens
    /// the meeting and asks "End the Meeting?", as its End in the app does. An ended meeting can't go on recording, so a
    /// tap never ends it at once.
    static func end(_ recordingID: String) -> URL {
        var parts = URLComponents(url: page, resolvingAgainstBaseURL: false)!
        parts.queryItems = [URLQueryItem(name: "end", value: recordingID)]
        return parts.url!
    }

    /// The recording `end(_:)` names, lowercase; nil for any other link.
    static func endID(_ url: URL) -> String? {
        guard matches(url) else { return nil }
        let value = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "end" }?.value
        return value.flatMap { $0.isEmpty ? nil : $0.lowercased() }
    }
}

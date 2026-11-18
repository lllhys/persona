import Foundation

/// Meeting or Note (#5243, docs/apple/recording.md「Meeting recordings」): what Record makes, on the iPhone and, since
/// #5493, on the watch. A meeting gets minutes and Persona can attend it (`kind: meeting`); a note gets neither
/// (`kind: note`). Record is always a meeting unless Note is picked for that one start (the iPhone's Recordings page, the
/// watch's recording page), and the iPhone's live sheet switches a running recording either way
/// (`MeetingRecorder.setMeeting`). Never a stored setting: every one-tap start elsewhere is a meeting. Shared so both
/// devices say the same words with the same marks.
nonisolated enum RecordingKindChoice: String, CaseIterable, Hashable, Sendable {
    case meeting
    case note

    init(_ entry: RecordingEntry) { self = entry.isMeeting ? .meeting : .note }

    var title: String {
        switch self {
        case .meeting: String(localized: "Meeting")
        case .note: String(localized: "Note [Recording Kind]")
        }
    }

    /// The mark the recording's row carries for it (`RecordingRow`).
    var symbol: String {
        switch self {
        case .meeting: Theme.Icon.meeting
        case .note: Theme.Icon.recording
        }
    }

    /// What a start of this kind marks the recording with on the watch, which reads no calendar: an empty meeting, which
    /// the phone names after the calendar event at the recording's start (`MeetingRecorder.tagWatchRecording`), or none
    /// for a note. Every segment and the end message carry it (`RecordingTransfer.meeting`).
    var watchMeeting: RecordingMeeting? { self == .meeting ? RecordingMeeting() : nil }
}

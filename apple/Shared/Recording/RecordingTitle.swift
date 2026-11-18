import Foundation

/// What a recording is called (#8873, docs/apple/recording.md「Titles」), in one place for every screen, which reads it
/// through `RecordingEntry.displayTitle`: its own title (the owner's, or the calendar event's this phone matched), else
/// the name the hub gave it (a calendar item it matched, or a title written from what was said; `hubName`), else its
/// kind and when it started. The owner's title always comes first, so a renamed recording keeps its name.
nonisolated enum RecordingTitle {
    /// Where the title shown comes from.
    enum Source: Equatable, Sendable {
        /// The recording's own `title`: the owner's, or the calendar event's.
        case own
        /// The hub's name for it (`hubName`).
        case hub
        /// Nobody named it: its kind and when it started.
        case time
    }

    static func source(_ entry: RecordingEntry) -> Source {
        if !entry.title.isEmpty { return .own }
        if !entry.isDay, let name = entry.hubName, !name.isEmpty { return .hub }
        return .time
    }

    static func text(_ entry: RecordingEntry) -> String {
        switch source(entry) {
        case .own: entry.title
        case .hub: entry.hubName ?? ""
        case .time: untitled(entry)
        }
    }

    /// A recording nobody named: its kind and when it started; a day recording, its day in its own time zone.
    static func untitled(_ entry: RecordingEntry) -> String {
        if entry.isDay, let date = entry.dayTitleDate { return String(localized: "Day · \(date)") }
        let when = entry.createdAt.formatted(date: .abbreviated, time: .shortened)
        if entry.isRolling { return String(localized: "Rolling · \(when)") }
        // A recording that isn't a meeting is a note (#8731, D7): it says so, as the Meetings list's pill does.
        return entry.isMeeting ? String(localized: "Meeting · \(when)") : String(localized: "Note · \(when)")
    }

    /// The name to keep from the hub's `name` and `name_source` (the upload's answer, reconciliation, the detail's meta):
    /// a calendar item's or the model's; nil for the recording's own title (this phone has it), and when the hub names it
    /// only by when it started (both absent), so a name it no longer gives goes.
    static func hubName(name: String?, source: String?) -> String? {
        guard source == "calendar" || source == "model" else { return nil }
        let name = (name ?? "").split(whereSeparator: \.isNewline).joined(separator: " ").trimmingCharacters(in: .whitespaces)
        return name.isEmpty ? nil : String(name.prefix(maxLength))
    }

    /// What to do with the hub's name for a recording (#8877).
    enum HubNaming: Equatable, Sendable {
        /// A name the owner gave on the web: it becomes the recording's own title, uploaded with its next revision.
        case adopt(String)
        /// Any other name, kept as `hubName` (nil: none).
        case keep(String?)
    }

    /// A name the owner gave on the web (`renamed`) is taken as the recording's own title when it is not already, unless
    /// this phone has a change of its own still to upload (`pending`: its title goes up and wins on the hub as the newer
    /// one); a calendar's or a generated name is kept as `hubName` (`hubName(name:source:)`).
    static func naming(_ entry: RecordingEntry, name: String?, source: String?, pending: Bool) -> HubNaming {
        guard source == "renamed" else { return .keep(hubName(name: name, source: source)) }
        let title = cleaned(name ?? "")
        return title.isEmpty || pending || title == entry.title ? .keep(nil) : .adopt(title)
    }

    /// The owner's title as kept: one line, trimmed, at most `maxLength` characters.
    static func cleaned(_ title: String) -> String {
        let line = title.split(whereSeparator: \.isNewline).joined(separator: " ").trimmingCharacters(in: .whitespaces)
        return String(line.prefix(maxLength))
    }

    /// The most of a title kept (the hub's calendar titles are at most 200 characters).
    static let maxLength = 200
}

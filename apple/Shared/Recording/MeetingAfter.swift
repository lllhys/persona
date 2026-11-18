import Foundation

// After the meeting, in one screen (#8731 V3, docs/design/DESIGN_SYSTEM.md "After the meeting"): what its summary card and
// its map show, worked out from the minutes and the meeting's own topics. Pure, for the tests.

/// The summary card: the minutes' headline and at most three core conclusions (an overview), or the first two points (the
/// older shape); under them how many decisions, to-dos and open questions the minutes hold. Nil when they say none of it.
nonisolated struct MeetingAfterSummary: Hashable, Sendable {
    var headline: String?
    var lines: [String]
    var decisions: Int
    var todos: Int
    var open: Int

    static let conclusionsShown = 3
    static let pointsShown = 2

    static func make(_ minutes: RecordingMinutes) -> MeetingAfterSummary? {
        let words = { (s: String?) -> String? in
            let t = s?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            return t.isEmpty ? nil : t
        }
        var headline: String?
        var lines: [String]
        var decisions = minutes.decisions.count
        if let overview = minutes.overview {
            headline = words(overview.headline)
            lines = overview.conclusions.compactMap { words($0.text) }.prefix(conclusionsShown).map { $0 }
            // An overview marks its decisions on the topics' bullets; the older list, when the hub sent one too, counts.
            if decisions == 0 {
                decisions = overview.groups.flatMap(\.topics).flatMap(\.bullets).filter(\.decision).count
            }
        } else {
            lines = minutes.points.compactMap { words($0.text) }.prefix(pointsShown).map { $0 }
        }
        let summary = MeetingAfterSummary(headline: headline, lines: lines, decisions: decisions, todos: minutes.actions.count,
                                          open: minutes.openQuestions.count)
        return summary.headline == nil && summary.lines.isEmpty && decisions + summary.todos + summary.open == 0 ? nil : summary
    }
}

/// The meeting map: the minutes' topics, each with its bullets as its note; before there are minutes (or in minutes
/// without an overview), the topics the room kept during the meeting; else none, and the screen shows no map.
nonisolated enum MeetingAfterMap {
    static func topics(minutes: RecordingMinutes?, live: [MeetingTopic]) -> [MeetingTopic] {
        if let overview = minutes?.overview {
            let topics = overview.groups.flatMap(\.topics).map { topic in
                let bullets = topic.bullets.map(\.text).filter { !$0.isEmpty }
                return MeetingTopic(title: topic.title, state: MeetingTopic.State.done.rawValue, at: nil,
                                    note: bullets.isEmpty ? nil : bullets.map { "• " + $0 }.joined(separator: "\n"))
            }
            if !topics.isEmpty { return topics }
        }
        return live
    }
}

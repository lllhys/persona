#if os(iOS)
import Foundation
import WidgetKit

/// Edit Widget's lists (docs/apple/home-widgets.md「Edit Widget」): the computers and accounts in the snapshot the app
/// last wrote, so a list needs no hub call and names each one as the widgets do. Before them comes the default of a
/// new widget, every computer or the fullest account, which the extension's entities give an empty id.
nonisolated enum HomeChoices {
    struct Option: Hashable, Sendable {
        var id: String
        var name: String
    }

    static func computers(_ s: HomeSnapshot?) -> [Option] { (s?.machines ?? []).map { Option(id: $0.id, name: $0.name) } }
    static func accounts(_ s: HomeSnapshot?) -> [Option] { (s?.usage?.accounts ?? []).map { Option(id: $0.id, name: $0.name) } }

    /// A chosen id as the entry keeps it: empty (every computer, the fullest account) is no filter.
    static func filter(_ id: String?) -> String? { id.flatMap { $0.isEmpty ? nil : $0 } }
}

/// Edit Widget › Show on the Agents widget: every agent, the coding agents on your computers, or Persona's own tasks.
nonisolated enum HomeAgentsShow: String, CaseIterable, Sendable {
    case all, computers, persona

    var kind: HomeSnapshot.AgentKind? {
        switch self {
        case .all: nil
        case .computers: .coding
        case .persona: .assistant
        }
    }
}

/// How the Smart Stack ranks a widget (docs/apple/home-widgets.md「Smart Stack」): first when something waits for you or a
/// recording runs, high when a meeting is close, low otherwise; nothing while the numbers are old or Persona is not set up.
enum HomeRelevance {
    static func of(_ kind: HomeKind, _ c: HomeContext) -> TimelineEntryRelevance { TimelineEntryRelevance(score: score(kind, c)) }

    static func score(_ kind: HomeKind, _ c: HomeContext) -> Float {
        guard c.snapshot.paired, !c.stale else { return 0 }
        let mood = c.mood(kind)
        if mood == .needsYou { return 100 }
        if kind == .record, c.snapshot.recording != nil { return 90 }
        if kind == .today || kind == .record, let next = c.nextEvent, next.start - c.now < 15 * 60 { return 70 }
        return mood == .working ? 30 : 10
    }
}
#endif

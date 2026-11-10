import Foundation

/// The one set of names for an agent's and a task's state on iOS, Mac, the watch and the widgets (#3960; docs/protocol/i18n.md
/// "Agent states", docs/protocol/agent-states.md), as the web has in `web/src/lib/state-labels.ts` (#3415): every screen takes
/// its state words from here, so a state is never 「需要你」 on one screen and 「等你」 on the next. The words are the catalog's
/// `… [State Names]` keys; `StateNamesTests` keeps other entries and other files from defining them again.
/// `interfaceText`, so the Live Activity follows the app's language at once.
nonisolated enum StateName: String, CaseIterable, Sendable {
    case needsYou, working, unread, seen, notRunning, unknown
    /// Waiting to be woken (`isWaiting`): a loop or a timer brings it back.
    case waiting
    /// Seen, but its last turn asks the user something (#5096).
    case awaitsReply
    /// Only a task or a summary is done; an agent is seen.
    case done
    /// A to-do or task the agent stopped on that wants a follow-up.
    case followUp
    /// A to-do or task whose agent ended its turn but waits to be brought back: its background work, or the hub's wake-up (#3274).
    case taskWaiting

    var text: String {
        switch self {
        case .needsYou: interfaceText("Needs You [State Names]")
        case .working: interfaceText("Working [State Names]")
        case .unread: interfaceText("Unread [State Names]")
        case .seen: interfaceText("Seen [State Names]")
        case .notRunning: interfaceText("Not Running [State Names]")
        case .unknown: interfaceText("Unknown [State Names]")
        case .waiting: interfaceText("Waiting [State Names]")
        case .awaitsReply: interfaceText("Awaiting Reply [State Names]")
        case .done: interfaceText("Done [State Names]")
        case .followUp: interfaceText("Follow-Up [State Names]")
        case .taskWaiting: interfaceText("Task Waiting [State Names]")
        }
    }

    /// The states a list counts in its headers and summaries: "3 need you".
    enum Counted: Sendable { case needsYou, working, unread }

    static func count(_ s: Counted, _ n: Int) -> String {
        switch s {
        case .needsYou: interfaceText("\(n) need you [State Names]")
        case .working: interfaceText("\(n) working [State Names]")
        case .unread: interfaceText("\(n) unread [State Names]")
        }
    }

    /// The button that marks every finished, unseen agent as seen, with how many when given.
    static func markAllSeen(_ n: Int? = nil) -> String {
        guard let n else { return interfaceText("Mark All as Seen [State Names]") }
        return interfaceText("Mark All as Seen (\(n)) [State Names]")
    }
}

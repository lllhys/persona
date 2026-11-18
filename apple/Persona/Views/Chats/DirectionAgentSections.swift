import Foundation

/// The Agents page By Pinned (#4352, `DevGrouping.direction`): a section per direction of the hub's tree, each to-do in
/// it with its Agents and their sub-Agents under it, in the tree's order. The listed Agents the tree does not have go to
/// By To-do's last sections (Started Manually, Unassigned), so none disappears. A pure function of the tree and the
/// list, unit-tested in `DirectionTreeTests`; `AgentListRows.directions` reads the same for the Mac's ↑ / ↓.
nonisolated struct DirectionAgentSections: Sendable {
    /// An Agent's row under a to-do.
    nonisolated struct Line: Identifiable, Sendable {
        var node: DirectionTree.AgentNode
        var key: SessionKey
        /// Its row in the list; nil for one the list does not have (a sub-Agent inside a session, one not reported
        /// yet), drawn from what the tree says.
        var session: Session?
        var depth: Int
        var id: String { node.id }
    }

    nonisolated struct TaskRows: Identifiable, Sendable {
        var task: DirectionTree.TaskNode
        var lines: [Line]
        var id: String { task.id }
    }

    nonisolated struct Entry: Identifiable, Sendable {
        var direction: DirectionTree.Direction
        var tasks: [TaskRows]
        /// Its fold id in the list.
        var key: String { "direction:" + direction.id }
        var id: String { key }
    }

    var entries: [Entry]
    /// The Agents in `shown` the tree does not have (`TodosLogic.buildTodoAgentGroups` with no to-dos): Started
    /// Manually, then Unassigned, folded unless one of them needs you.
    var others: [TodoAgentGroup]

    /// `shown`: the running Agents the list shows (a quick filter, a search, the machine menu applied; the pinned ones
    /// are in Pinned). `known`: every session the app has, so one the list leaves out stays out here too, while one it
    /// has never heard of (a sub-Agent) is drawn from the tree. `group`: the hub the tree came from. `filtering`: a
    /// quick filter or a search narrows the list; a to-do left without an Agent is not listed then, nor a direction
    /// left without a to-do.
    static func build(_ tree: DirectionTree, shown: [Session], known: Set<SessionKey>, group: String, filtering: Bool = false,
                      isMuted: (Session) -> Bool) -> DirectionAgentSections {
        var byKey: [SessionKey: Session] = [:]
        for s in shown where byKey[s.key] == nil { byKey[s.key] = s }
        var placed: Set<SessionKey> = []
        var entries: [Entry] = []
        for d in tree.withTasks {
            var tasks: [TaskRows] = []
            for t in d.tasks {
                var lines: [Line] = []
                for l in t.agentLines() {
                    let key = l.node.key(group: group)
                    guard !placed.contains(key) else { continue }
                    if let s = byKey[key] {
                        placed.insert(key)
                        lines.append(Line(node: l.node, key: key, session: s, depth: l.depth))
                    } else if !known.contains(key), !filtering {
                        placed.insert(key)
                        lines.append(Line(node: l.node, key: key, session: nil, depth: l.depth))
                    }
                }
                if !filtering || !lines.isEmpty { tasks.append(TaskRows(task: t, lines: lines)) }
            }
            if !tasks.isEmpty { entries.append(Entry(direction: d, tasks: tasks)) }
        }
        let rest = shown.filter { !placed.contains($0.key) }
        var others = TodosLogic.buildTodoAgentGroups(sessions: rest, todos: [], isMuted: isMuted)
        for i in others.indices where others[i].key == "todo:none" {
            others[i].folded = !entries.isEmpty && !others[i].sessions.contains { $0.state == .needsYou }
        }
        return DirectionAgentSections(entries: entries, others: others)
    }

    /// The fold ids of what it shows (Collapse All).
    var foldIDs: [String] { entries.map(\.key) + others.map(\.key) }
}

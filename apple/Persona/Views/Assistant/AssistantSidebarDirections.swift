import SwiftUI

// The sidebar's Pinned group (the leader directions), opened up (#4352, docs/apple/sidebar.md "Pinned directions"):
// each lead's row (#4331's, with its "N working · N need you") gets a chevron that folds open its to-dos, each with
// its state; a to-do folds open to its Agents, an Agent to its sub-Agents. All of it is the hub's direction tree
// (`AppModel.directionTree`, the one the counts come from too). Kept apart from AssistantSidebar.swift, which only
// places these rows. The secretary group has no row here: the main conversation's row stands for it.

/// One direction under Pinned: `head` (the leader conversation's own row, counts included) with a chevron on the
/// right, and while open the rows of its to-dos. Folded at first; this device remembers it per direction. Without a
/// tree (an older hub, another manager's page, or before it is read) only `head`, as the sidebar drew it before.
struct AssistantSidebarDirection<Head: View>: View {
    @Environment(AppModel.self) private var model
    var direction: DirectionTree.Direction?
    /// The direction's name as its row says it, for VoiceOver.
    var name: String
    var isShell: Bool
    /// The drawer the sidebar is in: closed when an Agent opens (nil as a column).
    var drawer: AssistantDrawer?
    /// Whether the head is the open conversation, and the fill the sidebar marks that with.
    var selected: Bool
    var selectedFill: Color
    /// The head row, told whether to draw its own selected and hover shade: not while the chevron sits beside it,
    /// where this view draws one shade across both.
    var head: (Bool) -> Head
    @StoredValue private var open: Bool
    @State private var openTasks: Set<String> = []
    @State private var openAgents: Set<String> = []
    @State private var shownTodo: TodoDetailRoute?

    init(direction: DirectionTree.Direction?, id: String, name: String, isShell: Bool, drawer: AssistantDrawer?,
         selected: Bool = false, selectedFill: Color = .clear, @ViewBuilder head: @escaping (_ drawsShade: Bool) -> Head) {
        self.direction = direction
        self.name = name
        self.isShell = isShell
        self.drawer = drawer
        self.selected = selected
        self.selectedFill = selectedFill
        self.head = head
        _open = StoredValue(wrappedValue: false, Self.foldKey(id))
    }

    /// Where this device keeps whether a direction is open.
    nonisolated static func foldKey(_ id: String) -> String { "assistant.direction.open." + id }

    var body: some View {
        // The one card presenter for every to-do row below: on the head, which stays while the tree replaces the
        // to-dos (one accepted or closed leaves it) or the direction folds, so an open card is not torn down under
        // the owner.
        headRow
            .sheet(item: $shownTodo) { r in
                NavigationStack {
                    TodoDetailView(todoID: r.id).sheetDone { shownTodo = nil }
                }
                #if os(macOS)
                .macSheet()
                #endif
            }
        if let direction, open {
            if direction.tasks.isEmpty {
                Text("No open to-dos")
                    .font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                    .padding(.leading, taskInset)
                    .padding(.vertical, Theme.Sidebar.rowPadding)
                    .scaledFrame(minHeight: Theme.Sidebar.row)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            ForEach(direction.tasks) { task in
                taskRow(task)
                if openTasks.contains(task.id) {
                    ForEach(task.agentLines(opened: openAgents)) { line in agentRow(line) }
                }
            }
        }
    }

    /// The head, and with a tree its chevron beside it, not inside it: the head keeps its own tap (the conversation),
    /// menu, swipe and counts. The selected and hover shade then spans both, as wide as every other row's.
    @ViewBuilder private var headRow: some View {
        if let direction {
            head(false)
                .safeAreaInset(edge: .trailing, spacing: 0) { toggle(direction) }
                .background {
                    if selected {
                        RoundedRectangle(cornerRadius: Theme.Sidebar.radius, style: .continuous).fill(selectedFill)
                    }
                }
                .macHoverShade(in: RoundedRectangle(cornerRadius: Theme.Sidebar.radius, style: .continuous))
        } else {
            head(true)
        }
    }

    /// The row's right end: the chevron that folds the direction (the counts stay in the head, as #4331 drew them).
    private func toggle(_ direction: DirectionTree.Direction) -> some View {
        Button {
            withAnimation(Theme.Motion.state) { open.toggle() }
        } label: {
            ThemedIcon(open ? Theme.Icon.collapse : Theme.Icon.expand)
                .font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                .padding(.trailing, Theme.Space.s)
                .padding(.vertical, Theme.Sidebar.rowPadding)
                .scaledFrame(minWidth: Theme.Size.tapTarget, minHeight: Theme.Sidebar.row)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .layoutPriority(1)
        .accessibilityLabel([name, direction.counts.label ?? ""].filter { !$0.isEmpty }
            .joined(separator: String(localized: ", ", comment: "Separator between the parts of a VoiceOver label")))
        .accessibilityValue(open ? String(localized: "Expanded") : String(localized: "Collapsed [Assistant Sidebar e64447]"))
        // A Mac pointer gets a worded hover label too (#6214): the bare chevron alone did not say what it folds.
        #if os(macOS)
        .help(open ? String(localized: "Direction Collapse Help [Leader Actions 6214]")
                   : String(localized: "Direction Expand Help [Leader Actions 6214]"))
        #endif
        .accessibilityIdentifier("sidebar-direction-toggle")
    }

    /// Set in from the direction's row as Auto Chats' sources are.
    private var taskInset: CGFloat { isShell ? Theme.Space.xl : Theme.Space.m + Theme.Sidebar.mark + Theme.Space.m }

    /// A to-do: its title and its state; a tap opens its card. Its Agents fold out from the chevron on the right.
    private func taskRow(_ task: DirectionTree.TaskNode) -> some View {
        let unfolded = openTasks.contains(task.id)
        let title = task.displayTitle
        return HStack(spacing: Theme.Space.xs) {
            Button { shownTodo = TodoDetailRoute(id: task.id) } label: {
                HStack(spacing: Theme.Space.s) {
                    Text(title).font(Theme.Font.secondary).foregroundStyle(Color.primary).lineLimit(1)
                    Spacer(minLength: Theme.Space.xs)
                    DirectionTaskStateWord(state: task.state)
                }
                .padding(.leading, taskInset)
                .padding(.vertical, Theme.Sidebar.rowPadding)
                .scaledFrame(minHeight: Theme.Sidebar.row)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel([title, task.state.text]
                .joined(separator: String(localized: ", ", comment: "Separator between the parts of a VoiceOver label")))
            .accessibilityHint(String(localized: "View to-do details"))
            .accessibilityIdentifier("sidebar-direction-task")
            if task.agents.isEmpty {
                Color.clear.scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.dot).accessibilityHidden(true)
            } else {
                foldButton(unfolded, label: String(localized: "Agents of \(title)"), id: "sidebar-direction-task-toggle") {
                    openTasks.formSymmetricDifference([task.id])
                }
            }
        }
        .padding(.trailing, Theme.Space.m)
        .macHoverShade(in: RoundedRectangle(cornerRadius: Theme.Sidebar.radius, style: .continuous))
    }

    /// An Agent of a to-do (or a sub-Agent, a level further in): its live state, its title; a tap opens its session.
    private func agentRow(_ line: DirectionTree.AgentLine) -> some View {
        let key = model.inHub(SessionKey(machineID: line.node.machineID, sessionID: line.node.sessionID))
        let session = model.session(key)
        let state = session?.state ?? line.node.agentState
        let title = !line.node.title.isEmpty ? line.node.title
            : session?.displayTitle ?? (line.node.agent.isEmpty ? String(localized: "Agent") : line.node.agent)
        let unfolded = openAgents.contains(line.id)
        return HStack(spacing: Theme.Space.xs) {
            Button {
                drawer?.close()
                openTodoAgentChat(key, in: model)
            } label: {
                HStack(spacing: Theme.Space.s) {
                    StatusDot(state: state)
                    Text(title).font(Theme.Font.secondary).foregroundStyle(Color.primary).lineLimit(1)
                    Spacer(minLength: Theme.Space.xs)
                    // Not the dot's color alone (HIG-040): every state says itself in words too.
                    Text(state.label).font(Theme.Font.caption).foregroundStyle(state.tone.text).lineLimit(1)
                }
                .padding(.leading, taskInset + Theme.Space.l * CGFloat(line.depth + 1))
                .padding(.vertical, Theme.Sidebar.rowPadding)
                .scaledFrame(minHeight: Theme.Sidebar.row)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel([title, state.label]
                .joined(separator: String(localized: ", ", comment: "Separator between the parts of a VoiceOver label")))
            .accessibilityAddTraits(.isButton)
            .accessibilityIdentifier("sidebar-direction-agent")
            if line.hasChildren {
                foldButton(unfolded, label: String(localized: "Subagents of \(title)"), id: "sidebar-direction-agent-toggle") {
                    openAgents.formSymmetricDifference([line.id])
                }
            } else {
                Color.clear.scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.dot).accessibilityHidden(true)
            }
        }
        .padding(.trailing, Theme.Space.m)
        .macHoverShade(in: RoundedRectangle(cornerRadius: Theme.Sidebar.radius, style: .continuous))
    }

    /// A chevron that folds what is under a row, with a 44 pt target, spoken as what it folds (a to-do's Agents, an
    /// Agent's subagents: not the row's bare title, which its open button already says) and expanded / collapsed.
    private func foldButton(_ unfolded: Bool, label: String, id: String, toggle: @escaping () -> Void) -> some View {
        Button {
            withAnimation(Theme.Motion.state) { toggle() }
        } label: {
            ThemedIcon(unfolded ? Theme.Icon.collapse : Theme.Icon.expand)
                .font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Sidebar.row)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityValue(unfolded ? String(localized: "Expanded") : String(localized: "Collapsed [Assistant Sidebar e64447]"))
        .accessibilityIdentifier(id)
    }
}

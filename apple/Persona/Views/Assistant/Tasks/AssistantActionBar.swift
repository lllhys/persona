import SwiftUI

// The phone's action bar above the assistant's composer (#2879, docs/assistant/unified-entry.md): three entries, Pending,
// Progress and Agents, in place of the dock's All and its pills. Each opens a tray above the bar, one at a time; the
// entries are drawn as the dock's capsules (`DockCapsule`), so the bar is as low as the dock was. The Mac keeps the dock.

/// One of the bar's entries, and the tray it opens.
nonisolated enum ActionTray: String, CaseIterable, Identifiable, Sendable {
    case pending, progress, agents

    var id: String { rawValue }
}

/// What the three trays list, from the conversation's tasks and the agents going on. Pure, so it is tested without a hub.
nonisolated struct ActionTrayContent: Equatable, Sendable {
    /// A task of the conversation, as it is now.
    struct TaskRow: Equatable, Sendable {
        var id: String
        var state: TaskState
        var createdAt: Date?
    }

    enum Row: Equatable, Sendable, Identifiable {
        case task(id: String, state: TaskState)
        case agent(DockAgent)

        var id: String {
            switch self {
            case .task(let id, _): "task:" + id
            case .agent(let a): "agent:" + a.key.path
            }
        }
    }

    /// What waits for the user: the tasks that ask, then the agents of its own (no task's) that ask.
    var pending: [Row] = []
    /// The tasks: going on first (what waits, then working, then the newest), then the finished ones, newest first.
    var progress: [Row] = []
    /// Every agent going on for the conversation, its tasks' and its own: waiting first, then the newest.
    var agents: [Row] = []
    /// How many of `progress` are still going on.
    var going = 0

    init() {}

    /// `own`: the agents no task stands for (`TasksStore.agents(of:)`); `taskAgents`: the agents of its tasks.
    init(tasks: [TaskRow], own: [DockAgent], taskAgents: [DockAgent]) {
        let finished: Set<TaskState> = [.done, .followup, .failed]
        let live = tasks.filter { !finished.contains($0.state) }.sorted { a, b in
            if a.state.priority != b.state.priority { return a.state.priority < b.state.priority }
            return (a.createdAt ?? .distantPast) > (b.createdAt ?? .distantPast)
        }
        let done = tasks.filter { finished.contains($0.state) }.sorted { ($0.createdAt ?? .distantPast) > ($1.createdAt ?? .distantPast) }
        progress = (live + done).map { .task(id: $0.id, state: $0.state) }
        going = live.count
        pending = live.filter { $0.state == .needsYou }.map { .task(id: $0.id, state: $0.state) }
            + own.filter(\.isWaiting).map { .agent($0) }
        var seen = Set<SessionKey>()
        let all = (own + taskAgents).filter { seen.insert($0.key).inserted }.sorted { a, b in
            if a.state.priority != b.state.priority { return a.state.priority < b.state.priority }
            if a.since != b.since { return (a.since ?? .distantPast) > (b.since ?? .distantPast) }
            return a.key.path < b.key.path
        }
        agents = all.map { .agent($0) }
    }

    func rows(_ tray: ActionTray) -> [Row] {
        switch tray {
        case .pending: pending
        case .progress: progress
        case .agents: agents
        }
    }

    /// The number its entry shows: what waits, what is going on, the agents.
    func count(_ tray: ActionTray) -> Int {
        switch tray {
        case .pending: pending.count
        case .progress: going
        case .agents: agents.count
        }
    }
}

extension ActionTray {
    var title: String {
        switch self {
        case .pending: String(localized: "Pending")
        case .progress: String(localized: "Progress")
        case .agents: String(localized: "Agents")
        }
    }

    var empty: String {
        switch self {
        case .pending: String(localized: "Nothing needs you right now.")
        case .progress: String(localized: "No tasks in this chat yet")
        case .agents: String(localized: "No agents right now")
        }
    }
}

/// The bar and the tray open over it.
struct AssistantActionBar: View {
    var conversation: String
    /// The conversation's model operating a Mac (#1343): its pill after the entries, as in the dock.
    var computer: ComputerActivity? = nil
    @State private var open: ActionTray?
    /// The open tray's rows, measured: the tray is as tall as they are, up to its cap.
    @State private var listHeight: CGFloat = 0
    private var store: TasksStore { .shared }

    var body: some View {
        let content = Self.content(conversation)
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if let open {
                tray(open, content)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            HStack(spacing: Theme.Space.s) {
                ForEach(ActionTray.allCases) { entry($0, count: content.count($0)) }
                if ComputerActivityPill.shows(computer) {
                    ComputerActivityPill(activity: computer)
                }
            }
            .padding(.horizontal, Theme.Space.xs)
            .padding(.top, Theme.Space.s)
            .dockChatFade()
            .zIndex(-1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .onChange(of: conversation) { open = nil }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("action-bar")
    }

    static func content(_ conversation: String) -> ActionTrayContent {
        let store = TasksStore.shared
        let tasks = store.tasks(of: conversation)
        let rows = tasks.map { ActionTrayContent.TaskRow(id: $0.id, state: store.state($0), createdAt: $0.createdAt) }
        let open: [TodoItem] = tasks.filter { $0.doneAt == nil }
        let sessions: [Session] = open.flatMap { store.sessions($0) }
        // DockAgent.going keeps the agents going on (waiting or working), never Persona's own conversations.
        let taskAgents: [DockAgent] = DockAgent.going(sessions)
        return ActionTrayContent(tasks: rows, own: store.agents(of: conversation), taskAgents: taskAgents)
    }

    private func entry(_ tray: ActionTray, count: Int) -> some View {
        let selected = open == tray
        let look: DockCapsule.Look = selected ? .selected : (tray == .pending && count > 0 ? .waiting(.needsYou) : .plain)
        return Button {
            Haptics.tap()
            withAnimation(Theme.Motion.state) { open = selected ? nil : tray }
        } label: {
            HStack(spacing: Theme.Space.xs) {
                Text(tray.title)
                if count > 0 { Text(count > 99 ? "99+" : "\(count)").monospacedDigit() }
            }
            .dockCapsule(look)
            .fixedSize()
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityIdentifier("action-bar.\(tray.rawValue)")
    }

    private func tray(_ tray: ActionTray, _ content: ActionTrayContent) -> some View {
        let rows = content.rows(tray)
        return VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: Theme.Space.s) {
                Text(tray.title).font(Theme.Font.sectionHeader).foregroundStyle(.secondary).lineLimit(1)
                Spacer(minLength: Theme.Space.s)
                if tray == .progress {
                    Button {
                        open = nil
                        store.showBoard = true
                    } label: {
                        Text("All").dockCapsule().fixedSize()
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("action-tray.board")
                }
                Button {
                    withAnimation(Theme.Motion.state) { open = nil }
                } label: {
                    ThemedIcon(Theme.Icon.close).font(Theme.Font.labelStrong).foregroundStyle(.secondary)
                        .scaledFrame(width: Theme.Size.badge, height: Theme.Size.badge)
                        .contentShape(.circle)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Close")
                .accessibilityIdentifier("action-tray.close")
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.s)
            if rows.isEmpty {
                Text(tray.empty).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(2)
                    .padding(.horizontal, Theme.Space.m)
                    .padding(.bottom, Theme.Space.m)
                    .accessibilityIdentifier("action-tray.empty")
            } else {
                // As tall as its rows; past the cap they scroll inside the tray, the bar stays where it is.
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) { ForEach(rows) { row($0) } }
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { listHeight = $0 }
                }
                .scrollBounceBehavior(.basedOnSize)
                .frame(height: min(listHeight, Theme.Size.box280))
                .padding(.bottom, Theme.Space.xs)
            }
        }
        .surface(in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("action-tray.\(tray.rawValue)")
    }

    @ViewBuilder private func row(_ row: ActionTrayContent.Row) -> some View {
        switch row {
        case .task(let id, let state):
            if let todo = store.todos[id] {
                Button {
                    open = nil
                    store.detailTask = id
                } label: {
                    line(title: todo.title, detail: state.label, waiting: state == .needsYou) {
                        TaskMark(state: state)
                    }
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("action-tray.task")
            }
        case .agent(let agent):
            Button {
                open = nil
                store.detailAgent = agent.key
            } label: {
                line(title: agent.title, detail: agent.isWaiting ? StateName.needsYou.text : StateName.working.text, waiting: agent.isWaiting) {
                    StatusDot(state: agent.state, size: Theme.Size.dotL)
                }
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("action-tray.agent")
        }
    }

    private func line<Mark: View>(title: String, detail: String, waiting: Bool, @ViewBuilder mark: () -> Mark) -> some View {
        HStack(spacing: Theme.Space.s) {
            mark().scaledFrame(width: Theme.Size.markM)
            Text(title).font(Theme.Font.body).lineLimit(1)
            Spacer(minLength: Theme.Space.s)
            Text(detail).font(Theme.Font.caption).lineLimit(1).fixedSize()
                .foregroundStyle(waiting ? AnyShapeStyle(AgentState.needsYou.tone.text) : AnyShapeStyle(.secondary))
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}

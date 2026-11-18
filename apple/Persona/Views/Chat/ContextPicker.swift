#if os(iOS)
import SwiftUI

// Add as Context › in the composer's + (#3721): a knowledge note, a to-do or an Inbox item picked from a searchable list
// goes over the box as a quote (`ContextRef`, sent as `<persona-context>` before the message), as Use as Context does
// for a page. What each kind carries to the model: a note its summary, latest timeline and path in the knowledge base
// (`KnowledgeDetailView.contextRef`); a to-do its title, notes and id; an Inbox item what Hand to AI sends
// (`InboxHandOffSheet.contextRef`). A note's and a to-do's block lead back to them (`persona://kb/…`, `persona://task/…`).

nonisolated enum ContextPickKind: String, CaseIterable, Identifiable, Sendable {
    case knowledge, todo, inbox
    var id: String { rawValue }
}

extension ContextPickKind {
    /// The row in the + list's Add as Context submenu.
    var menuTitle: String {
        switch self {
        case .knowledge: String(localized: "Knowledge Note…")
        case .todo: String(localized: "To-do…")
        case .inbox: String(localized: "Inbox Item…")
        }
    }

    var pageTitle: String {
        switch self {
        case .knowledge: String(localized: "Knowledge")
        case .todo: String(localized: "To-dos")
        case .inbox: String(localized: "Inbox")
        }
    }

    var symbol: String {
        switch self {
        case .knowledge: Theme.Icon.knowledge
        case .todo: Theme.Icon.todo
        case .inbox: Theme.Icon.inbox
        }
    }
}

/// One thing to pick: its title, a line of it, and what it is.
struct ContextPickRow: Identifiable, Hashable {
    enum Source: Hashable {
        case note(KBEntry)
        case todo(TodoItem)
        case inbox(InboxItem)
    }

    var source: Source
    var id: String {
        switch source {
        case .note(let e): "kb/\(e.kind)/\(e.id)"
        case .todo(let t): "todo/\(t.id)"
        case .inbox(let i): "inbox/\(i.id)"
        }
    }
    var title: String {
        switch source {
        case .note(let e): e.name
        case .todo(let t): t.title
        case .inbox(let i): i.title
        }
    }
    var line: String {
        switch source {
        case .note(let e): [KBKind(rawValue: e.kind)?.title, e.oneLine].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
        case .todo(let t): t.notes ?? ""
        case .inbox(let i): [i.sender, i.preview].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "：") // i18n-ignore: as the Inbox writes it
        }
    }
}

/// What the picker lists and how a pick becomes context (MVVM with `AssistantDeps`, tested with `FakeHub`).
@Observable final class ContextPickerModel {
    let kind: ContextPickKind
    private(set) var rows: [ContextPickRow] = []
    /// Read once at least (the list shows what it has, or that there is nothing).
    private(set) var loaded = false
    /// Why the list could not be read; shown while there is nothing to show.
    private(set) var error: String?
    @ObservationIgnored private let deps: AssistantDeps
    @ObservationIgnored private var recentNotes: [KBEntry]?
    @ObservationIgnored private var todos: [TodoItem]?
    @ObservationIgnored private var inbox: [InboxItem]?

    /// The kinds of knowledge entry listed before anything is typed: the ones written about (not chats, events or recordings).
    nonisolated static let recentKinds: [KBKind] = [.projects, .people, .docs, .meetings]
    /// At most this many before anything is typed.
    nonisolated static let recentLimit = 40

    init(kind: ContextPickKind, deps: AssistantDeps = .live) {
        self.kind = kind
        self.deps = deps
    }

    /// The list for `query`: knowledge from the hub's search (the latest entries while nothing is typed), to-dos and the
    /// Inbox read once and narrowed here.
    func load(query: String) async {
        guard let api = deps.api() else { return }
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            switch kind {
            case .knowledge:
                if q.isEmpty {
                    if recentNotes == nil {
                        var all: [KBEntry] = []
                        for k in Self.recentKinds { all += (try? await api.kbList(k).items) ?? [] }
                        recentNotes = Self.recent(all)
                    }
                    rows = (recentNotes ?? []).map { ContextPickRow(source: .note($0)) }
                } else {
                    let found = try await api.kbFind(q)
                    guard !Task.isCancelled else { return }
                    rows = found.map { ContextPickRow(source: .note($0.entry)) }
                }
            case .todo:
                if todos == nil { todos = try await api.todos() }
                rows = Self.todos(todos ?? [], matching: q).map { ContextPickRow(source: .todo($0)) }
            case .inbox:
                if inbox == nil { inbox = try await api.inbox(connectors: true) }
                rows = Self.inbox(inbox ?? [], matching: q).map { ContextPickRow(source: .inbox($0)) }
            }
            error = nil
        } catch is CancellationError {
            return
        } catch {
            if Task.isCancelled { return }
            if let shown = error.shown { self.error = shown } // a quiet one (going offline, cancelled) says nothing
        }
        loaded = true
    }

    /// The pick as context. A note reads its entry first for the summary, timeline and path (its line alone when that fails).
    func context(_ row: ContextPickRow) async -> ContextRef {
        switch row.source {
        case .note(let e):
            let detail = try? await deps.api()?.kbDetail(e.kind, id: e.id)
            return KnowledgeDetailView.contextRef(e, detail: detail, link: true)
        case .todo(let t):
            return Self.contextRef(t)
        case .inbox(let i):
            return InboxHandOffSheet.contextRef(i)
        }
    }

    /// A to-do as context: its title and notes, and its id so the agent can update it; the block opens it (`persona://task/`).
    nonisolated static func contextRef(_ t: TodoItem) -> ContextRef {
        var text = t.title
        if let notes = t.notes, !notes.isEmpty { text += "\n\n" + notes }
        text += "\n\n（待办 id \(t.id)\(t.doneAt != nil ? "，已完成" : "")）" // i18n-ignore: context for the agent
        return ContextRef(kind: "context", title: String(localized: "To-do · \(t.title)"), url: "persona://task/\(t.id)", text: text)
    }

    /// The newest entries first, at most `recentLimit`.
    nonisolated static func recent(_ entries: [KBEntry]) -> [KBEntry] {
        Array(entries.sorted { ($0.updated ?? .distantPast) > ($1.updated ?? .distantPast) }.prefix(recentLimit))
    }

    /// Open to-dos first, then done ones; the latest changed first in each; narrowed to the words typed.
    nonisolated static func todos(_ list: [TodoItem], matching q: String) -> [TodoItem] {
        list.filter { q.isEmpty || $0.title.localizedStandardContains(q) || ($0.notes ?? "").localizedStandardContains(q) }
            .sorted {
                if ($0.doneAt == nil) != ($1.doneAt == nil) { return $0.doneAt == nil }
                return ($0.updatedAt ?? $0.createdAt ?? .distantPast) > ($1.updatedAt ?? $1.createdAt ?? .distantPast)
            }
    }

    /// The latest first; narrowed to the words typed (title, sender, preview, account).
    nonisolated static func inbox(_ list: [InboxItem], matching q: String) -> [InboxItem] {
        list.filter { i in
            q.isEmpty || [i.title, i.preview, i.sender ?? "", i.accountName].contains { $0.localizedStandardContains(q) }
        }
        .sorted { ($0.updatedAt ?? $0.startedAt ?? .distantPast) > ($1.updatedAt ?? $1.startedAt ?? .distantPast) }
    }
}

/// The picker: the kind's list with search, loading, empty and error states; a tap puts the pick over the box.
struct ContextPickerSheet: View {
    var pick: (ContextRef) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var model: ContextPickerModel
    @State private var query = ""
    /// The row whose context is being read (a note's entry): the list waits for it.
    @State private var resolving: String?

    init(kind: ContextPickKind, deps: AssistantDeps = .live, pick: @escaping (ContextRef) -> Void) {
        self.pick = pick
        _model = State(initialValue: ContextPickerModel(kind: kind, deps: deps))
    }

    var body: some View {
        NavigationStack {
            List {
                if !model.loaded {
                    ProgressView().frame(maxWidth: .infinity)
                } else if model.rows.isEmpty {
                    if let error = model.error {
                        VStack(alignment: .leading, spacing: Theme.Space.s) {
                            Text("Couldn't load: \(error)").font(Theme.Font.secondary).foregroundStyle(.secondary)
                            Button("Retry") { Task { await model.load(query: query) } }
                        }
                    } else {
                        Text("No results").font(Theme.Font.secondary).foregroundStyle(.secondary)
                    }
                }
                ForEach(model.rows) { row in
                    Button { choose(row) } label: { label(row) }
                        .buttonStyle(.plain)
                        .disabled(resolving != nil)
                        .accessibilityIdentifier("context-pick-row")
                }
            }
            .themedGroupedPage()
            .pageSearchable(text: $query, prompt: String(localized: "Search"))
            .navigationTitle(model.kind.pageTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                SheetClose { dismiss() }
            }
            .task(id: query) {
                // The hub searches knowledge: a moment's pause while the words are still being typed.
                if model.kind == .knowledge, !query.isEmpty {
                    try? await Task.sleep(for: .milliseconds(250))
                    if Task.isCancelled { return }
                }
                await model.load(query: query)
            }
        }
    }

    private func label(_ row: ContextPickRow) -> some View {
        HStack(spacing: Theme.Space.m) {
            ThemedIcon(model.kind.symbol).foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(row.title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                if !row.line.isEmpty {
                    Text(row.line).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(2)
                }
            }
            Spacer(minLength: 0)
            if resolving == row.id { ProgressView() }
        }
        .contentShape(.rect)
    }

    private func choose(_ row: ContextPickRow) {
        resolving = row.id
        Task {
            let ref = await model.context(row)
            resolving = nil
            Haptics.tap()
            pick(ref)
            dismiss()
        }
    }
}
#endif

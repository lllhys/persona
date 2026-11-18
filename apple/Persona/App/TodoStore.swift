import Foundation
import Observation
import PersonaWire

/// Hub-wide todo list management (docs/assistant/todos.md, issue #401):
/// The hub's todos, cached locally in StateCache, refreshed on {"type":"todos"} WS event and (re)connect.
/// Supports 404 gracefully (older hubs without todos support: hides entrance, no error).
@MainActor @Observable
final class TodoStore {
    static let shared = TodoStore(managers: .live)

    private(set) var todos: [TodoItem] = [] {
        didSet {
            byID = Self.index(todos)
            let q = QuietTasks(todos)
            if q != quiet { quiet = q } // read by the badges and the Mac's menu bar: told only when it changes
        }
    }
    private(set) var byID: [String: TodoItem] = [:]
    /// The quiet tasks and their agents (#1731), which stay off the island, notifications and badges.
    private(set) var quiet = QuietTasks()
    private(set) var loading = false
    /// false when GET /api/v1/todos returned 404 (old hub without todos)
    private(set) var isSupported = true
    private(set) var spacesSupported = false
    private(set) var changingSpaces = false
    var error: String?

    @ObservationIgnored private let reads = CoalescedRefresh()
    @ObservationIgnored private var mutationRevision = 0
    @ObservationIgnored private var activeReadRevision: Int?
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var lastPushed: [String: TodoItem]
    @ObservationIgnored private let cacheWriter: (@MainActor ([TodoItem]) -> Void)?
    @ObservationIgnored private let hubListChanged: @MainActor () -> Void
    @ObservationIgnored private let managers: Managers
    @ObservationIgnored private let saver = Debouncer(.seconds(1))
    private static let stateName = "todo-items"

    /// Whose a to-do is on a hub with several managers (docs/assistant/managers.md "Pages"): the list is the hub's merged
    /// one, each to-do stamped with its `manager`; the pages list the current assistant's, a new one is made for it, and
    /// a change to one goes to its own manager.
    struct Managers {
        /// The current assistant's scope; nil with one manager (nothing is filtered or named).
        var scope: @MainActor () -> AssistantScope?
        /// The manager a request about a thing stamped with one names (`AppModel.itemManager`): none for the default's.
        var naming: @MainActor (String?) -> String?
        /// Whether the hub answered its roster in this run (`AppModel.rosterAsked`): before it has, a new to-do names
        /// no manager (`AppModel.namedAssistant`).
        var known: @MainActor () -> Bool = { true }
        /// The app's, asked each time: the store is first made while `AppModel.shared` restores what it saved.
        static var live: Managers {
            .init(scope: { AppModel.shared.assistantScope }, naming: { AppModel.shared.itemManager($0) },
                  known: { AppModel.shared.rosterAsked })
        }
        /// As `model` has them (the app's; a test's own model).
        static func of(_ model: AppModel) -> Managers {
            .init(scope: { [weak model] in model?.assistantScope }, naming: { [weak model] in model?.itemManager($0) },
                  known: { [weak model] in model?.rosterAsked == true })
        }
        /// One manager: a store of a test's own, which goes by no app's roster.
        static var one: Managers { .init(scope: { nil }, naming: { _ in nil }) }
    }

    /// Whose a new to-do is: the current assistant's, or the assistant's whose thing (an agent, a project's computer)
    /// it is made for, by that thing's `manager` stamp.
    enum Owner {
        case current
        case of(String?)
    }

    /// The manager a new to-do of `owner` is made for and stamped with; nil with one manager, and before the roster is
    /// read.
    private func manager(of owner: Owner) -> String? {
        guard managers.known(), let scope = managers.scope() else { return nil }
        switch owner {
        case .current: return scope.current
        case .of(let stamp): return scope.owner(stamp)
        }
    }

    /// The open to-dos a thing stamped `manager` (an agent, a project's computer) can be linked to: its own
    /// assistant's, whichever is current (docs/assistant/managers.md "Pages"). All of them with one manager.
    func openTodos(of manager: String?) -> [TodoItem] {
        guard let scope = managers.scope() else { return todos.filter { !$0.isDone } }
        let owner = scope.owner(manager)
        return todos.filter { !$0.isDone && scope.owner($0.manager) == owner }
    }

    /// Reads the list unless it is still the one tagged (`APIClient.todos(etag:)`); tests answer it themselves.
    typealias Read = @MainActor (APIClient, String?) async throws -> (list: [TodoItem]?, etag: String?)
    private let read: Read
    /// The last list read and its tag: when the hub says it did not change (304), that list is applied again, as a
    /// full read of it would have been, without reading it (#9590).
    private var lastRead: (etag: String, list: [TodoItem])?

    init(cachedTodos: [TodoItem]? = nil,
         cacheWriter: (@MainActor ([TodoItem]) -> Void)? = { StateCache.save(TodoStore.stateName, $0) },
         hubListChanged: @escaping @MainActor () -> Void = {
        AppModel.shared.summariesChanged() // its questions name their tasks
    }, managers: Managers = .one, read: @escaping Read = { try await $0.todos(etag: $1) }) {
        self.read = read
        self.managers = managers
        let initial = cachedTodos ?? StateCache.load(Self.stateName, as: [TodoItem].self) ?? []
        let initialIndex = Self.index(initial)
        todos = initial
        byID = initialIndex
        quiet = QuietTasks(initial)
        lastPushed = initialIndex
        self.cacheWriter = cacheWriter
        self.hubListChanged = hubListChanged
    }

    private static func index(_ list: [TodoItem]) -> [String: TodoItem] {
        var result: [String: TodoItem] = [:]
        for todo in list { result[todo.id] = todo }
        return result
    }

    /// The to-dos the pages list: the current assistant's, on a hub with several. `todos` stays whole: an agent's to-do
    /// and a quiet task are found whichever assistant's they are, and another assistant's opens from a link as itself.
    var scopedTodos: [TodoItem] {
        guard let scope = managers.scope() else { return todos }
        return todos.filter { scope.has($0.manager) }
    }
    var openTodos: [TodoItem] { scopedTodos.filter { !$0.isDone } }
    /// The To-dos badge (the drawer, the Mac's sidebar): the current assistant's open todos, quiet tasks left out (#1731,
    /// the main conversation tells about them). The list itself still has them.
    var badgeCount: Int { scopedTodos.count(where: { !$0.isDone && !$0.quiet }) }

    /// The manager a request about to-do `id` names: its own, as the list stamped it, whichever assistant is current
    /// (docs/protocol/protocol.md "Per-manager routing"); none for the default's, or one the list doesn't have.
    func named(_ id: String) -> String? { managers.naming(byID[id]?.manager) }

    /// The client a change to to-do `id` goes through: the one given (tests) or the hub's, naming the to-do's manager.
    private func client(for id: String, _ injected: APIClient?) -> APIClient? {
        (injected ?? AppModel.shared.api)?.manager(named(id))
    }

    /// An answer about one to-do comes from its manager without a stamp: it keeps the one the list gave it.
    private func stamped(_ answer: TodoItem, as manager: String?) -> TodoItem {
        guard answer.manager == nil, let manager else { return answer }
        var answer = answer
        answer.manager = manager
        return answer
    }

    func forget() {
        generation += 1
        lastRead = nil
        reads.reset()
        activeReadRevision = nil
        loading = false
        changingSpaces = false
        todos = []
        error = nil
        isSupported = true
        spacesSupported = false
        persist([])
    }

    /// Automatic invalidations share in-flight reads, without adding delay to unclassified todo/feed hints.
    /// The returned task covers this invalidation and at most one dirty follow-up, not an endless event pump.
    @discardableResult
    func reload(api: APIClient? = nil) -> Task<Void, Never> {
        reads.invalidate { [self] in await fetch(api: api) }
    }

    /// Manual refresh shares an eligible read, without starting a parallel GET or adding delay.
    func refresh(api: APIClient? = nil) async {
        let staleRead = activeReadRevision.map { $0 != mutationRevision } ?? false
        await reads.refresh(afterCurrent: staleRead) { [self] in await fetch(api: api) }
    }

    private func confirmWrite(api: APIClient) async {
        mutationRevision += 1
        await reads.refresh(afterCurrent: true) { [self] in await fetch(api: api) }
    }

    /// The hub's merged list, every manager's: read with none named, also after a change that named one.
    private func fetch(api injected: APIClient? = nil) async {
        guard let api = (injected ?? AppModel.shared.api)?.manager(nil) else { return }
        let requestedGeneration = generation
        let requestedRevision = mutationRevision
        activeReadRevision = requestedRevision
        loading = true
        defer {
            if generation == requestedGeneration {
                loading = false
                activeReadRevision = nil
            }
        }
        do {
            let answer = try await read(api, lastRead?.etag)
            guard generation == requestedGeneration, mutationRevision == requestedRevision, !Task.isCancelled else { return }
            let list: [TodoItem]
            if let fresh = answer.list {
                list = fresh
                lastRead = answer.etag.map { ($0, fresh) }
            } else if let kept = lastRead?.list {
                list = kept
            } else {
                return // a 304 to a tag this store did not send
            }
            isSupported = true
            spacesSupported = TodosLogic.supportsSpaces(list)
            apply(list)
            error = nil
            if byID != lastPushed {
                lastPushed = byID
                hubListChanged()
            }
        } catch let apiErr where TodosLogic.isOldHub(apiErr) {
            guard generation == requestedGeneration, mutationRevision == requestedRevision, !Task.isCancelled else { return }
            // Older hub without todos: hide entrance without error
            isSupported = false
            spacesSupported = false
            error = nil
        } catch {
            guard generation == requestedGeneration, mutationRevision == requestedRevision, !Task.isCancelled else { return }
            if let shown = error.shown { self.error = shown }
        }
    }

    private func apply(_ list: [TodoItem]) {
        guard list != todos else { return }
        todos = list
        persist(list)
    }

    private func persist(_ list: [TodoItem]) {
        guard let cacheWriter else { return }
        saver.run { cacheWriter(list) }
    }

    func todo(id: String) -> TodoItem? {
        byID[id]
    }

    /// A new to-do is the current assistant's (or `owner`'s): its manager is named, and the answer stamped with it.
    func create(title: String, notes: String? = nil, due: APIClient.TodoPlanChange.Due? = nil, owner: Owner = .current,
                api injected: APIClient? = nil) async throws -> TodoItem {
        let made = manager(of: owner)
        guard let api = (injected ?? AppModel.shared.api)?.manager(managers.naming(made)) else { throw APIError(status: 0, message: String(localized: "Not connected to a hub [Todo Store 04f822]")) }
        let targetGeneration = generation
        let created = try await stamped(api.createTodo(title: title, notes: notes, due: due), as: made)
        guard generation == targetGeneration else { return created }
        mutationRevision += 1
        var updated = todos
        updated.insert(created, at: 0)
        apply(updated)
        return created
    }

    func update(id: String, title: String? = nil, notes: String? = nil, done: Bool? = nil, api injected: APIClient? = nil) async throws -> TodoItem {
        guard let api = client(for: id, injected) else { throw APIError(status: 0, message: String(localized: "Not connected to a hub [Todo Store 04f822]")) }
        let targetGeneration = generation
        let owner = byID[id]?.manager
        let res = try await stamped(api.updateTodo(id: id, title: title, notes: notes, done: done), as: owner)
        guard generation == targetGeneration else { return res }
        mutationRevision += 1
        var updated = todos
        if let idx = updated.firstIndex(where: { $0.id == id }) {
            updated[idx] = res
        } else {
            updated.insert(res, at: 0)
        }
        apply(updated)
        return res
    }

    /// Changes a todo's due date, reminder or priority (#1683).
    @discardableResult
    func updatePlan(id: String, _ change: APIClient.TodoPlanChange, api injected: APIClient? = nil) async throws -> TodoItem {
        guard let api = client(for: id, injected) else { throw APIError(status: 0, message: String(localized: "Not connected to a hub [Todo Store 04f822]")) }
        let targetGeneration = generation
        let owner = byID[id]?.manager
        let res = try await stamped(api.updateTodoPlan(id: id, change), as: owner)
        guard generation == targetGeneration else { return res }
        mutationRevision += 1
        var updated = todos
        if let idx = updated.firstIndex(where: { $0.id == id }) {
            updated[idx] = res
        } else {
            updated.insert(res, at: 0)
        }
        apply(updated)
        return res
    }

    /// Moves a to-do to another due date (#2045: dragged on the calendar, or its "Change Due Date…"): drawn there at
    /// once, before the hub answers; a failed save puts it back and says why. The task ends with whether it was saved.
    @discardableResult
    func reschedule(_ id: String, to due: APIClient.TodoPlanChange.Due, api: APIClient? = nil) -> Task<Bool, Never> {
        guard let before = todo(id: id) else { return Task { false } }
        let targetGeneration = generation
        // Optimistic, as toggleDone: a read already in flight cannot replace it.
        mutationRevision += 1
        var updated = todos
        if let idx = updated.firstIndex(where: { $0.id == id }) {
            updated[idx] = TodoCalendar.moved(before, to: due)
            apply(updated)
        }
        return Task {
            do {
                try await updatePlan(id: id, APIClient.TodoPlanChange(due: due), api: api)
                return true
            } catch {
                // Back as it was: only this to-do, so what else changed meanwhile stays.
                guard generation == targetGeneration else { return false }
                mutationRevision += 1
                var back = todos
                if let idx = back.firstIndex(where: { $0.id == id }) {
                    back[idx] = before
                    apply(back)
                }
                // Felt as well as shown (offline has its own haptic, in `notDone`).
                if !error.isOffline { Haptics.error() }
                AppModel.shared.show(error)
                return false
            }
        }
    }

    /// `api` is the client to write through (tests pass a fake hub's); the app uses the current hub connection.
    /// Closes an open to-do or reopens a closed one. `done` says which when the row knows better than `doneAt`: a failed
    /// start waiting on the owner reads open and is closed for good (#3713, `TodoTriageLogic.checksAsOpen`).
    func toggleDone(_ todo: TodoItem, done: Bool? = nil, api: APIClient? = nil) {
        let targetGeneration = generation
        let newDone = done ?? !todo.isDone
        let original = todos
        // Optimistic update; a read already in flight cannot replace it.
        mutationRevision += 1
        var updated = todos
        if let idx = updated.firstIndex(where: { $0.id == todo.id }) {
            updated[idx].doneAt = newDone ? Date() : nil
            updated[idx].startError = nil // closed by the owner, or reopened: no longer failed (the hub does the same)
            apply(updated)
        }
        Task {
            do {
                _ = try await update(id: todo.id, done: newDone, api: api)
            } catch {
                // Revert on failure: `todos` already holds the optimistic list, so put back the one saved before it.
                guard generation == targetGeneration else { return }
                mutationRevision += 1
                apply(original)
                AppModel.shared.show(error)
            }
        }
    }

    /// Marks a to-do seen (Got It, or opening it: #5707, docs/assistant/todo-status-sop.md "Seen") or, with `seen` false,
    /// takes the mark off (Undo). The list moves it at once (principle 7); the hub's mark takes the place of the local
    /// one, and a failure puts back the mark it had and throws. `after`: the moment that put it in Just finished, so the
    /// mark shown before the hub answers is not earlier than it on a clock behind the hub's. Only the mark is taken from
    /// the answer: the list's own fields (its ask) are not in it.
    func setSeen(id: String, _ seen: Bool, after: Date = .distantPast, api injected: APIClient? = nil) async throws {
        guard let api = client(for: id, injected) else { throw APIError(status: 0, message: String(localized: "Not connected to a hub [Todo Store 04f822]")) }
        let targetGeneration = generation
        let before = byID[id]?.seenAt
        markSeen(id, seen ? max(Date(), after) : nil)
        do {
            let res = try await api.setTodoSeen(id: id, seen: seen)
            guard generation == targetGeneration else { return }
            markSeen(id, res.seenAt)
        } catch {
            if generation == targetGeneration { markSeen(id, before) }
            throw error
        }
    }

    /// Sets to-do `id`'s seen mark in the list; a read already in flight cannot put the old one back.
    private func markSeen(_ id: String, _ at: Date?) {
        guard let idx = todos.firstIndex(where: { $0.id == id }) else { return }
        mutationRevision += 1
        var updated = todos
        updated[idx].seenAt = at
        apply(updated)
    }

    func delete(id: String, api injected: APIClient? = nil) async throws {
        guard let api = client(for: id, injected) else { throw APIError(status: 0, message: String(localized: "Not connected to a hub [Todo Store 04f822]")) }
        let targetGeneration = generation
        try await api.deleteTodo(id: id)
        guard generation == targetGeneration else { return }
        mutationRevision += 1
        var updated = todos
        updated.removeAll { $0.id == id }
        apply(updated)
    }

    func linkAgent(todoID: String, machineID: String, sessionID: String, api injected: APIClient? = nil) async throws {
        guard let api = client(for: todoID, injected) else { throw APIError(status: 0, message: String(localized: "Not connected to a hub [Todo Store 04f822]")) }
        let targetGeneration = generation
        do {
            try await api.linkTodoAgent(todoID: todoID, machineID: machineID, sessionID: sessionID)
        } catch {
            if generation == targetGeneration {
                await confirmWrite(api: api)
            }
            throw error
        }
        if generation == targetGeneration {
            await confirmWrite(api: api)
        }
    }

    func unlinkAgent(todoID: String, machineID: String, sessionID: String, api injected: APIClient? = nil) async throws {
        guard let api = client(for: todoID, injected) else { throw APIError(status: 0, message: String(localized: "Not connected to a hub [Todo Store 04f822]")) }
        let targetGeneration = generation
        do {
            try await api.unlinkTodoAgent(todoID: todoID, machineID: machineID, sessionID: sessionID)
        } catch {
            if generation == targetGeneration {
                await confirmWrite(api: api)
            }
            throw error
        }
        if generation == targetGeneration {
            await confirmWrite(api: api)
        }
    }

    /// Membership changes immediately; the hub then supplies the authoritative Agent attachments.
    @discardableResult
    func setSpace(todoID: String, space: TodoItem.Space, linked: Bool, api injected: APIClient? = nil) async throws -> Bool {
        guard !changingSpaces else { throw APIError(status: 409, message: String(localized: "Updating projects. Try again in a moment.")) }
        guard let api = client(for: todoID, injected) else { throw APIError(status: 0, message: String(localized: "Not connected to a hub [Todo Store 04f822]")) }
        let targetGeneration = generation
        changingSpaces = true
        defer { if generation == targetGeneration { changingSpaces = false } }
        mutationRevision += 1
        let original = todos
        let via = "\(space.machineID)/\(space.space)"
        var optimistic = todos
        for index in optimistic.indices {
            guard linked || optimistic[index].id == todoID else { continue }
            optimistic[index].spaces?.removeAll { $0.machineID == space.machineID && $0.space == space.space }
            if !linked || optimistic[index].id != todoID {
                optimistic[index].agents.removeAll { $0.source == "space" && $0.via == via }
            }
            if linked && optimistic[index].id == todoID {
                optimistic[index].spaces = (optimistic[index].spaces ?? []) + [space]
            }
        }
        apply(optimistic)
        do {
            if linked {
                try await api.linkTodoSpace(todoID: todoID, machineID: space.machineID, space: space.space)
            } else {
                try await api.unlinkTodoSpace(todoID: todoID, machineID: space.machineID, space: space.space)
            }
            guard generation == targetGeneration else { return true }
            await confirmWrite(api: api)
            return true
        } catch {
            guard generation == targetGeneration else { throw error }
            mutationRevision += 1
            apply(original)
            if TodosLogic.isLegacySpacesRoute(error) {
                spacesSupported = false
                let upgrade = APIError(status: 404, message: String(localized: "This hub can't attach a whole group yet. Update the hub and try again."))
                self.error = upgrade.message
                throw upgrade
            }
            let message = error.shown
            self.error = message
            await confirmWrite(api: api)
            guard generation == targetGeneration else { throw error }
            self.error = message
            throw error
        }
    }

    func link(_ target: TodoLinkTarget, to todoID: String, api: APIClient? = nil) async throws {
        switch target {
        case .space(let project):
            try await setSpace(todoID: todoID, space: project, linked: true, api: api)
        case .agent(let machine, let session, _):
            try await linkAgent(todoID: todoID, machineID: machine, sessionID: session, api: api)
        }
    }

    func unlink(_ target: TodoLinkTarget, from todoID: String, api: APIClient? = nil) async throws {
        switch target {
        case .space(let project):
            try await setSpace(todoID: todoID, space: project, linked: false, api: api)
        case .agent(let machine, let session, _):
            try await unlinkAgent(todoID: todoID, machineID: machine, sessionID: session, api: api)
        }
    }

    /// Creation failure throws; a later link failure returns a message that preserves the created todo.
    func createAndLink(title: String, target: TodoLinkTarget, owner: Owner = .current, api: APIClient? = nil) async throws -> String? {
        if target.isSpace && changingSpaces {
            throw APIError(status: 409, message: String(localized: "Updating projects. Try again in a moment."))
        }
        let created = try await create(title: title, owner: owner, api: api)
        do {
            try await link(target, to: created.id, api: api)
            return nil
        } catch {
            return TodosLogic.createdLinkFailure(error, isSpace: target.isSpace)
        }
    }
}

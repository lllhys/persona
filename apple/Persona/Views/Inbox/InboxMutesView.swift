import SwiftUI

/// Muted (the inbox's More menu): the repositories, chats and senders muted from an item's menu, in every source,
/// newest first; with several managers, the current assistant's. A swipe or a long press takes one back; nothing it
/// covered comes back, only what comes in later (docs/assistant/inbox.md「Muting」).
struct InboxMutesView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var model: InboxMutesModel

    init(deps: InboxMutesModel.Deps = .live) { _model = State(initialValue: InboxMutesModel(deps: deps)) }

    var body: some View {
        NavigationStack {
            LoadableContent(state: model.state, retry: { Task { await model.load() } }) { mutes in
                List {
                    ForEach(mutes) { m in row(m).themedRow() }
                }
                .listStyle(.plain)
                .overlay {
                    if model.isEmpty {
                        EmptyStateView(symbol: Theme.Icon.mute, title: String(localized: "Nothing Muted"), message: String(localized: "Touch and hold an item in the Inbox to mute its repo, conversation, or sender."))
                    }
                }
            }
            .themedPage()
            .navigationTitle("Muted [Inbox Mutes View e5e468]")
            #if os(iOS) || os(visionOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                SheetClose { dismiss() }
            }
            .task(id: model.readKey) { await model.load() }
        }
    }

    private func row(_ m: InboxMute) -> some View {
        HStack(spacing: Theme.Space.m) {
            ThemedIcon(Theme.Icon.mute).foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(m.name).font(Theme.Font.body).lineLimit(1)
                Text([m.sourceName ?? m.source, m.tagName].joined(separator: " · "))
                    .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, Theme.Space.xs)
        .accessibilityIdentifier("inbox-mute")
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button { unmute(m) } label: { Label("Unmute [Inbox Mutes View 8d0173]", systemImage: Theme.Icon.unmute) }.tint(Theme.accent)
        }
        .contextMenu {
            Button { unmute(m) } label: { Label("Unmute [Inbox Mutes View 8d0173]", systemImage: Theme.Icon.unmute) }
        }
    }

    private func unmute(_ m: InboxMute) {
        Task { await model.unmute(m) }
    }
}

/// The Muted list's state: the mutes read, newest first; with several managers the current assistant's, read and taken
/// back with it named (docs/assistant/managers.md "Pages"). Unmute takes one off at once; a failure puts it back.
@MainActor @Observable final class InboxMutesModel {
    struct Deps {
        var api: () -> APIClient?
        /// The current assistant on a hub with several (`AppModel.assistantManager`); nil with one manager.
        var current: () -> HubManager?
        /// Says what went wrong (a toast).
        var show: (any Error) -> Void
        static var live: Deps {
            .init(api: { AppModel.shared.api }, current: { AppModel.shared.namedAssistantManager }, show: { AppModel.shared.show($0) })
        }
    }

    private(set) var state: Loadable<[InboxMute]> = .loading
    @ObservationIgnored private let deps: Deps
    /// The latest load: an older one that answers after it doesn't land.
    @ObservationIgnored private var reads = 0
    /// Whose mutes `state` holds (nil: the one manager's).
    @ObservationIgnored private var owner: String?

    init(deps: Deps = .live) { self.deps = deps }

    /// What the list follows: another current assistant, or it coming or going, reads it again.
    var readKey: [String] { deps.current().map { [$0.id, "\($0.online)"] } ?? [] }

    var isEmpty: Bool { state.value?.isEmpty == true }

    /// With several managers the current one is named, the default too (the device's idea of the default may be
    /// stale), and its mutes are stamped with it.
    func load() async {
        guard let api = deps.api() else { return }
        reads += 1
        let read = reads
        let current = deps.current()
        if owner != current?.id {
            owner = current?.id
            state = .loading // the list before was another assistant's
        }
        do {
            let list = try await (current.map { api.manager($0.id) } ?? api).inboxMutes()
            guard read == reads else { return }
            state = .loaded(Self.newestFirst(list.map { var m = $0; if let current { m.manager = current.id }; return m }))
        } catch {
            if error.isCancellation || read != reads { return } // the sheet closed, or a newer load answers: not a failure
            if case .loaded = state {
                deps.show(error)
            } else {
                state = .failed(current.flatMap { $0.online ? nil : String(localized: "Offline: its mutes show here once it is back.") } ?? Self.said(error))
            }
        }
    }

    /// Takes a mute back at once, naming its manager even when it is the default now (it may have changed since the list
    /// was read); a failure puts it back.
    func unmute(_ m: InboxMute) async {
        guard case .loaded(let list) = state, let api = deps.api() else { return }
        let gone = { (x: InboxMute) in x.id == m.id && x.manager == m.manager }
        withAnimation(Theme.Motion.standard) { state = .loaded(list.filter { !gone($0) }) }
        do {
            try await api.manager(m.manager).unmuteInbox(m.id)
            // A reload read before the unmute reached the hub may have brought it back.
            if case .loaded(let now) = state { state = .loaded(now.filter { !gone($0) }) }
        } catch {
            // Only this mute: what changed meanwhile (another unmute, a reload) stays.
            if owner == m.manager, case .loaded(let now) = state { state = .loaded(Self.newestFirst(now.filter { !gone($0) } + [m])) }
            deps.show(error)
        }
    }

    private static func newestFirst(_ list: [InboxMute]) -> [InboxMute] {
        list.sorted { ($0.createdAt ?? .distantPast) > ($1.createdAt ?? .distantPast) }
    }

    /// A first load's failure in words: the hub's own message, else no network or no answer in the app's
    /// language (the system's text for those follows the phone's language, not the app's).
    private static func said(_ error: any Error) -> String {
        if let shown = error.shown { return shown }
        return error.isOffline ? String(localized: "Can't reach the hub") : String(localized: "The hub didn't respond. Try again later.")
    }
}

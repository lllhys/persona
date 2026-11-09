import SwiftUI

/// Muted (the inbox's More menu): the repositories, chats and senders muted from an item's menu, in every source,
/// newest first; with several managers, under each one's name. A swipe or a long press takes one back; nothing it
/// covered comes back, only what comes in later (docs/assistant/inbox.md「静音」).
struct InboxMutesView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var model: InboxMutesModel

    init(deps: InboxMutesModel.Deps = .live) { _model = State(initialValue: InboxMutesModel(deps: deps)) }

    var body: some View {
        NavigationStack {
            LoadableContent(state: model.state, retry: { Task { await model.load() } }) { mutes in
                List {
                    if let shares = model.shownShares {
                        ForEach(shares) { share in
                            Section {
                                ManagerHeading(manager: share.manager, count: share.failed == nil ? share.items.count : nil).listRow()
                                if let failed = share.failed {
                                    Text(failed).font(Theme.Font.label).foregroundStyle(.secondary).themedRow()
                                } else {
                                    ForEach(share.items) { m in row(m).themedRow() }
                                }
                            }
                        }
                    } else {
                        ForEach(mutes) { m in row(m).themedRow() }
                    }
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
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done [Extension System Presentation c0b3fb]") { dismiss() }.lineLimit(1)
                }
            }
            .task(id: model.rosterKey) { await model.load() }
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

/// The Muted list's state: every mute read, newest first, and with several managers each one's (read and taken back with
/// it named, docs/assistant/managers.md "Grouping"). Unmute takes one off at once; a failure puts it back.
@MainActor @Observable final class InboxMutesModel {
    struct Deps {
        var api: () -> APIClient?
        var roster: () -> ManagerRoster?
        /// Says what went wrong (a toast).
        var show: (any Error) -> Void
        static var live: Deps {
            .init(api: { AppModel.shared.api }, roster: { AppModel.shared.managerRoster }, show: { AppModel.shared.show($0) })
        }
    }

    private(set) var state: Loadable<[InboxMute]> = .loading
    /// Several managers: each one's mutes, or why they couldn't be read, the default first; nil with one.
    private(set) var shares: [ManagerShare<InboxMute>]?
    @ObservationIgnored private let deps: Deps
    /// The latest load: an older one that answers after it doesn't land.
    @ObservationIgnored private var reads = 0

    init(deps: Deps = .live) { self.deps = deps }

    /// What the list follows: a change of the managers reads it again.
    var rosterKey: [String] { deps.roster()?.readKey ?? [] }

    /// The managers to show: ones with mutes or a failure.
    var shownShares: [ManagerShare<InboxMute>]? { shares?.filter { $0.failed != nil || !$0.items.isEmpty } }

    /// Nothing to show: no mutes, and no manager whose couldn't be read.
    var isEmpty: Bool { shownShares?.isEmpty ?? (state.value?.isEmpty == true) }

    func load() async {
        guard let api = deps.api() else { return }
        reads += 1
        let read = reads
        if let roster = deps.roster(), roster.isMulti {
            guard let each = await roster.readEach(api, said: Self.said, { try await $0.inboxMutes() }), read == reads else { return }
            shares = each.map { var s = $0; s.items = Self.newestFirst(s.items); return s }
            state = .loaded(Self.newestFirst(each.flatMap(\.items)))
            return
        }
        do {
            let list = Self.newestFirst(try await api.inboxMutes())
            guard read == reads else { return }
            shares = nil
            state = .loaded(list)
        } catch {
            if error.isCancellation || read != reads { return } // the sheet closed, or a newer load answers: not a failure
            if case .loaded = state { deps.show(error) } else { state = .failed(Self.said(error)) }
        }
    }

    /// Takes a mute back at once, naming its manager even when it is the default now (it may have changed since the list
    /// was read); a failure puts it back, into its manager's group.
    func unmute(_ m: InboxMute) async {
        guard case .loaded(let list) = state, let api = deps.api() else { return }
        let gone = { (x: InboxMute) in x.id == m.id && x.manager == m.manager }
        let home = shares?.first { $0.items.contains(where: gone) }?.id
        withAnimation(Theme.Motion.standard) {
            state = .loaded(list.filter { !gone($0) })
            shares = shares?.map { var s = $0; s.items.removeAll(where: gone); return s }
        }
        do {
            try await api.manager(m.manager).unmuteInbox(m.id)
            // A reload read before the unmute reached the hub may have brought it back.
            if case .loaded(let now) = state { state = .loaded(now.filter { !gone($0) }) }
            shares = shares?.map { var s = $0; s.items.removeAll(where: gone); return s }
        } catch {
            // Only this mute: what changed meanwhile (another unmute, a reload) stays.
            state = .loaded(Self.newestFirst((state.value ?? []).filter { !gone($0) } + [m]))
            shares = shares?.map { s in
                guard s.id == home else { return s }
                var s = s
                s.items = Self.newestFirst(s.items.filter { !gone($0) } + [m])
                return s
            }
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

    /// What a manager's place says when its mutes couldn't be read.
    private static func said(_ manager: HubManager, _ error: any Error) -> String {
        manager.online ? said(error) : String(localized: "Offline: its mutes show here once it is back.")
    }
}

nonisolated extension InboxMute: ManagerStamped {}

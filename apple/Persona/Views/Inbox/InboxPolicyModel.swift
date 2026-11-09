import Foundation
import Observation

/// What a rule sheet is about (docs/assistant/inbox.md "Policies"): a new or existing rule from an item's chat, sender or
/// repository, or an existing rule from the Rules list.
struct InboxPolicyTarget: Identifiable, Hashable, Sendable {
    var item: InboxItem?
    var option: InboxItem.MuteOption
    var source: String
    var policy: InboxPolicy?

    var id: String { (manager.map { "\($0):" } ?? "") + "\(source):\(option.tag):\(option.value)" }
    /// Whose rule it is (#1850): the item's manager, else the one the Rules list read it from.
    var manager: String? { item != nil ? item?.manager : policy?.manager }

    init(item: InboxItem, option: InboxItem.MuteOption) {
        self.item = item
        self.option = option
        source = item.source
    }

    init(policy: InboxPolicy) {
        option = policy.option
        source = policy.source
        self.policy = policy
    }

    /// The sheet's first line: where it is from and what kind of thing ("Lark · Chat").
    var line: String {
        [item?.accountName.nilIfEmpty ?? policy?.sourceName ?? source, InboxPolicy.kindName(option.tag)].joined(separator: " · ")
    }
}

private extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}

extension InboxPolicy {
    /// Whether a rule's tier reaches an item at once, as the hub does on save: the same thing, open, triaged, not in
    /// progress.
    static func reaches(_ source: String, _ option: InboxItem.MuteOption, _ it: InboxItem) -> Bool {
        it.source == source && !it.isDone && it.isTriaged && it.triage != .working
            && (it.mutes ?? []).contains { $0.tag == option.tag && $0.value == option.value }
    }

    /// The items a rule's tier moves, as they will be: the tier, the section, the reason "Your rule: <name>", and no
    /// urgency outside Needs You. Ones already in that tier, and any tier left to triage, move nothing.
    static func moved(by tier: Tier, of option: InboxItem.MuteOption, in source: String, among items: [InboxItem]) -> [InboxItem] {
        guard tier != .unset else { return [] }
        let reason = String(localized: "Your rule: \(option.name)")
        return items.compactMap { it in
            guard reaches(source, option, it), it.tier != tier.rawValue else { return nil }
            var moved = it
            moved.tier = tier.rawValue
            if let s = it.status, s != InboxItem.Tier.working.rawValue { moved.status = tier.rawValue }
            moved.reason = reason
            if tier != .action { moved.urgent = false }
            return moved
        }
    }

    /// A failure in words, as the Muted list says it: the hub's own message, else no network or no answer in the app's
    /// language (the system's text for those follows the phone's language, not the app's).
    static func said(_ error: Error) -> String {
        if let shown = error.shown { return shown }
        return error.isOffline ? String(localized: "Can't reach the hub") : String(localized: "The hub didn't respond. Try again later.")
    }
}

/// The rule sheet of one chat / sender / repository: where its items go and what the AI twin does with them, prefilled
/// from the rule it has. Save shows the tier on the items it reaches at once (the hub does the same, the next push
/// confirms it); a failure puts them back.
@MainActor @Observable final class InboxPolicyModel {
    struct Deps {
        var api: () -> APIClient?
        /// The inbox's items now, and showing changed ones (the optimistic tier, and putting them back).
        var items: () -> [InboxItem]
        var show: ([InboxItem]) -> Void
        var toast: (Toast) -> Void
        var roster: () -> ManagerRoster? = { nil }
        static var live: Deps {
            .init(api: { AppModel.shared.api }, items: { InboxStore.shared.items }, show: { InboxStore.shared.showRuleChange($0) },
                  toast: { AppModel.shared.toast = $0 }, roster: { AppModel.shared.managerRoster })
        }
    }

    let target: InboxPolicyTarget
    private(set) var policy: InboxPolicy?
    private(set) var loaded: Bool
    private(set) var busy = false
    var tier: InboxPolicy.Tier
    var twin: InboxPolicy.Twin
    @ObservationIgnored private let deps: Deps

    init(target: InboxPolicyTarget, deps: Deps = .live) {
        self.target = target
        self.deps = deps
        policy = target.policy
        loaded = target.policy != nil
        tier = target.policy?.tier ?? .unset
        twin = target.policy?.twin ?? .unset
    }

    /// Save is off until the rules are read, and for a new rule that sets neither part.
    var canSave: Bool { loaded && !busy && (policy != nil || tier != .unset || twin != .unset) }

    /// The manager the sheet's requests name (`ManagerRoster.naming`).
    private var named: String? { deps.roster().map { $0.naming(target.manager) } ?? target.manager }

    /// Reads the rule this target already has; none from an older hub (no list): start from nothing.
    func load() async {
        guard !loaded else { return }
        defer { loaded = true }
        guard let list = try? await deps.api()?.manager(named).inboxPolicies() else { return }
        let same = list.first { $0.source == target.source && $0.tag == target.option.tag && $0.value == target.option.value }
        policy = same
        tier = same?.tier ?? .unset
        twin = same?.twin ?? .unset
    }

    /// Saves the parts chosen, or neither (`remove`), which removes the rule. True when the hub took it. Removing a rule
    /// opened from the Rules list names the manager the list read it from, even when that is the default now.
    func save(remove: Bool = false) async -> Bool {
        let (tier, twin) = remove ? (InboxPolicy.Tier.unset, InboxPolicy.Twin.unset) : (self.tier, self.twin)
        let stamped = target.item == nil && tier == .unset && twin == .unset
        guard let api = deps.api()?.manager(stamped ? target.manager : named), !busy else { return false }
        guard policy != nil || target.item != nil else { return false }
        busy = true
        defer { busy = false }
        let owner = AppModel.shared.pushedBy(target.manager)
        let items = deps.items().filter { AppModel.shared.pushedBy($0.manager) == owner } // the hub applies it to its manager's
        let before = Dictionary(items.map { ($0.key, $0) }, uniquingKeysWith: { a, _ in a })
        let moved = InboxPolicy.moved(by: tier, of: target.option, in: target.source, among: items)
        deps.show(moved)
        let name = target.option.name
        do {
            if let id = policy?.id {
                try await api.updateInboxPolicy(id, tier: tier, twin: twin)
            } else if let item = target.item {
                try await api.setInboxPolicy(itemID: item.id, tag: target.option.tag, tier: tier, twin: twin)
            }
            let removed = tier == .unset && twin == .unset
            deps.toast(Toast(removed ? String(localized: "Rule removed for “\(name)”.") : String(localized: "Rule saved for “\(name)”."), style: .success))
            return true
        } catch {
            deps.show(moved.compactMap { before[$0.key] })
            if !error.isCancellation {
                deps.toast(Toast(String(localized: "Couldn't save the rule: \(InboxPolicy.said(error))"), style: .error))
            }
            return false
        }
    }
}

/// Rules (the Inbox's More menu): every source's rules, newest first, each with Edit and Remove. With several managers,
/// each one's rules under its name, read and removed with it named (docs/assistant/managers.md "Grouping").
@MainActor @Observable final class InboxRulesModel {
    struct Deps {
        var api: () -> APIClient?
        var toast: (Toast) -> Void
        var roster: () -> ManagerRoster? = { nil }
        static var live: Deps {
            .init(api: { AppModel.shared.api }, toast: { AppModel.shared.toast = $0 }, roster: { AppModel.shared.managerRoster })
        }
    }

    /// Every rule read, newest first (several managers: all of theirs).
    private(set) var state: Loadable<[InboxPolicy]> = .loading
    /// Several managers: each one's rules, or why they couldn't be read, the default first; nil with one.
    private(set) var shares: [ManagerShare<InboxPolicy>]?
    @ObservationIgnored private let deps: Deps
    /// The latest load: an older one that answers after it doesn't land.
    @ObservationIgnored private var reads = 0

    init(deps: Deps = .live) { self.deps = deps }

    /// What the list follows: a change of the managers reads it again.
    var rosterKey: [String] { deps.roster()?.readKey ?? [] }

    /// The managers to show: ones with rules or a failure.
    var shownShares: [ManagerShare<InboxPolicy>]? { shares?.filter { $0.failed != nil || !$0.items.isEmpty } }

    /// Nothing to show: no rules, and no manager whose couldn't be read.
    var isEmpty: Bool { shownShares?.isEmpty ?? (state.value?.isEmpty == true) }

    /// Reads the list; a failure after it showed once is a toast and the list stays.
    func load() async {
        guard let api = deps.api() else { return }
        reads += 1
        let read = reads
        if let roster = deps.roster(), roster.isMulti {
            guard let each = await roster.readEach(api, said: Self.said, { try await $0.inboxPolicies() }), read == reads else { return }
            shares = each.map { var s = $0; s.items = InboxPolicy.newestFirst(s.items); return s }
            state = .loaded(InboxPolicy.newestFirst(each.flatMap(\.items)))
            return
        }
        do {
            let list = InboxPolicy.newestFirst(try await api.inboxPolicies())
            guard read == reads else { return }
            shares = nil
            state = .loaded(list)
        } catch {
            if error.isCancellation || read != reads { return } // the sheet closed, or a newer load answers: not a failure
            let said = String(localized: "Couldn't load the rules: \(InboxPolicy.said(error))")
            if case .loaded = state { deps.toast(Toast(said, style: .error)) } else { state = .failed(said) }
        }
    }

    /// What a manager's place says when its rules couldn't be read.
    private static func said(_ manager: HubManager, _ error: any Error) -> String {
        manager.online ? String(localized: "Couldn't load the rules: \(InboxPolicy.said(error))")
            : String(localized: "Offline: its rules show here once it is back.")
    }

    /// Takes a rule out at once, naming its manager even when it is the default now (it may have changed since the list
    /// was read); a failure puts it back, into its manager's group.
    func remove(_ p: InboxPolicy) async {
        guard case .loaded(let list) = state, let api = deps.api() else { return }
        let gone = { (q: InboxPolicy) in q.id == p.id && q.manager == p.manager }
        let home = shares?.first { $0.items.contains(where: gone) }?.id
        state = .loaded(list.filter { !gone($0) })
        shares = shares?.map { var s = $0; s.items.removeAll(where: gone); return s }
        do {
            try await api.manager(p.manager).deleteInboxPolicy(p.id)
            // A reload read before the removal reached the hub may have brought it back.
            if case .loaded(let now) = state { state = .loaded(now.filter { !gone($0) }) }
            shares = shares?.map { var s = $0; s.items.removeAll(where: gone); return s }
            deps.toast(Toast(String(localized: "Rule removed for “\(p.name)”."), style: .success))
        } catch {
            // Only this rule: what changed meanwhile (another removal, a reload) stays.
            state = .loaded(InboxPolicy.newestFirst((state.value ?? []).filter { !gone($0) } + [p]))
            shares = shares?.map { s in
                guard s.id == home else { return s }
                var s = s
                s.items = InboxPolicy.newestFirst(s.items.filter { !gone($0) } + [p])
                return s
            }
            deps.toast(Toast(String(localized: "Couldn't remove the rule: \(InboxPolicy.said(error))"), style: .error))
        }
    }
}

nonisolated extension InboxPolicy: ManagerStamped {}

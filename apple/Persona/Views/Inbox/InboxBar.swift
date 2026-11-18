import Foundation

/// The Inbox's section bar (#9053, docs/assistant/inbox.md "Section bar"): one row of choices at the top of the list,
/// Needs You · In Progress · FYI · Done · All, so FYI and Done are a tap away instead of several screens below a long
/// Needs You. Pure: the page hands it what its projection already counted, and spec/fixtures/inbox/bar*.json pin it for
/// the web and Android too.
nonisolated enum InboxChoice: String, CaseIterable, Identifiable, Sendable {
    case action, working, fyi, done, all

    var id: String { rawValue }

    /// The open section the choice shows alone; Done and All are not one of them.
    var tier: InboxItem.Tier? {
        switch self {
        case .action: .action
        case .working: .working
        case .fyi: .fyi
        case .done, .all: nil
        }
    }

    /// The section's own name, as its title in the list says it.
    var title: String {
        switch self {
        case .action, .working, .fyi: tier?.title ?? ""
        case .done: String(localized: "Done [Inbox View 59f6c8]")
        case .all: String(localized: "All")
        }
    }

    /// Whether the section's source / time groups apply: Done is always by when it was done, In Progress is never grouped.
    var groups: Bool { self != .done && self != .working }
}

nonisolated enum InboxBar {
    /// What a choice's count is drawn as: Needs You's in the Needs-you tone (#4538), the others' neutral.
    enum Look: Equatable, Sendable { case needs, neutral }

    /// One choice on the bar: its count (nil: none drawn; 0 stays in the data, the badge hides it) and the count's look.
    struct Entry: Equatable, Identifiable, Sendable {
        var choice: InboxChoice
        var count: Int?
        var look: Look?
        var id: String { choice.rawValue }
    }

    /// The rows each section has under the source and kind filters and the search (the counts on the bar).
    struct Rows: Equatable, Sendable {
        /// Needs You's items and the agents waiting on the user.
        var action = 0
        var working = 0
        var fyi = 0
        var noise = 0
        /// Done rows the filters let through: the list's own, or 1 while only the history's loaded pages have one (the
        /// bar asks only whether Done has a row, so the history is not counted).
        var done = 0
        /// The hub's day counts name a done item on a day Done can list (#2967; false from a hub without them).
        var doneDays = false
        /// A source, a kind or a search is on: the hub's day counts don't apply then.
        var filtered = false

        var open: Int { action + working + fyi + noise }

        func count(_ tier: InboxItem.Tier) -> Int {
            switch tier {
            case .action: action
            case .working: working
            case .fyi: fyi
            case .noise: noise
            }
        }

        /// Done has something to show: a row, or (nothing filtered) a day the hub counts.
        var hasDone: Bool { done > 0 || !filtered && doneDays }
    }

    /// The choices on the bar, in order: Needs You and All always; In Progress and FYI while they have a row or are
    /// chosen (so a choice that empties can still be left); Done while it has something to show or is chosen.
    static func entries(_ rows: Rows, chosen: InboxChoice) -> [Entry] {
        var out = [Entry(choice: .action, count: rows.action, look: .needs)]
        if rows.working > 0 || chosen == .working { out.append(Entry(choice: .working, count: rows.working, look: .neutral)) }
        if rows.fyi > 0 || chosen == .fyi { out.append(Entry(choice: .fyi, count: rows.fyi, look: .neutral)) }
        if rows.hasDone || chosen == .done { out.append(Entry(choice: .done)) }
        out.append(Entry(choice: .all))
        return out
    }

    /// Whether the list draws an open section's rows: the chosen one alone, or every one under All (Collapsed has no
    /// choice of its own and shows only there).
    static func draws(_ tier: InboxItem.Tier, chosen: InboxChoice) -> Bool {
        chosen == .all || chosen.tier == tier
    }

    /// Whether the list draws Done.
    static func drawsDone(chosen: InboxChoice) -> Bool { chosen == .all || chosen == .done }

    /// Whether the list shows an open section: drawn for the choice, with a row. The page asks this, as the shared cases do.
    static func shows(_ tier: InboxItem.Tier, _ rows: Rows, chosen: InboxChoice) -> Bool {
        draws(tier, chosen: chosen) && rows.count(tier) > 0
    }

    /// Whether the list shows Done: drawn for the choice, with something to show.
    static func showsDone(_ rows: Rows, chosen: InboxChoice) -> Bool { drawsDone(chosen: chosen) && rows.hasDone }

    /// The sections shown, by the ids the shared cases use: the chosen one when it has rows, under All every one with rows.
    static func shown(_ rows: Rows, chosen: InboxChoice) -> [String] {
        let open = InboxItem.Tier.allCases.filter { shows($0, rows, chosen: chosen) }.map(\.rawValue)
        return open + (showsDone(rows, chosen: chosen) ? ["done"] : [])
    }

    /// What the list says with nothing to show (nil: it has rows). All Caught Up only when nothing is open anywhere; a
    /// chosen section with nothing in it says so in its own words; the filtered and no-match states stay as they were.
    static func empty(_ rows: Rows, chosen: InboxChoice, source: String, kind: String, query: String) -> InboxPage.EmptyKind? {
        let whole = InboxPage.emptyKind(source: source, kind: kind, query: query)
        switch chosen {
        case .done:
            guard !rows.hasDone else { return nil }
            return whole == .caughtUp ? .section : whole
        case .all:
            return rows.open > 0 ? nil : whole
        case .action, .working, .fyi:
            guard let tier = chosen.tier, rows.count(tier) == 0 else { return nil }
            return rows.open > 0 ? .section : whole
        }
    }

    /// Where an item opened from outside the list puts the list: its own section, Done for a done one, and All with
    /// Collapsed open for a collapsed one (Collapsed has no choice).
    static func landing(_ item: InboxItem) -> (choice: InboxChoice, openCollapsed: Bool) {
        if item.isDone { return (.done, false) }
        switch item.triage {
        case .action: return (.action, false)
        case .working: return (.working, false)
        case .fyi: return (.fyi, false)
        case .noise: return (.all, true)
        }
    }

    /// The agents waiting on the user that head Needs You: none under a source (they have none), only under no kind or
    /// Agent, never a muted one, and under a search only those it finds (by the card's title as the row draws it).
    @MainActor static func waiting(_ interactions: [Interaction], source: String, kind: String, query: String,
                                   muted: (SessionKey) -> Bool) -> [Interaction] {
        guard source.isEmpty, kind.isEmpty || kind == "agent" else { return [] }
        return interactions.filter { ix in
            !muted(ix.sessionKey) && (query.isEmpty || [ix.sessionTitle ?? "", ix.summary ?? "", ix.title].contains { $0.localizedCaseInsensitiveContains(query) })
        }
    }
}

extension InboxBar.Rows {
    /// The bar's counts from the page's projection: the Agent kind keeps only the waiting agents (no item is one), and
    /// Done looks through the history's loaded pages only while the list has no done row of its own.
    @MainActor init(_ projection: InboxProjection, agents: Int, filter: InboxProjection.Filter, history: [InboxItem],
                    doneDays: [String: Int]?) {
        let tiers = filter.kind == "agent" ? [:] : projection.byTier
        action = (tiers[.action]?.count ?? 0) + agents
        working = tiers[.working]?.count ?? 0
        fyi = tiers[.fyi]?.count ?? 0
        noise = tiers[.noise]?.count ?? 0
        done = projection.doneItems.isEmpty ? (history.contains { $0.isDone && filter.matches($0) } ? 1 : 0) : projection.doneItems.count
        // only a day Done's groups can place counts (`InboxGroups.dayCounts`), so Done is on the bar only when it lists
        // a group; the first such day answers, not a pass over the whole history
        self.doneDays = doneDays?.contains { $0.value > 0 && InboxGroups.isDay($0.key) } ?? false
        filtered = !filter.source.isEmpty || !filter.kind.isEmpty || !filter.query.isEmpty
    }
}

/// The bar's choice while the page is up (`InboxPage`): Needs You each time the Inbox is entered, never stored, so a
/// remembered FYI or Done can't hide new requests. A search looks through the whole Inbox: text appearing keeps the
/// choice aside and shows All, a pick narrows the matches, and clearing the text brings the choice back.
nonisolated struct InboxBarState: Equatable, Sendable {
    var chosen: InboxChoice = .action
    /// The choice kept aside while a search is on.
    var saved: InboxChoice?
    /// The search text the page had last.
    var query = ""

    /// A choice tapped.
    mutating func pick(_ choice: InboxChoice) { chosen = choice }

    /// The search text became `text`.
    mutating func search(_ text: String) {
        if query.isEmpty && !text.isEmpty {
            saved = chosen
            chosen = .all
        } else if !query.isEmpty && text.isEmpty {
            chosen = saved ?? .action
            saved = nil
        }
        query = text
    }

    /// The Inbox entered with `text` in its search (a page that keeps it; the iPhone's starts empty).
    mutating func enter(_ text: String) {
        query = text
        chosen = text.isEmpty ? .action : .all
        saved = text.isEmpty ? nil : .action
    }
}

/// The item page in front over the Inbox's stack, so a list put beneath it (an item opened from outside the Inbox, a
/// place's kept path brought back, #6599) opens on that item's choice and closing the item lands where the item is
/// (docs/assistant/inbox.md "Section bar"). Only the list's first appearance reads it: pages pushed from the list itself
/// (its rows, an item's chain) leave the choice as it was. Not observed: nothing draws it.
@MainActor final class InboxBarLanding {
    static let shared = InboxBarLanding()
    private(set) var shown: InboxItem?
    /// The item last in front, kept after its page goes: a place left with it open brings it back on top.
    private var last: InboxItem?

    func appeared(_ item: InboxItem) {
        shown = item
        last = item
    }

    func disappeared(_ item: InboxItem) {
        if shown?.key == item.key { shown = nil }
    }

    /// The Inbox comes back with what it kept open: its item is in front again before any of its pages appear. In the
    /// Sidebar layout the new list is the root and appears at once, before the item's page does.
    func broughtBack() {
        if let last { shown = last }
    }
}

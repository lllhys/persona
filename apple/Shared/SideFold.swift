import Foundation

/// Side sessions live in their parent's chat (#7944, docs/apple/ios.md "Side sessions"): a Claude desktop app's side
/// chat, /btw question or spawned session (`parent_agent_id`) is no row or count of its own once the hub folds them
/// (`sessions_side_fold` in its hello). The hub names the listed row a side folds under (`folded_under`), says it is a
/// side (`folded_as: "side"`) or that it belongs to one (`folded_side`: a side's own Codex children and workers), and
/// rolls the sides up on the parent (`children.sides*`). These are the rules every Apple client applies to the rows it
/// holds; the views only draw what `fold` gives them.
///
/// Folding is done once per list change, where the lists land (`AppModel.sessionsByMachine`) and where the Agents list's
/// paged window is projected (`AgentListProjection.build`, before `ChildSessions.fold`), never in a view's `body`
/// (docs/apple/performance.md P4/P5). It sits in front of the hub's child fold (`ChildSessions`, #8010), which drops
/// every folded row from the Agents list and shows children as cards in their parent's chat: a side is no such card,
/// it lives behind its parent's Side Sessions chip, it counts nowhere on its own (the dock, badges, widgets, the watch,
/// Siri, machines pages too), and its own result unread makes its parent unread. Without the feature for a row's hub,
/// nothing here changes it.
nonisolated enum SideFold {
    /// What a hub lists in its hello when every manager behind it folds side sessions.
    static let feature = "sessions_side_fold"
    /// `folded_as` of a side session.
    static let side = "side"

    // MARK: Rows and cards

    /// A row that folds into its parent's chat by the hub's own fields: a side session, or a row that belongs to one.
    /// Never one of Persona's own conversations, nor a row that names itself.
    static func isSideRow(_ s: Session) -> Bool {
        guard !s.assistant, let under = s.foldedUnder, under != s.sessionID else { return false }
        return s.foldedAs == side || s.foldedSide != nil
    }

    /// A card whose home is its parent's chat: asked by a side session or by a row that belongs to one.
    static func isSideCard(_ ix: Interaction) -> Bool {
        guard let under = ix.foldedUnder, under != ix.sessionID else { return false }
        return ix.foldedAs == side || ix.foldedSide != nil
    }

    /// The side session whose chat shows a folded row or card: the side itself, or the side its `folded_side` names.
    static func chatKey(group: String, machineID: String, sessionID: String, foldedAs: String?, foldedSide: String?) -> SessionKey {
        let id = foldedAs == side ? sessionID : foldedSide ?? sessionID
        return SessionKey(group: group, machineID: machineID, sessionID: id)
    }

    // MARK: The parent's rollup and folded state

    /// What a parent's sides add up to: how many sides (`folded_as: "side"`), and how many of the rows in their chats
    /// work, need the user, and (sides only) have a result not looked at yet.
    struct Rollup: Hashable, Sendable {
        var sides = 0
        var working = 0
        var needsYou = 0
        var unread = 0

        /// The hub's rollup on a row whose sides this client does not hold (a paged window).
        init(_ c: SessionChildren?) {
            sides = c?.sides ?? 0
            working = c?.sidesWorking ?? 0
            needsYou = c?.sidesNeedsYou ?? 0
            unread = c?.sidesUnread ?? 0
        }

        init(sides: Int = 0, working: Int = 0, needsYou: Int = 0, unread: Int = 0) {
            self.sides = sides
            self.working = working
            self.needsYou = needsYou
            self.unread = unread
        }

        var isEmpty: Bool { sides == 0 }
    }

    /// The state a parent shows and is counted by with its sides (the hub's `Session.FoldedState`, sides only): it needs
    /// the user when it or a row of its sides does; it works when one of them works, unless it has a result not looked at
    /// yet or awaits the user's reply; it is unread when it or one of its sides is; otherwise its own.
    static func foldedState(_ s: Session, _ r: Rollup) -> AgentState {
        if s.state == .needsYou || r.needsYou > 0 { return .needsYou }
        if r.working > 0 && s.state != .unread && !(s.state == .idle && s.awaitsReply) { return .working }
        if s.state == .unread || r.unread > 0 { return .unread }
        return s.state
    }

    // MARK: Folding a list

    /// One list folded (`fold`).
    struct Result: Sendable {
        /// The rows the lists show and count: every row but the folded ones, each parent in its folded state.
        var listed: [Session]
        /// Each parent's side sessions, flat (sides of sides too), in the sheet's order (`ordered`).
        var sides: [SessionKey: [Session]] = [:]
        /// Each folded row (a side, or a row of a side's chat) → the listed row whose chat is its home.
        var home: [SessionKey: SessionKey] = [:]
        /// Each folded row → the side session whose chat shows it: itself, or the side its `folded_side` names.
        var chat: [SessionKey: SessionKey] = [:]
        /// Each parent's rollup: from the rows held, else (a paged row) the hub's `children`.
        var rollups: [SessionKey: Rollup] = [:]

        init(listed: [Session] = []) { self.listed = listed }
    }

    /// Folds the rows of the hubs in `groups` (those whose hello lists `feature`): a row that is a side or belongs to one
    /// and whose `folded_under` names a row held here on the same machine leaves the list and goes under that row; one
    /// whose parent is not held (another page, a hub that hides it) stays a row of its own, so nothing is out of sight.
    /// Rows keep their order. The usual list, with no side in it, costs one pass and comes back as it was.
    ///
    /// Folding what was folded gives it back: a parent carries its rollup in `children` and its folded state already.
    static func fold(_ sessions: [Session], groups: Set<String>) -> Result {
        guard !groups.isEmpty else { return Result(listed: sessions) }
        var candidates: [Int] = []
        var rolled = false
        for i in sessions.indices where groups.contains(sessions[i].group) {
            if isSideRow(sessions[i]) { candidates.append(i) } else if (sessions[i].children?.sides ?? 0) > 0 { rolled = true }
        }
        guard !candidates.isEmpty || rolled else { return Result(listed: sessions) }
        var index: [SessionKey: Int] = [:]
        index.reserveCapacity(sessions.count)
        for i in sessions.indices where index[sessions[i].key] == nil { index[sessions[i].key] = i }
        // Each folded row's parent, by index; -1 for a row that stays.
        var parent = [Int](repeating: -1, count: sessions.count)
        var members: [Int: [Int]] = [:]
        for i in candidates {
            let s = sessions[i]
            guard let under = s.foldedUnder,
                  let p = index[SessionKey(group: s.group, machineID: s.machineID, sessionID: under)], p != i,
                  !sessions[p].assistant, !isSideRow(sessions[p]) else { continue }
            parent[i] = p
            members[p, default: []].append(i)
        }
        var out = Result()
        for (p, list) in members {
            let key = sessions[p].key
            var sides: [Session] = []
            var r = Rollup()
            for i in list {
                let s = sessions[i]
                out.home[s.key] = key
                out.chat[s.key] = chatKey(group: s.group, machineID: s.machineID, sessionID: s.sessionID,
                                          foldedAs: s.foldedAs, foldedSide: s.foldedSide)
                if s.countedState == .needsYou { r.needsYou += 1 }
                if s.countedState == .working { r.working += 1 }
                if s.foldedAs == side {
                    sides.append(s)
                    if s.state == .unread { r.unread += 1 }
                }
            }
            // Rows of a side's chat held without the side itself: the hub's count of sides stands for it.
            r.sides = sides.isEmpty ? sessions[p].children?.sides ?? 0 : sides.count
            out.sides[key] = ordered(sides)
            out.rollups[key] = r
        }
        var listed: [Session] = []
        listed.reserveCapacity(sessions.count - out.home.count)
        for i in sessions.indices where parent[i] < 0 {
            var s = sessions[i]
            if let list = members[i], let r = out.rollups[s.key] {
                s = folded(s, members: list.map { sessions[$0] }, rollup: r)
            } else if groups.contains(s.group), let c = s.children, c.sides > 0, out.rollups[s.key] == nil {
                // A paged row whose sides are not held: the hub's rollup says what they add up to.
                let r = Rollup(c)
                out.rollups[s.key] = r
                s.state = foldedState(s, r)
            }
            listed.append(s)
        }
        out.listed = listed
        return out
    }

    /// The parent as the lists show it: its folded state (since its oldest waiting side when a side makes it need the
    /// user), its sides' pending cards with its own, its time the newest of theirs, and the rollup in `children` so that
    /// folding it again changes nothing.
    private static func folded(_ parent: Session, members: [Session], rollup r: Rollup) -> Session {
        var s = parent
        let state = foldedState(parent, r)
        if state == .needsYou, parent.state != .needsYou {
            s.stateSince = members.filter { $0.countedState == .needsYou }.compactMap { $0.stateSince ?? $0.updatedAt }.min() ?? s.stateSince
        }
        s.state = state
        s.pendingInteractions += members.reduce(0) { $0 + $1.pendingInteractions }
        if let newest = members.compactMap(\.updatedAt).max(), newest > (s.updatedAt ?? .distantPast) { s.updatedAt = newest }
        var c = s.children ?? SessionChildren()
        c.sides = r.sides
        c.sidesWorking = r.working
        c.sidesNeedsYou = r.needsYou
        c.sidesUnread = r.unread
        s.children = c
        return s
    }

    /// The sheet's order (docs/apple/ios.md "Side sessions"): the ones that need the user first, then the working ones,
    /// then the rest by their last activity, newest first; the stopped ones last, newest first too.
    static func ordered(_ sides: [Session]) -> [Session] {
        func rank(_ s: Session) -> Int {
            if s.countedState == .needsYou { return 0 }
            if s.countedState == .working { return 1 }
            return s.state.isLive && s.status != .offline ? 2 : 3
        }
        return sides.enumerated().sorted { a, b in
            let x = rank(a.element), y = rank(b.element)
            if x != y { return x < y }
            let p = a.element.updatedAt ?? .distantPast, q = b.element.updatedAt ?? .distantPast
            return p != q ? p > q : a.offset < b.offset
        }.map(\.element)
    }

    // MARK: Cards

    // A side's card is shown on its parent by its `folded_under` (`Interaction.requestHome`, as every folded child's,
    // #8010): Needs You counts it there once, and the parent's chat has it. Only which side's chat it is, is ours.

    /// The side session whose chat shows a card, nil when it is no side's.
    static func chat(_ ix: Interaction, fold: Result, on: Bool) -> SessionKey? {
        guard on else { return nil }
        if isSideCard(ix) {
            return chatKey(group: ix.group, machineID: ix.machineID, sessionID: ix.sessionID, foldedAs: ix.foldedAs, foldedSide: ix.foldedSide)
        }
        return fold.chat[ix.sessionKey]
    }

    // MARK: The feature, remembered

    /// Whether `group`'s hub folded sides at its last hello: what the app goes by until this launch's hello comes, and
    /// what Siri and the watch go by (they read no hello).
    static func remembered(_ group: String, in defaults: UserDefaults = .standard) -> Bool {
        defaults.bool(forKey: key(group))
    }

    /// Notes what `group`'s hello said; written only when it changes.
    static func remember(_ on: Bool, group: String, in defaults: UserDefaults = .standard) {
        if defaults.bool(forKey: key(group)) != on { defaults.set(on, forKey: key(group)) }
    }

    /// The groups of `groups` whose hub folded sides at its last hello.
    static func rememberedGroups(_ groups: some Sequence<String>, in defaults: UserDefaults = .standard) -> Set<String> {
        Set(groups.filter { remembered($0, in: defaults) })
    }

    private static func key(_ group: String) -> String { "hubSideFold." + group }
}

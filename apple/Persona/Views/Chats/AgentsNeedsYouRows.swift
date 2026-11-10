import SwiftUI

/// The Agents tab's Needs You, counted one way wherever it is named (docs/design/PRINCIPLES.md, "Consistency first"):
/// one row per Agent session that waits for the user, so the section's header, the Needs You filter and the tab's badge
/// all count sessions, never cards (spec/fixtures/needs-you/cases.json). A session waits while it has a pending card, or
/// while the hub says it needs the user and no card was read (a prompt only its terminal shows); its cards sit under its
/// name, oldest first. A card that belongs to no session (`personad ask` outside herdr) is a row of its own. Rows go
/// oldest first: by their oldest card or, with none, by when the session started waiting.
nonisolated extension AgentsNeedsYou {
    struct Row: Identifiable, Equatable {
        /// The session that waits; a sessionless card's own key.
        let key: SessionKey
        /// Its pending cards, oldest first; empty when it waits in its terminal.
        fileprivate(set) var cards: [Interaction]
        /// Since when it waits: its oldest card's time, else when its state turned to needs you.
        let since: Date?

        /// `agent:<machine>/<session>`, or `card:<id>` for a sessionless card (with the group in front when not the
        /// default), as the shared fixture names rows.
        var id: String {
            let group = key.group.isEmpty ? "" : key.group + "|"
            if let only = cards.first, only.isSessionless { return "card:" + group + only.id }
            return "agent:" + group + key.machineID + "/" + key.sessionID
        }
    }

    /// The rows for `cards` (pending, oldest first, as `AppModel.pendingInteractions` gives them) and `waiting` (the
    /// sessions whose state needs the user, card or not), both already narrowed to what the caller shows.
    static func rows(cards: [Interaction], waiting: [Session]) -> [Row] {
        var rows: [Row] = []
        var at: [SessionKey: Int] = [:]
        for ix in cards {
            if ix.isSessionless {
                rows.append(Row(key: ix.sessionKey, cards: [ix], since: ix.createdAt))
            } else if let i = at[ix.sessionKey] {
                rows[i].cards.append(ix)
            } else {
                at[ix.sessionKey] = rows.count
                rows.append(Row(key: ix.sessionKey, cards: [ix], since: ix.createdAt))
            }
        }
        for s in waiting where at[s.key] == nil {
            at[s.key] = rows.count
            rows.append(Row(key: s.key, cards: [], since: s.stateSince ?? s.updatedAt))
        }
        // Stable: rows that started waiting at the same moment keep the order they came in.
        return rows.enumerated()
            .sorted { a, b in
                let x = a.element.since ?? .distantPast, y = b.element.since ?? .distantPast
                return x != y ? x < y : a.offset < b.offset
            }
            .map(\.element)
    }

    /// The sessions that wait for the user whether or not a card was read, as the Needs You filter matches them;
    /// never the Persona tab's own conversations.
    static func waiting(_ sessions: [Session]) -> [Session] {
        sessions.filter { !$0.assistant && $0.countedState == .needsYou }
    }

    /// The Agents tab's badge: its rows less the ones that are not the tab's to announce (`TabBadges.place`): the
    /// muted agents and spaces, and a quiet task's agents, which the main conversation tells about.
    static func badge(_ rows: [Row], place: (SessionKey) -> TabBadges.Place) -> Int {
        rows.count { place($0.key) == .agents }
    }
}

/// A session that waits for the user with no card read (a prompt only its terminal shows), in the Needs You section:
/// a card's header, the session's place, its last line; a tap opens the session, whose terminal answers it.
struct NeedsYouWaitingCard: View {
    var session: Session?
    var machine: String?
    /// The list's cell is the card: no chrome of its own (as `InteractionPreviewCard`'s `inCell`).
    var inCell = true
    var open: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            HStack(spacing: Theme.Space.s) {
                ThemedIcon(InteractionKind.screen.symbol)
                    .foregroundStyle(StatusPalette.needsYou.dot)
                    .scaledFrame(width: Theme.Size.icon) // the cards' icon column
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(InteractionKind.screen.title).font(Theme.Font.heading).lineLimit(1)
                    Text(NeedsYouWaitingCard.place(session, machine: machine))
                        .font(Theme.Font.label)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
                #if !os(macOS)
                ThemedIcon(Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                #endif
            }
            if let line = session?.lastText?.trimmingCharacters(in: .whitespacesAndNewlines), !line.isEmpty {
                Text(line)
                    .font(Theme.Font.body).lineHeight(.body)
                    .lineLimit(3)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .modifier(UnderTitle())
            }
        }
        .modifier(Chrome(on: !inCell))
        .contentShape(.rect)
        .onTapGesture(perform: open)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isButton)
    }

    /// The session's name and its computer, as a card's place line reads.
    static func place(_ session: Session?, machine: String?) -> String {
        [session?.displayTitle, machine].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// The Mac: under the title, after the icon's column, as the cards' lines sit (`InteractionPreviewCard`).
    private struct UnderTitle: ViewModifier {
        func body(content: Content) -> some View {
            #if os(macOS)
            HStack(spacing: Theme.Space.s) {
                Spacer(minLength: 0).scaledFrame(width: Theme.Size.icon)
                content
            }
            #else
            content
            #endif
        }
    }

    private struct Chrome: ViewModifier {
        var on: Bool
        func body(content: Content) -> some View {
            if on { content.card() } else { content.padding(.vertical, Theme.Space.xs) }
        }
    }
}

/// Compact List: a session that waits in its terminal, as one row like a card's (`CompactInteractionRow`).
struct CompactWaitingRow: View {
    var session: Session?
    var machine: String?
    var since: Date?

    var body: some View {
        HStack(spacing: CompactList.gap) {
            ThemedIcon(InteractionKind.screen.symbol)
                .font(CompactList.metaStrong)
                .foregroundStyle(StatusPalette.needsYou.dot)
                .scaledFrame(width: CompactList.mark)
            HStack(alignment: .firstTextBaseline, spacing: CompactList.gap) {
                Text(InteractionKind.screen.title).font(CompactList.titleUnread).lineLimit(1).fixedSize()
                Text(NeedsYouWaitingCard.place(session, machine: machine))
                    .font(CompactList.meta).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: Theme.Space.xs)
            VisibleTimelineView(interval: 30) { now in
                Text(Fmt.relative(since, now: now)).font(CompactList.meta.monospacedDigit())
                    .foregroundStyle(.secondary).lineLimit(1)
            }
            .fixedSize()
        }
        .accessibilityElement(children: .combine)
    }
}

import SwiftUI

// A hand-off conversation's write-back as a card in the chat (#5932, docs/assistant/inbox.md "Item actions"): the
// item's summary, the reply or conclusion, and Send / Got It, which confirm the item's suggestion. The item is the one
// home of the request: the card reads it for whether it can still be sent and how it ended, and is not counted in
// Needs you. Mirrors web/src/views/assistant/HandoffCard.tsx.

struct HandoffCardView: View {
    var item: FeedItem
    var card: HandoffCardBody
    var itemID: String
    @State private var editing = false
    @State private var text = ""
    @State private var openRecap = false
    /// The item read by id when neither the list nor the history has it (an older, done one).
    @State private var fetched: InboxItem?
    @State private var model: InboxSuggestionModel?
    @Environment(\.chatPin) private var pin
    private var inbox: InboxStore { .shared }
    private var tasks: TasksStore { .shared }
    private var pinKey: String { "handoff-card-" + item.id }

    private var inboxItem: InboxItem? {
        inbox.items.first { $0.id == itemID } ?? inbox.history.first { $0.id == itemID } ?? fetched
    }

    /// No later card about the item in this conversation: only the newest says how the item ended.
    private var newest: Bool {
        let last = tasks.feeds[item.conversation]?.last { $0.kind == .inboxSuggestion && $0.ref == itemID }
        return last == nil || last?.id == item.id
    }

    var body: some View {
        let state = HandoffCardState.of(card, conversation: item.conversation, item: inboxItem, newest: newest)
        if state == .live && tasks.isLater(item) {
            laterLine
        } else {
            full(state)
        }
    }

    private func full(_ state: HandoffCardState) -> some View {
        let resolved = state != .live && state != .unknown
        return VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack(spacing: Theme.Space.s) {
                ThemedIcon(card.source == "mail" ? Theme.Icon.mail : Theme.Icon.inbox)
                    .font(Theme.Font.labelStrong)
                    .foregroundStyle(Theme.loop)
                Text([card.title, sourceName].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(Theme.Font.captionStrong).foregroundStyle(.secondary).lineLimit(1)
            }
            if let recap = card.recap, !recap.isEmpty {
                // Unfolding keeps the card's top where it is in the chat (DESIGN_SYSTEM.md 4.7.1), as Edit does.
                Button { toggleInPlace(pin, pinKey, animation: Theme.Motion.state, closing: openRecap) { openRecap.toggle() } } label: {
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text("Summary [Hand-Off Card]").font(Theme.Font.captionStrong).foregroundStyle(.secondary)
                        Text(recap).font(Theme.Font.body).lineLimit(openRecap ? nil : 3).multilineTextAlignment(.leading)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("handoffCard.recap")
            }
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Label(heading(state), systemImage: icon(state))
                    .font(Theme.Font.captionStrong)
                    .foregroundStyle(tint(state))
                    .lineLimit(1)
                if editing && state == .live {
                    TextField(String(localized: "Reply"), text: $text, axis: .vertical)
                        .font(Theme.Font.body)
                        .lineLimit(2...8)
                } else {
                    Text(shown(state))
                        .font(Theme.Font.body)
                        .foregroundStyle(resolved ? .secondary : .primary)
                }
            }
            .padding(Theme.Space.m)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(resolved && !sent(state) ? Theme.Fill.well : tint(state).opacity(0.12),
                        in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            .accessibilityIdentifier("handoffCard.suggestion")
            if let note = card.note, !note.isEmpty, !resolved {
                Text(note).font(Theme.Font.caption).foregroundStyle(.secondary)
            }
            if let error = model?.error ?? model?.notice {
                Text(error).font(Theme.Font.caption).foregroundStyle(model?.error != nil ? Theme.danger : .secondary)
            }
            if state == .live, let it = inboxItem, let s = it.suggestion {
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: Theme.Space.s) { buttons(it, s) }
                    VStack(spacing: Theme.Space.s) { buttons(it, s) }
                }
                .disabled(model?.busy != nil)
            }
        }
        .card(padding: Theme.Space.l)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("handoffCard")
        .animation(Theme.Motion.state, value: state)
        .chatPinned(pinKey)
        .task(id: itemID) { await fetchIfMissing() }
    }

    @ViewBuilder private func buttons(_ it: InboxItem, _ s: InboxItem.Suggestion) -> some View {
        Button {
            Task { await confirm(it, s) }
        } label: {
            Text(card.isReply ? String(localized: "Send") : String(localized: "Got It"))
                .lineLimit(1).fixedSize(horizontal: true, vertical: false).frame(maxWidth: .infinity)
        }
        .draftDecisionButton(prominent: true)
        .accessibilityIdentifier("handoffCard.confirm")
        if card.isReply {
            Button {
                if text.isEmpty { text = card.text }
                toggleInPlace(pin, pinKey, animation: Theme.Motion.state, closing: editing) { editing.toggle() }
            } label: {
                Text(editing ? String(localized: "Done [Task Views 9b8ee8]") : String(localized: "Edit [Task Views 928324]"))
                    .lineLimit(1).fixedSize(horizontal: true, vertical: false)
            }
            .draftDecisionButton()
            .accessibilityIdentifier("handoffCard.edit")
        }
        Button { withAnimation(Theme.Motion.state) { tasks.later(item.id) } } label: {
            Text("Later [Task Views 9d2ca2]").lineLimit(1).fixedSize(horizontal: true, vertical: false).frame(maxWidth: .infinity)
        }
        .draftDecisionButton()
        .accessibilityIdentifier("handoffCard.later")
    }

    /// Later: one line until it is opened again; the item waits in the Inbox meanwhile.
    private var laterLine: some View {
        Button { withAnimation(Theme.Motion.state) { tasks.unlater(item.id) } } label: {
            Label(String(localized: "Waiting in your Inbox: \(card.title)"), systemImage: Theme.Icon.inbox)
                .font(Theme.Font.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.xs)
                .background(Theme.Fill.well, in: .capsule)
        }
        .buttonStyle(.plain)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityIdentifier("handoffCard.later")
    }

    private func confirm(_ it: InboxItem, _ s: InboxItem.Suggestion) async {
        let m = model ?? InboxSuggestionModel.live(itemID, manager: it.manager)
        model = m
        let edited = editing && !text.isEmpty && text != card.text ? text : nil
        if case .done? = await m.confirm(s, text: edited) { editing = false }
    }

    private func fetchIfMissing() async {
        guard inbox.items.first(where: { $0.id == itemID }) == nil, inbox.history.first(where: { $0.id == itemID }) == nil,
              fetched == nil, let api = AppModel.shared.api else { return }
        fetched = try? await api.inboxItem(itemID)
    }

    /// The account it came in on (Feishu, the mail address), else the source's id.
    private var sourceName: String { card.account ?? card.source }

    private func sent(_ state: HandoffCardState) -> Bool {
        if case .sent = state { return true }
        return false
    }

    private func shown(_ state: HandoffCardState) -> String {
        if case .sent(let text) = state { return text }
        return editing && !text.isEmpty ? text : card.text
    }

    private func heading(_ state: HandoffCardState) -> String {
        switch state {
        case .sent: String(localized: "Replied as you")
        case .selfReplied: String(localized: "You replied at the source [Hand-Off Card]")
        case .filed: card.isReply ? String(localized: "Handled; this reply wasn't sent [Hand-Off Card]") : String(localized: "Conclusion · handled [Hand-Off Card]")
        case .replaced: String(localized: "No longer current; nothing was sent [Hand-Off Card]")
        case .live, .unknown: card.isReply ? String(localized: "Reply to \(card.title) as you") : String(localized: "Conclusion [Hand-Off Card]")
        }
    }

    private func icon(_ state: HandoffCardState) -> String {
        switch state {
        case .sent, .selfReplied: Theme.Icon.done
        case .filed, .replaced: Theme.Icon.handledElsewhere
        case .live, .unknown: card.isReply ? Theme.Icon.draftReply : Theme.Icon.concluded
        }
    }

    private func tint(_ state: HandoffCardState) -> Color {
        switch state {
        case .sent: Theme.positive
        case .selfReplied, .filed, .replaced: .secondary
        case .live, .unknown: Theme.accent
        }
    }
}

/// Over the main conversation's composer: "Wrap up: <title>" for each open item handed to it, which asks it for the
/// final summary and reply through the same hand-off (the item's id goes with it, #5932).
struct WrapUpChips: View {
    var key: SessionKey
    @State private var busy: String?
    @State private var error: String?
    private var inbox: InboxStore { .shared }

    var body: some View {
        let items = HandoffWrapUp.items(inbox.items, sessionID: key.sessionID)
        if !items.isEmpty {
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                ForEach(items) { it in
                    Button { Task { await wrapUp(it) } } label: {
                        Label(String(localized: "Wrap up: \(it.title)"), systemImage: busy == it.id ? Theme.Icon.time : Theme.Icon.inbox)
                            .font(Theme.Font.captionStrong)
                            .foregroundStyle(Theme.accent)
                            .lineLimit(1)
                            .padding(.horizontal, Theme.Space.m)
                            .padding(.vertical, Theme.Space.xs)
                            .background(Theme.accent.opacity(0.14), in: .capsule)
                    }
                    .buttonStyle(.plain)
                    .disabled(busy != nil)
                    .accessibilityIdentifier("wrapUp.chip")
                }
                if let error {
                    Text(error).font(Theme.Font.caption).foregroundStyle(Theme.danger).lineLimit(2)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .transition(.move(edge: .bottom).combined(with: .opacity))
        }
    }

    private func wrapUp(_ it: InboxItem) async {
        guard busy == nil, let api = AppModel.shared.api?.manager(AppModel.shared.itemManager(it.manager)) else { return }
        busy = it.id
        error = nil
        defer { busy = nil }
        do {
            _ = try await api.inboxHandoff(it.id, text: String(localized: "Let's wrap this up: write the final summary and reply back to this Inbox item."), clientID: UUID())
        } catch {
            self.error = String(localized: "Couldn't ask to wrap up: \(error.localizedDescription)")
        }
    }
}

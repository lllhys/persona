import SwiftUI

/// The answers to suggested rules (#5802; docs/assistant/unified-entry.md "A rule after the same answer three times"):
/// Always and Not Now on the line under a plan card, a to-do's decision or a question a supervisor passed up. What each
/// tap came to is kept here and not in the card, so a card drawn again still says it, and a suggestion answered on one
/// card is gone from every other card that carries it until the hub's own copies catch up.
@MainActor @Observable final class RuleSuggestions {
    /// The card a suggestion is drawn under: where its result stays, and whose manager the answer goes to.
    enum Card: Hashable, Sendable {
        case plan(id: String, conversation: String)
        /// `round`: the decision's `asked_at`. A to-do's next decision is another card, so what Always made on one
        /// decision is never drawn under the next.
        case todo(String, round: String)
        /// `manager`: the one that reported the card (`Interaction.manager`).
        case interaction(uid: String, manager: String?)

        var key: String {
            switch self {
            case .plan(let id, _): "plan|" + id
            case .todo(let id, let round): "todo|" + id + "|" + round
            case .interaction(let uid, _): "card|" + uid
            }
        }
    }

    enum Answer: Hashable, Sendable { case always, notNow }

    /// What the line under a card shows.
    enum Line: Equatable, Sendable {
        case offered(RuleSuggestion)
        /// A request is out: another tap does nothing.
        case sending(RuleSuggestion, Answer)
        /// Always was taken: what was made and where it is listed and removed.
        case made(String)
    }

    /// What Always made on a card: the words, and the suggestion they answer.
    struct Made: Equatable, Sendable {
        var suggestion: String
        var text: String
    }

    static let shared = RuleSuggestions()

    /// How long an answered suggestion stays off the cards whatever their copy says: a plan and a card keep the copy
    /// they were read with until they are read again. The hub itself keeps it quiet for 30 days.
    static let quietFor: TimeInterval = 3600

    private(set) var sending: [String: Answer] = [:]
    /// When each suggestion (by group and id) was answered here, or found gone.
    private(set) var settled: [String: Date] = [:]
    /// What Always made, by group and card, and the suggestion it answered.
    private(set) var made: [String: Made] = [:]
    /// What Always would create, worded once per suggestion (`RuleSuggestion.createsText`), not on every redraw.
    @ObservationIgnored private var details: [RuleSuggestion: String] = [:]
    @ObservationIgnored private let api: (String) -> APIClient?
    @ObservationIgnored private let failed: (String) -> Void
    /// After an answer or a stale suggestion (its id): the hub's copies changed, so the app's copies drop it and what
    /// the card was read from is read again.
    @ObservationIgnored private let changed: (Card, String) -> Void
    /// The manager a to-do's requests name (`TodoStore.named`) and a conversation's (`AppModel.feedManager`).
    @ObservationIgnored private let named: (String) -> String?
    @ObservationIgnored private let feedManager: (String, String) -> String?
    @ObservationIgnored private let now: () -> Date

    init(api: @escaping (String) -> APIClient? = { AppModel.shared.api($0) },
         failed: @escaping (String) -> Void = { AppModel.shared.toast = Toast($0, style: .error) },
         changed: @escaping (Card, String) -> Void = { RuleSuggestions.reread($0, answered: $1) },
         named: @escaping (String) -> String? = { TodoStore.shared.named($0) },
         feedManager: @escaping (String, String) -> String? = { AppModel.shared.feedManager(of: $0, group: $1) },
         now: @escaping () -> Date = { .now }) {
        self.api = api
        self.failed = failed
        self.changed = changed
        self.named = named
        self.feedManager = feedManager
        self.now = now
    }

    /// The live `changed`. The hub words a to-do's decision each time the to-dos are read and sends a pending card
    /// again without the line, but it sends no event for a plan, and a feed read only asks for what came since the
    /// newest item: the plans the app holds would keep their copy, and the line would come back once `quietFor` is
    /// over. So the store drops the answered suggestion from its plans itself (`TasksStore.ruleSuggestionSettled`).
    static func reread(_ card: Card, answered suggestion: String, tasks: TasksStore = .shared) {
        tasks.ruleSuggestionSettled(suggestion)
        switch card {
        case .plan(_, let conversation): tasks.load(conversation)
        case .todo: tasks.reloadTodos()
        case .interaction: break // the hub sends the card again without the line
        }
    }

    private static func key(_ suggestion: RuleSuggestion, _ group: String) -> String { group + "|" + suggestion.id }
    private static func key(_ card: Card, _ group: String) -> String { group + "|" + card.key }

    /// The line under `card`: what Always made there, else the suggestion it carries while it is still to answer.
    /// Nil: nothing is drawn (no suggestion, one this build can't describe, or one answered already).
    func line(_ suggestion: RuleSuggestion?, card: Card, group: String) -> Line? {
        let result = made[Self.key(card, group)]
        guard let suggestion, suggestion.offered else { return result.map { Line.made($0.text) } }
        let key = Self.key(suggestion, group)
        // The result stays while the card carries nothing else to answer: its own suggestion (the hub's copy has not
        // caught up) or one answered a moment ago. Another suggestion on the same card is offered in its place.
        let answered = quiet(key)
        if let result, result.suggestion == suggestion.id || answered { return .made(result.text) }
        if answered { return nil }
        if let answer = sending[key] { return .sending(suggestion, answer) }
        return .offered(suggestion)
    }

    /// Answered here, or found gone, a little while ago.
    private func quiet(_ key: String) -> Bool {
        guard let at = settled[key] else { return false }
        return now().timeIntervalSince(at) < Self.quietFor
    }

    /// The scope and what Always creates, in words.
    func detail(_ suggestion: RuleSuggestion) -> String {
        if let known = details[suggestion] { return known }
        if details.count >= 32 { details.removeAll() }
        let text = suggestion.createsText
        details[suggestion] = text
        return text
    }

    /// Always: the hub counts again and makes the rule only if the answers still earn it.
    func always(_ suggestion: RuleSuggestion, card: Card, group: String) async {
        await send(.always, suggestion, card: card, group: group)
    }

    /// Not Now: quiet for this kind and scope for 30 days.
    func notNow(_ suggestion: RuleSuggestion, card: Card, group: String) async {
        await send(.notNow, suggestion, card: card, group: group)
    }

    private func manager(of card: Card, group: String) -> String? {
        switch card {
        case .plan(_, let conversation): feedManager(conversation, group)
        case .todo(let id, _): named(id)
        case .interaction(_, let manager): manager
        }
    }

    private func send(_ answer: Answer, _ suggestion: RuleSuggestion, card: Card, group: String) async {
        let key = Self.key(suggestion, group)
        guard suggestion.offered, sending[key] == nil, !quiet(key) else { return }
        guard let api = api(group)?.manager(manager(of: card, group: group)) else {
            failed(String(localized: "Not connected to a hub [Todo Store 04f822]"))
            return
        }
        sending[key] = answer
        defer { sending[key] = nil }
        do {
            switch answer {
            case .always:
                let result = try await api.ruleSuggestionAlways(suggestion.id)
                made[Self.key(card, group)] = Made(suggestion: suggestion.id, text: result.text(asked: suggestion))
            case .notNow:
                try await api.ruleSuggestionNotNow(suggestion.id)
            }
            settle(key, card, suggestion.id)
        } catch let e as APIError where e.status == 404 {
            // Stale: the answers don't earn it any more, or it was answered elsewhere. The line goes.
            settle(key, card, suggestion.id)
        } catch let e as APIError where e.status == 409 {
            failed(String(localized: "The standing authorizations changed meanwhile. Try again."))
        } catch {
            failed(String(localized: "Couldn't save your answer: \(error.localizedDescription)"))
        }
    }

    private func settle(_ key: String, _ card: Card, _ suggestion: String) {
        let at = now()
        settled = settled.filter { at.timeIntervalSince($0.value) < Self.quietFor }
        settled[key] = at
        changed(card, suggestion)
    }
}

/// The quiet line under a card that carries a suggested rule: what the hub noticed, what Always would create and for
/// what, Always and Not Now; after Always, what was made and where it is listed and removed. Nothing without a suggestion.
struct RuleSuggestionLine: View {
    var suggestion: RuleSuggestion?
    var card: RuleSuggestions.Card
    var group: String
    /// Under a card's own surface: inset to its content's edge.
    var inset = false
    /// The line's height while it is drawn (0 otherwise), for a card that has to leave it room.
    var height: Binding<CGFloat>? = nil
    private var model: RuleSuggestions { .shared }

    var body: some View {
        switch model.line(suggestion, card: card, group: group) {
        case .offered(let rule)?: framed(offer(rule, sending: nil))
        case .sending(let rule, let sent)?: framed(offer(rule, sending: sent))
        case .made(let text)?: framed(RuleSuggestionMadeNote(text: text))
        case nil: EmptyView()
        }
    }

    private func offer(_ rule: RuleSuggestion, sending: RuleSuggestions.Answer?) -> RuleSuggestionOffer {
        RuleSuggestionOffer(rule: rule, detail: model.detail(rule), sending: sending) { which in send(which, rule) }
    }

    private func framed(_ content: some View) -> some View {
        content
            .padding(.top, Theme.Space.xs)
            .padding(.horizontal, inset ? Theme.Space.l : Theme.Space.flush)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height?.wrappedValue = $0 }
            .onDisappear { height?.wrappedValue = 0 }
    }

    private func send(_ which: RuleSuggestions.Answer, _ rule: RuleSuggestion) {
        Task {
            switch which {
            case .always: await model.always(rule, card: card, group: group)
            case .notNow: await model.notNow(rule, card: card, group: group)
            }
        }
    }
}

/// The suggestion still to answer.
private struct RuleSuggestionOffer: View {
    var rule: RuleSuggestion
    var detail: String
    var sending: RuleSuggestions.Answer?
    var answer: (RuleSuggestions.Answer) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                ThemedIcon(Theme.Icon.taskTrust).font(Theme.Font.caption).foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(rule.text)
                    if !detail.isEmpty { Text(detail) }
                }
                .font(Theme.Font.caption)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            HStack(spacing: Theme.Space.s) {
                Spacer(minLength: 0)
                button(.notNow, title: String(localized: "Not Now [Rule Suggestion]"), id: "rule-suggestion.not-now")
                button(.always, title: String(localized: "Always [Rule Suggestion]"), id: "rule-suggestion.always")
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("rule-suggestion")
    }

    private func button(_ which: RuleSuggestions.Answer, title: String, id: String) -> some View {
        let busy = sending == which
        return Button {
            answer(which)
        } label: {
            Text(title).lineLimit(1).opacity(busy ? 0 : 1)
                .overlay { if busy { ProgressView().controlSize(.small) } }
        }
        .font(Theme.Font.captionStrong)
        .themedButton()
        .disabled(sending != nil)
        .accessibilityIdentifier(id)
    }
}

/// What Always made, in the suggestion's place.
private struct RuleSuggestionMadeNote: View {
    var text: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
            ThemedIcon(Theme.Icon.successFilled).font(Theme.Font.caption).foregroundStyle(Theme.positive)
            Text(text)
                .font(Theme.Font.caption)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("rule-suggestion.made")
    }
}

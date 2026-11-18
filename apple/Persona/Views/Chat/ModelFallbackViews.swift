import SwiftUI

/// What an upstream refusal and its fallback say (#2528, #4489), as the web (`chat-modelfallback`): the refusal's own line
/// in the transcript, and the card above the composer while the session still runs on the fallback model. Models are
/// named by their short names (`SessionModes.modelName`; the raw id when it has none).
enum ModelFallbackText {
    /// `Session.turnErrorCode` when the turn failed because the fallback model is missing: Continue would go to it again.
    static let unavailableCode = "model_fallback_unavailable"

    static func model(_ id: String?) -> String { SessionModes.modelName(id) ?? id ?? "?" }

    // MARK: The refusal's line

    static var refusalTitle: String { String(localized: "The upstream refused this request") }
    static var refusalTextLabel: String { String(localized: "Refusal text") }

    /// The facts under the title, in the web's order: the switch (or that none is recorded), later turns, category,
    /// explanation. The refusal's words go apart (`ModelRefusal.text`).
    static func refusalFacts(_ r: ModelRefusal) -> [String] {
        var out: [String] = []
        if r.isFallback {
            let from = model(r.originalModel), to = model(r.fallbackModel)
            out.append(String(localized: "Claude Code switched from \(from) to \(to) on its own"))
            if r.scope == "session" { out.append(String(localized: "Later turns of this session may keep using it")) }
        } else {
            out.append(String(localized: "The record doesn't say another model was used"))
        }
        if let c = r.category { out.append(String(localized: "Category: \(c)")) }
        if let e = r.explanation { out.append(String(localized: "Explanation: \(e)")) }
        return out
    }

    /// A Persona conversation's one sentence for the refusal (#5202): the switch, by the model's short name, or only
    /// that the upstream refused. The node's own recovery adds the cause in plain words (#7824); a runtime-written
    /// record without one of the known categories keeps the earlier sentence untouched. The rest goes under Details.
    static func plainRefusal(_ r: ModelRefusal) -> String {
        guard r.isFallback else { return refusalTitle }
        let to = model(r.fallbackModel)
        if let cause = r.recoveryCategory?.phrase {
            return String(localized: "The upstream refused this request, so it switched to \(to) (\(cause)).")
        }
        return String(localized: "The upstream refused this request, so it switched to \(to).")
    }

    /// The short line while the configured model answered a probe: the next request runs on it again (#7824).
    static func returningLine(_ r: ModelRefusal) -> String? {
        guard r.recoveryReturning, let o = r.originalModel, !o.isEmpty else { return nil }
        return String(localized: "\(model(o)) is available again; your next message switches back")
    }

    /// A Persona conversation's card title while on the fallback model (#5202), naming the model that answers now. The
    /// node's own recovery (#7824) says the switch with its cause, the same sentence as the refusal's line; a
    /// runtime-written record keeps the earlier title.
    static func plainCard(_ s: Session) -> String? {
        guard let f = s.modelFallback, f.isFallback else { return nil }
        if f.recoveryCategory != nil { return plainRefusal(f) }
        let m = model(f.fallbackModel)
        return String(localized: "After an upstream refusal, this conversation answers with \(m).")
    }

    /// The same word as `ReadinessCard`'s Details.
    static var detailsLabel: String { String(localized: "Details [Readiness Card 979a33]") }

    /// The refusal's words when it has any (shown as they are).
    static func words(_ r: ModelRefusal) -> String? {
        guard let t = r.text, !t.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        return t
    }

    // MARK: The card above the composer

    struct Card: Equatable {
        var title: String
        /// Shown under the title; in a Persona conversation only the configured model while going back is pending.
        var lines: [String]
        /// A Persona conversation's record, folded under Details (#5202); empty for a terminal session.
        var details: [String] = []
        /// Back to configured model is offered: a Persona conversation not already going back.
        var offersRestore: Bool
    }

    /// Nil unless the session still runs on a fallback model. `headless`: a Persona conversation, which can go back from
    /// here; a terminal session gets only the /model step.
    static func card(_ s: Session, headless: Bool) -> Card? {
        guard let f = s.modelFallback, f.isFallback else { return nil }
        let pending = s.modelRestore?.pending == true
        var lines: [String] = []
        if let o = f.originalModel { let m = model(o); lines.append(String(localized: "Originally requested model: \(m)")) }
        let fallback = model(f.fallbackModel)
        lines.append(String(localized: "Fallback model: \(fallback)"))
        if pending, let to = s.modelRestore?.to { let m = model(to); lines.append(String(localized: "Configured model: \(m)")) }
        if let e = s.turnError, !e.isEmpty { lines.append(String(localized: "Error since: \(e)")) }
        if pending { lines.append(String(localized: "Which model actually runs is only known from this session's next reply.")) }
        lines.append(headless
            ? String(localized: "No message is resent; the same request may be refused again.")
            : String(localized: "For a terminal session, type /model in Claude Code yourself; it may also change the default model for new sessions."))
        // A successful probe says the next request runs on the configured model again (#7824): one visible line.
        let returning = returningLine(f)
        if let returning { lines.append(returning) }
        let pendingTitle = String(localized: "The next start will use the configured model")
        guard headless else {
            return Card(title: pending ? pendingTitle : String(localized: "After an upstream refusal, this session switched to a fallback model"),
                        lines: lines, offersRestore: false)
        }
        // A Persona conversation is for everyday people (#5202): one sentence, the configured model while pending,
        // and the record under Details.
        let configured = pending ? s.modelRestore?.to.map { m in String(localized: "Configured model: \(model(m))") } : nil
        var visible = configured.map { [$0] } ?? []
        if let returning { visible.append(returning) }
        return Card(title: pending ? pendingTitle : plainCard(s) ?? pendingTitle, lines: visible,
                    details: lines.filter { $0 != configured && $0 != returning }, offersRestore: !pending)
    }

    /// The words an exhausted recovery chain adds to the last API error's card (#7824). Nil for any other session,
    /// including a runtime-written fallback record, so only the node's own exhausted marker changes the failure card.
    struct ExhaustedRecovery: Equatable {
        /// The cause in plain words, as a full line on the card.
        var causeLine: String
        /// When the configured model is available again, in the user's time zone; nil when the upstream named none.
        var resetLine: String?
        /// The models tried, shown only inside the folded details.
        var triedLine: String?
    }

    static func exhaustedRecovery(_ s: Session?, now: Date = Date(), calendar: Calendar = .current) -> ExhaustedRecovery? {
        guard let f = s?.modelFallback, f.recoveryExhausted, let cause = f.recoveryCategory?.phrase else { return nil }
        return ExhaustedRecovery(
            causeLine: String(localized: "Every model tried failed: \(cause)."),
            resetLine: f.recoveryReset.map {
                String(localized: "Available again: \(Session.usageLimitClock($0, now: now, calendar: calendar))")
            },
            triedLine: f.recoveryTried.map { String(localized: "Models tried: \($0)") }) // decoding drops an empty one
    }

    /// Whether the last API error's card offers Retry, before the chat's own checks (the last line, nothing working):
    /// the usual transient rule, or an exhausted recovery chain on a session that can send (#7824), whose Retry runs the
    /// request again even when the error text alone calls it final (a safeguards refusal, for example).
    static func offersRetry(transient: Bool?, session: Session?) -> Bool {
        transient != false || (session?.modelFallback?.recoveryExhausted == true && session?.canSend == true)
    }

    /// A refused going back, in words: the codes the hub documents, else its own words.
    static func restoreError(_ error: Error) -> String {
        switch (error as? APIError)?.code ?? "" {
        case "restore_busy": String(localized: "This conversation is working, so nothing was closed. Try again once it stops.")
        case "restore_unsupported": String(localized: "This session can't go back to the configured model from here. Use /model in its terminal.")
        case "restore_nothing": String(localized: "This session no longer runs on a fallback model.")
        case "restore_unknown": String(localized: "Persona can't tell right now which model this session uses or is configured with, so nothing was closed. Try again later.")
        case "node_outdated": String(localized: "personad on this computer is too old to go back to the configured model. Update it.")
        default: error.shown ?? String(localized: "Try again later.")
        }
    }
}

extension AppModel {
    /// Back to configured model on a Persona conversation's fallback card; a refusal is said in a toast. True when the
    /// hub took it (the card then waits for the session to say the next start is pending).
    func restoreConfiguredModel(_ key: SessionKey) async -> Bool {
        guard let api = api(key.group) else { return false }
        Haptics.tap()
        do {
            try await api.restoreModel(key)
            return true
        } catch {
            Haptics.error()
            toast = Toast(ModelFallbackText.restoreError(error), style: .error)
            return false
        }
    }
}

/// An upstream refusal in the transcript (#4489): a line of its own after the exchange, never folded into the work row.
/// What happened (the switch, or that none is recorded), then the refusal's words as they are. In a Persona
/// conversation (`plainRefusals`, #5202) one sentence with the model's short name, the record under a closed Details.
struct RefusalNotice: View {
    var refusal: ModelRefusal
    @Environment(\.plainRefusals) private var plain
    @State private var open = false

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.s) {
            ThemedIcon(Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.caution)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                if plain {
                    Text(ModelFallbackText.plainRefusal(refusal)).font(Theme.Font.labelStrong).foregroundStyle(Theme.cautionText)
                    DisclosureGroup(isExpanded: $open) {
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) { record }
                            .frame(maxWidth: .infinity, alignment: .leading)
                    } label: {
                        Text(ModelFallbackText.detailsLabel).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    }
                    .accessibilityIdentifier("refusal-details")
                } else {
                    Text(ModelFallbackText.refusalTitle).font(Theme.Font.labelStrong).foregroundStyle(Theme.cautionText)
                    record
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .accessibilityElement(children: plain ? .contain : .combine)
        .accessibilityIdentifier("refusal-notice")
        .contextMenu { CopyButton(text: ModelFallbackText.words(refusal) ?? refusal.publicText) }
    }

    /// The record: the switch or that none is recorded, later turns, category, explanation, then the words as they are.
    @ViewBuilder private var record: some View {
        ForEach(ModelFallbackText.refusalFacts(refusal), id: \.self) { line in
            Text(line).font(Theme.Font.label).foregroundStyle(.secondary)
        }
        if let words = ModelFallbackText.words(refusal) {
            Text(ModelFallbackText.refusalTextLabel).font(Theme.Font.label).foregroundStyle(.tertiary)
                .padding(.top, Theme.Space.xxs)
            Text(words).font(Theme.Font.label).foregroundStyle(.secondary).textSelection(.enabled)
                .accessibilityIdentifier("refusal-text")
        }
    }
}

extension EnvironmentValues {
    /// The chat is a Persona conversation, for everyday people: a refusal says one plain sentence (#5202).
    @Entry var plainRefusals = false
}

/// Over the composer while the session still runs on a fallback model (#4489): which models, the error since, and what
/// can be done. A terminal session: only the /model step. A Persona conversation: Back to configured model, and New Main
/// Chat on the main conversation where the hub has it. While going back is pending: only that the next start uses the
/// configured model, and which; nothing more is claimed.
struct ModelFallbackCard: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var session: Session
    /// The conversation is answering: going back waits until it stops (the hub would refuse, `restore_busy`).
    var working: Bool
    @State private var busy = false

    var body: some View {
        if let card = ModelFallbackText.card(session, headless: session.assistant == true) {
            let restore = card.offersRestore && !working && !busy
            let newMain = session.assistant == true && model.canStartNewMain(key)
            ReadinessCard(title: card.title, detail: card.lines.joined(separator: "\n"), tone: .caution,
                          actionTitle: restore ? String(localized: "Switch Back [Model Fallback Card]") : nil,
                          action: restore ? { go() } : nil,
                          altTitle: newMain ? String(localized: "New Main Chat [Model Fallback Card]") : nil,
                          alt: newMain ? { model.newMainAsked = true } : nil,
                          details: card.details,
                          busy: busy)
                .accessibilityIdentifier("model-fallback-card")
        }
    }

    private func go() {
        busy = true
        Task {
            _ = await model.restoreConfiguredModel(key)
            busy = false
        }
    }
}

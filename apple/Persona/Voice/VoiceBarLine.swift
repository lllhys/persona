import Foundation

// What the voice bar says (docs/assistant/voice.md 6.2): the first line's state beside the orb, and the second line about the
// bound conversation, with the buttons that go with it. Pure: the bar only draws them, the tests hold them to the cases
// (docs/assistant/voice.md 6.10「VoiceBarLine 的各种相位」).

/// The voice bar's second line: what the bound conversation does, what needs the user, or why the last instruction did
/// not go, and what can be done about it. Every button is a single line; the bar puts the ones that do not fit in ⋯.
nonisolated struct VoiceBarLine: Equatable, Sendable {
    /// What the line is about. VoiceOver hears a change of it, not of its words (the time goes on); the bar picks its
    /// mark and color by it.
    enum Kind: Equatable, Sendable {
        /// Nothing going on: what is said goes to the conversation.
        case ready
        case muted
        /// An instruction on its way, or the conversation being started for it.
        case handing
        case working
        /// The conversation waits on something outside: a deploy, CI, another agent.
        case waiting
        /// The voice's upstream is paused while the conversation works (it wakes up by itself).
        case paused
        /// A card wants a tap: a plan, a permission, a terminal dialog, a question the voice is not asking.
        case needsTap
        /// The voice is asking a question card's question: what the user says is the answer.
        case asking
        /// The last instruction did not go (refused, failed, or not known to have gone).
        case notSent
        /// The computer is offline: nothing said can go.
        case offline
        /// A system call to Persona on hold (#9290): nothing said goes and nothing plays until it is off hold.
        case held

        /// Worth saying to VoiceOver as it comes (docs/assistant/voice.md 6.6「无障碍」): it asks for the user, or the voice stops.
        var announced: Bool {
            switch self {
            case .needsTap, .asking, .notSent, .offline, .paused, .held: true
            case .ready, .muted, .handing, .working, .waiting: false
            }
        }
    }

    enum Action: Equatable, Hashable, Sendable, Identifiable {
        /// Stop Claude: interrupts the agent (`POST …/interrupt`), never the voice.
        case stopAgent
        /// Resend: the refused instruction again, under its id.
        case resend(run: String)
        /// Clear and Send: the computer's unsent line cleared first (`run.clearable`), then the instruction.
        case clearAndSend(run: String)
        /// Send Anyway: an instruction the user-speech gate held back (`unprompted`: no fresh request was heard) sent
        /// under its id, the user's tap their confirmation (#5904).
        case sendAnyway(run: String)
        /// Send Back as Feedback: the plan card answered with the refused words as its feedback (the app's own resolve).
        case returnPlan(card: String, feedback: String, run: String)
        /// Never Mind: the line about it goes away.
        case dismiss(run: String)

        var id: String {
            switch self {
            case .stopAgent: "stop"
            case .resend(let run): "resend-" + run
            case .clearAndSend(let run): "clear-" + run
            case .sendAnyway(let run): "anyway-" + run
            case .returnPlan(let card, _, let run): "plan-" + card + "-" + run
            case .dismiss(let run): "dismiss-" + run
            }
        }

        /// What the plan card is answered with for Send Back as Feedback: sent back, the words as its feedback (docs/assistant/voice.md
        /// 1.2 I.4; the app's own resolve, with this device's token: decision 5 holds, the user tapped it).
        var resolution: Resolution? {
            guard case .returnPlan(_, let feedback, _) = self else { return nil }
            return Resolution(approve: false, feedback: feedback)
        }

        /// The accessibility identifier's tail (`voice-line-<name>`).
        var name: String {
            switch self {
            case .stopAgent: "stop"
            case .resend: "resend"
            case .clearAndSend: "clear"
            case .sendAnyway: "anyway"
            case .returnPlan: "plan"
            case .dismiss: "dismiss"
            }
        }
    }

    var kind: Kind
    var text: String
    /// Since when it has been so (the agent working): the bar adds how long.
    var since: Date?
    var actions: [Action]

    /// The second line now. `cards`: the bound conversation's pending ones; `label`: who works in it (「Claude」,
    /// 「Codex」, the persona's name for its own conversations); `dismissed`: refusals put away with Never Mind; `resendable`:
    /// the refused runs the connection up now still knows (`VoiceConversation.canResend`); `held`: a system call to
    /// Persona on hold (`VoiceConversation.held`).
    static func make(phase: VoicePhase, link: VoiceLink, runs: [VoiceRun], session: Session?, cards: [Interaction],
                     muted: Bool, held: Bool = false, asking: String? = nil, notice: VoiceFailure? = nil, label: String,
                     dismissed: Set<String> = [], resendable: Set<String> = []) -> VoiceBarLine {
        let live = runs.filter { !dismissed.contains($0.id) && $0.state != .folded }
        // What did not go, while it is the latest word on it: a later instruction that went takes its place.
        if let last = live.last, [.refused, .failed, .unknown].contains(last.state) {
            return notSent(last, cards: cards, label: label, resendable: resendable.contains(last.id))
        }
        if notice?.code == .machineOffline {
            return VoiceBarLine(kind: .offline, text: String(localized: "This computer is offline; what you say can’t be sent"), since: nil, actions: [])
        }
        let working = session?.status == .working || session?.state == .working
        if held {
            // Not the paused, listening, muted or asking line: speaking does nothing until the call is off hold. Stop Claude
            // still works.
            let stop: [Action] = working && session?.canInterrupt != false ? [.stopAgent] : []
            return VoiceBarLine(kind: .held, text: String(localized: "The call is on hold; nothing you say is sent"), since: nil, actions: stop)
        }
        if let asking, cards.contains(where: { $0.id == asking }) {
            return VoiceBarLine(kind: .asking, text: String(localized: "\(label) is asking you; just say your answer"), since: nil, actions: [])
        }
        if !cards.isEmpty {
            return VoiceBarLine(kind: .needsTap, text: String(localized: "Needs a tap from you"), since: nil, actions: [])
        }
        let open = live.filter(\.state.isOpen)
        let stop: [Action] = working && session?.canInterrupt != false ? [.stopAgent] : []
        let since = working ? session?.stateSince ?? open.compactMap(\.startedAt?.date).min()
            : open.compactMap(\.startedAt?.date).min()
        if link == .paused {
            // The first line says the voice is paused (Voice Paused): this one says what for, and that speaking wakes it.
            return VoiceBarLine(kind: .paused, text: String(localized: "Waiting for \(label) · Speak to resume"), since: nil, actions: stop)
        }
        if session?.starting == true {
            return VoiceBarLine(kind: .handing, text: String(localized: "Starting \(label)…"), since: nil, actions: [])
        }
        if !working, open.contains(where: { $0.state == .sending }) {
            return VoiceBarLine(kind: .handing, text: String(localized: "Handing it to \(label)…"), since: nil, actions: [])
        }
        if let work = session?.work, work.phase == "blocked", work.on != "user" {
            let line = work.line.flatMap { $0.isEmpty ? nil : $0 }
            return VoiceBarLine(kind: .waiting, text: line.map { String(localized: "\(label) is waiting: \($0)") } ?? String(localized: "\(label) is waiting on something else"),
                                since: nil, actions: stop)
        }
        if !working, open.contains(where: { $0.state == .blocked }) {
            return VoiceBarLine(kind: .waiting, text: String(localized: "\(label) is waiting on something else"), since: nil, actions: [])
        }
        if working || open.contains(where: { [.sent, .working, .needsYou].contains($0.state) }) {
            let activity = session?.activity.flatMap { $0.isEmpty ? nil : $0 }
            // Who works in it, then what it does now in the hub's own words (「Claude 在跑测试」).
            let text = activity.map { String(localized: "\(label) \($0)") }
            return VoiceBarLine(kind: .working, text: text ?? String(localized: "\(label) is working"),
                                since: since, actions: stop)
        }
        if open.contains(where: { $0.state == .held }) {
            return VoiceBarLine(kind: .working, text: String(localized: "\(label) answered; work continues in the background"), since: nil, actions: [])
        }
        if muted {
            return VoiceBarLine(kind: .muted, text: String(localized: "Muted; results and questions are still read aloud"), since: nil, actions: [])
        }
        return VoiceBarLine(kind: .ready, text: String(localized: "Just speak; it goes to \(label)"), since: nil, actions: [])
    }

    /// Why `run` did not go, in the user's words (docs/assistant/voice.md 1.3), and what can be done: send it again (when the
    /// connection up now still knows it), send anyway what the user-speech gate held back, clear the computer's line
    /// first, give it to the plan card as its feedback.
    private static func notSent(_ run: VoiceRun, cards: [Interaction], label: String, resendable: Bool) -> VoiceBarLine {
        let dismiss = Action.dismiss(run: run.id)
        func line(_ text: String, _ actions: [Action] = []) -> VoiceBarLine {
            VoiceBarLine(kind: .notSent, text: text, since: nil, actions: actions + [dismiss])
        }
        switch run.state {
        case .unknown:
            return line(String(localized: "Not sure it was sent; check the chat"))
        case .failed:
            return line(String(localized: "Couldn’t hand it to \(label)"))
        default:
            break
        }
        let resend: [Action] = resendable ? [.resend(run: run.id)] : []
        switch run.code {
        case "input_draft":
            let draft = (run.draft ?? run.clearable ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            let text = draft.isEmpty ? String(localized: "\(label)’s input box on the computer has unsent text")
                : String(localized: "\(label)’s input box on the computer has unsent text: “\(draft)”")
            return line(text, run.clearable != nil && resendable ? [.clearAndSend(run: run.id)] : [])
        case "dialog_open":
            if let plan = cards.last(where: { $0.kind == .plan }) {
                return line(String(localized: "\(label) is waiting on its plan; this wasn’t sent"),
                            [.returnPlan(card: plan.id, feedback: run.task, run: run.id)])
            }
            return line(String(localized: "\(label) is waiting for you to handle the card above"))
        case "unprompted":
            // The gate heard no fresh request (docs/assistant/voice.md 4.4): never worded as if someone else said it. What
            // was held back is quoted, as a draft is: Send Anyway confirms words the user can see (last, so a long one
            // only loses its end to the bar's line limit).
            let task = run.task.trimmingCharacters(in: .whitespacesAndNewlines)
            let text = task.isEmpty ? String(localized: "Didn’t hear you ask for this just now, so it wasn’t sent")
                : String(localized: "Didn’t hear you ask for this just now, so it wasn’t sent: “\(task)”")
            return line(text, resendable && !task.isEmpty ? [.sendAnyway(run: run.id)] : [])
        case "on_screen":
            return line(String(localized: "Approve it by tapping on screen"))
        case "empty":
            return line(String(localized: "Didn’t catch that; say it again"))
        case "open_elsewhere":
            return line(String(localized: "This chat is open elsewhere; it can’t be sent"))
        case "engine_missing":
            return line(String(localized: "\(label)’s engine isn’t on that computer; it can’t be sent"))
        case "runtime_away":
            return line(String(localized: "\(label) is offline right now; it can’t be sent"))
        default:
            return line(String(localized: "This wasn’t sent"), resend)
        }
    }
}

/// The first line beside the orb: the voice's own state, in a few words, and the orb's phase for it.
nonisolated struct VoiceBarStatus: Equatable, Sendable {
    var text: String
    var orb: VoiceOrb.Phase

    /// `working`: the bound conversation works (the orb's ring laps); `tool`: a read-only tool the voice runs; `held`: a
    /// system call to Persona on hold (the call screen's On Hold, whatever the connection does meanwhile).
    static func make(phase: VoicePhase, link: VoiceLink, muted: Bool, working: Bool, tool: String? = nil, held: Bool = false) -> VoiceBarStatus {
        if held, !phase.isEnded { return VoiceBarStatus(text: String(localized: "On Hold [Call Screen]"), orb: .paused) }
        switch phase {
        case .connecting:
            return VoiceBarStatus(text: String(localized: "Connecting… [Feishu Bot Model 8be0ea]"), orb: .connecting)
        case .reconnecting:
            return VoiceBarStatus(text: String(localized: "Reconnecting…"), orb: .connecting)
        case .ended:
            return VoiceBarStatus(text: String(localized: "Voice Ended"), orb: .ended)
        case .live(let state):
            switch link {
            case .paused: return VoiceBarStatus(text: String(localized: "Voice Paused"), orb: .paused)
            case .waking: return VoiceBarStatus(text: String(localized: "Resuming…"), orb: .connecting)
            case .reconnecting: return VoiceBarStatus(text: String(localized: "Reconnecting…"), orb: .connecting)
            case .live, .other: break
            }
            if state == .speaking { return VoiceBarStatus(text: String(localized: "Speaking"), orb: .speaking) }
            if muted { return VoiceBarStatus(text: String(localized: "Muted [Inbox Mutes View e5e468]"), orb: .muted) }
            if let tool = tool.flatMap({ $0.isEmpty ? nil : $0 }) {
                return VoiceBarStatus(text: String(localized: "\(tool)…"), orb: working ? .working : .listening)
            }
            if state == .hearing { return VoiceBarStatus(text: String(localized: "Hearing you"), orb: .hearing) }
            return VoiceBarStatus(text: String(localized: "Listening [Voice Bar Line 226686]"), orb: working ? .working : .listening)
        }
    }
}

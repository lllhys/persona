import Foundation
import Observation

/// The main conversation's readiness on this device (docs/assistant/onboarding.md 4.4, 5.1): asked when the Persona page shows,
/// after pairing, after a computer is added or confirmed, after a provider is saved, when the machines change; kept
/// from `warm` too. On a hub without `assistant_readiness` (or one answering 404) it stays nil and every page behaves
/// as before. Also holds the card of a start or a send the hub refused with a code (5.2).
@MainActor @Observable
final class AssistantReadinessModel {
    private(set) var readiness: AssistantReadiness?
    /// The hub answered 404: an older hub, the pages as before.
    private(set) var unsupported = false
    private(set) var probing = false
    /// The last Try a Message's result (`probe`), until the next ask without one.
    private(set) var probe: AssistantReadiness.Probe?
    /// A coded refusal of a start or a send: a card where the send was, not a toast.
    var failure: CodedFailure?

    @ObservationIgnored private var at = Date.distantPast
    @ObservationIgnored private var asked = 0
    @ObservationIgnored private var answered = 0
    @ObservationIgnored private var inflight: Task<Void, Never>?
    /// Moves on each `forget`: an answer, a probe's too, about the manager shown before doesn't land.
    @ObservationIgnored private var generation = 0

    /// How long an answer is good for before a visit asks again.
    static let fresh: TimeInterval = 5

    /// The readiness the pages draw: nil on an older hub and before the first answer.
    var current: AssistantReadiness? { unsupported ? nil : readiness }

    /// What holds the send button (nil: send as before).
    var blocking: AssistantReadiness.Step? { current?.blockingStep }

    /// An answer from elsewhere (`warm`): kept unless a later ask has answered.
    func land(_ r: AssistantReadiness) {
        readiness = r
        unsupported = false
        at = .now
    }

    /// Asks the hub. `force`: even with a fresh answer; `probe`: try one word on the main conversation (costs a little).
    /// A later ask's answer is never overwritten by an earlier one's; a probe's always lands (it alone has Try a Message's result).
    func refresh(api: APIClient?, force: Bool = false, probe: Bool = false) async {
        guard let api else { return }
        if !probe, !force, Date.now.timeIntervalSince(at) < Self.fresh { return }
        if !probe, !force, let inflight { await inflight.value; return }
        asked += 1
        let mine = asked
        let gen = generation
        if probe { probing = true }
        let task = Task { [weak self] in
            do {
                let r = try await api.assistantReadiness(probe: probe)
                guard let self, gen == self.generation, probe || mine > self.answered else { return }
                self.answered = max(self.answered, mine)
                self.land(r)
                if probe { self.probe = r.probe } else if r.probe == nil { self.probe = nil }
            } catch {
                // 404: an older hub. 503 assistant_offline and the rest: keep what was there, ask again next time.
                guard let self, gen == self.generation, (error as? APIError)?.status == 404, probe || mine > self.answered else { return }
                self.answered = max(self.answered, mine)
                self.unsupported = true
                self.readiness = nil
                self.at = .now
            }
        }
        if !probe { inflight = task }
        await task.value
        if probe, gen == generation { probing = false }
        if inflight == task { inflight = nil }
    }

    /// Later: the checklist stops coming up by itself; the card still shows when something is missing.
    func dismiss(api: APIClient?) async {
        guard let api else { return }
        try? await api.dismissAssistantReadiness()
        readiness?.dismissed = true
    }

    /// The Persona page shows another manager: what was read of the one before, and an answer about it still on its way,
    /// are not this one's.
    func forget() {
        generation += 1
        answered = asked
        inflight = nil
        probing = false
        reset()
    }

    /// For tests and screenshots.
    func reset(_ r: AssistantReadiness? = nil) {
        readiness = r
        unsupported = false
        probe = nil
        failure = nil
        at = r == nil ? .distantPast : .now
    }
}

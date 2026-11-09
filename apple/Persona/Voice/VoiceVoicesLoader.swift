import Foundation
import Observation

/// The voices of the voice extension's model as a picker shows them (docs/assistant/voice.md 5.7, #2411): the call's
/// ⋯ › Voice and the Voice row on the voice extension's settings page. Asked when a picker is shown, again a little
/// later after a failure (`retryDelays`), whenever a picker opens without a list (`opened`), and on Retry. A list the
/// hub marks stale (it could not read the service's just now and answered with the last good one) is shown as it is
/// and asked for again the next time a picker opens.
@MainActor @Observable final class VoiceVoicesLoader {
    enum State: Equatable {
        /// Not asked yet.
        case idle
        case loading
        case loaded(VoiceVoices)
        /// The last ask failed; another follows by itself while `retryDelays` lasts, and on Retry or the next open.
        case failed
    }

    /// The asks that follow a failure by themselves, this long after it.
    static let retryDelays: [Duration] = [.seconds(2), .seconds(5), .seconds(15)]
    /// A picker that opens asks again only this long after the last ask began: a menu drawn again as the state
    /// changes can never ask in a loop.
    static let reopenGap: TimeInterval = 2

    private(set) var state: State = .idle

    @ObservationIgnored let extensionID: String
    /// The model the list is for (nil: the extension's default).
    @ObservationIgnored private(set) var model: String?
    @ObservationIgnored private let api: () -> APIClient?
    @ObservationIgnored private let clock: any VoiceClock
    @ObservationIgnored private var shown = false
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var retries = 0
    @ObservationIgnored private var askedAt: Date?
    /// Bumped by every ask and by `stop`: an answer that comes back after it is dropped.
    @ObservationIgnored private var generation = 0

    init(extensionID: String, api: @escaping () -> APIClient?, clock: any VoiceClock = SystemVoiceClock()) {
        self.extensionID = extensionID
        self.api = api
        self.clock = clock
    }

    /// The list, once there is one (stale or not).
    var voices: VoiceVoices? {
        if case .loaded(let v) = state { v } else { nil }
    }

    /// The picker is shown for `model`: asks for its list unless it has it or is asking already.
    func show(model: String?) {
        if shown, model == self.model, state != .idle { return }
        if shown, model != self.model { state = .idle } // another model's list is not this one's
        shown = true
        self.model = model
        retries = 0
        ask()
    }

    /// The picker opened: asks at once when there is no list, or only a stale one, unless an ask began just now.
    func opened() {
        guard shown else { return }
        switch state {
        case .loading: return
        case .loaded(let v) where !v.stale: return
        default: break
        }
        if let askedAt, clock.now.timeIntervalSince(askedAt) < Self.reopenGap { return }
        retries = 0
        ask()
    }

    /// Retry, tapped.
    func retry() {
        guard shown else { return }
        retries = 0
        ask()
    }

    /// The picker went away: nothing more is asked until it is shown again (a list it has stays).
    func stop() {
        task?.cancel()
        task = nil
        generation += 1
        if voices == nil, state != .idle { state = .idle }
    }

    private func ask() {
        task?.cancel()
        generation += 1
        let generation = generation
        askedAt = clock.now
        // A stale list stays on screen while it is asked for again.
        if voices == nil, state != .loading { state = .loading }
        let api = api()
        let extensionID = extensionID
        let model = model
        task = Task { [weak self] in
            var answer: VoiceVoices?
            if let api { answer = try? await api.voiceVoices(extensionID, model: model) }
            guard !Task.isCancelled else { return }
            self?.finish(answer, generation: generation)
        }
    }

    private func finish(_ answer: VoiceVoices?, generation: Int) {
        guard generation == self.generation else { return }
        if let answer {
            retries = 0
            if state != .loaded(answer) { state = .loaded(answer) }
            return
        }
        // Asking again for a stale list failed: it stays.
        if voices != nil { return }
        if state != .failed { state = .failed }
        guard retries < Self.retryDelays.count else { return }
        let delay = Self.retryDelays[retries]
        retries += 1
        let clock = clock
        task = Task { [weak self] in
            try? await clock.sleep(for: delay)
            guard !Task.isCancelled, let self, generation == self.generation else { return }
            self.ask()
        }
    }
}

/// What a picker says about the voice now: its name in the list, else its id; the list's default when none was chosen.
nonisolated enum VoiceTimbreName {
    static func current(chosen: String?, in list: VoiceVoices?) -> String? {
        guard let id = chosen ?? list?.default, !id.isEmpty else { return nil }
        return list?.voices.first { $0.id == id }?.name ?? id
    }
}

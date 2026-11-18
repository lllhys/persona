#if os(iOS)
import Foundation

/// What runs a system call's conversation (#9290): the voice for a call to Persona, the urgent call's turns for an
/// urgent call. `PersonaCalls` drives it from the call's machine; it reports back through `events`.
@MainActor protocol CallEngine: AnyObject {
    /// What the engine tells `PersonaCalls`.
    var events: (@MainActor (CallEngineEvent) -> Void)? { get set }
    /// The call started (calling Persona: the voice starts) or was answered (an urgent call).
    func begin()
    /// The system activated the call's audio session: the audio starts (only now).
    func audioActivated()
    /// The system took the session away: the audio stops, the call goes on.
    func audioDeactivated()
    func setMuted(_ muted: Bool)
    func setHeld(_ held: Bool)
    /// The system ended the call (End, a reset): the engine ends, and does not report it back.
    func hangUp()
}

nonisolated enum CallEngineEvent: Equatable, Sendable {
    /// Calling Persona: the voice's first `ready`.
    case ready
    /// The audio runs.
    case running
    /// A new engine is about to start on the call's audio (a route change rebuilt it).
    case starting
    /// It ended by itself. `note`: why, in a line for the page (nil: nothing to say).
    case ended(failed: Bool, note: String?)
    case interrupted
    case interruptionEnded
    /// The page muted the voice itself (the bar's Mute).
    case muted(Bool)
}

extension VoiceEnd {
    /// A system call that ends so failed: the system hears `failed`, else that the other side ended it.
    var failsCall: Bool {
        switch self {
        case .user, .left, .background, .replaced, .interrupted, .microphoneTaken, .idle, .urgentCall: false
        case .microphoneBusy, .permissionDenied, .audioFailed, .audioReset, .lost, .rejected, .incompatible, .modelUnavailable,
             .noHub, .failed: true
        }
    }
}

/// A call to Persona: a `VoiceConversation(surface: .call)` on the main conversation, on the system's audio session.
@MainActor final class OwnerCallEngine: CallEngine {
    let key: SessionKey
    let extensionID: String
    var events: (@MainActor (CallEngineEvent) -> Void)?
    /// The voice, once the call started.
    private(set) var voice: VoiceConversation?
    private let recent: [VoiceLine]
    private let audio: any SystemCallAudio
    private let calls: VoiceCalls
    private let deps: (any SystemCallAudio, @escaping @MainActor (VoiceCallEvent) -> Void) -> VoiceDeps
    private var muted = false
    private var held = false
    /// The voice's start, on its way to `VoiceCalls` (it runs after the Start action was handled).
    private var starting: Task<Void, Never>?
    /// Hung up: the voice never starts again, even if its start had not run yet.
    private var over = false

    /// `deps`: the voice's, on `audio` and reporting to the engine (`VoiceDeps.systemCall`; the tests' fakes).
    init(key: SessionKey, extensionID: String, recent: [VoiceLine], audio: any SystemCallAudio, calls: VoiceCalls,
         deps: @escaping (any SystemCallAudio, @escaping @MainActor (VoiceCallEvent) -> Void) -> VoiceDeps) {
        self.key = key
        self.extensionID = extensionID
        self.recent = recent
        self.audio = audio
        self.calls = calls
        self.deps = deps
    }

    func begin() {
        guard voice == nil, !over else { return }
        let voice = VoiceConversation(key: key, extensionID: extensionID, surface: .call, recent: recent,
                                      deps: deps(audio) { [weak self] event in self?.heard(event) })
        // Muted or held before it began (while it dialled): so from its first buffer.
        voice.setCallMuted(muted)
        voice.setHeld(held)
        audio.setHeld(held)
        audio.onRunning = { [weak self] in self?.events?(.running) }
        audio.onStarting = { [weak self] in self?.events?(.starting) }
        self.voice = voice
        let calls = calls
        starting = Task { [weak self] in
            // Ended before this ran (an End or a reset right after the Start): the voice never begins, so nothing claims
            // the microphone or opens a socket for a system call that is over.
            guard !Task.isCancelled, let self, !self.over, !voice.phase.isEnded else { return }
            self.starting = nil
            await calls.begin(voice)
        }
    }

    func audioActivated() { audio.activate() }

    func audioDeactivated() { audio.deactivate() }

    func setMuted(_ muted: Bool) {
        self.muted = muted
        voice?.setCallMuted(muted)
    }

    func setHeld(_ held: Bool) {
        self.held = held
        audio.setHeld(held)
        voice?.setHeld(held)
    }

    func hangUp() {
        over = true
        starting?.cancel()
        starting = nil
        guard let voice, !voice.phase.isEnded else { return }
        if calls.current === voice {
            calls.end(.user, from: .call)
        } else {
            voice.end(.user)
        }
    }

    private func heard(_ event: VoiceCallEvent) {
        switch event {
        case .ready:
            events?(.ready)
        case .ended(let end):
            // The voice stays the app's ended call, so the main conversation says why (「通话已结束 · …」) until its page
            // goes; its Continue calls Persona again (`VoiceEndedStrip`), never picks this voice up (`VoiceCalls.resume`).
            events?(.ended(failed: end.failsCall, note: VoiceEndNote.make(end, extensionID: extensionID)?.text))
        case .interrupted:
            events?(.interrupted)
        case .interruptionEnded:
            events?(.interruptionEnded)
        case .muted(let on):
            muted = on
            events?(.muted(on))
        }
    }
}

/// An urgent call: `UrgentCallCenter`'s session, answered on Join, talking once the system activated the audio.
@MainActor final class UrgentCallEngine: CallEngine {
    let id: UUID
    var events: (@MainActor (CallEngineEvent) -> Void)?

    init(id: UUID) {
        self.id = id
    }

    private var session: UrgentCallSession? { UrgentCallCenter.shared.session(for: id) }

    func begin() { UrgentCallCenter.shared.answered(id) }

    func audioActivated() {
        session?.start()
        events?(.running)
    }

    /// The turns go on by themselves once the audio is back (they hold while the call does).
    func audioDeactivated() {}

    func setMuted(_ muted: Bool) { session?.setMuted(muted) }

    func setHeld(_ held: Bool) { session?.setHeld(held) }

    func hangUp() { UrgentCallCenter.shared.endedOnPhone(id) }
}
#endif

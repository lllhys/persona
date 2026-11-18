#if os(iOS)
import AVFoundation
#endif
import Foundation

/// The buttons a CarPlay voice state can show. The template takes at most `CPVoiceControlState.maximumActionButtonCount`.
nonisolated enum CarPlayVoiceButton: String, Equatable, Sendable {
    case end, interrupt, resume
}

/// One state of the CarPlay voice control template, as plain data (the CarPlay delegate builds the real states from it).
nonisolated struct CarPlayVoiceStateSpec: Equatable, Sendable {
    var id: String
    /// Longest first: CarPlay picks the variant that fits the screen.
    var titles: [String]
    /// A `Theme.Icon` value.
    var icon: String
    var repeats: Bool
    var buttons: [CarPlayVoiceButton]
}

/// When the CarPlay screen starts the conversation by itself: once after it connects and once each time it comes back to
/// the foreground. Becoming active alone does not count: a notification banner, Siri or a call ending makes the scene
/// resign and become active again, and that must not reopen the microphone after the user pressed End or the
/// conversation ended by itself (idle, no permission, no voice plugin). Then only Continue or Siri start it again.
nonisolated struct CarPlayAutoStart: Equatable, Sendable {
    private(set) var armed = false

    /// The scene connected or is entering the foreground.
    mutating func arm() { armed = true }

    /// The scene became active: true at most once per `arm()`.
    mutating func takeOnBecomingActive() -> Bool {
        defer { armed = false }
        return armed
    }
}

/// What "Talk to Persona" (TalkToPersonaIntent) may do. Apple: the voice screen must be on screen while voice is
/// active, so CarPlay being connected is not enough; Persona has to be the app showing in the car.
nonisolated enum CarPlayTalk: Equatable, Sendable {
    case start
    case notConnected
    case notOnScreen

    /// `connected` / `foreground`: Persona's own CarPlay scene. `carOnRoute`: the phone's sound goes to a car (CarPlay),
    /// which also holds with no scene of Persona's: it has one only once opened on the car's screen since it launched,
    /// and only with its CarPlay entitlement. A car without Persona's scene is not "no car".
    static func decide(connected: Bool, foreground: Bool, carOnRoute: Bool = false) -> CarPlayTalk {
        guard connected else { return carOnRoute ? .notOnScreen : .notConnected }
        return foreground ? .start : .notOnScreen
    }

    /// No car: on the phone it is a call to Persona instead (#9290). Not with CarPlay connected and Persona elsewhere
    /// on the car's screen (or not opened there yet): that still says to open it there, since the call would need the
    /// phone unlocked.
    var callsOnThePhone: Bool { self == .notConnected }

    /// What Siri says back (on the phone `.notConnected` calls instead; the Vision app says it).
    var sentence: String {
        switch self {
        case .start: String(localized: "OK, let's keep talking on CarPlay")
        case .notConnected: String(localized: "Connect to CarPlay and open Persona to keep talking.")
        case .notOnScreen: String(localized: "Open Persona on the car's screen first to keep talking")
        }
    }
}

/// Whether CarPlay is connected and showing Persona's voice screen right now. The CarPlay scene delegate writes it, the
/// Siri intent (TalkToPersonaIntent) reads it.
@MainActor enum CarPlayVoicePresence {
    static var isConnected = false
    /// The CarPlay scene is in the foreground (between entering the foreground and entering the background).
    static var isForeground = false

    /// Persona's scene alone: enough to know the voice screen is the one in the car (`.start`), not that no car is there.
    static var talk: CarPlayTalk { CarPlayTalk.decide(connected: isConnected, foreground: isForeground) }
}

#if os(iOS)
extension CarPlayVoicePresence {
    /// What Talk to Persona does now (#9290). Without Persona's scene, whether a car is there is the audio route's answer,
    /// read off the main actor (a system service): CarPlay showing Maps, or Persona not yet opened on the car's screen
    /// since this launch (Siri may have just launched the app), is a car, not a call on the phone.
    @concurrent nonisolated static func talkNow(car: @Sendable () -> Bool = soundGoesToACar) async -> CarPlayTalk {
        let (connected, foreground) = await MainActor.run { (isConnected, isForeground) }
        return CarPlayTalk.decide(connected: connected, foreground: foreground, carOnRoute: !connected && car())
    }

    /// The phone's sound goes to a car through CarPlay (its audio route), whichever app the car shows.
    nonisolated static func soundGoesToACar() -> Bool {
        isCar(AVAudioSession.sharedInstance().currentRoute.outputs.map(\.portType))
    }

    nonisolated static func isCar(_ outputs: [AVAudioSession.Port]) -> Bool { outputs.contains(.carAudio) }
}
#endif

/// The CarPlay voice screen as data: its states, which state a phase shows, and the wording of its buttons.
/// Pure (no CarPlay import), so it is testable on any platform.
nonisolated enum CarPlayVoiceScreen {
    /// The ended state's second line when the template shows no buttons (no "Continue" to tap): Siri starts it again.
    static let resumeBySiri = String(localized: "Say “Talk to Persona” to talk again")

    /// The template's states. `endedReason` is the sentence shown in the ended state; an empty or missing one falls back
    /// to the default. `buttonsShown` is false where the template cannot show buttons; the ended state then points at Siri.
    /// `initial` goes first: a new template shows its first state, and CarPlay ignores a state activated before the
    /// template is on screen or too soon after another, so the state meant to be seen must already be the default.
    static func states(endedReason: String?, initial: String? = nil, buttonsShown: Bool = true) -> [CarPlayVoiceStateSpec] {
        var all = baseStates(endedReason: endedReason, buttonsShown: buttonsShown)
        if let initial, let index = all.firstIndex(where: { $0.id == initial }) {
            all.insert(all.remove(at: index), at: 0)
        }
        return all
    }

    private static func baseStates(endedReason: String?, buttonsShown: Bool) -> [CarPlayVoiceStateSpec] {
        let reason = endedReason?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let endedHeadline = reason.isEmpty ? String(localized: "Ended") : reason
        let endedHint = buttonsShown ? String(localized: "Tap Continue to talk again") : resumeBySiri
        return [
            CarPlayVoiceStateSpec(id: "connecting", titles: [String(localized: "Connecting to Persona"), String(localized: "Connecting")], icon: Theme.Icon.voiceConnecting,
                                  repeats: true, buttons: [.end]),
            CarPlayVoiceStateSpec(id: "listening", titles: [String(localized: "Listening, go ahead"), String(localized: "Listening")], icon: Theme.Icon.voiceListening,
                                  repeats: true, buttons: [.end]),
            CarPlayVoiceStateSpec(id: "speaking", titles: [String(localized: "Persona is speaking. Cut in anytime"), String(localized: "Persona is speaking")],
                                  icon: Theme.Icon.voiceSpeaking, repeats: true, buttons: [.interrupt, .end]),
            CarPlayVoiceStateSpec(id: "working", titles: [String(localized: "Persona is working on it"), String(localized: "Working [Car Play Voice Screen 694b71]")], icon: Theme.Icon.voiceWorking,
                                  repeats: true, buttons: [.end]),
            CarPlayVoiceStateSpec(id: "ended", titles: [endedHeadline, endedHint], icon: Theme.Icon.voiceEnded,
                                  repeats: false, buttons: [.resume]),
        ]
    }

    #if os(iOS)
    /// Which state id a `VoiceConversation` shows. No call yet is the ended state.
    /// `working` when the bound conversation is busy or a tool/ask is open; else the live audio state.
    static func stateID(for phase: VoicePhase?, tools: Int = 0, asking: Bool = false, targetBusy: Bool = false,
                        link: VoiceLink = .live) -> String {
        guard let phase else { return "ended" }
        switch phase {
        case .connecting, .reconnecting:
            return "connecting"
        case .live(let live):
            if link == .paused || targetBusy || tools > 0 || asking { return "working" }
            switch live {
            case .speaking: return "speaking"
            case .listening, .hearing: return "listening"
            }
        case .ended:
            return "ended"
        }
    }

    /// The state a template built on connecting opens on: connecting when the conversation is about to start by itself
    /// (there is no call yet, and would otherwise open on Ended for a moment), else the phase's own state.
    static func openingStateID(for phase: VoicePhase?, startingNow: Bool, tools: Int = 0, asking: Bool = false,
                               targetBusy: Bool = false, link: VoiceLink = .live) -> String {
        let active: Bool = {
            guard let phase else { return false }
            return !phase.isEnded
        }()
        if startingNow, !active { return "connecting" }
        return stateID(for: phase, tools: tools, asking: asking, targetBusy: targetBusy, link: link)
    }

    /// The ended state's headline for a `VoiceEnd` (I3 words live on the phone page; CarPlay needs a short line).
    static func endedHeadline(for end: VoiceEnd) -> String {
        switch end {
        case .user: String(localized: "Ended. Tap Continue to talk again")
        case .left: String(localized: "Persona doesn't listen when it's not on screen. Go back to Persona to keep talking")
        case .background: String(localized: "Ended in the background")
        case .replaced: String(localized: "Switched to another voice chat")
        case .interrupted: String(localized: "A call or another app took the audio, so the chat stopped")
        case .microphoneTaken: String(localized: "The microphone is recording or dictating. Stop that first, then talk")
        case .urgentCall: String(localized: "Ended for Persona's urgent call")
        case .microphoneBusy: String(localized: "The microphone is recording or dictating. Stop that first, then talk")
        case .permissionDenied: CarPlayMicGate.denied
        case .audioFailed: String(localized: "Couldn't open the microphone, so the chat stopped")
        case .audioReset: String(localized: "The audio service reset, so the chat stopped")
        case .idle: String(localized: "No one spoke for a while, so the chat stopped")
        case .lost: String(localized: "Can't reach the hub, so the chat stopped")
        case .rejected(let status): String(localized: "The hub refused the connection (\(status))")
        case .incompatible: String(localized: "The voice extension's protocol doesn't match. Upgrade the voice extension on the hub")
        case .modelUnavailable(let why):
            why.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? String(localized: "This voice model isn't available right now") : why
        case .noHub: CarPlayMicGate.noHub
        case .failed(_, let message):
            message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                ? String(localized: "Voice error, so the chat stopped")
                : message.trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }
    #endif

    /// The button's label on the voice screen.
    static func title(_ button: CarPlayVoiceButton) -> String {
        switch button {
        case .end: String(localized: "Stop [Car Play Voice Screen c7b24e]")
        case .interrupt: String(localized: "Interrupt [Car Play Voice Screen 268c79]")
        case .resume: String(localized: "Continue")
        }
    }

    /// The button's image: a `Theme.Icon` value.
    static func icon(_ button: CarPlayVoiceButton) -> String {
        switch button {
        case .end: Theme.Icon.end
        case .interrupt: Theme.Icon.voiceInterruptSpeech
        case .resume: Theme.Icon.voiceResume
        }
    }
}

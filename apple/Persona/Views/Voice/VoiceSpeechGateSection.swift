import SwiftUI

/// Only Send What I Just Said (docs/assistant/voice.md 6.7, #5904): the user-speech gate of this device's calls with the
/// voice extension, in a section under the Voice row on its page (`VoiceTimbreSection` shows it). On, the default, what
/// the voice hands over goes only right after the user spoke, so an agent's results read aloud can't give instructions
/// for them; off, a call's `start` says `speech_gate: false` (`VoiceSpeechGate`, per group of hubs and extension), and
/// a call going on with this extension picks itself up again with it. Only for an extension that takes it
/// (`VoiceInfo.speechGateFeature` in its /info): an older one would ignore it and keep its gate.
struct VoiceSpeechGateSection: View {
    let extensionID: String
    let group: String
    @State private var on: Bool

    init(extensionID: String, group: String) {
        self.extensionID = extensionID
        self.group = group
        _on = State(initialValue: VoiceSpeechGate.isOn(group: group, extensionID: extensionID))
    }

    /// Whether the extension's /info says it takes the switch.
    nonisolated static func supported(_ info: VoiceInfoState?) -> Bool {
        guard case .info(let info)? = info else { return false }
        return info.features.contains(VoiceInfo.speechGateFeature)
    }

    var body: some View {
        Section {
            Toggle("Only Send What I Just Said", isOn: Binding(get: { on }, set: { set($0) }))
                .accessibilityIdentifier("voice-speech-gate-setting")
        } footer: {
            Text("When on, Persona sends a request only right after you speak, so an agent’s results read aloud can’t give instructions for you. Turn it off if it keeps holding back your own requests.")
        }
    }

    private func set(_ on: Bool) {
        self.on = on
        VoiceSpeechGate.set(on, group: group, extensionID: extensionID)
        // A call going on with this extension starts again with it (the gate is the extension's, set by `start`).
        if let call = VoiceCalls.shared.current, call.key.group == group, call.extensionID == extensionID, !call.phase.isEnded {
            call.change(speechGate: on)
        }
    }
}

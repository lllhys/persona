import Foundation
import Testing
@testable import Persona

/// Only Send What I Just Said and Send Anyway on the app's side (#5904, docs/assistant/voice.md 3.2, 6.2, 6.7): the start
/// frame's field, the preference, a change during a call, and when the settings page shows the switch. These use what
/// #5904 adds; the regression tests that also build against the code before it are in VoiceBarLineTests,
/// VoiceModeTests and VoiceConversationTests.
@MainActor
struct VoiceSpeechGateTests {
    private func object(_ text: String) -> [String: Any]? {
        (try? JSONSerialization.jsonObject(with: Data(text.utf8))) as? [String: Any]
    }

    @Test func startSaysSpeechGateAsTheCallHasIt() throws {
        #expect(object(VoiceMessage.start(VoiceStart(speechGate: false)))?["speech_gate"] as? Bool == false)
        #expect(object(VoiceMessage.start(VoiceStart(speechGate: true)))?["speech_gate"] as? Bool == true)
        let plain = try #require(object(VoiceMessage.start(VoiceStart())))
        #expect(plain["speech_gate"] == nil, "nil is left out, as an app from before #5904 sends it")
    }

    @Test func thePreferenceIsOnUnlessTurnedOff() {
        let group = "g-" + UUID().uuidString
        let key = VoiceSpeechGate.key(group: group, extensionID: "voice")
        defer { UserDefaults.standard.removeObject(forKey: key) }
        #expect(key == "voice.speechGate." + group + ".voice")
        #expect(VoiceSpeechGate.key(group: "", extensionID: "voice") == "voice.speechGate.-.voice")
        #expect(VoiceSpeechGate.isOn(group: group, extensionID: "voice"))
        VoiceSpeechGate.set(false, group: group, extensionID: "voice")
        #expect(!VoiceSpeechGate.isOn(group: group, extensionID: "voice"))
        #expect(VoiceSpeechGate.isOn(group: group, extensionID: "other"), "per extension")
        VoiceSpeechGate.set(true, group: group, extensionID: "voice")
        #expect(UserDefaults.standard.object(forKey: key) == nil, "on is the default: nothing kept")
    }

    /// Turned off on the settings page during a call: a new connection whose start says so; the same again does nothing.
    @Test func aChangeDuringACallPicksItUpAgain() async throws {
        let rig = VoiceRig()
        let call = rig.call()
        await call.begin()
        rig.socket?.ready(rig.key)
        #expect(rig.sockets.count == 1 && rig.socket?.start?["speech_gate"] as? Bool == true)
        call.change(speechGate: false)
        #expect(rig.sockets.count == 2)
        #expect(try #require(rig.socket?.start)["speech_gate"] as? Bool == false)
        call.change(speechGate: false)
        #expect(rig.sockets.count == 2)
    }

    @Test func theSwitchShowsOnlyForAnExtensionThatTakesIt() {
        #expect(VoiceSpeechGateSection.supported(.info(VoiceInfo(protocolVersion: 2, features: ["target", VoiceInfo.speechGateFeature]))))
        #expect(!VoiceSpeechGateSection.supported(.info(VoiceInfo(protocolVersion: 2, features: ["target", "runs"]))))
        #expect(!VoiceSpeechGateSection.supported(.missing))
        #expect(!VoiceSpeechGateSection.supported(nil))
    }
}

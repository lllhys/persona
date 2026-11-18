#if os(iOS)
import Foundation
import Intents

/// "Call Persona using Persona" through SiriKit (#9290, docs/apple/siri.md「Calling Persona」). In a build only when
/// config.json app.siri_calls is on: apple/siri-calls.yml adds the Intents extension (PersonaCallIntents), the Siri
/// entitlement and Info.plist's `PersonaSiriCalls`. The extension hands the call to the app as a user activity, which
/// places it as a redial from Recents does (`PersonaCalls.continueActivity`). Siri uses the extension only once the
/// owner has allowed Persona to use Siri, which only the app can ask. The App Shortcut (`CallPersonaIntent`) needs
/// none of this.
enum SiriCalls {
    /// This build has the SiriKit extension (Info.plist, read from memory).
    static var builtIn: Bool { Bundle.main.object(forInfoDictionaryKey: "PersonaSiriCalls") as? Bool == true }

    private static var asked = false

    /// Becoming active: asks once whether Persona may use Siri. With a hub paired (not over the first pairing) and no
    /// call on (not over a call's page); the system shows its prompt only while the owner hasn't decided.
    static func askOnce(paired: Bool) {
        guard builtIn, paired, !asked, PersonaCalls.shared.calls.isEmpty else { return }
        asked = true
        // The status is asked of a system service: off the main actor.
        Task.detached {
            guard INPreferences.siriAuthorizationStatus() == .notDetermined else { return }
            // Answered on the system's queue.
            INPreferences.requestSiriAuthorization { @Sendable status in
                PersonaCalls.log.notice("Siri calls: authorization \(status.rawValue, privacy: .public)")
            }
        }
    }
}
#endif

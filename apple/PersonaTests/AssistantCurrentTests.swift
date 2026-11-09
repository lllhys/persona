import Foundation
import Observation
@testable import Persona
import Testing

/// The Persona page's conversation is stored before its observers are told (#2189). SwiftUI can draw a page while it is
/// told; a page that read the old value then missed the change for good, and New Chat left the conversation on screen.
@MainActor @Suite(.serialized) struct AssistantCurrentTests {
    @MainActor final class Seen {
        var values: [SessionKey?] = []
    }

    /// Runs `body` with the remembered conversation put back afterwards: the setter writes it to the standard defaults.
    private func keepingDefaults(_ body: () -> Void) {
        let saved = UserDefaults.standard.object(forKey: "assistant.current")
        defer { UserDefaults.standard.set(saved, forKey: "assistant.current") }
        body()
    }

    @Test func anObserverToldOfTheChangeReadsTheNewValue() {
        keepingDefaults {
            let model = AppModel()
            let main = SessionKey(machineID: "server", sessionID: "main")
            let other = SessionKey(machineID: "server", sessionID: "other")
            model.assistantCurrent = main
            let seen = Seen()
            withObservationTracking { _ = model.assistantCurrent } onChange: {
                MainActor.assumeIsolated { seen.values.append(model.assistantCurrent) }
            }
            model.assistantCurrent = nil // New Chat
            #expect(seen.values == [nil], "told of New Chat, the page must already read no conversation")
            withObservationTracking { _ = model.assistantCurrent } onChange: {
                MainActor.assumeIsolated { seen.values.append(model.assistantCurrent) }
            }
            model.assistantCurrent = other
            #expect(seen.values == [nil, other])
            #expect(UserDefaults.standard.string(forKey: "assistant.current") == other.path)
        }
    }

    @Test func anEqualValueTellsNoOneButIsStillRemembered() {
        keepingDefaults {
            let model = AppModel()
            model.assistantCurrent = nil
            UserDefaults.standard.removeObject(forKey: "assistant.current")
            let seen = Seen()
            withObservationTracking { _ = model.assistantCurrent } onChange: {
                MainActor.assumeIsolated { seen.values.append(model.assistantCurrent) }
            }
            model.assistantCurrent = nil
            #expect(seen.values.isEmpty, "as @Observable's own setter: an equal value tells no one")
            #expect(UserDefaults.standard.string(forKey: "assistant.current") == "", "a chosen New Chat is still remembered")
        }
    }
}

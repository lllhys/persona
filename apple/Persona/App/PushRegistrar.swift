import Foundation
import UserNotifications
#if os(macOS)
import AppKit
#else
import UIKit
#endif

/// Registers for APNs and tells the hub where to push (interactions, finished turns). The iPhone and the Mac app the
/// same way, as kind `iphone` / `mac`: same bundle id, so the same APNs topic, and both through the relay for a hub
/// without the app's key. While the Mac app is connected the hub leaves the agents' alerts to it (MacAttentionWatcher
/// posts them); a Mac never gets the phone's wake pushes.
final class PushRegistrar {
    static let shared = PushRegistrar()

    func registerIfPossible() {
        guard AppModel.shared.isPaired, !AppModel.shared.isDemo else { return }
        #if DEBUG
        // Simulator and snapshot automation: no permission alert covering the screenshots.
        if UserDefaults.standard.string(forKey: "pairURL") != nil { return }
        #endif
        Task {
            // Asked here, not when the first notification is due: a push that comes while the app is closed shows
            // only if this was allowed before.
            let center = UNUserNotificationCenter.current()
            let granted = (try? await center.requestAuthorization(options: [.alert, .sound, .badge, .timeSensitive])) ?? false
            await PushHealthStore.shared.update { $0.notificationsAllowed = granted }
            #if os(macOS)
            if granted { NSApplication.shared.registerForRemoteNotifications() }
            #else
            if granted { UIApplication.shared.registerForRemoteNotifications() }
            #endif
        }
    }

    /// Registers with every group: at its first hub, the one that issued the credential (docs/archive/operations/plan-hub-mesh.md 2.6: the
    /// group's other hubs pass their pushes to it, one push per device), each group sealing to a key of its own
    /// (docs/protocol/multi-hub.md「推送」).
    func didRegister(token: Data) {
        let hex = token.map { String(format: "%02x", $0) }.joined()
        let registry = HubStore.load()
        let clients = registry.groups.compactMap { g in registry.primary(g).map { (group: g, api: APIClient(config: $0.config, group: g)) } }
            .filter { !$0.api.config.isDemo }
        guard !clients.isEmpty else { return }
        #if DEBUG
        let env = "sandbox"
        #else
        let env = "production"
        #endif
        #if os(macOS)
        let kind = "mac"
        let features: [String]? = nil
        #else
        let kind = "iphone"
        let features: [String]? = Self.features(RequestClientMetadata.current)
        #endif
        let base = APIClient.DeviceBody(deviceID: Self.deviceID, name: UIDevice.current.name, kind: kind, apnsToken: hex,
                                        apnsEnv: env, topic: Bundle.main.bundleIdentifier, features: features)
        Task {
            // A hub without the app's APNs key pushes through the relay, sealed to this group's key (docs/operations/push-relay.md);
            // the notification service extension opens it. The hub decides: with its own key it ignores these.
            var grant: String?
            var outcome = PushHealth.Relay.notConfigured
            if let relay = PushRelay.url, let topic = base.topic {
                do {
                    grant = try await PushRelay.grant(relay: relay, token: hex, env: env, topic: topic)
                    outcome = .registered
                } catch {
                    PushRelay.log.error("relay registration: \(error.localizedDescription, privacy: .public)")
                    outcome = PushRelay.outcome(of: error)
                }
            }
            let relayError = PushHealth(relay: outcome).relayError
            await PushHealthStore.shared.update { $0.apnsError = nil; $0.relay = outcome }
            for c in clients {
                var body = base
                if let grant, let relay = PushRelay.url, let key = PushCrypto.ensureKey(group: c.group) {
                    body.relayGrant = grant
                    body.relayURL = relay.absoluteString
                    body.pushKey = PushCrypto.publicKey(key)
                } else {
                    body.relayError = relayError
                }
                do {
                    let route = try await c.api.register(device: body) // nil: an older hub, which does not say
                    await PushHealthStore.shared.update { $0.routes[c.group] = route }
                } catch {}
            }
        }
    }

    /// APNs gave no token (no network, a build without the push entitlement): Settings › Notifications says so.
    func didFail(_ error: any Error) {
        let text = error.localizedDescription
        Task { @MainActor in PushHealthStore.shared.update { $0.apnsError = text } }
    }

    /// What this app takes in pushes beyond the first ones (`features`, docs/protocol/protocol.md devices): withdrawals
    /// (AppDelegate removes the notifications they name, #4349), and MCP forms and links (`INTERACTION_ELICITATION`,
    /// #2433) where `client_hello` declares them, the iOS app alone (`LiveConnection.helloFeatures`): the hub pushes
    /// those cards only to such a phone, and waits for it while no app that shows them is open. An older hub drops the name.
    nonisolated static func features(_ client: RequestClientMetadata) -> [String] {
        ["withdraw"] + LiveConnection.helloFeatures(client).filter { $0 == "elicitation" }
    }

    /// This device's id at the hub: its push token and its notification settings hang off it.
    static var deviceID: String {
        if let id = UserDefaults.standard.string(forKey: "deviceID") { return id }
        let id = UUID().uuidString
        UserDefaults.standard.set(id, forKey: "deviceID")
        return id
    }
}

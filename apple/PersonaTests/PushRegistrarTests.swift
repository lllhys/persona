import Testing
import UserNotifications
@testable import Persona

/// The system as the registrar sees it: the notification switch, the permission prompt and APNs registration.
@MainActor
private final class FakeSystem {
    var paired = true
    /// The system's switch now (nil: never asked).
    var allowed: Bool?
    private(set) var requests = 0
    private(set) var registrations = 0
    /// The prompt on screen, waiting for an answer.
    private(set) var prompt: CheckedContinuation<Bool, Never>?

    var system: PushRegistrar.System {
        PushRegistrar.System(
            paired: { self.paired },
            request: {
                self.requests += 1
                if let allowed = self.allowed { return allowed } // asked before: no prompt, the switch's answer
                return await withCheckedContinuation { self.prompt = $0 }
            },
            allowed: { self.allowed },
            register: { self.registrations += 1 })
    }

    /// The prompt's tap reaches the system's switch first; the request's answer comes after.
    func answer(_ granted: Bool) {
        allowed = granted
    }

    func closePrompt() {
        prompt?.resume(returning: allowed ?? false)
        prompt = nil
    }
}

/// Allowing notifications in Settings after a denial registers for pushes when the app comes back, without a relaunch
/// (#1861): before, the hub had no token for this device until Persona was quit and opened again.
@MainActor
struct PushRegistrarTests {
    private func registrar(_ fake: FakeSystem) -> (PushRegistrar, PushHealthStore) {
        let health = PushHealthStore(health: PushHealth())
        return (PushRegistrar(system: fake.system, health: health), health)
    }

    @Test func allowingAfterADenialRegistersOnReturn() async {
        let fake = FakeSystem()
        fake.allowed = false
        let (r, health) = registrar(fake)
        await r.registerIfPossible()?.value
        #expect(fake.registrations == 0 && health.health.notificationsAllowed == false)

        await r.foreground()?.value // still off: nothing
        #expect(fake.registrations == 0 && !r.registered)

        fake.allowed = true // allowed in Settings, then back to Persona
        await r.foreground()?.value
        #expect(fake.registrations == 1 && r.registered)
        #expect(health.health.notificationsAllowed == true)

        // Later returns don't register again, and don't even check.
        #expect(r.foreground() == nil)
        #expect(fake.registrations == 1)
    }

    @Test func allowedAtLaunchRegistersOnce() async {
        let fake = FakeSystem()
        fake.allowed = true
        let (r, _) = registrar(fake)
        await r.registerIfPossible()?.value
        #expect(fake.registrations == 1)
        #expect(r.foreground() == nil)
        #expect(fake.registrations == 1)
    }

    /// The prompt's answer makes the app active again before the launch's request has its answer: the check waits for
    /// that request, so one answer registers once.
    @Test func anOpenPromptIsNotAnsweredTwice() async throws {
        let fake = FakeSystem()
        let (r, health) = registrar(fake)
        let launch = r.registerIfPossible()
        for _ in 0..<100 where fake.prompt == nil { await Task.yield() }
        try #require(fake.prompt != nil)

        fake.answer(true)
        let back = r.foreground()
        for _ in 0..<20 { await Task.yield() }
        fake.closePrompt()
        await launch?.value
        await back?.value
        #expect(fake.registrations == 1)
        #expect(health.health.notificationsAllowed == true)
    }

    /// Coming back never shows the prompt: a permission not asked yet is the launch's or the pairing's to ask.
    @Test func returningNeverPrompts() async {
        let fake = FakeSystem()
        let (r, health) = registrar(fake)
        let back = r.foreground()
        for _ in 0..<100 where fake.prompt == nil { await Task.yield() }
        #expect(fake.requests == 0 && fake.prompt == nil)
        fake.closePrompt() // a prompt shown by mistake would hold the check forever
        await back?.value
        #expect(fake.registrations == 0)
        #expect(health.health.notificationsAllowed == nil)
    }

    @Test func unpairedDoesNothing() async {
        let fake = FakeSystem()
        fake.paired = false
        fake.allowed = true
        let (r, _) = registrar(fake)
        #expect(r.registerIfPossible() == nil)
        #expect(r.foreground() == nil)
        #expect(fake.requests == 0 && fake.registrations == 0)
    }

    /// Pairing another hub registers again, so that hub learns the token too.
    @Test func pairingRegistersAgain() async {
        let fake = FakeSystem()
        fake.allowed = true
        let (r, _) = registrar(fake)
        await r.registerIfPossible()?.value
        await r.registerIfPossible()?.value
        #expect(fake.registrations == 2)
    }

    /// Settings › Notifications reads the hub's route for this device again (#1859): the one Settings › Devices shows,
    /// not the one kept since launch.
    @Test func theRouteIsReadAgainFromTheHub() async {
        let fake = FakeSystem()
        let (r, health) = registrar(fake)
        health.update { $0.routes["g"] = APIClient.PushRoute(route: "relay") } // what the launch's registration said
        let failing = APIClient.PushRoute(route: "relay", reason: "push_failed", detail: "503 push relay not configured")
        func tokens(_ push: [String: APIClient.PushRoute]?) -> APIClient.DeviceTokens {
            APIClient.DeviceTokens(tokens: [], current: "me", push: push)
        }

        await r.refreshRoute(group: "g") { tokens(["me": failing, "watch": APIClient.PushRoute(route: "relay")]) }
        #expect(health.health.routes["g"] == failing)
        #expect(health.health.status(group: "g", allowed: true).level == .lastFailed)

        // An older hub sends no routes: what is known stays.
        await r.refreshRoute(group: "g") { tokens(nil) }
        #expect(health.health.routes["g"] == failing)

        // Unreachable: unchecked, not the route from before; a cancelled read is no answer either way.
        await r.refreshRoute(group: "g") { throw URLError(.timedOut) }
        #expect(health.health.unchecked?["g"] == URLError(.timedOut).localizedDescription)
        #expect(health.health.status(group: "g", allowed: true).level == .unknown)
        await r.refreshRoute(group: "g") { throw CancellationError() }
        #expect(health.health.unchecked?["g"] == URLError(.timedOut).localizedDescription)

        // Back, with no registration from this device at that hub.
        await r.refreshRoute(group: "g") { tokens(["watch": APIClient.PushRoute(route: "relay")]) }
        #expect(health.health.routes["g"] == nil && health.health.unchecked == nil)
    }

    @Test func theSystemsSwitch() {
        #expect(UNAuthorizationStatus.notDetermined.allowsNotifications == nil)
        #expect(UNAuthorizationStatus.denied.allowsNotifications == false)
        #expect(UNAuthorizationStatus.authorized.allowsNotifications == true)
        #expect(UNAuthorizationStatus.provisional.allowsNotifications == true)
        #expect(UNAuthorizationStatus.ephemeral.allowsNotifications == true)
    }
}

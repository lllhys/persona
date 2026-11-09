import XCTest

/// An MCP form's push (#2433) against scripts/mock/mock_hub.py with `MOCK_ELICITATION=1`
/// (TEST_RUNNER_PERSONA_ELICITATION_HUB=http://127.0.0.1:<port>), on a stock simulator (it answers the notification question):
/// - the app posts what the hub's push looks like (`-showTestNotification elicitation`, category INTERACTION_ELICITATION, no
///   buttons) about the PR review chat; tapped from the background, it opens that chat with the form;
/// - launched without `-pairURL`, the app registers for pushes with `features: ["withdraw", "elicitation"]` (GET /mock/devices).
///   A simulator that gives no APNs token skips that part.
/// TEST_RUNNER_PERSONA_SHOTS=<dir> also saves the screens (TEST_RUNNER_PERSONA_SHOTS_APPEARANCE names them light / dark).
final class ElicitationPushTests: XCTestCase {
    private let review = "macbook/a1f3c2d4-0000-4000-8000-000000000006"
    @MainActor private var springboard: XCUIApplication { XCUIApplication(bundleIdentifier: "com.apple.springboard") }
    private var app: XCUIApplication!

    private func hub() throws -> String {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_ELICITATION_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_ELICITATION=1 (PERSONA_ELICITATION_HUB)")
        }
        return hub
    }

    private func pairURL(_ hub: String) -> String {
        "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
    }

    /// The system's notification question, answered.
    @MainActor private func allowNotifications() {
        let alert = springboard.alerts.firstMatch
        guard alert.waitForExistence(timeout: 12) else { return }
        for label in ["允许", "Allow"] where alert.buttons[label].exists {
            alert.buttons[label].tap()
            return
        }
    }

    @MainActor func testTappingThePushOpensTheForm() throws {
        let hub = try hub()
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-pairURL", pairURL(hub), "-tab", "settings",
                               "-showTestNotification", "elicitation", "-showTestNotificationIn", review]
        app.launchPinned()
        allowNotifications()
        sleep(1)
        XCUIDevice.shared.press(.home) // the push arrives while the app is in the background

        let banner = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "deploy-mcp")).firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 15), "no notification banner:\n\(springboard.debugDescription)")
        sleep(1) // on screen, not just in the tree
        shot("elicitation-push", XCUIScreen.main.screenshot())
        banner.tap()

        XCTAssertTrue(app.wait(for: .runningForeground, timeout: 10))
        let form = app.descendants(matching: .any)["elicitation.ix_elicit_form"]
        XCTAssertTrue(form.waitForExistence(timeout: 30), "the push did not open the chat with the form")
        shot("elicitation-push-opened", app.screenshot())
    }

    @MainActor func testRegistrationTakesMCPFormPushes() throws {
        let hub = try hub()
        app = XCUIApplication()
        app.launchArguments = ["-pairURL", pairURL(hub), "-tab", "settings"]
        app.launchPinned()
        allowNotifications()
        app.terminate()
        // Paired now: without `-pairURL` (which keeps DEBUG runs from registering) the app registers for pushes.
        app.launchArguments = ["-tab", "settings"]
        app.launchPinned()
        allowNotifications()
        var features: [String]?
        let deadline = Date.now.addingTimeInterval(30)
        while Date.now < deadline, features == nil {
            if let data = try? Data(contentsOf: URL(string: hub + "/mock/devices")!),
               let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               let last = (obj["devices"] as? [[String: Any]])?.last {
                features = last["features"] as? [String]
            }
            if features == nil { usleep(500_000) }
        }
        guard let features else { throw XCTSkip("this simulator gave the app no APNs token: no push registration to read") }
        XCTAssertEqual(features, ["withdraw", "elicitation"])
    }

    @MainActor private func shot(_ name: String, _ s: XCUIScreenshot) {
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let look = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"].map { "-" + $0 } ?? ""
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + look + ".png"))
        }
    }
}

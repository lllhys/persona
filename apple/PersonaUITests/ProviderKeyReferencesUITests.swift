import XCTest

/// A model provider's key as a secret provider's reference on the iPhone (#3962 step 4c), against the mock hub's
/// made-up provider (`MOCK_EXTENSIONS=1 MOCK_SECRET_HANDOFF=1`, `TEST_RUNNER_PERSONA_SECRET_REFERENCES_HUB`): the model
/// provider `gateway`'s key is a reference of Example Vault, so its page says where the key comes from and its form opens with "Use a
/// Reference" on; the server's own key, another reference, opens the same way on its Computers page; a preset's key step
/// takes a reference typed in plain text. Nothing is saved. Light and dark; with `PERSONA_SHOTS`, a screenshot of each.
final class ProviderKeyReferencesUITests: XCTestCase {
    private let env = ProcessInfo.processInfo.environment

    @MainActor func testAProvidersKeyAsAReference() throws {
        guard let hub = env["PERSONA_SECRET_REFERENCES_HUB"] else {
            throw XCTSkip("Needs MOCK_EXTENSIONS=1 MOCK_SECRET_HANDOFF=1 (PERSONA_SECRET_REFERENCES_HUB)")
        }
        continueAfterFailure = false
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        for scheme in ["light", "dark"] {
            // The provider's page, then its form.
            let app = XCUIApplication()
            app.launchArguments = ["-pairURL", pair, "-dev.openChat", "", "-tab", "settings", "-ui.scheme", scheme,
                                   "-openLauncherPage", "gateway"]
            app.launchPinned()
            dismissSystemAlerts(wait: 4)
            let from = app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "Example Vault")).firstMatch
            XCTAssertTrue(from.waitForExistence(timeout: 20), app.debugDescription)
            shot(app, "page-" + scheme)
            let edit = app.navigationBars.buttons["编辑"].firstMatch
            XCTAssertTrue(edit.waitForExistence(timeout: 5), app.debugDescription)
            edit.tap()
            let use = app.switches["wizard.key.use-reference"]
            XCTAssertTrue(use.waitForExistence(timeout: 10), app.debugDescription)
            XCTAssertEqual(use.value as? String, "1")
            XCTAssertTrue(app.textFields["wizard.key.reference"].exists, app.debugDescription)
            shot(app, "form-" + scheme)

            // The server's own key, on the form's Computers page: its row is under the models step's Advanced.
            app.buttons["wizard.next"].tap()
            let advanced = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "高级")).firstMatch
            reach(advanced, in: app, timeout: 10)
            advanced.tap()
            // Inside Advanced the group's identifier replaces the row's own: found by its label there.
            let computers = app.buttons.matching(NSPredicate(format: "identifier IN %@ AND label BEGINSWITH %@",
                                                             ["wizard.computers", "wizard.advanced"], "电脑")).firstMatch
            reach(computers, in: app)
            computers.tap()
            let keys = app.descendants(matching: .any)["computers.keys"].firstMatch
            reach(keys, in: app)
            keys.tap()
            let server = app.switches["computers.key.server.use-reference"]
            reach(server, in: app)
            XCTAssertEqual(server.value as? String, "1")
            XCTAssertTrue(app.textFields["computers.key.server.reference"].exists, app.debugDescription)
            shot(app, "computer-key-" + scheme)
            app.terminate()

            // A preset's key step: the switch on, a reference typed.
            app.launchArguments = ["-pairURL", pair, "-dev.openChat", "", "-tab", "settings", "-ui.scheme", scheme,
                                   "-openModelLaunchers", "YES", "-openProviderWizard", "pick"]
            app.launchPinned()
            dismissSystemAlerts(wait: 2)
            let preset = app.buttons["wizard.preset.example-gateway/team"]
            reach(preset, in: app, timeout: 20)
            preset.tap()
            let presetUse = app.switches["wizard.preset.key.use-reference"]
            XCTAssertTrue(presetUse.waitForExistence(timeout: 10), app.debugDescription)
            XCTAssertEqual(presetUse.value as? String, "0")
            // The switch's knob, not the row's label.
            presetUse.coordinate(withNormalizedOffset: CGVector(dx: 0.94, dy: 0.5)).tap()
            XCTAssertEqual(presetUse.value as? String, "1", app.debugDescription)
            let field = app.textFields["wizard.preset.key.reference"]
            XCTAssertTrue(field.waitForExistence(timeout: 5), app.debugDescription)
            field.tap()
            field.typeText("op://Example/Team gateway/credential")
            XCTAssertEqual(field.value as? String, "op://Example/Team gateway/credential")
            shot(app, "preset-key-" + scheme)
            app.terminate()
        }
    }

    /// Scrolls until `element` is on screen: rows below the fold exist only once scrolled to.
    @MainActor private func reach(_ element: XCUIElement, in app: XCUIApplication, timeout: TimeInterval = 5) {
        _ = element.waitForExistence(timeout: timeout)
        for _ in 0..<10 where !(element.exists && element.isHittable) { app.swipeUp() }
        XCTAssertTrue(element.waitForExistence(timeout: 5) && element.isHittable, app.debugDescription)
    }

    /// Answers the system's own questions (notifications, local network) so they never cover a screenshot.
    @MainActor private func dismissSystemAlerts(wait: TimeInterval = 1) {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        var deadline = Date.now.addingTimeInterval(wait)
        while Date.now < deadline {
            let alert = springboard.alerts.firstMatch
            if alert.waitForExistence(timeout: 0.5) {
                for label in ["允许", "Allow", "好", "OK"] where alert.buttons[label].exists { alert.buttons[label].tap(); break }
                deadline = Date.now.addingTimeInterval(1.5) // another may follow
            }
        }
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = env["PERSONA_SHOTS"] {
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("provider-key-references-" + name + ".png"))
        }
    }
}

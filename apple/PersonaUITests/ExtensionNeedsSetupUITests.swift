import XCTest

/// An extension's page that still needs a secret (#8853), against the mock hub's Example Notes (`MOCK_EXTENSIONS=1`,
/// `TEST_RUNNER_PERSONA_NEEDS_SETUP_HUB`): the header's "Still needs" line opens the API key's sheet, and the
/// configuration's heading is not shown over groups that have their own. Light and dark; with `PERSONA_SHOTS`, a
/// screenshot of the header and of the sheet.
final class ExtensionNeedsSetupUITests: XCTestCase {
    private let env = ProcessInfo.processInfo.environment

    @MainActor func testStillNeedsOpensTheSecret() throws {
        guard let hub = env["PERSONA_NEEDS_SETUP_HUB"] else {
            throw XCTSkip("Needs MOCK_EXTENSIONS=1 (PERSONA_NEEDS_SETUP_HUB)")
        }
        continueAfterFailure = false
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        for scheme in ["light", "dark"] {
            let app = XCUIApplication()
            app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-ui.scheme", scheme, "-openExtension", "example-notes"]
            app.launchPinned()
            dismissSystemAlerts(wait: 4)
            XCTAssertTrue(app.switches["plugin-enabled"].waitForExistence(timeout: 20), app.debugDescription)
            XCTAssertFalse(app.descendants(matching: .any)["extension-section-configuration"].exists,
                           "the configuration's heading stands over groups with their own")
            let line = app.buttons["plugin-needs-setup"]
            XCTAssertTrue(line.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(line.label.contains("API Key"), line.label)
            shot(app, "header-" + scheme)
            line.tap()
            let field = app.descendants(matching: .any)["plugin-secret-value"].firstMatch
            XCTAssertTrue(field.waitForExistence(timeout: 10), app.debugDescription)
            shot(app, "sheet-" + scheme)
            app.terminate()
        }
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
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("needs-setup-" + name + ".png"))
        }
    }
}

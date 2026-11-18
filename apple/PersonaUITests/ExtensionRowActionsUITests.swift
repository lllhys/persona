import XCTest

/// UI version 6's row actions (#827) on the mock hub's `showcase › row-actions` (`MOCK_EXTENSIONS=1`,
/// `TEST_RUNNER_PERSONA_EXTENSIONS_HUB`): a long press opens the row's menu with all its actions, destructive last; a
/// swipe from the trailing edge shows the `swipe` ones; the destructive one asks first, and Cancel leaves it. In both
/// styles, light and dark; with `PERSONA_SHOTS`, screenshots. Skipped for an app from before
/// (`TEST_RUNNER_PERSONA_SHOWCASE_TAG=before`: `ExtensionShowcaseShotsUITests` shoots the page then).
final class ExtensionRowActionsUITests: XCTestCase {
    private let env = ProcessInfo.processInfo.environment

    @MainActor func testMenuSwipeAndConfirm() throws {
        guard let hub = env["PERSONA_EXTENSIONS_HUB"] else { throw XCTSkip("Needs MOCK_EXTENSIONS=1 (PERSONA_EXTENSIONS_HUB)") }
        if env["PERSONA_SHOWCASE_TAG"] == "before" { throw XCTSkip("An app from before has no row actions") }
        continueAfterFailure = false
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        for style in ["glass", "beautiful"] { for scheme in ["light", "dark"] {
            let look = scheme + "-" + style
            let app = XCUIApplication()
            app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-ui.style", style, "-ui.scheme", scheme,
                                   "-openExtension", "showcase", "-openExtensionPage", "row-actions"]
            app.launchPersona()
            dismissSystemAlerts(wait: 4)
            XCTAssertTrue(app.descendants(matching: .any)["extension-ui-ready"].waitForExistence(timeout: 20), app.debugDescription)

            // A long press: the menu with all four, delete last.
            let four = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "四个动作")).firstMatch
            XCTAssertTrue(four.waitForExistence(timeout: 10), app.debugDescription)
            four.press(forDuration: 1.2)
            let delete = app.buttons["plugin-ui-row-action-delete"]
            XCTAssertTrue(delete.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(app.buttons["plugin-ui-row-action-edit"].exists && app.buttons["plugin-ui-row-action-pin"].exists)
            shot(app, "menu-" + look)
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.08)).tap()
            XCTAssertTrue(delete.waitForNonExistence(timeout: 5), "the menu closes")

            // A swipe: the two marked swipe, delete at the edge; it asks first.
            four.swipeLeft()
            XCTAssertTrue(delete.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertFalse(app.buttons["plugin-ui-row-action-edit"].exists, "only the swipe ones")
            shot(app, "swipe-" + look)
            delete.tap()
            let alert = app.alerts.firstMatch
            XCTAssertTrue(alert.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(alert.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "删除这一项")).firstMatch.exists, alert.debugDescription)
            shot(app, "confirm-" + look)
            // The first match: iOS 27 nests an alert's button in one with the same identifier.
            let byID = alert.buttons.matching(identifier: "plugin-ui-confirm-cancel").firstMatch
            let cancel = byID.exists ? byID : alert.buttons.matching(identifier: "取消").firstMatch
            cancel.tap()
            XCTAssertTrue(alert.waitForNonExistence(timeout: 5))
            app.terminate()
        } }
    }

    /// The notification question and the icon change note are SpringBoard's.
    @MainActor private func dismissSystemAlerts(wait: TimeInterval = 1) {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        var deadline = Date.now.addingTimeInterval(wait)
        while Date.now < deadline {
            let alert = springboard.alerts.firstMatch
            guard alert.waitForExistence(timeout: 0.5) else { continue }
            guard let label = ["允许", "Allow", "好", "OK"].first(where: { alert.buttons[$0].exists }) else {
                XCTFail("a system alert nobody answers:\n\(springboard.debugDescription)")
                return
            }
            alert.buttons[label].tap()
            deadline = Date.now.addingTimeInterval(1.5)
        }
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let png = app.screenshot()
        let a = XCTAttachment(screenshot: png)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = env["PERSONA_SHOTS"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("row-actions-\(name).png"))
        }
    }
}

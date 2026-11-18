import XCTest

/// The iPhone's guided setup (#8199, docs/extensions/iphone.md "Guided setup"), the whole way through against
/// scripts/mock/mock_hub.py with MOCK_IPHONE=setup MOCK_EXTENSIONS=1 (`TEST_RUNNER_PERSONA_IPHONE_SETUP_HUB`; the
/// simulator's model and iOS in MOCK_IPHONE_MODEL / MOCK_IPHONE_IOS, a short MOCK_COMPUTER_STEP): the extension turned on
/// in place, a Mac chosen and its iPhone helper installed, pairing starting by itself and waiting for Trust, Developer
/// Mode, Prepare, this iPhone's consent, then Try It. Every screen moves on from the hub's answer, not from a tap.
/// PERSONA_SHOTS keeps each screen, named by its look (TEST_RUNNER_PERSONA_UI_LOOKS).
final class IPhoneSetupUITests: XCTestCase {
    /// TEST_RUNNER_PERSONA_UI_LOOKS: the looks to go through, `name=args;name=args` (args such as
    /// `-ui.scheme dark -AppleLanguages (en)`); the mock starts over before each. One look with no arguments by default.
    @MainActor func testTheWholeGuide() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_IPHONE_SETUP_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_IPHONE=setup (PERSONA_IPHONE_SETUP_HUB)")
        }
        continueAfterFailure = false
        let looks = (ProcessInfo.processInfo.environment["PERSONA_UI_LOOKS"] ?? "=").split(separator: ";").map { look in
            let parts = look.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false).map(String.init)
            return (name: parts[0], args: (parts.count > 1 ? parts[1] : "").split(separator: " ").map(String.init))
        }
        for look in looks {
            try startOver(hub)
            try walk(hub, look: look.name, args: look.args)
        }
    }

    /// The mock's guided setup from the start (`GET /mock/iphone?reset=1`).
    private func startOver(_ hub: String) throws {
        let done = expectation(description: "reset")
        URLSession.shared.dataTask(with: URL(string: hub + "/mock/iphone?reset=1")!) { _, _, _ in done.fulfill() }.resume()
        wait(for: [done], timeout: 20)
    }

    @MainActor private func walk(_ hub: String, look: String, args: [String]) throws {
        self.look = look
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=" + UITestToken.device
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openIPhoneSetup", "YES"] + args
        app.launchPinned()
        dismissSystemAlerts(wait: 4) // a look switches the app icon, and SpringBoard says so over the page
        defer { app.terminate() }

        // The overview: five steps, the first waiting for the user.
        let start = app.buttons["iphone-setup-start"].firstMatch
        XCTAssertTrue(start.waitForExistence(timeout: 30), "no overview:\n\(app.debugDescription)")
        XCTAssertTrue(app.descendants(matching: .any)["iphone-setup-row-5"].exists)
        shot(app, "iphone-setup-overview")
        start.tap()

        // 1: phone control turned on in place.
        let turnOn = app.buttons["iphone-setup-turn-on"].firstMatch
        XCTAssertTrue(turnOn.waitForExistence(timeout: 10))
        shot(app, "iphone-setup-1")
        turnOn.tap()

        // 2: only Macs can be picked; the Linux computer says why not.
        let macbook = app.buttons["iphone-setup-computer-macbook"].firstMatch
        XCTAssertTrue(macbook.waitForExistence(timeout: 15), "no computer list:\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["iphone-setup-computer-server"].firstMatch.isEnabled, "the Linux computer can't be picked")
        shot(app, "iphone-setup-2")
        macbook.tap()
        let state = app.descendants(matching: .any)["iphone-setup-state"].firstMatch
        XCTAssertTrue(waitFor(state, ["iPhone 助手程序", "iPhone helper"], timeout: 10), "installing: \(state.label)")
        shot(app, "iphone-setup-2-installing")

        // 3: pairing starts by itself once this iPhone is on the cable, and says to tap Trust.
        XCTAssertTrue(waitFor(state, ["信任", "Trust"], timeout: 30), "pairing: \(state.label)")
        shot(app, "iphone-setup-3")

        // 4: Developer Mode off, then on (the mock's clock), then Prepare.
        XCTAssertTrue(waitFor(state, ["开发者模式", "Developer Mode"], timeout: 30), "Developer Mode: \(state.label)")
        shot(app, "iphone-setup-4")
        let prepare = app.buttons["iphone-setup-prepare"].firstMatch
        XCTAssertTrue(prepare.waitForExistence(timeout: 30), "no Prepare:\n\(app.debugDescription)")
        XCTAssertTrue(state.label.contains("不会重装") || state.label.contains("Nothing is reinstalled"), "says nothing is reinstalled: \(state.label)")
        shot(app, "iphone-setup-4-prepare")
        prepare.tap()

        // 5: this iPhone's own consent, here.
        let consent = app.switches["iphone-consent"].firstMatch
        XCTAssertTrue(consent.waitForExistence(timeout: 30), "no consent switch:\n\(app.debugDescription)")
        shot(app, "iphone-setup-5")
        consent.switches.firstMatch.tap()

        // Done: Try It takes a screenshot and opens Weather.
        let tryIt = app.buttons["iphone-try"].firstMatch
        XCTAssertTrue(tryIt.waitForExistence(timeout: 20), "no Try It:\n\(app.debugDescription)")
        shot(app, "iphone-setup-done")
        tryIt.tap()
        let result = app.descendants(matching: .any)["iphone-try-result"].firstMatch
        XCTAssertTrue(waitFor(result, ["天气", "Weather"], timeout: 20), "Try It: \(result.label)")
        shot(app, "iphone-setup-tried")
    }

    /// Until e's label has one of words (the test runs in Chinese or English).
    private func waitFor(_ e: XCUIElement, _ words: [String], timeout: TimeInterval) -> Bool {
        let deadline = Date.now.addingTimeInterval(timeout)
        while Date.now < deadline {
            if e.exists, words.contains(where: { e.label.contains($0) }) { return true }
            usleep(200_000)
        }
        return false
    }

    /// SpringBoard's alerts over the app: 「你已更改“Persona”的图标」 when a look switches the icon, the notification question.
    private func dismissSystemAlerts(wait: TimeInterval = 1) {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        var deadline = Date.now.addingTimeInterval(wait)
        while Date.now < deadline {
            let alert = springboard.alerts.firstMatch
            if alert.waitForExistence(timeout: 0.5) {
                for label in ["好", "OK", "允许", "Allow"] where alert.buttons[label].exists { alert.buttons[label].tap(); break }
                deadline = Date.now.addingTimeInterval(1.5) // another may follow
            }
        }
    }

    /// The look being walked, in the screenshots' names.
    private var look = ""

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        dismissSystemAlerts()
        let full = "\(name)-\(UITestStyle.name)" + (look.isEmpty ? "" : "-" + look)
        let png = app.screenshot()
        let attachment = XCTAttachment(screenshot: png)
        attachment.name = full
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(full + ".png"))
        }
    }
}

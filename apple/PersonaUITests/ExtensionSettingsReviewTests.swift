import XCTest

/// Settings › Extensions › an extension whose newer version asks for more: Review (#3692). TestFlight 224 asked the
/// hub for the preview in one request and showed a bare spinner while the hub downloaded the package; on a slow link
/// the request timed out after two minutes with nothing said. Against a hub that runs operations the page now shows the
/// operation right under Updates (the download's bar, Review held while it runs), a download that fails says why with
/// Retry, the Update sheet opens once the package is checked, and Update installs it with the page still open.
///
/// `MOCK_EXTENSIONS=1 MOCK_EXTENSION_OPS=stall MOCK_EXTENSION_OPS_STEP=3` →
/// `TEST_RUNNER_PERSONA_EXTENSIONS_REVIEW_HUB=http://127.0.0.1:<port>`; an older hub for the page as it was:
/// `MOCK_EXTENSIONS=1 MOCK_EXTENSION_PREVIEW_DELAY=8` → `TEST_RUNNER_PERSONA_EXTENSIONS_REVIEW_OLD_HUB`. Each look (light, then
/// dark; `TEST_RUNNER_PERSONA_LOOKS` to choose) starts from `/mock/reset`. `TEST_RUNNER_PERSONA_SHOTS=<dir>` keeps the screenshots.
final class ExtensionSettingsReviewTests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var looks: [String] { (env["PERSONA_LOOKS"] ?? "light dark").split(separator: " ").map(String.init) }

    @MainActor private func launch(_ hub: String, look: String, extra: [String] = [], page: Bool = true) -> XCUIApplication {
        _ = try? Data(contentsOf: URL(string: hub + "/mock/reset")!)
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            for label in ["允许", "Allow"] where alert.buttons[label].exists { alert.buttons[label].tap(); return true }
            return false
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-ui.scheme", look] + (page ? ["-openExtension", "example-gateway"] : []) + extra
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 3) { springboard.buttons[label].tap() }
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String, look: String) {
        guard let dir = env["PERSONA_SHOTS"] else { return }
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "ext-review-\(name)-\(UITestStyle.name)-\(look).png"))
    }

    @MainActor private func scrollTo(_ el: XCUIElement, in app: XCUIApplication, swipes: Int = 10) {
        for _ in 0..<swipes where !(el.exists && el.isHittable) { app.swipeUp() }
    }

    /// The Review button, scrolled to, on the extension's page.
    @MainActor private func reviewButton(in app: XCUIApplication) -> XCUIElement {
        XCTAssertTrue(app.switches["plugin-enabled"].firstMatch.waitForExistence(timeout: 15), "no extension page:\n\(app.debugDescription)")
        let review = app.buttons["plugin-upgrade"].firstMatch
        scrollTo(review, in: app)
        XCTAssertTrue(review.isHittable, "no Review button:\n\(app.debugDescription)")
        return review
    }

    @MainActor private func waitFor(_ el: XCUIElement, value want: String, timeout: TimeInterval) -> Bool {
        let end = Date.now.addingTimeInterval(timeout)
        while Date.now < end {
            if el.exists, el.value as? String == want { return true }
            usleep(150_000)
        }
        return false
    }

    @MainActor func testReviewShowsTheDownloadAFailureAndTheSheet() throws {
        guard let hub = env["PERSONA_EXTENSIONS_REVIEW_HUB"] else {
            throw XCTSkip("needs mock_hub.py with MOCK_EXTENSIONS=1 MOCK_EXTENSION_OPS=stall (PERSONA_EXTENSIONS_REVIEW_HUB)")
        }
        for look in looks {
            let app = launch(hub, look: look)
            let review = reviewButton(in: app)
            review.tap()

            // The hub downloads in the background: the operation's card, its download step going, Review held.
            let card = app.descendants(matching: .any)["extension-operation"]
            XCTAssertTrue(card.waitForExistence(timeout: 10), "\(look): no operation on the page:\n\(app.debugDescription)")
            let download = app.descendants(matching: .any)["extension-operation-step-download"]
            scrollTo(download, in: app)
            XCTAssertTrue(waitFor(download, value: "running", timeout: 5), "\(look): the download is not going:\n\(app.debugDescription)")
            shot(app, "downloading", look: look)
            // Rows off the screen are not in the tree until scrolled to: Cancel sits under the eight steps.
            let cancel = app.buttons["extension-operation-cancel"]
            scrollTo(cancel, in: app)
            XCTAssertTrue(cancel.exists, "\(look): no Cancel while it downloads:\n\(app.debugDescription)")
            for _ in 0..<4 where !review.isHittable { app.swipeDown() }
            XCTAssertFalse(review.isEnabled, "\(look): Review can start another while one downloads")

            // It stalls: the step, why, and Retry, instead of a spinner that never ends.
            scrollTo(card, in: app)
            XCTAssertTrue(waitFor(card, value: "failed", timeout: 30), "\(look): no failure:\n\(app.debugDescription)")
            let retry = app.buttons["extension-operation-retry"]
            scrollTo(retry, in: app)
            XCTAssertTrue(retry.exists, "\(look): no Retry:\n\(app.debugDescription)")
            let error = app.descendants(matching: .any)["extension-operation-error"]
            XCTAssertTrue(error.exists && error.label.contains("停滞"), "\(look): no reason: \(error.label)")
            shot(app, "failed", look: look)

            // Retry downloads it again; checked, the Update sheet opens by itself.
            retry.tap()
            let update = app.buttons["market-confirm"]
            XCTAssertTrue(update.waitForExistence(timeout: 45), "\(look): no Update sheet once it was checked:\n\(app.debugDescription)")
            XCTAssertTrue(app.staticTexts["新版本多要的"].exists, "\(look): what it asks for more is not shown")
            shot(app, "consent", look: look)

            // Update: the sheet closes at once, the same operation installs, and the page says it is up to date.
            update.tap()
            XCTAssertTrue(update.waitForNonExistence(timeout: 10), "\(look): the sheet stayed after Update:\n\(app.debugDescription)")
            XCTAssertTrue(review.waitForNonExistence(timeout: 20), "\(look): still offered after it went in:\n\(app.debugDescription)")
            XCTAssertFalse(app.staticTexts["有新版本 1.1.0"].exists, "\(look): 1.1.0 is still the newer version")
            XCTAssertTrue(card.waitForNonExistence(timeout: 5), "\(look): the card stays after it went through")
            XCTAssertTrue(app.switches["plugin-auto-upgrade"].firstMatch.exists, "\(look): Settings closed after the update")
            shot(app, "updated", look: look)
            app.terminate()
        }
    }

    /// The card at the largest text size, on this page (Review) and on the market page (Install), light and dark: the
    /// download going, then why it stopped and Retry, with the title, its line and the steps wrapped, not cut; and the
    /// market page's header, its name and line wrapped with the state under them (#4058).
    @MainActor func testTheCardAtTheLargestText() throws {
        guard let hub = env["PERSONA_EXTENSIONS_REVIEW_HUB"] else {
            throw XCTSkip("needs mock_hub.py with MOCK_EXTENSIONS=1 MOCK_EXTENSION_OPS=stall (PERSONA_EXTENSIONS_REVIEW_HUB)")
        }
        let large = ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"]
        for look in looks {
            for page in ["settings", "market"] {
                let app: XCUIApplication
                if page == "settings" {
                    app = launch(hub, look: look, extra: large)
                    XCTAssertTrue(app.switches["plugin-enabled"].firstMatch.waitForExistence(timeout: 15), "\(look): no extension page:\n\(app.debugDescription)")
                    let review = app.buttons["plugin-upgrade"].firstMatch
                    scrollTo(review, in: app, swipes: 30)
                    XCTAssertTrue(review.isHittable, "\(look): no Review button:\n\(app.debugDescription)")
                    review.tap()
                } else {
                    // The market page of an extension not installed yet: Install runs the same operation. One launch in
                    // the first run came up on the Assistant page with the page arguments unused (docs/lessons/testing.md,
                    // "-pairURL"); the page is where the test starts, not what it checks, so that launch is tried once more.
                    let args = large + ["-openMarketExtension", "3f2a9c1d0b/example-calendar"]
                    var launched = launch(hub, look: look, extra: args, page: false)
                    if !launched.buttons["market-install"].waitForExistence(timeout: 15) {
                        launched.terminate()
                        launched = launch(hub, look: look, extra: args, page: false)
                    }
                    app = launched
                    let install = app.buttons["market-install"]
                    XCTAssertTrue(install.waitForExistence(timeout: 15), "\(look): no Install:\n\(app.debugDescription)")
                    // The page's header above it: the name and its line wrap, the state under them.
                    shot(app, "market-header-large", look: look)
                    scrollTo(install, in: app)
                    install.tap()
                }
                // At this size the card starts below the screen, where a list has not made it yet: scroll while it starts,
                // then bring its title near the top so its line and steps are on the screen too.
                let card = app.descendants(matching: .any)["extension-operation"]
                let shown = Date.now.addingTimeInterval(15)
                while Date.now < shown, !(card.exists && card.frame.minY < app.frame.maxY - 80) { app.swipeUp() }
                XCTAssertTrue(card.exists, "\(look) \(page): no operation:\n\(app.debugDescription)")
                let lift = card.frame.minY - app.frame.height * 0.2
                if lift > 0 {
                    let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.9))
                    from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: 0, dy: -lift)), withVelocity: .slow, thenHoldForDuration: 0.3)
                }
                let prefix = page == "settings" ? "" : "market-"
                shot(app, prefix + "downloading-large", look: look)
                XCTAssertTrue(waitFor(card, value: "failed", timeout: 30), "\(look) \(page): no failure:\n\(app.debugDescription)")
                let retry = app.buttons["extension-operation-retry"]
                scrollTo(retry, in: app)
                XCTAssertTrue(retry.isHittable, "\(look) \(page): Retry out of reach:\n\(app.debugDescription)")
                shot(app, prefix + "failed-large", look: look)
                app.terminate()
            }
        }
    }

    /// A hub without operations answers the preview when it has the package: the page waits with a spinner as before
    /// (the only way it can), and the sheet opens when the answer comes.
    @MainActor func testAnOlderHubAnswersWhenItHasThePackage() throws {
        guard let hub = env["PERSONA_EXTENSIONS_REVIEW_OLD_HUB"] else {
            throw XCTSkip("needs mock_hub.py with MOCK_EXTENSIONS=1 MOCK_EXTENSION_PREVIEW_DELAY=8 (PERSONA_EXTENSIONS_REVIEW_OLD_HUB)")
        }
        for look in looks {
            let app = launch(hub, look: look)
            reviewButton(in: app).tap()
            XCTAssertFalse(app.descendants(matching: .any)["extension-operation"].waitForExistence(timeout: 3), "\(look): an older hub has no operations")
            app.swipeUp()
            shot(app, "older-hub-waiting", look: look)
            let update = app.buttons["market-confirm"]
            XCTAssertTrue(update.waitForExistence(timeout: 20), "\(look): no Update sheet:\n\(app.debugDescription)")
            app.navigationBars.buttons["sheet-close"].firstMatch.tap()
            XCTAssertTrue(update.waitForNonExistence(timeout: 5), "\(look): ✕ did not close the sheet")
            app.terminate()
        }
    }
}

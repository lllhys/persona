import XCTest

/// Screenshots of the mock hub's showcase pages (`MOCK_EXTENSIONS=1`, `TEST_RUNNER_PERSONA_EXTENSIONS_HUB`), for the
/// before and after of a UI change to extension pages (#827): `TEST_RUNNER_PERSONA_SHOWCASE_PAGES=styles,grid` opens
/// each page in both styles, light and dark, and keeps its top and, scrolled, the rest in `TEST_RUNNER_PERSONA_SHOTS`
/// (`showcase-<tag>-<page>-<n>-<scheme>-<style>.png`, the tag from `TEST_RUNNER_PERSONA_SHOWCASE_TAG`, "after" when
/// unset). Nothing is checked but that each page opens: it also runs against an app from before the change.
final class ExtensionShowcaseShotsUITests: XCTestCase {
    private let env = ProcessInfo.processInfo.environment

    @MainActor func testShowcasePages() throws {
        guard let hub = env["PERSONA_EXTENSIONS_HUB"] else { throw XCTSkip("Needs MOCK_EXTENSIONS=1 (PERSONA_EXTENSIONS_HUB)") }
        let pages = (env["PERSONA_SHOWCASE_PAGES"] ?? "").split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
        guard !pages.isEmpty else { throw XCTSkip("Names no pages (PERSONA_SHOWCASE_PAGES)") }
        let tag = env["PERSONA_SHOWCASE_TAG"] ?? "after"
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        for page in pages { for style in ["glass", "beautiful"] { for scheme in ["light", "dark"] {
            let app = XCUIApplication()
            app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-ui.style", style, "-ui.scheme", scheme,
                                   "-openExtension", "showcase", "-openExtensionPage", page]
            app.launchPersona()
            dismissSystemAlerts(wait: 4)
            XCTAssertTrue(app.descendants(matching: .any)["extension-ui-ready"].waitForExistence(timeout: 20),
                          "\(page) did not open:\n\(app.debugDescription)")
            dismissSystemAlerts(wait: 1)
            let name = "\(tag)-\(page)"
            shot(app, "\(name)-1-\(scheme)-\(style)")
            app.swipeUp()
            shot(app, "\(name)-2-\(scheme)-\(style)")
            app.swipeUp()
            shot(app, "\(name)-3-\(scheme)-\(style)")
            app.terminate()
        } } }
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
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("showcase-\(name).png"))
        }
    }
}

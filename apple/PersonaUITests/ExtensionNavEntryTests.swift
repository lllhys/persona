import Foundation
import XCTest

/// Extensions' main-navigation pages (nav_page) opened straight from the navigation (#2961): the new shell's drawer on
/// iPhone, the sidebar column on iPad, after the places to go, with the extension's badge. Against
/// scripts/mock/mock_hub.py with MOCK_EXTENSIONS=1 (its example-gateway › Usage, badge 3): `TEST_RUNNER_PERSONA_EXTENSIONS_HUB=
/// http://127.0.0.1:<port>`; `TEST_RUNNER_PERSONA_SHOTS=<dir>` keeps the screenshots, named by
/// `TEST_RUNNER_PERSONA_EXTENSIONS_APPEARANCE` and the style.
final class ExtensionNavEntryTests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private let entry = "sidebar-extension-example-gateway-usage"

    @MainActor private func launch(_ args: [String]) throws -> XCUIApplication {
        guard let hub = env["PERSONA_EXTENSIONS_HUB"] else { throw XCTSkip("needs mock_hub.py with MOCK_EXTENSIONS=1 (PERSONA_EXTENSIONS_HUB)") }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair] + args
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 3) { springboard.buttons[label].tap() }
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let look = env["PERSONA_EXTENSIONS_APPEARANCE"] ?? "light"
        let device = UIDevice.current.userInterfaceIdiom == .pad ? "ipad" : "iphone"
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir)
            .appending(path: "nav-entry-\(device)-\(name)-\(UITestStyle.name)-\(look).png"))
    }

    /// The entry is in the navigation, says its badge, and opens the extension's page there.
    @MainActor private func checkEntry(_ app: XCUIApplication, _ name: String) {
        let row = app.descendants(matching: .any)[entry]
        XCTAssertTrue(row.waitForExistence(timeout: 20), "the extension's entry is not in the navigation:\n\(app.debugDescription)")
        XCTAssertEqual(row.label, "用量")
        XCTAssertEqual(row.value as? String, "3", "the extension's badge")
        shot(app, name)
        row.tap()
        XCTAssertTrue(app.descendants(matching: .any)["extension-ui-ready"].waitForExistence(timeout: 15),
                      "the extension's page did not open:\n\(app.debugDescription)")
        shot(app, name + "-page")
    }

    @MainActor func testTheShellDrawerListsTheExtensionsPages() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom == .pad, "the shell's drawer is the phone's")
        let app = try launch(["-tab", "assistant", "-dev.openChat", ""])
        let face = app.buttons["assistant-sidebar-button"]
        XCTAssertTrue(face.waitForExistence(timeout: 15), app.debugDescription)
        face.tap()
        XCTAssertTrue(app.buttons["sidebar-inbox"].waitForExistence(timeout: 5), "the drawer did not open")
        checkEntry(app, "drawer")
    }

    /// The usage place's root has ☰ and no Back; the extension page it pushes has Back and no ☰.
    @MainActor func testTheUsageRootAndItsSubPageHaveOneLeadingButton() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom == .pad, "the shell's drawer is the phone's")
        let app = try launch(["-tab", "assistant", "-dev.openChat", "", "-demo.on", "NO"])
        let face = app.buttons["assistant-sidebar-button"]
        XCTAssertTrue(face.waitForExistence(timeout: 15), app.debugDescription)
        face.tap()
        checkEntry(app, "usage")
        let bar = app.navigationBars["用量"] // i18n-ignore: the mock extension's title
        XCTAssertTrue(bar.waitForExistence(timeout: 10))
        XCTAssertFalse(bar.buttons["BackButton"].exists, "the usage root has Back beside ☰")
        assertSides(bar, leading: 1, trailing: 0, "usage root")
        XCTAssertTrue(app.buttons["shell-place-sidebar"].exists)

        let detail = app.buttons["查看用量明细"] // i18n-ignore: the mock's made-up detail
        for _ in 0..<10 where !(detail.exists && detail.isHittable) {
            app.swipeUp(velocity: .slow)
        }
        XCTAssertTrue(detail.isHittable, "no usage detail row after scrolling:\n\(app.debugDescription)")
        detail.tap()
        let child = app.navigationBars["用量明细"] // i18n-ignore: the mock's detail title
        XCTAssertTrue(child.waitForExistence(timeout: 10), "the usage detail did not push:\n\(app.debugDescription)")
        XCTAssertTrue(child.buttons["BackButton"].exists)
        assertSides(child, leading: 1, trailing: 0, "usage sub-page")
        XCTAssertFalse(app.buttons["shell-place-sidebar"].exists, "the usage sub-page has ☰ beside Back")
        shot(app, "usage-subpage")
    }

    @MainActor private func assertSides(_ bar: XCUIElement, leading: Int, trailing: Int, _ name: String) {
        let center = bar.frame.midX
        let left = bar.children(matching: .button).allElementsBoundByIndex.filter { $0.frame.midX < center }
        let right = bar.children(matching: .button).allElementsBoundByIndex.filter { $0.frame.midX > center }
        XCTAssertEqual(left.count, leading, "\(name): leading controls: \(bar.debugDescription)")
        XCTAssertLessThanOrEqual(right.count, max(trailing, 1), "\(name): more than one trailing control: \(bar.debugDescription)")
    }

    @MainActor func testTheIPadSidebarListsTheExtensionsPages() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom != .pad, "the sidebar column is iPad's")
        for orientation in [UIDeviceOrientation.landscapeLeft, .portrait] {
            XCUIDevice.shared.orientation = orientation
            let app = try launch(["-tab", "assistant"])
            let row = app.descendants(matching: .any)[entry]
            // Portrait folds the column away: Chat History brings it back.
            if !row.waitForExistence(timeout: 8) {
                let toggle = app.buttons["assistant-sidebar-button"]
                if toggle.waitForExistence(timeout: 5) { toggle.tap() }
            }
            checkEntry(app, orientation == .portrait ? "sidebar-portrait" : "sidebar-landscape")
            app.terminate()
        }
        XCUIDevice.shared.orientation = .portrait
    }

    /// Landscape evidence holds the real page for an outer simctl capture; XCUITest's own landscape image can be wrong.
    @MainActor func testTheIPadUsagePageInLandscapeViaSimctl() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom != .pad, "the iPad's persistent sidebar")
        guard let dir = env["PERSONA_SHOTS"] else { throw XCTSkip("PERSONA_SHOTS chooses the evidence directory") }
        XCUIDevice.shared.orientation = .landscapeLeft
        let app = try launch(["-tab", "assistant"])
        checkEntry(app, "landscape-simctl")
        let window = app.windows.firstMatch
        XCTAssertGreaterThan(window.frame.width, window.frame.height)
        // Hold for the outer `simctl io screenshot`; XCUITest's own landscape image may be rotated or black-framed.
        sleep(30)
    }
}

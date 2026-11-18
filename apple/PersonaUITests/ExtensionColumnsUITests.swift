import Foundation
import XCTest

/// UI version 8's columns layout on iOS (#9158), on the mock hub's made-up Today page (scripts/mock/mock_hub.py with
/// `MOCK_EXTENSIONS=1 MOCK_EXTENSION_COLUMNS=1`, `TEST_RUNNER_PERSONA_COLUMNS_HUB=http://127.0.0.1:<port>`): on an
/// iPhone the page is the stack it always was, one card under another and a card's rows one to a line; on an iPad on
/// its side, two columns of cards in reading order, Needs me across the whole width with its rows two to a line, and
/// the cards of a line as tall as each other, at the default text size and the largest. Each section's link beside its title is a finger's size, says its words,
/// and pushes its page. `TEST_RUNNER_PERSONA_SHOTS=<dir>` keeps the screenshots, `TEST_RUNNER_PERSONA_COLUMNS_SCHEME`
/// (light / dark) and `TEST_RUNNER_PERSONA_COLUMNS_TEXT` (`large`: the largest accessibility size) choose the look;
/// `TEST_RUNNER_PERSONA_COLUMNS_OUTER_SHOTS=1` has the iPad's taken by the simulator (`outerShot`).
final class ExtensionColumnsUITests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var pad: Bool { UIDevice.current.userInterfaceIdiom == .pad }
    private var scheme: String { env["PERSONA_COLUMNS_SCHEME"] ?? "light" }
    private var largeText: Bool { env["PERSONA_COLUMNS_TEXT"] == "large" }
    private let entry = "sidebar-extension-example-today-today"

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
        super.tearDown()
    }

    @MainActor private func launch() throws -> XCUIApplication {
        guard let hub = env["PERSONA_COLUMNS_HUB"] else {
            throw XCTSkip("needs mock_hub.py with MOCK_EXTENSIONS=1 MOCK_EXTENSION_COLUMNS=1 (PERSONA_COLUMNS_HUB)")
        }
        continueAfterFailure = false
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-dev.openChat", "", "-demo.on", "NO", "-ui.scheme", scheme]
        if largeText { app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"] }
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 3) { springboard.buttons[label].tap() }
        return app
    }

    /// The Today page from the navigation: the iPad's sidebar column, the iPhone shell's drawer.
    @MainActor private func openToday(_ app: XCUIApplication) {
        let row = app.descendants(matching: .any)[entry]
        if !row.waitForExistence(timeout: 10) {
            let face = app.buttons["assistant-sidebar-button"]
            XCTAssertTrue(face.waitForExistence(timeout: 10), app.debugDescription)
            face.tap()
        }
        XCTAssertTrue(row.waitForExistence(timeout: 10), "the Today entry is not in the navigation:\n\(app.debugDescription)")
        row.tap()
        XCTAssertTrue(app.descendants(matching: .any)["extension-ui-ready"].waitForExistence(timeout: 20),
                      "the Today page did not open:\n\(app.debugDescription)")
        XCTAssertTrue(pageRow(app, "Approve the travel request").waitForExistence(timeout: 10), app.debugDescription)
    }

    /// A row of the page by its title: a button when it has an action (its label starts with the title), else its words.
    @MainActor private func pageRow(_ app: XCUIApplication, _ title: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "(elementType == %d OR elementType == %d) AND label BEGINSWITH %@",
            XCUIElement.ElementType.button.rawValue, XCUIElement.ElementType.staticText.rawValue, title)).firstMatch
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let png = app.screenshot()
        let attachment = XCTAttachment(screenshot: png)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let device = pad ? "ipad" : "iphone"
        let text = largeText ? "-ax" : ""
        let base = URL(fileURLWithPath: dir).appending(path: "today-\(device)-\(name)\(text)-\(scheme)")
        if pad, env["PERSONA_COLUMNS_OUTER_SHOTS"] == "1" {
            outerShot(base)
        } else {
            try? png.pngRepresentation.write(to: base.appendingPathExtension("png"))
        }
    }

    /// XCUITest's own image of an iPad on its side comes out turned and cut. With `PERSONA_COLUMNS_OUTER_SHOTS=1` the
    /// run's script takes the simulator's (`simctl io screenshot`) instead: the test leaves `<name>.ready` and waits
    /// until the script has left `<name>.taken`.
    @MainActor private func outerShot(_ base: URL) {
        let ready = base.appendingPathExtension("ready"), taken = base.appendingPathExtension("taken")
        try? FileManager.default.removeItem(at: taken)
        FileManager.default.createFile(atPath: ready.path, contents: nil)
        let done = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in FileManager.default.fileExists(atPath: taken.path) },
                                             object: nil)
        XCTAssertEqual(XCTWaiter().wait(for: [done], timeout: 60), .completed, "no screenshot taken of \(base.lastPathComponent)")
    }

    /// A section's link: hit as a finger's size, named by its own words.
    @MainActor private func checkLink(_ app: XCUIApplication, section: String, words: String) -> XCUIElement {
        let link = app.buttons["plugin-ui-section-link-" + section]
        XCTAssertTrue(link.waitForExistence(timeout: 10), "no link beside \(section)'s title:\n\(app.debugDescription)")
        XCTAssertEqual(link.label, words)
        XCTAssertGreaterThanOrEqual(link.frame.height, 44, "\(section)'s link is shorter than a finger: \(link.frame)")
        XCTAssertGreaterThanOrEqual(link.frame.width, 44, "\(section)'s link is narrower than a finger: \(link.frame)")
        return link
    }

    /// The link pushes its card's page, and Back comes back to the dashboard.
    @MainActor private func checkLinkPushes(_ app: XCUIApplication, _ link: XCUIElement) {
        link.tap()
        let page = app.staticTexts["Everything of this card would be listed here."]
        XCTAssertTrue(page.waitForExistence(timeout: 10), "the link pushed nothing:\n\(app.debugDescription)")
        shot(app, "pushed")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(pageRow(app, "Approve the travel request").waitForExistence(timeout: 10), "Back did not return")
    }

    /// iPhone upright: the stack, one column; the links beside the titles.
    @MainActor func testTheIPhoneDrawsOneColumn() throws {
        try XCTSkipIf(pad, "the iPhone's one column")
        let app = try launch()
        openToday(app)
        XCTAssertFalse(app.descendants(matching: .any)["extension-columns-card-needs"].exists, "a grid on an iPhone upright")
        let all = checkLink(app, section: "needs", words: "All")
        shot(app, "top")
        // Needs me's rows one to a line, however many it asks for side by side.
        let first = pageRow(app, "Approve the travel request"), second = pageRow(app, "Reply about the design review")
        XCTAssertTrue(second.exists)
        XCTAssertGreaterThanOrEqual(second.frame.minY, first.frame.maxY - 1, "Needs me's rows side by side on an iPhone")
        XCTAssertEqual(second.frame.minX, first.frame.minX, accuracy: 1, "Needs me's rows not in one column")
        // The next card under the first one.
        let calendar = pageRow(app, "Weekly sync")
        for _ in 0..<6 where !(calendar.exists && calendar.isHittable) { app.swipeUp(velocity: .slow) }
        XCTAssertGreaterThan(calendar.frame.minY, all.frame.maxY)
        let link = checkLink(app, section: "calendar", words: "Calendar")
        checkLinkPushes(app, link)
        // The last card at the end of the one column.
        let last = pageRow(app, "Docs sweep")
        for _ in 0..<12 where !(last.exists && last.isHittable) { app.swipeUp(velocity: .slow) }
        XCTAssertTrue(last.isHittable, "Running Agents is not at the end:\n\(app.debugDescription)")
        shot(app, "end")
    }

    /// iPad on its side: two columns of cards in reading order; Needs me across the whole width, its rows two to a line;
    /// the cards of a line as tall as each other.
    @MainActor func testTheIPadOnItsSideDrawsColumns() throws {
        try XCTSkipIf(!pad, "the iPad's columns")
        XCUIDevice.shared.orientation = .landscapeLeft
        let app = try launch()
        openToday(app)
        // The grid's cards (the page's scroll view carries the page's own identifier, extension-ui-ready).
        let grid = app.descendants(matching: .any)["extension-columns-card-needs"]
        let window = app.windows.firstMatch
        XCTAssertGreaterThan(window.frame.width, window.frame.height, "not on its side")
        func card(_ key: String) -> XCUIElement { app.descendants(matching: .any)["extension-columns-card-" + key] }
        let needs = card("needs"), calendar = card("calendar"), lark = card("lark"), wechat = card("wechat"), agents = card("agents")
        // At the largest text too: the interface stops growing at xxxLarge, where a column keeps about 446 pt, and the
        // 13-inch iPad's detail column holds two.
        XCTAssertTrue(grid.waitForExistence(timeout: 10), "no grid on an iPad on its side:\n\(app.debugDescription)")
        XCTAssertTrue(needs.waitForExistence(timeout: 10) && calendar.exists && lark.exists, app.debugDescription)
        shot(app, "landscape")

        // Needs me spans: as wide as Calendar and Lark today with the gap between them.
        XCTAssertEqual(needs.frame.minX, calendar.frame.minX, accuracy: 1)
        XCTAssertEqual(needs.frame.maxX, lark.frame.maxX, accuracy: 1, "Needs me is not as wide as the line under it")
        // Calendar | Lark today on one line, under Needs me, in reading order, as tall as each other (Lark today has a row
        // fewer: it is stretched, not equal by chance).
        XCTAssertGreaterThan(calendar.frame.minY, needs.frame.maxY)
        XCTAssertEqual(calendar.frame.minY, lark.frame.minY, accuracy: 1, "Calendar and Lark today are not on one line")
        XCTAssertLessThan(calendar.frame.maxX, lark.frame.minX, "Lark today is not after Calendar")
        XCTAssertEqual(calendar.frame.height, lark.frame.height, accuracy: 1, "the line's cards are not as tall as each other")
        XCTAssertEqual(calendar.frame.width, lark.frame.width, accuracy: 1)
        // Needs me's rows two to a line.
        let first = pageRow(app, "Approve the travel request"), second = pageRow(app, "Reply about the design review")
        XCTAssertEqual(first.frame.minY, second.frame.minY, accuracy: 1, "Needs me's rows are not side by side")
        XCTAssertGreaterThan(second.frame.minX, first.frame.maxX)
        // The next line: WeChat | Running Agents, the same width as the line above.
        for _ in 0..<6 where !(agents.exists && agents.isHittable) { app.swipeUp(velocity: .slow) }
        XCTAssertTrue(wechat.exists && agents.exists, app.debugDescription)
        XCTAssertEqual(wechat.frame.minY, agents.frame.minY, accuracy: 1)
        XCTAssertEqual(wechat.frame.height, agents.frame.height, accuracy: 1)
        XCTAssertEqual(wechat.frame.width, calendar.frame.width, accuracy: 1)
        if !needs.isHittable { app.swipeDown(velocity: .fast) }

        let link = checkLink(app, section: "calendar", words: "Calendar")
        _ = checkLink(app, section: "needs", words: "All")
        checkLinkPushes(app, link)
        XCTAssertTrue(grid.waitForExistence(timeout: 10), "Back did not bring the grid back")
    }
}

import XCTest

/// iPad (docs/apple/ios.md「iPad」): the app runs natively (not the phone-sized window), the lists sit in a sidebar wide
/// enough for their cards, in portrait and landscape. Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB), on an iPad
/// simulator; skipped on iPhone. TEST_RUNNER_PERSONA_SHOTS=<dir> keeps a screenshot of each.
final class IPadLayoutTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard UIDevice.current.userInterfaceIdiom == .pad else { throw XCTSkip("iPad only") }
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-dev.openChat", "", "-openAgentsFresh", "YES"]
    }

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
        super.tearDown()
    }

    @MainActor private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("ipad-\(name).png"))
    }

    @MainActor func testFillsTheScreenWithASidebar() {
        for (name, orientation) in [("portrait", UIDeviceOrientation.portrait), ("landscape", .landscapeLeft)] {
            XCUIDevice.shared.orientation = orientation
            app.launchPinned()
            let empty = app.staticTexts["选一个 Agent"]
            XCTAssertTrue(empty.waitForExistence(timeout: 20), "no detail column in \(name):\n\(app.debugDescription)")
            let window = app.windows.firstMatch.frame
            // Native: the window is the screen, not a phone-sized box in the middle.
            XCTAssertGreaterThan(window.width, 700, "\(name): window \(window)")
            // The column (the Agents tab's part of the one sidebar, #4206) is wider than the system's narrow sidebar, and
            // the detail column keeps most of the rest.
            let column = app.descendants(matching: .any)["unified-sidebar"].firstMatch
            XCTAssertTrue(column.waitForExistence(timeout: 5), "\(name): no column:\n\(app.debugDescription)")
            XCTAssertGreaterThanOrEqual(column.frame.maxX, 330, "\(name): sidebar too narrow (\(column.frame))")
            XCTAssertGreaterThan(empty.frame.minX, column.frame.maxX - 1, "\(name): the detail column is beside the list")
            shot("agents-\(name)")
            // A chat opens beside the list (the rows below the Needs You cards: scrolled to, the list builds them lazily).
            let row = app.buttons.matching(identifier: "agents.row").firstMatch
            for _ in 0..<8 where !(row.exists && row.isHittable) { column.swipeUp(velocity: .slow) }
            row.tap()
            XCTAssertTrue(empty.waitForNonExistence(timeout: 10), "\(name): the chat did not open in the detail column")
            XCTAssertTrue(column.exists, "\(name): the list went away when the chat opened")
            shot("chat-\(name)")
            // TEST_RUNNER_PERSONA_HOLD=<seconds>: stay on each page so `simctl io screenshot` can take it (XCUITest's own
            // screenshot comes out turned in landscape).
            if let hold = ProcessInfo.processInfo.environment["PERSONA_HOLD"].flatMap(UInt32.init) { sleep(hold) }
            app.terminate()
        }
    }

    /// The list column reaches the top of the screen (the strip under the status bar has its color), and when the
    /// column is closed the strip is the detail's again (2026-10-31: the list looked cut off at the top).
    /// Machines from the Agents menu opens in the detail column, its cards side by side; a card opens the machine there too,
    /// the list column staying (docs/server/machines.md「App」).
    @MainActor func testMachinesInTheDetailColumn() {
        XCUIDevice.shared.orientation = .landscapeLeft
        app.launchPinned()
        let menu = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "分组")).firstMatch
        XCTAssertTrue(menu.waitForExistence(timeout: 20), app.debugDescription)
        menu.tap()
        let item = app.buttons["机器"]
        XCTAssertTrue(item.waitForExistence(timeout: 5), app.debugDescription)
        item.tap()
        // The cards, not the machine names on the Agents rows in the list column.
        func card(_ name: String) -> XCUIElement { app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name + "、")).firstMatch }
        let nas = card("NAS"), server = card("服务器")
        XCTAssertTrue(server.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(app.descendants(matching: .any)["unified-sidebar"].firstMatch.exists, "the Agents list stays in its column")
        XCTAssertEqual(nas.frame.minY, server.frame.minY, accuracy: 2, "the first two cards side by side")
        XCTAssertLessThan(nas.frame.minX, server.frame.minX, "the offline NAS first")
        shot("machines")
        server.tap()
        XCTAssertTrue(app.descendants(matching: .any)["metric-range"].firstMatch.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(app.descendants(matching: .any)["unified-sidebar"].firstMatch.exists, "still split")
        sleep(1)
        shot("machine")
    }

    @MainActor func testSidebarReachesTheTop() throws {
        XCUIDevice.shared.orientation = .portrait
        let base = app.launchArguments
        for (name, tint) in [("glass", "style"), ("glass", "coral")] {
            let style = "\(name)-\(tint)"
            app.launchArguments = base + ["-ui.style", name, "-ui.color", tint]
            app.launchPersona() // this test names its styles itself
            XCTAssertTrue(app.staticTexts["选一个 Agent"].waitForExistence(timeout: 20))
            sleep(1)
            var image = app.screenshot().image
            shot("top-\(style)")
            let scale = image.scale
            func color(_ x: CGFloat, _ y: CGFloat) -> [Int] {
                guard let cg = image.cgImage?.cropping(to: CGRect(x: x * scale, y: y * scale, width: 1, height: 1)),
                      let data = cg.dataProvider?.data, let p = CFDataGetBytePtr(data) else { return [] }
                return [Int(p[0]), Int(p[1]), Int(p[2])]
            }
            // The same to the eye: the system's glass edge over the list's top tints it a few levels.
            func near(_ a: [Int], _ b: [Int]) -> Bool { a.count == 3 && b.count == 3 && zip(a, b).allSatisfy { abs($0 - $1) <= 8 } }
            // Above the list column (in the status bar), the same color as the column under it; the detail column's
            // strip stays the detail's.
            XCTAssertTrue(near(color(8, 2), color(8, 34)), "\(style): the strip above the list (\(color(8, 2))) is not the list's color (\(color(8, 34)))")
            let right = app.windows.firstMatch.frame.width - 40
            XCTAssertTrue(near(color(right, 2), color(right, 400)), "\(style): the strip above the detail changed")
            // Close the column: what is at the top there is the detail's color again, as below it.
            // The column's own Hide Sidebar since the finger's slide (#4206), where the system's was.
            let toggle = app.buttons.matching(NSPredicate(format: "identifier IN %@ OR label IN %@", ["sidebar-fold-column", "split-hide-list"], ["隐藏边栏", "Hide Sidebar", "边栏", "Sidebar"])).firstMatch
            XCTAssertTrue(toggle.exists, "no sidebar button:\n\(app.debugDescription)")
            toggle.tap()
            sleep(1)
            image = app.screenshot().image
            XCTAssertTrue(near(color(8, 2), color(8, 400)), "\(style): the list's color stayed at the top after closing it (\(color(8, 2)))")
            app.terminate()
        }
    }
}

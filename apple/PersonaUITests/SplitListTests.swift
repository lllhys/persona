import XCTest

/// A list beside its page at regular width (iPad, iPhone Duo open; #2624, #4206): the Sidebar layout's column has one
/// width and only slides in and out. No handle resizes it, a width kept by the handle before is not read, the column
/// stays beside the page on its side, and whether it shows is remembered across launches. Run on its side (the iPad, or
/// the open Duo on its side). Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB); PERSONA_SHOTS keeps the screens.
final class SplitListTests: XCTestCase {
    private var hub = ""

    override func setUpWithError() throws {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .landscapeLeft
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)") }
        hub = h
    }

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
        super.tearDown()
    }

    @MainActor func testTheColumnHasOneWidthBesideThePage() throws {
        // A width the handle kept before (the most it allowed): not read any more.
        let app = try launch(fresh: true, extra: ["-sidebar.list.width", "480"])
        let width = listWidth(app)
        XCTAssertEqual(width, expectedWidth(app), accuracy: 2, "the column's one width")
        XCTAssertFalse(app.descendants(matching: .any)["split.divider.sidebar.list"].exists, "a handle still resizes the column")
        assertBesideThePage(app)
        shot(app, "split-one-width-landscape")
    }

    @MainActor func testHidingAndShowingIsRemembered() throws {
        let app = try launch(fresh: true)
        let width = listWidth(app)
        element(app, "sidebar-fold-column").tap() // the column's head folds it; the page's button brings it back
        XCTAssertTrue(element(app, "sidebar-main").until("exists == false OR isHittable == false"), "the column did not go")
        shot(app, "split-hidden-landscape")
        app.terminate()

        let again = try launch(fresh: false, waitForColumn: false)
        let show = showButton(again)
        XCTAssertTrue(show.waitForExistence(timeout: 10), "no Show Sidebar:\n\(again.debugDescription)")
        XCTAssertFalse(element(again, "sidebar-main").isHittable, "hidden is not kept across launches")
        show.tap()
        XCTAssertTrue(element(again, "sidebar-main").until("exists == true AND isHittable == true"), "the column did not come back")
        XCTAssertEqual(listWidth(again), width, accuracy: 2, "the column came back at another width")
        assertBesideThePage(again)
        again.terminate()

        let third = try launch(fresh: false)
        XCTAssertTrue(element(third, "sidebar-main").isHittable, "shown is not kept across launches")
        shot(third, "split-shown-again-landscape")
    }

    /// A finger slides the column (#4206, as Feishu's): from the line beside it out of the way, from the screen's edge
    /// back; a short drag snaps back; where it was left is kept across launches; it comes back at its one width, beside.
    @MainActor func testDraggingSlidesTheColumnAndIsRemembered() throws {
        let app = try launch(fresh: true)
        let width = listWidth(app)
        let main = element(app, "sidebar-main")
        // A short drag from the line: back where it was.
        drag(app, from: width, by: -60)
        XCTAssertTrue(main.until("exists == true AND isHittable == true"), "a short drag closed the column")
        // Most of the way: away.
        drag(app, from: width, by: -(width * 0.8))
        XCTAssertTrue(main.until("exists == false OR isHittable == false"), "dragged from the line, the column did not go")
        shot(app, "split-dragged-away-landscape")
        app.terminate()

        let again = try launch(fresh: false, waitForColumn: false)
        XCTAssertTrue(showButton(again).waitForExistence(timeout: 10), "no Show Sidebar:\n\(again.debugDescription)")
        XCTAssertFalse(element(again, "sidebar-main").isHittable, "dragged away is not kept across launches")
        // From the screen's edge: back.
        drag(again, from: 2, by: width * 0.8)
        XCTAssertTrue(element(again, "sidebar-main").until("exists == true AND isHittable == true"), "dragged from the edge, the column did not come")
        XCTAssertEqual(listWidth(again), width, accuracy: 2, "the column came back at another width")
        assertBesideThePage(again)
        shot(again, "split-dragged-back-landscape")
        again.terminate()

        let third = try launch(fresh: false)
        XCTAssertTrue(element(third, "sidebar-main").isHittable, "dragged back is not kept across launches")
    }

    // MARK: -

    /// A finger's drag sideways at the middle of the window's height, from `x`.
    @MainActor private func drag(_ app: XCUIApplication, from x: CGFloat, by dx: CGFloat) {
        let window = app.windows.firstMatch.frame
        let start = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: window.minX + x, dy: window.midY))
        start.press(forDuration: 0.05, thenDragTo: start.withOffset(CGVector(dx: dx, dy: 0)), withVelocity: .slow, thenHoldForDuration: 0.05)
        sleep(1)
    }

    @MainActor private func launch(fresh: Bool, extra: [String] = [], waitForColumn: Bool = true) throws -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-dev.openChat", "", "-tab", "assistant", "-app.tab", "assistant", "-app.navigation", "sidebar"]
            + (fresh ? ["-openSplitFresh", "YES"] : []) + extra
        app.launchPinned()
        guard app.windows.firstMatch.waitForExistence(timeout: 10), app.windows.firstMatch.frame.width > 900 else {
            throw XCTSkip("a list beside its page on its side only (iPad, iPhone Duo open)")
        }
        if waitForColumn {
            XCTAssertTrue(element(app, "sidebar-main").waitForExistence(timeout: 25), "no column:\n\(app.debugDescription)")
            sleep(1)
        }
        return app
    }

    @MainActor private func element(_ app: XCUIApplication, _ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    @MainActor private func showButton(_ app: XCUIApplication) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier == %@ OR label IN %@", "assistant-sidebar-button", ["显示边栏", "Show Sidebar"])).firstMatch
    }

    /// `Theme.Size.sidebar` on the iPad, `Theme.Size.sidebarPhone` on the open Duo.
    @MainActor private func expectedWidth(_ app: XCUIApplication) -> CGFloat {
        UIDevice.current.userInterfaceIdiom == .pad ? 400 : 320
    }

    /// The column's width: the Sidebar layout's column at the leading edge.
    @MainActor private func listWidth(_ app: XCUIApplication) -> CGFloat {
        sleep(1)
        let column = element(app, "unified-sidebar")
        return column.exists && column.frame.minX < 40 ? column.frame.width : 0
    }

    /// The page starts where the column ends, not under it (the column laid over a dimmed page, #4206).
    @MainActor private func assertBesideThePage(_ app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
        let face = app.buttons.matching(NSPredicate(format: "identifier IN %@", ["assistant-menu", "persona-avatar"])).firstMatch
        guard face.waitForExistence(timeout: 5) else { return XCTFail("no face on Persona's page", file: file, line: line) }
        XCTAssertGreaterThan(face.frame.minX, listWidth(app) - 1, "the page is under the column", file: file, line: line)
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }
}

private extension XCUIElement {
    func until(_ format: String, timeout: TimeInterval = 5) -> Bool {
        XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: format), object: self)], timeout: timeout) == .completed
    }
}

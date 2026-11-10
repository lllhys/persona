import XCTest

/// The line between a list and its page (#2624, regular width: iPad, iPhone Duo open): its handle drags the list wider
/// or narrower, well past the least it closes the list, and from the page's edge it brings it back; the width is
/// remembered. Agents and the assistant's history. Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB);
/// PERSONA_SHOTS keeps the screens.
final class SplitDividerTests: XCTestCase {
    private var hub = ""

    override func setUpWithError() throws {
        continueAfterFailure = false
        // Upright: another test may have left the iPad on its side. TEST_RUNNER_PERSONA_ORIENTATION=landscape: on its side
        // (the iPhone Duo open, whose synthetic drags miss upright).
        XCUIDevice.shared.orientation = ProcessInfo.processInfo.environment["PERSONA_ORIENTATION"] == "landscape" ? .landscapeLeft : .portrait
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)") }
        hub = h
    }

    @MainActor func testTheAgentsListIsDraggedWiderNarrowerAndKept() throws {
        let app = launch(agents: true, fresh: true)
        let handle = try divider(app, "agents.list")
        let start = listWidth(app)
        drag(handle, by: 60)
        let wider = listWidth(app)
        // As far as the finger went (the first drag once ran away to the most or the least).
        XCTAssertEqual(wider, start + 60, accuracy: 25, "dragged 60 right, the list follows (\(start) → \(wider))")
        shot(app, "split-agents-wider")
        drag(handle, by: -130)
        let narrower = listWidth(app)
        XCTAssertLessThan(narrower, wider - 40, "dragged left, the list is narrower (\(wider) → \(narrower))")
        shot(app, "split-agents-narrower")
        app.terminate()
        let again = launch(agents: true, fresh: false)
        _ = try divider(again, "agents.list")
        XCTAssertEqual(listWidth(again), narrower, accuracy: 2, "the width is kept across launches")
    }

    @MainActor func testDraggedPastItsLeastTheListClosesAndComesBackFromTheEdge() throws {
        let app = launch(agents: true, fresh: true)
        let handle = try divider(app, "agents.list")
        drag(handle, by: -320)
        XCTAssertTrue(wait(for: NSPredicate(format: "label == %@", "显示列表"), on: handle), "the list closed: \(handle.label)")
        XCTAssertLessThan(handle.frame.midX, 60, "the handle waits at the page's edge")
        shot(app, "split-agents-closed")
        drag(handle, by: 160)
        XCTAssertTrue(wait(for: NSPredicate(format: "label == %@", "调整列表宽度"), on: handle), "the list came back: \(handle.label)")
        XCTAssertGreaterThan(listWidth(app), 200, "the list is back at its width")
    }

    @MainActor func testTheAssistantsHistoryIsDraggedToo() throws {
        let app = launch(agents: false, fresh: true)
        let handle = try divider(app, "assistant.list")
        let before = handle.frame.midX
        drag(handle, by: 60)
        XCTAssertGreaterThan(handle.frame.midX, before + 20, "the history's edge moved right (\(before) → \(handle.frame.midX))")
        shot(app, "split-assistant-wider")
        drag(handle, by: -320)
        XCTAssertTrue(wait(for: NSPredicate(format: "label == %@", "显示列表"), on: handle), "the history closed")
        shot(app, "split-assistant-closed")
        drag(handle, by: 160)
        XCTAssertTrue(wait(for: NSPredicate(format: "label == %@", "调整列表宽度"), on: handle), "the history came back")
    }

    // MARK: -

    @MainActor private func launch(agents: Bool, fresh: Bool) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-dev.openChat", ""] + (agents ? ["-tab", "dev", "-app.tab", "chats"] : ["-tab", "assistant", "-app.tab", "assistant"])
            + (fresh ? ["-openSplitFresh", "YES"] : [])
        app.launchPinned()
        return app
    }

    @MainActor private func divider(_ app: XCUIApplication, _ key: String) throws -> XCUIElement {
        guard app.windows.firstMatch.waitForExistence(timeout: 10), app.windows.firstMatch.frame.width > 600 else {
            throw XCTSkip("a list beside its page in regular width only (iPad, iPhone Duo open)")
        }
        let handle = app.descendants(matching: .any)["split.divider." + key].firstMatch
        XCTAssertTrue(handle.waitForExistence(timeout: 25), "no divider:\n\(app.debugDescription)")
        sleep(1)
        return handle
    }

    /// The list column's width: its collection view at the leading edge.
    @MainActor private func listWidth(_ app: XCUIApplication) -> CGFloat {
        sleep(1)
        let list = app.collectionViews.allElementsBoundByIndex.first { $0.frame.minX < 40 && $0.frame.width < app.windows.firstMatch.frame.width * 0.7 }
        return list?.frame.width ?? 0
    }

    @MainActor private func drag(_ handle: XCUIElement, by dx: CGFloat) {
        let from = handle.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        from.press(forDuration: 0.15, thenDragTo: from.withOffset(CGVector(dx: dx, dy: 0)), withVelocity: .slow, thenHoldForDuration: 0.2)
        sleep(1)
    }

    @MainActor private func wait(for predicate: NSPredicate, on element: XCUIElement) -> Bool {
        XCTWaiter().wait(for: [expectation(for: predicate, evaluatedWith: element)], timeout: 5) == .completed
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let style = ProcessInfo.processInfo.environment["PERSONA_STYLE"].map { "-" + $0 } ?? ""
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + style + ".png"))
        }
    }
}

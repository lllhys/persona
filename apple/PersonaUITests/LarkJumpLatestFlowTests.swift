import XCTest

/// Jump to Latest (#1034) against the isolated history mock: back to the newest messages on the device at once, then up to
/// date from the hub without pulling a reader back down.
final class LarkJumpLatestFlowTests: XCTestCase {
    private func fixture(_ change: [String: Any]? = nil) async throws -> [String: Any] {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"],
              let url = URL(string: hub + "/test/lark-history/state") else { throw XCTSkip("needs history mock") }
        var r = URLRequest(url: url)
        if let change { r.httpMethod = "POST"; r.httpBody = try JSONSerialization.data(withJSONObject: change) }
        let (data, response) = try await URLSession.shared.data(for: r)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    @MainActor private func launch() throws -> XCUIApplication {
        continueAfterFailure = false
        let hub = try XCTUnwrap(ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"])
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock",
            "-tab", "feishu", "-demo.on", "NO", "-integrations.lark.enabled", "YES",
            "-larkTop", "NO", "-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryL"]
        app.launchPinned()
        XCTAssertTrue(app.buttons["shell-feishu"].firstMatch.waitForExistence(timeout: 20))
        app.buttons["shell-feishu"].firstMatch.tapCenter()
        return app
    }

    /// Opens the jump chat once its list row shows what the mock says (unread or not).
    @MainActor private func open(_ app: XCUIApplication, _ feed: String, unread: Bool) {
        let chat = app.buttons["feishu-chat-" + feed]
        XCTAssertTrue(chat.waitForExistence(timeout: 20))
        let shows = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in chat.label.contains("28") == unread }, object: nil)
        XCTAssertEqual(XCTWaiter.wait(for: [shows], timeout: 15), .completed, "the list row is not up to date")
        chat.tapCenter()
        app.buttons["lark.chat.more"].tapCenter()
        XCTAssertTrue(app.buttons["lark.pins.open"].waitForExistence(timeout: 15))
        app.tap() // close the menu without leaving the chat
        XCTAssertTrue(row(app, unread ? 21 : 48).waitForExistence(timeout: 15))
    }

    /// The chat's newest page goes into the app's cache (opened at its newest), then the chat has unread messages.
    @MainActor private func seeded() async throws -> (XCUIApplication, String) {
        let state = try await fixture(["reset": true, "unread": false])
        let feed = try XCTUnwrap(state["jump_feed"] as? String)
        var app = try launch()
        open(app, feed, unread: false)
        app.terminate()
        _ = try await fixture(["unread": true])
        app = try launch()
        open(app, feed, unread: true)
        return (app, feed)
    }

    @MainActor private func row(_ app: XCUIApplication, _ i: Int) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "历史记录 \(i)：")).firstMatch
    }
    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot()); a.name = name; a.lifetime = .keepAlways; add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }
    @MainActor private func wait(_ timeout: TimeInterval, _ message: String, _ ok: @escaping () -> Bool) {
        let e = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in ok() }, object: nil)
        let result = XCTWaiter.wait(for: [e], timeout: timeout)
        if result != .completed {
            let app = XCUIApplication()
            shot(app, "failed-\(name)")
            print("jump-latest failed: \(message); rows \((1...90).filter { self.row(app, $0).exists })")
        }
        XCTAssertEqual(result, .completed, message)
    }

    @MainActor func testWithNothingOfTheNewestHereItWaitsAndSaysSo() async throws {
        // Held from the start: the app's background sync cannot put the chat's newest page in its cache either.
        // A long chat: the pages that load below by themselves from the first unread do not reach its end.
        let state = try await fixture(["reset": true, "hold_latest": true, "total": 120])
        let app = try launch()
        open(app, try XCTUnwrap(state["jump_feed"] as? String), unread: true)
        let jump = app.buttons["lark.jumpLatest"]
        XCTAssertTrue(jump.waitForExistence(timeout: 5))
        jump.tapCenter()
        wait(5, "the button says it is loading") { jump.exists && (jump.value as? String) == "正在加载" }
        XCTAssertFalse(row(app, 120).exists)
        shot(app, "jump-waiting")
        _ = try await fixture(["hold_latest": false])
        wait(10, "the newest arrived") { self.row(app, 120).exists && self.row(app, 120).isHittable }
        wait(5, "at the newest the button goes") { !jump.exists }
        app.terminate()
    }

    @MainActor func testBackToTheNewestAtOnceThenNewOnesComeIn() async throws {
        let (app, _) = try await seeded()
        _ = try await fixture(["hold_latest": true])
        let jump = app.buttons["lark.jumpLatest"]
        XCTAssertTrue(jump.waitForExistence(timeout: 5))
        jump.tapCenter()
        // The hub's latest page is held: what shows is the device's own.
        wait(2, "back to the newest without the network") { self.row(app, 48).exists && self.row(app, 48).isHittable }
        shot(app, "jump-instant")
        _ = try await fixture(["arrive": 1, "hold_latest": false])
        wait(10, "the new one is added below") { self.row(app, 49).exists && self.row(app, 49).isHittable }
        wait(5, "at the newest the button goes") { !jump.exists }
        shot(app, "jump-caught-up")
        app.terminate()
    }

    @MainActor func testManyNewWhileAwayAtTheBottomShowTheLatestPage() async throws {
        let (app, _) = try await seeded()
        _ = try await fixture(["arrive": 30]) // 49…78 came while reading back there
        let jump = app.buttons["lark.jumpLatest"]
        XCTAssertTrue(jump.waitForExistence(timeout: 5))
        jump.tapCenter()
        wait(10, "the latest page at the bottom") { self.row(app, 78).exists && self.row(app, 78).isHittable }
        wait(5, "at the newest the button goes") { !jump.exists }
        shot(app, "jump-gap-at-bottom")
        app.terminate()
    }

    @MainActor func testReadingUpWhileCatchingUpIsNotPulledDown() async throws {
        let (app, _) = try await seeded()
        _ = try await fixture(["arrive": 30, "hold_latest": true])
        let jump = app.buttons["lark.jumpLatest"]
        XCTAssertTrue(jump.waitForExistence(timeout: 5))
        jump.tapCenter()
        wait(2, "at the device's newest") { self.row(app, 48).exists && self.row(app, 48).isHittable }
        app.scrollViews.firstMatch.swipeDown(velocity: .slow) // reading up again before the hub answers
        try await Task.sleep(for: .seconds(1))
        let reading = try XCTUnwrap((30...47).map { row(app, $0) }.first { $0.exists && $0.isHittable && $0.frame.minY > 200 })
        let y = reading.frame.minY
        _ = try await fixture(["hold_latest": false])
        try await Task.sleep(for: .seconds(3))
        XCTAssertEqual(reading.frame.minY, y, accuracy: 3, "pulled back down")
        XCTAssertFalse(row(app, 78).exists)
        XCTAssertTrue(jump.exists, "more is newer")
        shot(app, "jump-reading-up")
        jump.tapCenter()
        wait(20, "the newer pages load in order at the bottom") { self.row(app, 78).exists && self.row(app, 78).isHittable }
        shot(app, "jump-caught-up-in-order")
        app.terminate()
    }
}

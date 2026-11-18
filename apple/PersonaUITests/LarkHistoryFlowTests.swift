import XCTest

/// Real scroll gestures and row coordinates against the isolated history mock.
final class LarkHistoryFlowTests: XCTestCase {
    private func fixture(_ change: [String: Any]? = nil) async throws -> [String: Any] {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"],
              let url = URL(string: hub + "/test/lark-history/state") else { throw XCTSkip("needs history mock") }
        var r = URLRequest(url: url)
        if let change { r.httpMethod = "POST"; r.httpBody = try JSONSerialization.data(withJSONObject: change) }
        let (data, response) = try await URLSession.shared.data(for: r)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    @MainActor private func open(_ change: [String: Any] = [:], feed: String = "f2", waitFor newest: Int = 48,
                                 size: String = "UICTContentSizeCategoryL") async throws -> XCUIApplication {
        continueAfterFailure = false
        _ = try await fixture(["reset": true].merging(change) { _, value in value })
        let hub = try XCTUnwrap(ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"])
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock",
            "-tab", "feishu", "-demo.on", "NO", "-integrations.lark.enabled", "YES",
            "-larkTop", "NO", "-UIPreferredContentSizeCategoryName", size]
        app.launchPinned()
        XCTAssertTrue(app.buttons["shell-feishu"].firstMatch.waitForExistence(timeout: 20))
        app.buttons["shell-feishu"].firstMatch.tapCenter()
        XCTAssertTrue(app.buttons["feishu-chat-" + feed].waitForExistence(timeout: 20))
        app.buttons["feishu-chat-" + feed].tapCenter()
        app.buttons["lark.chat.more"].tapCenter()
        XCTAssertTrue(app.buttons["lark.pins.open"].waitForExistence(timeout: 15))
        app.tap() // close the menu without leaving the chat
        // At the largest text sizes, messages other suites left in this simulator's outbox can fill the whole screen
        // under the newest one: the chat being open is enough there, the test scrolls up itself.
        if size == "UICTContentSizeCategoryL" { XCTAssertTrue(row(app, newest).waitForExistence(timeout: 15)) }
        return app
    }

    @MainActor private func row(_ app: XCUIApplication, _ i: Int) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "历史记录 \(i)：")).firstMatch
    }
    @MainActor private func top(_ app: XCUIApplication) {
        let first = row(app, 29)
        for _ in 0..<7 where !first.isHittable { app.scrollViews.firstMatch.swipeDown(velocity: .slow) }
        XCTAssertTrue(first.isHittable)
    }
    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot()); a.name = name; a.lifetime = .keepAlways; add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }
    @MainActor private func wait(_ element: XCUIElement, _ predicate: String, _ value: String) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: predicate, value), object: element)
        let result = XCTWaiter.wait(for: [expectation], timeout: 10)
        if result != .completed { shot(XCUIApplication(), "failed-wait-\(name)") }
        XCTAssertEqual(result, .completed, "\(predicate) \(value)")
    }
    private func completed() async throws {
        for _ in 0..<50 {
            if (try await fixture()["completed"] as? Int ?? 0) > 0 { return }
            try await Task.sleep(for: .milliseconds(100))
        }
        XCTFail("history did not complete")
    }

    @MainActor func testAutomaticPrependKeepsVisibleMessage() async throws {
        let app = try await open(["hold": true])
        top(app)
        try await Task.sleep(for: .milliseconds(300))
        let y = row(app, 29).frame.minY
        let calls = try await fixture()["calls"] as? [String]
        XCTAssertEqual(calls, ["29"], "one request at the boundary")
        XCTAssertTrue(app.descendants(matching: .any)["lark.history.loading"].exists)
        shot(app, "history-loading")
        // Release this page only; a further request would wait.
        _ = try await fixture(["hold": false])
        try await completed()
        _ = try await fixture(["hold": true])
        try await Task.sleep(for: .seconds(1))
        let after = try await fixture()["calls"] as? [String]
        shot(app, "history-prepended")
        let visible = (1...48).filter { row(app, $0).exists && row(app, $0).isHittable }
        print("history-prepend: row29 before \(y) after \(row(app, 29).exists ? row(app, 29).frame.minY : -1) visible \(visible) calls \(after ?? [])")
        XCTAssertTrue(row(app, 29).isHittable)
        XCTAssertEqual(row(app, 29).frame.minY, y, accuracy: 3, "prepend moved the visible message")
        XCTAssertEqual(after, ["29"], "a page away from the top asks for nothing more")
        XCTAssertFalse(app.buttons["加载更早的消息"].exists)
        _ = try await fixture(["hold": false])
        app.terminate()
    }

    @MainActor func testFailureWaitsForRetryAndDuplicatePageStops() async throws {
        let app = try await open(["fail": true])
        top(app)
        let retry = app.buttons["lark.history.retry"]
        XCTAssertTrue(retry.waitForExistence(timeout: 10))
        let calls = try await fixture()["calls"] as? [String]
        try await Task.sleep(for: .seconds(1))
        let unchanged = try await fixture()["calls"] as? [String]
        XCTAssertEqual(unchanged, calls)
        shot(app, "history-retry")
        _ = try await fixture(["fail": false, "duplicate": true])
        reveal(app, retry).tapCenter()
        try await Task.sleep(for: .seconds(1))
        let tapped = try await fixture()["calls"] as? [String] ?? []
        print("history-retry: calls \(tapped) label \(retry.exists ? retry.label : "-") frame \(retry.exists ? retry.frame : .zero)")
        wait(retry, "label == %@", "暂时无法加载更早的消息")
        let stopped = try await fixture()["calls"] as? [String]
        try await Task.sleep(for: .seconds(1))
        let stillStopped = try await fixture()["calls"] as? [String]
        XCTAssertEqual(stillStopped, stopped)
        XCTAssertTrue(row(app, 29).exists)
        shot(app, "history-stopped-duplicate")
        app.terminate()
    }

    @MainActor func testOldHubAndChangedAccountShowSpecificMessages() async throws {
        // An old hub's answers (no account on the page): the old /api/v1/lark only; a messaging source always says
        // its account (#2138).
        if ProcessInfo.processInfo.environment["PERSONA_MESSAGING"] == "1" { throw XCTSkip("the old /api/v1/lark only") }
        let app = try await open(["legacy": true])
        top(app)
        let retry = app.buttons["lark.history.retry"]
        XCTAssertTrue(retry.waitForExistence(timeout: 10))
        wait(retry, "label == %@", "更新 Hub 后可加载更早的消息")
        shot(app, "history-upgrade")
        _ = try await fixture(["legacy": false, "account": "another-synthetic-account"])
        reveal(app, retry).tapCenter()
        wait(retry, "label == %@", "飞书账号已切换，请重新打开会话")
        shot(app, "history-account-changed")
        app.terminate()
    }

    /// Scrolls the header into view: a page stops up to half a screen before the top, where the bar can cover it.
    /// Under the bar it still counts as hittable, but a tap at its center goes to the bar.
    @MainActor private func reveal(_ app: XCUIApplication, _ element: XCUIElement) -> XCUIElement {
        let bar = app.navigationBars.firstMatch
        let covered = { !element.isHittable || (bar.exists && element.frame.midY < bar.frame.maxY) }
        for _ in 0..<5 where covered() { app.scrollViews.firstMatch.swipeDown(velocity: .slow) }
        // A tap on a list still moving only stops it.
        Thread.sleep(forTimeInterval: 1)
        XCTAssertFalse(covered())
        return element
    }

    @MainActor private func calls() async throws -> [String] { try await fixture()["calls"] as? [String] ?? [] }

    @MainActor func testEmptyPageAndCursorCycleStopWithoutALoop() async throws {
        for change in [["empty": true], ["cycle": true]] {
            let app = try await open(change)
            top(app)
            // A cycling page's rows stay and the reader with them: the stopped header is up at the start.
            let retry = app.buttons["lark.history.retry"]
            for _ in 0..<8 where !retry.isHittable { app.scrollViews.firstMatch.swipeDown(velocity: .slow) }
            XCTAssertTrue(retry.waitForExistence(timeout: 10))
            wait(retry, "label == %@", "暂时无法加载更早的消息")
            try await Task.sleep(for: .seconds(1.5))
            let stopped = try await calls()
            XCTAssertEqual(stopped, ["29"], "\(change): one request, then waits for a tap")
            XCTAssertTrue(row(app, change["cycle"] != nil ? 9 : 29).exists, "what was loaded stays")
            shot(app, "history-stopped-\(change.keys.first!)")
            app.terminate()
        }
    }

    @MainActor func testShortPagesFillTheScreenAndStop() async throws {
        let app = try await open(["short": true], waitFor: 48)
        // Two messages a page: it keeps asking while the top is near, one page at a time, then stops.
        var last: [String] = []
        for _ in 0..<40 {
            try await Task.sleep(for: .milliseconds(500))
            let now = try await calls()
            if now == last, now.count >= 2 { break }
            last = now
        }
        try await Task.sleep(for: .seconds(1.5))
        let settled = try await calls()
        XCTAssertEqual(settled, last, "no request loop")
        XCTAssertEqual(Set(settled).count, settled.count, "each boundary asked once")
        XCTAssertGreaterThanOrEqual(settled.count, 2)
        XCTAssertTrue(row(app, 48).isHittable, "the newest stays in view")
        shot(app, "history-short-filled")
        app.terminate()
    }

    @MainActor func testLateRepliesAfterLeavingOrAnAccountSwitchAreDropped() async throws {
        let app = try await open(["hold": true])
        top(app)
        try await Task.sleep(for: .milliseconds(300))
        let first = try await calls()
        XCTAssertEqual(first, ["29"])
        // Leave while the page is on its way, then let it answer.
        app.navigationBars.buttons["BackButton"].tapCenter()
        XCTAssertTrue(app.buttons["feishu-chat-f2"].waitForExistence(timeout: 10))
        _ = try await fixture(["hold": false])
        try await completed()
        app.buttons["feishu-chat-f2"].tapCenter()
        XCTAssertTrue(row(app, 48).waitForExistence(timeout: 15))
        try await Task.sleep(for: .seconds(1))
        let reopened = try await calls()
        print("history-reopen: calls \(reopened) row28 \(row(app, 28).exists)")
        XCTAssertEqual(reopened, ["29"], "opening at the newest asks for no history")
        XCTAssertFalse(row(app, 28).exists, "the late page did not land in the reopened chat")
        shot(app, "history-reopened")
        // The account changes while the next page is on its way: its rows never land.
        _ = try await fixture(["hold": true, "completed": 0])
        top(app)
        try await Task.sleep(for: .milliseconds(300))
        _ = try await fixture(["account": "another-synthetic-account", "hold": false])
        try await Task.sleep(for: .seconds(2))
        // Either the request is dropped as the account changes, or its reply is refused and says why.
        let retry = app.buttons["lark.history.retry"]
        if retry.exists { wait(retry, "label == %@", "飞书账号已切换，请重新打开会话") }
        XCTAssertFalse(row(app, 28).exists, "no rows from the other account's reply")
        shot(app, "history-account-switched")
        app.terminate()
    }

    @MainActor func testDirectMessageLoadsHistory() async throws {
        let app = try await open(feed: "f1")
        top(app)
        try await completed()
        try await Task.sleep(for: .seconds(1))
        XCTAssertTrue(row(app, 29).isHittable)
        let made = try await calls()
        XCTAssertEqual(made, ["29"])
        shot(app, "history-direct-message")
        app.terminate()
    }

    @MainActor func testFastScrollReachesTheStartOnePageAtATime() async throws {
        let app = try await open()
        let first = row(app, 1)
        for _ in 0..<12 where !first.isHittable {
            app.scrollViews.firstMatch.swipeDown(velocity: .fast)
            // Never a blank list on the way up.
            XCTAssertGreaterThan((1...48).filter { row(app, $0).exists }.count, 0)
        }
        XCTAssertTrue(first.isHittable, "reached the first message")
        let made = try await calls()
        XCTAssertEqual(made, ["29", "9"], "each boundary once, in order")
        XCTAssertFalse(app.buttons["lark.history.retry"].exists)
        XCTAssertFalse(app.descendants(matching: .any)["lark.history.loading"].exists, "no more history: no header")
        shot(app, "history-start-reached")
        app.terminate()
    }

    @MainActor func testArrivingMessageDoesNotMoveTheReader() async throws {
        let app = try await open()
        top(app)
        try await completed()
        try await Task.sleep(for: .seconds(1))
        let reading = row(app, 29)
        XCTAssertTrue(reading.isHittable)
        let y = reading.frame.minY
        let polled = try await fixture()["latest"] as? Int ?? 0
        _ = try await fixture(["arrive": 1])
        // The push makes the chat read its newest page again, which brings message 49 in below.
        for _ in 0..<50 {
            if (try await fixture()["latest"] as? Int ?? 0) > polled { break }
            try await Task.sleep(for: .milliseconds(100))
        }
        let after = try await fixture()["latest"] as? Int ?? 0
        XCTAssertGreaterThan(after, polled, "the arrival was read")
        try await Task.sleep(for: .seconds(1))
        XCTAssertTrue(reading.isHittable)
        XCTAssertEqual(reading.frame.minY, y, accuracy: 3, "an arrival moved what is being read")
        XCTAssertFalse(row(app, 49).isHittable, "not pulled down to the newest")
        shot(app, "history-arrival-while-reading")
        app.terminate()
    }

    @MainActor func testLargeTextKeepsTheHeaderOnOneLine() async throws {
        // An old hub's answers (no account on the page): the old /api/v1/lark only; a messaging source always says
        // its account (#2138).
        if ProcessInfo.processInfo.environment["PERSONA_MESSAGING"] == "1" { throw XCTSkip("the old /api/v1/lark only") }
        let app = try await open(["legacy": true], waitFor: 48, size: "UICTContentSizeCategoryAccessibilityXXXL")
        let retry = app.buttons["lark.history.retry"]
        // Rows are tall at this size: a quick flick each time to reach the top.
        for _ in 0..<12 where !retry.isHittable { app.scrollViews.firstMatch.swipeDown(velocity: .fast) }
        XCTAssertTrue(retry.waitForExistence(timeout: 10))
        wait(retry, "label == %@", "更新 Hub 后可加载更早的消息")
        XCTAssertLessThanOrEqual(retry.frame.maxX, app.frame.maxX)
        shot(app, "history-upgrade-large")
        app.terminate()
    }

    @MainActor func testTopicGroupKeepsTheManualButton() async throws {
        let app = try await open(["topic": true])
        let load = app.buttons["加载更早的话题"] // a topic group's own wording (#1827)
        for _ in 0..<8 where !load.isHittable { app.scrollViews.firstMatch.swipeDown(velocity: .slow) }
        XCTAssertTrue(load.waitForExistence(timeout: 10))
        try await Task.sleep(for: .seconds(1.5))
        let untouched = try await calls()
        XCTAssertEqual(untouched, [], "a topic group does not load by itself")
        shot(app, "history-topic-button")
        reveal(app, load).tapCenter()
        try await completed()
        XCTAssertTrue(row(app, 28).waitForExistence(timeout: 10))
        let made = try await calls()
        XCTAssertEqual(made, ["29"])
        app.terminate()
    }
}

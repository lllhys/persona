import XCTest

/// Interaction pushes carry buttons (NotificationActions): the app posts a look-alike local notification
/// (`-showTestNotification`), the test expands it on SpringBoard and taps 「拒绝」, and the mock hub
/// (scripts/mock/mock_hub.py, PERSONA_MOCK_HUB=http://127.0.0.1:<port>) must have received the resolve.
final class NotificationActionTests: XCTestCase {
    @MainActor func testDenyFromTheNotification() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-showTestNotification", "permission"]
        app.launchPinned()

        // The system's permission alert (worded in the simulator's language).
        let allowAlert = springboard.alerts.buttons.matching(NSPredicate(format: "label IN %@", ["允许", "Allow"])).firstMatch
        if allowAlert.waitForExistence(timeout: 12) { allowAlert.tap() }
        sleep(1)
        XCUIDevice.shared.press(.home) // the push arrives while the app is in the background

        let banner = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "需要你授权")).firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 15), "no notification banner:\n\(springboard.debugDescription)")
        banner.press(forDuration: 1.5)

        let deny = springboard.buttons["拒绝"]
        XCTAssertTrue(deny.waitForExistence(timeout: 5), "the expanded notification has no 拒绝 button")
        XCTAssertTrue(springboard.buttons["允许"].exists, "the expanded notification has no 允许 button")
        let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        shot.name = "notification-actions"
        shot.lifetime = .keepAlways
        add(shot)
        deny.tap()

        let deadline = Date.now.addingTimeInterval(10)
        var resolved: [[String: Any]] = []
        while Date.now < deadline {
            if let data = try? Data(contentsOf: URL(string: hub + "/mock/resolved")!),
               let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               let list = obj["resolved"] as? [[String: Any]], !list.isEmpty {
                resolved = list
                break
            }
            usleep(300_000)
        }
        let last = try XCTUnwrap(resolved.last, "the hub never got the resolve")
        XCTAssertEqual(last["id"] as? String, "ix_test_permission")
        XCTAssertEqual((last["resolution"] as? [String: Any])?["allow"] as? Bool, false)
    }

    /// A decision's push (#2056): its Take Recommendation answers the to-do's round from the notification. Needs the mock
    /// hub with MOCK_TODO_ASKS=1, whose PR check decision recommends an option.
    @MainActor func testTakeRecommendationFromTheNotification() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB) with MOCK_TODO_ASKS=1")
        }
        let todos = URL(string: hub + "/api/v1/todos")!
        func reviewAsk() -> [String: Any]? {
            var request = URLRequest(url: todos)
            request.setValue("Bearer mock", forHTTPHeaderField: "Authorization")
            let done = DispatchSemaphore(value: 0)
            nonisolated(unsafe) var body: Data?
            URLSession.shared.dataTask(with: request) { data, _, _ in body = data; done.signal() }.resume()
            _ = done.wait(timeout: .now() + 5)
            let obj = body.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
            let list = obj?["todos"] as? [[String: Any]] ?? []
            return list.first { $0["id"] as? String == "todo_review" }?["ask"] as? [String: Any]
        }
        guard reviewAsk() != nil else { throw XCTSkip("the mock hub lists no decision: start it with MOCK_TODO_ASKS=1") }

        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-showTestNotification", "decide"]
        app.launchPinned()

        let allowAlert = springboard.alerts.buttons.matching(NSPredicate(format: "label IN %@", ["允许", "Allow"])).firstMatch
        if allowAlert.waitForExistence(timeout: 12) { allowAlert.tap() }
        sleep(4) // the to-dos load before the push is put together
        XCUIDevice.shared.press(.home)

        let banner = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "等你决定")).firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 15), "no notification banner:\n\(springboard.debugDescription)")
        banner.press(forDuration: 1.5)

        let take = springboard.buttons["按推荐来"]
        XCTAssertTrue(take.waitForExistence(timeout: 5), "the expanded notification has no 按推荐来 button")
        let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        shot.name = "notification-take-recommendation"
        shot.lifetime = .keepAlways
        add(shot)
        take.tap()

        // Taking it answers the round: the hub drops the to-do's ask.
        let deadline = Date.now.addingTimeInterval(15)
        while Date.now < deadline, reviewAsk() != nil { usleep(500_000) }
        XCTAssertNil(reviewAsk(), "the hub still lists the decision: the recommendation was not taken")
    }
}

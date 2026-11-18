import XCTest

/// The system notification's real buttons and pages, against an in-memory mock only; this test does not replace checking APNs / NSE delivery on a real device.
final class ExtensionNotificationUITests: XCTestCase {
    private var environment: [String: String] { ProcessInfo.processInfo.environment }
    private var hub: String { get throws {
        guard let value = environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("需要扩展通知专用 mock") }
        return value
    } }

    private func state(reset: Bool = false) async throws -> [[String: Any]] {
        var request = URLRequest(url: URL(string: try hub + "/mock/extension-notifications")!)
        if reset { request.httpMethod = "POST"; request.httpBody = Data("{}".utf8) }
        let (data, _) = try await URLSession.shared.data(for: request)
        let result = try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        return try XCTUnwrap(result["writes"] as? [[String: Any]])
    }

    private func waitUntilReady() async throws {
        let until = Date.now.addingTimeInterval(120)
        while Date.now < until {
            let (data, _) = try await URLSession.shared.data(from: URL(string: try hub + "/mock/extension-notifications")!)
            let object = try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
            if object["ready"] as? Bool == true { return }
            try await Task.sleep(for: .milliseconds(200))
        }
        XCTFail("本地通知类别和后台监听未准备就绪")
    }

    @MainActor private func launch(_ sample: String) async throws -> (XCUIApplication, XCUIApplication) {
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + (try hub).addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-showTestNotification", sample]
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let allow = app.alerts.buttons.matching(NSPredicate(format: "label IN %@", ["允许", "Allow"])).firstMatch
        if allow.waitForExistence(timeout: 3) { allow.tap() }
        let systemAllow = springboard.alerts.buttons.matching(NSPredicate(format: "label IN %@", ["允许", "Allow"])).firstMatch
        if systemAllow.waitForExistence(timeout: 3) { systemAllow.tap() }
        try await waitUntilReady()
        XCUIDevice.shared.press(.home)
        let banner = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "扩展通知测试")).firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 15), springboard.debugDescription)
        return (app, springboard)
    }

    @MainActor private func expand(_ springboard: XCUIApplication) {
        let banner = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "扩展通知测试")).firstMatch
        banner.press(forDuration: 1.5)
        XCTAssertTrue(springboard.buttons["回复"].waitForExistence(timeout: 5), springboard.debugDescription)
        XCTAssertTrue(springboard.buttons["打开处理"].exists)
        XCTAssertTrue(springboard.buttons["停用示例"].exists)
    }

    @MainActor private func shot(_ name: String) throws {
        guard let directory = environment["PERSONA_SHOTS"] else { return }
        try XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: directory).appending(path: name + ".png"))
    }

    private func writes() async throws -> [[String: Any]] {
        let until = Date.now.addingTimeInterval(12)
        while Date.now < until {
            let recorded = try await state()
            if !recorded.isEmpty { return recorded }
            try await Task.sleep(for: .milliseconds(200))
        }
        XCTFail("mock 未收到通知操作")
        return []
    }

    @MainActor func testBackgroundTextReplyUsesOriginalStateAndDoesNotOpenAPage() async throws {
        _ = try await state(reset: true)
        let (app, springboard) = try await launch("extension-background")
        expand(springboard)
        try shot("extension-notification-buttons")
        springboard.buttons["回复"].tap()
        let send = springboard.buttons["发送"].firstMatch
        XCTAssertTrue(send.waitForExistence(timeout: 10), springboard.debugDescription)
        // The system's reply bar is a TextView on newer versions, not a TextField; the notification body's TextView is above it.
        let editors = springboard.textFields.allElementsBoundByIndex + springboard.textViews.allElementsBoundByIndex
        let field = try XCTUnwrap(editors.max { $0.frame.minY < $1.frame.minY }, "系统回复栏未出现")
        field.tap()
        field.typeText("收到")
        XCTAssertTrue(send.isEnabled)
        send.tap()
        let recorded = try await writes()
        XCTAssertEqual(recorded.count, 1)
        XCTAssertEqual(recorded[0]["action"] as? String, "reply")
        XCTAssertEqual(recorded[0]["state"] as? String, "notice-state")
        XCTAssertEqual((recorded[0]["values"] as? [String: String])?["text"], "收到")
        app.activate()
        XCTAssertFalse(app.staticTexts["通知操作已执行"].exists)
        XCTAssertFalse(app.staticTexts["从通知打开的页面"].exists)
    }

    @MainActor func testForegroundActionReplacesAnOpenSettingsSheetAndRunsOnce() async throws {
        _ = try await state(reset: true)
        let (app, springboard) = try await launch("extension-foreground")
        expand(springboard)
        springboard.buttons["打开处理"].tap()
        let toast = app.descendants(matching: .any).matching(identifier: "toast").firstMatch
        XCTAssertTrue(toast.waitForExistence(timeout: 3), "完整扩展页面也应显示动作结果 toast")
        XCTAssertTrue(toast.label.contains("示例处理完成"))
        XCTAssertTrue(toast.isHittable)
        XCTAssertTrue(app.staticTexts["通知操作已执行"].waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertTrue(app.staticTexts["原始通知状态：notice-state"].exists)
        try shot("extension-notification-foreground")
        let recorded = try await writes()
        XCTAssertEqual(recorded.filter { $0["action"] as? String == "open" }.count, 1)
        XCTAssertEqual(recorded.first?["state"] as? String, "notice-state")
        app.buttons["sheet-close"].firstMatch.tap()
        XCTAssertFalse(app.staticTexts["通知操作已执行"].exists)
    }

    @MainActor func testTappingTheNotificationDrawsThePageWithoutExecutingAButton() async throws {
        _ = try await state(reset: true)
        let (app, springboard) = try await launch("extension-tap")
        springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "扩展通知测试")).firstMatch.tap()
        XCTAssertTrue(app.staticTexts["从通知打开的页面"].waitForExistence(timeout: 15), app.debugDescription)
        let recorded = try await writes()
        XCTAssertTrue(recorded.allSatisfy { $0["action"] == nil })
        XCTAssertEqual(recorded.first?["state"] as? String, "notice-state")
        try shot("extension-notification-tap")
    }
}

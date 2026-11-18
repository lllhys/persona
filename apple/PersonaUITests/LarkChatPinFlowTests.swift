import XCTest

/// Writes only to the opt-in Pin fixture; no real Feishu state is touched.
final class LarkChatPinFlowTests: XCTestCase {
    private func fixture(_ change: [String: Any]? = nil) async throws -> [String: Any] {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"],
              let url = URL(string: hub + "/test/lark-chat-pins/state") else { throw XCTSkip("needs chat Pin mock") }
        var request = URLRequest(url: url)
        if let change { request.httpMethod = "POST"; request.httpBody = try JSONSerialization.data(withJSONObject: change) }
        let (data, response) = try await URLSession.shared.data(for: request)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }
    @MainActor private func open(large: Bool = false, change: [String: Any] = [:]) async throws -> XCUIApplication {
        continueAfterFailure = false
        _ = try await fixture(["reset": true, "live": false].merging(change) { _, v in v })
        let hub = try XCTUnwrap(ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"])
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock",
            "-tab", "feishu", "-demo.on", "NO", "-integrations.lark.enabled", "YES",
            "-UIPreferredContentSizeCategoryName", large ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launchPinned()
        XCTAssertTrue(app.buttons["shell-feishu"].firstMatch.waitForExistence(timeout: 20))
        app.buttons["shell-feishu"].firstMatch.tapCenter()
        XCTAssertTrue(app.buttons["feishu-chat-f2"].waitForExistence(timeout: 20))
        app.buttons["feishu-chat-f2"].tapCenter()
        XCTAssertTrue(app.buttons["lark.chat.more"].waitForExistence(timeout: 15))
        XCTAssertTrue(app.staticTexts["评审记录 4：这个方案需要保留。"].waitForExistence(timeout: 15))
        let initialReads = try await fixture()["reads"] as? Int
        XCTAssertEqual(initialReads, 0, "opening chat must not mark Pins read")
        return app
    }
    @MainActor private func pins(_ app: XCUIApplication) {
        // The iPhone shell's one trailing menu keeps Pins and Persona summary together.
        app.buttons["lark.chat.more"].tapCenter()
        let open = app.buttons["lark.pins.open"]
        XCTAssertTrue(open.waitForExistence(timeout: 10), "Pins is not in the one trailing menu:\n\(app.debugDescription)")
        open.tapCenter()
        XCTAssertTrue(app.buttons["lark.pins.close"].waitForExistence(timeout: 10), "Pins did not open from the one menu")
    }
    @MainActor private func reveal(_ app: XCUIApplication, _ element: XCUIElement) {
        for _ in 0..<8 where !element.isHittable { app.swipeUp(velocity: .slow) }
        XCTAssertTrue(element.isHittable)
    }
    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot()); a.name = name; a.lifetime = .keepAlways; add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }
    @MainActor func testListPaginationEncryptionAndLocate() async throws {
        let app = try await open()
        let last = app.staticTexts["评审记录 4：这个方案需要保留。"]
        let before = last.frame.minY
        pins(app)
        XCTAssertTrue(app.buttons["lark.pin.1000"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["仅持有密钥的人可见的示例正文"].exists)
        XCTAssertFalse(app.staticTexts["撤回后不应显示的示例正文"].exists)
        XCTAssertFalse(app.buttons["lark.pin.menu.1003"].exists)
        shot(app, "chat-pins-list")
        app.buttons["lark.pins.close"].tapCenter()
        XCTAssertTrue(app.buttons["lark.pins.close"].waitForNonExistence(timeout: 10))
        XCTAssertEqual(last.frame.minY, before, accuracy: 2)
        pins(app)
        reveal(app, app.buttons["lark.pin.1007"])
        shot(app, "chat-pins-next-page")
        reveal(app, app.buttons["lark.pin.1005"])
        app.buttons["lark.pin.1005"].tapCenter()
        XCTAssertTrue(app.textFields["lark.thread.box"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["话题中的被 Pin 回复"].exists)
        shot(app, "chat-pins-thread-target")
        app.buttons["sheet-close"].firstMatch.tapCenter()
        pins(app)
        XCTAssertTrue(app.buttons["lark.pin.1000"].waitForExistence(timeout: 10))
        app.buttons["lark.pin.1000"].tapCenter()
        XCTAssertTrue(app.buttons["lark.pins.close"].waitForNonExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["评审记录 1：这个方案需要保留。"].exists)
        shot(app, "chat-pins-chat-target")
        app.terminate()
    }
    @MainActor func testEmptyOfflineAndUpgradeStates() async throws {
        let app = try await open(change: ["count": 0])
        pins(app)
        XCTAssertTrue(app.staticTexts["还没有 Pin 消息"].waitForExistence(timeout: 10))
        shot(app, "chat-pins-empty")
        _ = try await fixture(["count": 8])
        XCTAssertTrue(app.buttons["lark.pin.1000"].waitForExistence(timeout: 10))
        _ = try await fixture(["offline": true])
        XCTAssertTrue(app.buttons["lark.pins.retry"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["lark.pin.1000"].exists)
        shot(app, "chat-pins-offline-keeps-rows")
        if ProcessInfo.processInfo.environment["PERSONA_MESSAGING"] != "1" { // an old hub: the old /api/v1/lark only (#2138)
            _ = try await fixture(["offline": false, "error": "hub"])
            XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "升级 Hub")).firstMatch.waitForExistence(timeout: 10))
            shot(app, "chat-pins-upgrade-hub")
        } else {
            _ = try await fixture(["offline": false])
        }
        _ = try await fixture(["error": "upstream"])
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "升级飞书网关")).firstMatch.waitForExistence(timeout: 10))
        shot(app, "chat-pins-upgrade-gateway")
        _ = try await fixture(["error": "", "account": "changed-owner"])
        XCTAssertTrue(app.staticTexts["飞书账号已切换，请重新打开会话"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["lark.pin.1000"].exists)
        shot(app, "chat-pins-account-changed")
        app.terminate()
    }
    @MainActor func testPaginationRetryAndMissingTargetKeepContext() async throws {
        let app = try await open(change: ["fail_page": true])
        pins(app)
        XCTAssertTrue(app.buttons["lark.pin.1000"].waitForExistence(timeout: 10))
        reveal(app, app.buttons["lark.pins.moreRetry"])
        shot(app, "chat-pins-page-error")
        _ = try await fixture(["fail_page": false, "live": false])
        app.buttons["lark.pins.moreRetry"].tapCenter()
        reveal(app, app.buttons["lark.pin.1007"])
        app.buttons["lark.pins.close"].tapCenter()
        pins(app)
        XCTAssertTrue(app.buttons["lark.pin.1000"].waitForExistence(timeout: 10))
        _ = try await fixture(["locate_fail": true, "live": false])
        app.buttons["lark.pin.1000"].tapCenter()
        XCTAssertTrue(app.staticTexts["暂时无法定位这条消息，当前聊天位置已保留"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["lark.pin.1000"].exists)
        shot(app, "chat-pins-locate-error")
        app.terminate()
    }
    @MainActor func testAddRemoveAndLostReceipt() async throws {
        let app = try await open()
        app.staticTexts["评审记录 4：这个方案需要保留。"].press(forDuration: 1)
        XCTAssertTrue(app.buttons["lark.pin.add"].waitForExistence(timeout: 10))
        app.buttons["lark.pin.add"].tapCenter()
        XCTAssertTrue(app.buttons["添加 Pin"].waitForExistence(timeout: 10))
        shot(app, "chat-pins-add-confirm")
        app.buttons["添加 Pin"].tapCenter()
        XCTAssertTrue(app.staticTexts["已添加 Pin"].waitForExistence(timeout: 10))
        pins(app)
        XCTAssertTrue(app.buttons["lark.pin.added"].waitForExistence(timeout: 10))
        app.buttons["lark.pin.menu.added"].tapCenter()
        app.buttons["移除 Pin"].tapCenter()
        shot(app, "chat-pins-remove-confirm")
        app.buttons["移除 Pin"].tapCenter()
        XCTAssertTrue(app.buttons["lark.pin.added"].waitForNonExistence(timeout: 10))
        _ = try await fixture(["unknown_write": true, "live": false])
        app.buttons["lark.pin.menu.1000"].tapCenter()
        app.buttons["移除 Pin"].tapCenter(); app.buttons["移除 Pin"].tapCenter()
        XCTAssertTrue(app.buttons["lark.pins.retry"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["lark.pin.1000"].exists)
        let beforeRecheck = try await fixture()["writes"] as? Int
        XCTAssertEqual(beforeRecheck, 3)
        shot(app, "chat-pins-lost-receipt")
        app.buttons["lark.pins.retry"].tapCenter()
        XCTAssertTrue(app.buttons["lark.pin.1000"].waitForNonExistence(timeout: 10))
        let afterRecheck = try await fixture()["writes"] as? Int
        XCTAssertEqual(afterRecheck, 3, "rechecking must not repeat the mutation")
        app.terminate()
    }
    @MainActor func testLargeText() async throws {
        let app = try await open(large: true)
        pins(app)
        XCTAssertTrue(app.buttons["lark.pin.1000"].waitForExistence(timeout: 10))
        shot(app, "chat-pins-large")
        app.swipeUp(velocity: .slow)
        shot(app, "chat-pins-large-scrolled")
        app.terminate()
    }
}

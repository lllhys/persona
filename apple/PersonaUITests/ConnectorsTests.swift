import XCTest

/// Settings › Connectors: this device's data, the mail accounts (moved here from the inbox's ⋯ menu), Lark and the hub's own
/// connectors (GitHub) open from the settings list. Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB).
final class ConnectorsTests: XCTestCase {
    @MainActor func testEachConnectorOpens() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openConnectors", "YES", "-dev.openChat", ""]
        app.launchPinned()
        func row(_ label: String) -> XCUIElement {
            let r = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", label)).firstMatch
            let list = app.descendants(matching: .any)["connectors-list"].firstMatch
            XCTAssertTrue(list.waitForExistence(timeout: 10))
            // Scroll only the connector list and only when needed; dragging the whole short page can dismiss Settings.
            for _ in 0..<4 where !(r.exists && r.isHittable) { list.swipeDown() }
            for _ in 0..<8 where !(r.exists && r.isHittable) { list.swipeUp() }
            XCTAssertTrue(r.exists && r.isHittable, "no connector \(label):\n\(app.debugDescription)")
            return r
        }
        func shot(_ name: String) {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/conn-\(name).png"))
        }
        func back() {
            let b = app.navigationBars.buttons.element(boundBy: 0)
            XCTAssertTrue(b.waitForExistence(timeout: 5))
            b.tap()
        }
        XCTAssertTrue(app.staticTexts["连接器"].waitForExistence(timeout: 15) || row("飞书").exists, "no 连接器 section")
        shot("list")

        row("飞书").tap()
        XCTAssertTrue(app.staticTexts["已连接"].waitForExistence(timeout: 10), "飞书 page shows no connection:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["/agent"].exists)
        shot("feishu")
        back()

        // A connector of the hub's (GET /api/v1/connectors): GitHub, signed in with a machine's gh login.
        let github = row("GitHub")
        shot("list-github")
        github.tap()
        XCTAssertTrue(app.staticTexts["alice"].waitForExistence(timeout: 10), "GitHub page shows no account:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "已连接")).firstMatch.waitForExistence(timeout: 10),
                      "GitHub must show a connected state, optionally followed by its manager name")
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "来自 MacBook 的 gh 登录")).firstMatch.exists)
        shot("github")
        // GitHub's inbox options (docs/assistant/inbox.md「连接器：GitHub」): the CI choice and the muted repositories.
        let mode = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "CI 失败")).firstMatch
        for _ in 0..<4 where !(mode.exists && mode.isHittable) { app.swipeUp() }
        XCTAssertTrue(mode.waitForExistence(timeout: 5), "no CI choice:\n\(app.debugDescription)")
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "example-org/noisy-repo")).firstMatch.exists,
                      "no muted repository")
        shot("github-inbox")
        mode.tap()
        let all = app.buttons["全部"].firstMatch
        XCTAssertTrue(all.waitForExistence(timeout: 5), "no CI choices menu")
        all.tap()
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "所有分支的 CI 失败都算")).firstMatch
            .waitForExistence(timeout: 5), "the choice did not stick")
        back()

        row("邮箱").tap()
        XCTAssertTrue(app.buttons["添加邮箱"].waitForExistence(timeout: 10), "mail page has no 添加邮箱:\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["完成"].exists, "pushed, the mail page should not have the sheet's 完成")
        shot("mail")
        back()

        row("这台 iPhone").tap()
        XCTAssertTrue(app.navigationBars["这台 iPhone"].waitForExistence(timeout: 10) || app.staticTexts["这台 iPhone"].exists, "no device page")
        XCTAssertTrue(app.switches.firstMatch.exists, "no data source toggles")
        shot("device")
    }
}

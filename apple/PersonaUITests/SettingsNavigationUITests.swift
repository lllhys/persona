import XCTest

final class SettingsNavigationUITests: XCTestCase {
    override func setUp() { continueAfterFailure = false }

    @MainActor private func launch(_ extra: [String] = [], hubKey: String = "PERSONA_SETTINGS_HUB") throws -> XCUIApplication {
        guard let hub = ProcessInfo.processInfo.environment[hubKey] else {
            throw XCTSkip("Needs an isolated mock hub in \(hubKey)")
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-dev.openChat", "", "-AppleLanguages", "(zh-Hans)", "-AppleLocale", "zh_CN"] + extra
        app.launchPinned()
        return app
    }

    @MainActor private func element(_ id: String, _ app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
    }

    @MainActor private func reveal(_ id: String, _ app: XCUIApplication) -> XCUIElement {
        let target = element(id, app)
        let list = element("settings-list", app)
        for _ in 0..<12 where !target.isHittable { list.swipeUp() }
        return target
    }

    @MainActor func testThreeGroupsAndCommonContainers() throws {
        let app = try launch()
        XCTAssertTrue(element("settings-list", app).waitForExistence(timeout: 30))
        XCTAssertTrue(element("settings-models", app).exists)
        XCTAssertTrue(element("settings-assistant", app).exists)
        XCTAssertTrue(element("settings-assistant-switch", app).exists)
        XCTAssertTrue(reveal("settings-appearance", app).exists)
        XCTAssertTrue(element("settings-group-device", app).exists)
        XCTAssertTrue(reveal("settings-pairing", app).exists)
        XCTAssertTrue(element("settings-group-hub", app).exists)
        XCTAssertTrue(element("settings-persona", app).exists)
        XCTAssertFalse(element("settings-new-chat-check", app).exists)
    }

    @MainActor func testSearchAliasesOpenADevicePage() throws {
        let app = try launch()
        XCTAssertTrue(element("settings-list", app).waitForExistence(timeout: 30))
        let field = app.searchFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText("听写")
        let result = element("settings-search-language", app)
        XCTAssertTrue(result.waitForExistence(timeout: 10))
        result.tap()
        XCTAssertTrue(element("dictation-language", app).waitForExistence(timeout: 10))
    }

    @MainActor func testSearchDeveloperAliasesAndNoResults() throws {
        let app = try launch(["-openAppearance", "search:commit"])
        XCTAssertTrue(element("settings-search-about", app).waitForExistence(timeout: 30))
        element("settings-search-about", app).tap()
        XCTAssertTrue(element("settings-diagnostics", app).waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["构建"].exists)
        element("settings-diagnostics", app).tap()
        XCTAssertTrue(app.staticTexts["构建"].waitForExistence(timeout: 10))
    }

    @MainActor func testLegacySettingsPathsStillOpen() throws {
        let app = try launch(["-openTriageSettings", "YES"])
        XCTAssertTrue(element("triage-enabled", app).waitForExistence(timeout: 30))
    }

    @MainActor func testSettingsAssistantSwitchOpensSharedList() throws {
        let app = try launch()
        let header = element("settings-assistant-switch", app)
        XCTAssertTrue(header.waitForExistence(timeout: 30), "no Settings assistant switch:\n\(app.debugDescription)")
        XCTAssertTrue(header.label.contains("的设置, "), "the full Settings title and current state: \(header.label)")
        XCTAssertFalse(header.label.contains("别的助手有事等你处理"), "Settings has no other-assistant waiting dot: \(header.label)")
        header.tap()
        XCTAssertTrue(element("assistant-switcher-list", app).waitForExistence(timeout: 10),
                      "the shared list did not open:\n\(app.debugDescription)")
        XCTAssertTrue(element("assistant-switcher-new", app).waitForExistence(timeout: 10))
        XCTAssertTrue(element("assistant-switcher-manage", app).exists)
    }

    @MainActor func testSingleAssistantSwitchHasNoDefaultOrWaitingBadge() throws {
        let app = try launch(hubKey: "PERSONA_SETTINGS_SINGLE_HUB")
        let header = element("settings-assistant-switch", app)
        XCTAssertTrue(header.waitForExistence(timeout: 30), "no single-assistant Settings switch:\n\(app.debugDescription)")
        header.tap()
        let list = element("assistant-switcher-list", app)
        XCTAssertTrue(list.waitForExistence(timeout: 10), "the single-assistant list did not open:\n\(app.debugDescription)")
        let row = element("assistant-switcher-row-assistant", app)
        XCTAssertTrue(row.waitForExistence(timeout: 10), "no current assistant row:\n\(app.debugDescription)")
        let rows = list.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH %@", "assistant-switcher-row-"))
        XCTAssertEqual(rows.count, 1, "one installed assistant means one row:\n\(app.debugDescription)")
        // AssistantSwitcherText.spoken is the row's accessibility representation of its Default chip and waiting badge.
        XCTAssertFalse(row.label.contains("默认"), "one assistant has no Default chip: \(row.label)")
        XCTAssertFalse(row.label.contains("件等你处理"), "the only, current assistant has no waiting count: \(row.label)")
    }
}

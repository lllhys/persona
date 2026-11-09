import XCTest

/// Synthetic report feed only: MOCK_TASKS=1 MOCK_TASKS_REPORTS=1 MOCK_INJECTED=1 MOCK_EXCHANGES=1
/// MOCK_TASKS_NO_ASKS=1 MOCK_TASKS_AT=5.5. The same check covers iOS exchanges and the Mac report card.
final class ReportEvidenceUITests: XCTestCase {
    override func setUpWithError() throws {
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] != nil else {
            throw XCTSkip("needs mock_hub.py with the report exchange fixture (PERSONA_MOCK_HUB)")
        }
        continueAfterFailure = false
    }

    @MainActor func testReportKeepsItsBodyWithoutAppendingEvidence() throws {
        let env = ProcessInfo.processInfo.environment
        let hub = env["PERSONA_MOCK_HUB"]!
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant", "-openChatsFresh", "YES",
                               "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001",
                               "-AppleLanguages", "(zh-Hans)", "-AppleLocale", "zh_CN"]
        #if os(macOS)
        app.launchArguments += ["-macAppearance", env["PERSONA_SHOTS_APPEARANCE"] ?? "light", "-macWindowSize", "1280x900"]
        #endif
        app.launchPinned()
        defer { app.terminate() }
        let any = app.descendants(matching: .any)
        #if os(macOS)
        let report = any.matching(identifier: "report-line").firstMatch
        #else
        let report = any.matching(identifier: "exchange.report").firstMatch
        #endif
        XCTAssertTrue(report.waitForExistence(timeout: 25), app.debugDescription)
        XCTAssertTrue(has(app, "网络层测试补完了，PR 在 https://github.com/example/weather/pull/45").exists)
        XCTAssertFalse(has(app, "actions/runs/1001").exists, "evidence-only links must not be rendered")
        XCTAssertFalse(has(app, "· https://github.com/example/weather/pull/45").exists, "the body link must not be duplicated")
        #if os(iOS)
        let expand = report.descendants(matching: .button).matching(identifier: "exchange.expand").firstMatch
        if expand.exists && expand.isHittable { expand.tap() }
        // Expansion pins the header; move down far enough to show the entire report, not just its first lines.
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.78))
            .press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.38)))
        #endif
        XCTAssertTrue(has(app, "缓存层按你说的没动。").exists, "the original report body must remain intact")
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = "report-no-evidence-" + (env["PERSONA_SHOTS_APPEARANCE"] ?? "unknown")
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = env["PERSONA_SHOTS"] {
            #if os(macOS)
            let platform = "macos"
            #else
            let platform = "ios"
            #endif
            let appearance = env["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
            try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/\(platform)-report-\(appearance).png"))
        }
    }

    private func has(_ app: XCUIApplication, _ text: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }
}

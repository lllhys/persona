import XCTest

final class ReportFoldUITests: XCTestCase {
    private var app: XCUIApplication!
    private var hub = ""
    private let chat = "macbook/a1f3c2d4-0000-4000-8000-0000000c1e07"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
        app = XCUIApplication()
    }

    private func launch(fresh: Bool) {
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", chat, "-openAllWork", "YES"] + (fresh ? ["-openChatsFresh", "YES"] : [])
        app.launchPinned()
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "report-\(name)"
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor @discardableResult
    private func see(_ e: XCUIElement, _ what: String, timeout: TimeInterval = 10) -> XCUIElement {
        XCTAssertTrue(e.waitForExistence(timeout: timeout), "missing: \(what)")
        return e
    }

    private func text(_ s: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
    }

    @MainActor private func reveal(_ result: XCUIElement) {
        for _ in 0..<4 {
            if result.isHittable && result.frame.minY > app.frame.height * 0.25 { return }
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.08, dy: 0.35))
            let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.08, dy: 0.8))
            start.press(forDuration: 0.05, thenDragTo: end)
        }
        XCTAssertTrue(result.isHittable)
    }

    @MainActor func testCompletedToolResultsAreCollapsedUntilOpened() throws {
        launch(fresh: true)
        let result = see(app.buttons.matching(identifier: "chat.subagent-result")
            .matching(NSPredicate(format: "label CONTAINS %@", "结算流程")).firstMatch, "the collapsed subagent result", timeout: 20)
        XCTAssertFalse(text("用时 1 分 48 秒").exists)
        see(text("来自 release-bot"), "the peer's message", timeout: 5)
        XCTAssertTrue(text("发版分支已经切好了").exists)
        XCTAssertFalse(text("改动：").exists)
        shot("folded")

        result.tap()
        see(text("结算回归：42 个用例全部通过"), "the report's heading")
        see(text("用时 1 分 48 秒"), "the tool result")
        shot("open")
        reveal(result)
        result.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: text("用时 1 分 48 秒"))
        waitForExpectations(timeout: 5)

        app.terminate()
        launch(fresh: false)
        see(app.buttons.matching(identifier: "chat.subagent-result").firstMatch, "the collapsed result after reopening", timeout: 20)
        XCTAssertFalse(text("用时 1 分 48 秒").exists)
    }

    /// The background subagent's notice, opened: its report folded too.
    @MainActor func testABackgroundSubagentsNoticeFoldsItsReport() throws {
        launch(fresh: true)
        let note = see(app.descendants(matching: .any).matching(identifier: "task-note").firstMatch, "the background notice", timeout: 20)
        note.tap()
        see(text("搜索和筛选的回归跑完了"), "the report's first words")
        XCTAssertFalse(text("改动：").exists, "folded")
        shot("notice-folded")
    }
}

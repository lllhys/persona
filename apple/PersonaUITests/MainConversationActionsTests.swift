import XCTest

/// The main conversation's Compact Context and New Main Chat (#1762): the avatar's menu has both on the main conversation only,
/// Compact Context shows Compacting Context… above the composer and then Context compacted, New Main Chat… asks first (Include Old Chat Summary on)
/// and lands on the empty main page. A failure stays until closed.
///   PORT=<port> MOCK_MAIN_ACTIONS=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:<port> xcodebuild test … -only-testing:PersonaUITests/MainConversationActionsTests
/// The failure test wants the mock started with MOCK_COMPACTION=failed too (TEST_RUNNER_PERSONA_MOCK_COMPACTION=failed).
final class MainConversationActionsTests: XCTestCase {
    private var hub = ""
    private let main = "server/b2e4d6f8-0000-4000-8000-000000000001"
    private let side = "server/b2e4d6f8-0000-4000-8000-000000000003"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_MAIN_ACTIONS=1 (PERSONA_MOCK_HUB)")
        }
        hub = h
    }

    @MainActor private func launch(_ open: String, _ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-openAssistant", open] + extra
        app.launchPinned()
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        let suffix = ProcessInfo.processInfo.environment["SHOT_SUFFIX"].map { "-" + $0 } ?? ""
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/persona-mainactions-\(name)\(suffix).png"))
    }

    @MainActor private func openMenu(_ app: XCUIApplication) {
        let a = app.buttons["shell-assistant"]
        XCTAssertTrue(a.waitForExistence(timeout: 15), "no avatar in the top bar:\n\(app.debugDescription)")
        a.tap()
    }

    @MainActor private func waitForMain(_ app: XCUIApplication) {
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "今天有")).firstMatch.waitForExistence(timeout: 20),
                      "the main conversation did not open:\n\(app.debugDescription)")
    }

    /// Only the main conversation has them.
    @MainActor func testNotOnASideConversation() {
        let app = launch(side)
        XCTAssertTrue(app.staticTexts["已经在日历里加好了。"].waitForExistence(timeout: 20))
        openMenu(app)
        XCTAssertTrue(app.buttons["会话信息"].waitForExistence(timeout: 3))
        XCTAssertFalse(app.buttons["压缩上下文"].exists, "压缩上下文 is the main conversation's")
        XCTAssertFalse(app.buttons["清理上下文"].exists, "清理上下文 is the main conversation's")
        XCTAssertFalse(app.buttons["新建主对话…"].exists, "新建主对话 is the main conversation's")
    }

    /// Compact Context: running, then done, above the composer.
    @MainActor func testCompact() {
        let app = launch(main)
        waitForMain(app)
        openMenu(app)
        let compact = app.buttons["压缩上下文"]
        XCTAssertTrue(compact.waitForExistence(timeout: 3), "no 压缩上下文:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["新建主对话…"].exists)
        shot(app, "menu")
        compact.tap()
        let note = app.descendants(matching: .any)["chat.compaction"]
        XCTAssertTrue(app.staticTexts["正在压缩上下文…"].waitForExistence(timeout: 5), "no running note:\n\(app.debugDescription)")
        shot(app, "running")
        XCTAssertTrue(app.staticTexts["已压缩上下文"].waitForExistence(timeout: 10), "no done note:\n\(app.debugDescription)")
        shot(app, "done")
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: note)
        XCTAssertEqual(XCTWaiter.wait(for: [gone], timeout: 10), .completed, "the done note goes after a moment")
    }

    /// A failure from just now shows with its reason and goes when closed.
    @MainActor func testFailedCloses() throws {
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_COMPACTION"] == "failed" else {
            throw XCTSkip("needs the mock with MOCK_COMPACTION=failed")
        }
        let app = launch(main)
        waitForMain(app)
        let failed = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "压缩失败：")).firstMatch
        XCTAssertTrue(failed.waitForExistence(timeout: 10), "no failure note:\n\(app.debugDescription)")
        shot(app, "failed")
        app.buttons["chat.compaction.close"].tap()
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: failed)
        XCTAssertEqual(XCTWaiter.wait(for: [gone], timeout: 5), .completed, "closing it hides it")
    }

    /// New Main Chat…: the question (carry on), then the empty main page. Last: it archives the mock's main conversation.
    @MainActor func testZNewMain() {
        let app = launch(main)
        waitForMain(app)
        openMenu(app)
        let item = app.buttons["新建主对话…"]
        XCTAssertTrue(item.waitForExistence(timeout: 3))
        item.tap()
        let carry = app.switches["newmain.carry"]
        XCTAssertTrue(carry.waitForExistence(timeout: 5), "no question:\n\(app.debugDescription)")
        XCTAssertEqual(carry.value as? String, "1", "带上旧对话的摘要 is on by default")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "旧主对话 · 至")).firstMatch.exists)
        shot(app, "confirm")
        app.buttons["newmain.confirm"].tap()
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: carry)
        XCTAssertEqual(XCTWaiter.wait(for: [gone], timeout: 8), .completed, "the question goes once it is done")
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "今天有")).firstMatch.waitForExistence(timeout: 2),
                       "the old main conversation is not on screen")
        sleep(1)
        shot(app, "after")
    }
}

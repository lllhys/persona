import XCTest

/// Remove from List on the personal agents page (#138): a row's long-press menu takes it out (no swipe actions), the toast's Undo
/// puts it back, and the folded Removed (N) section at the end brings one back with Restore. A failing source's notice shows
/// above the Agents still listed (#3216). Against
/// scripts/mock/mock_hub.py with MOCK_PERSONAL_AGENTS=1 (TEST_RUNNER_PERSONA_PERSONAL_HUB); screenshots in PERSONA_SHOTS.
final class PersonalAgentRemoveTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let a = XCTAttachment(screenshot: app.screenshot())
            a.lifetime = .keepAlways
            add(a)
        }
        super.tearDown()
    }

    private func label(_ en: String, _ zh: String) -> NSPredicate { NSPredicate(format: "label == %@ OR label == %@", en, zh) }

    private func row(_ title: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
    }

    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        let style = ProcessInfo.processInfo.environment["PERSONA_SHOTS_SUFFIX"] ?? ""
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name)\(style).png"))
    }

    /// The personal agents page in the mock's `state`, every conversation listed (the mock forgets what earlier runs removed).
    @MainActor private func open(state: String = "configured") throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_PERSONAL_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_PERSONAL_AGENTS=1 (PERSONA_PERSONAL_HUB)")
        }
        _ = try Data(contentsOf: URL(string: hub + "/mock/personal-agents?reset=1&state=" + state)!)
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-dev.openChat", ""]
        app.launchPinned()
        let personal = app.buttons["agents.personal"]
        XCTAssertTrue(personal.waitForExistence(timeout: 30), app.debugDescription)
        personal.tap()
        XCTAssertTrue(row("代码评审").waitForExistence(timeout: 15), app.debugDescription)
    }

    /// A failing source's notice shows above the Agents still listed (#3216), and neither with every source working nor on an
    /// empty list, which shows the source's hint on its own (the entry only shows with Agents, so the list empties while open).
    @MainActor func testSourceNoticeOnlyAboveListedAgents() throws {
        let notice = { self.app.descendants(matching: .any).matching(identifier: "personalAgents.sourceNotice").firstMatch }
        try open(state: "partial")
        XCTAssertTrue(notice().waitForExistence(timeout: 10), app.debugDescription)
        shot("source-notice")
        app.terminate()
        try open(state: "partial_setup")
        XCTAssertTrue(notice().waitForExistence(timeout: 10), "a source that isn't set up gets the notice too")
        shot("source-notice-setup")
        app.terminate()
        try open()
        XCTAssertFalse(notice().waitForExistence(timeout: 3), "no notice when every source works")
        shot("all-healthy")
        let hub = ProcessInfo.processInfo.environment["PERSONA_PERSONAL_HUB"]!
        _ = try Data(contentsOf: URL(string: hub + "/mock/personal-agents?state=no_accounts")!)
        let hint = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@ OR label BEGINSWITH %@",
                                                        "No personal agent account", "还没有连接个人 Agent 账号")).firstMatch
        XCTAssertTrue(hint.waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertFalse(notice().exists, "an empty list shows the hint on its own")
        shot("empty-hint")
    }

    @MainActor func testFailedPinSaveReportsAndRollsBack() throws {
        try open()
        let hub = ProcessInfo.processInfo.environment["PERSONA_PERSONAL_HUB"]!
        _ = try Data(contentsOf: URL(string: hub + "/mock/personal-agents?prefs_fail=500")!)
        let pin = app.buttons.matching(label("Pin", "置顶")).firstMatch
        row("代码评审").press(forDuration: 1)
        XCTAssertTrue(pin.waitForExistence(timeout: 5), app.debugDescription)
        pin.tap()

        let toast = app.descendants(matching: .any).matching(identifier: "toast").firstMatch
        XCTAssertTrue(toast.waitForExistence(timeout: 5), "a refused preference save must be visible")
        XCTAssertTrue(toast.label.contains("Couldn't save:") || toast.label.contains("没保存："), toast.label)
        shot("prefs-save-failed")

        row("代码评审").press(forDuration: 1)
        XCTAssertTrue(pin.waitForExistence(timeout: 5), "the refused pin was rolled back, so the menu offers Pin again")
    }

    @MainActor func testRemoveAndUndoBringsItBack() throws {
        try open()
        shot("list")
        let remove = app.buttons.matching(label("Remove from List", "从列表移除")).firstMatch
        row("代码评审").press(forDuration: 1)
        XCTAssertTrue(remove.waitForExistence(timeout: 5), app.debugDescription)
        remove.tap()
        let undo = app.buttons["toast-action"]
        XCTAssertTrue(undo.waitForExistence(timeout: 5), "the toast offers Undo")
        XCTAssertTrue(row("代码评审").waitForNonExistence(timeout: 5), "the row leaves the list")
        let back = app.navigationBars.buttons.firstMatch
        XCTAssertFalse(back.frame.intersects(app.descendants(matching: .any).matching(identifier: "toast").firstMatch.frame), "the toast is clear of Back")
        shot("removed-toast")
        undo.tap()
        XCTAssertTrue(row("代码评审").waitForExistence(timeout: 10), "Undo puts it back")
    }

    @MainActor func testMenuRemovesAndRemovedSectionRestores() throws {
        try open()
        row("菜谱整理").press(forDuration: 1)
        let remove = app.buttons.matching(label("Remove from List", "从列表移除")).firstMatch
        XCTAssertTrue(remove.waitForExistence(timeout: 5), app.debugDescription)
        shot("menu")
        remove.tap()
        XCTAssertTrue(row("菜谱整理").waitForNonExistence(timeout: 5), "the row leaves the list")
        let removed = app.descendants(matching: .any).matching(identifier: "personalAgents.removed").firstMatch
        if !removed.waitForExistence(timeout: 10) { app.swipeUp() }
        XCTAssertTrue(removed.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(removed.label.contains("1"), removed.label)
        removed.tap()
        let restore = app.buttons["personalAgents.restore"].firstMatch
        XCTAssertTrue(restore.waitForExistence(timeout: 5), app.debugDescription)
        app.swipeUp()
        shot("removed-section")
        restore.tap()
        XCTAssertTrue(removed.waitForNonExistence(timeout: 10), "nothing removed any more")
        XCTAssertTrue(row("菜谱整理").waitForExistence(timeout: 10), "Restore puts it back")
    }
}

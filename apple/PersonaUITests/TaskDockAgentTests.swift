import XCTest

/// The main conversation's agents opened by hand (#1113), in the action bar's trays (#3393): In Progress lists the
/// tasks going on, an agent linked to a hand-made to-do once as that to-do's row (/orders); its All opens the board, and
/// a tap on the row opens the agent's page in the task's sheet. Against
/// scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 and MOCK_TASKS_DOCK unset (PERSONA_MOCK_HUB;
/// PERSONA_DOCK_MODE names the screenshots). With PERSONA_SHOTS set it writes the
/// dock, the board and the agent's sheet there (PERSONA_DOCK_APPEARANCE names the simulator's appearance in the file).
final class TaskDockAgentTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let mode = env["PERSONA_DOCK_MODE"] ?? "default"
        let appearance = env["PERSONA_DOCK_APPEARANCE"] ?? "unknown"
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/dock-\(mode)-\(appearance)-\(name).png"))
    }

    @MainActor func testAgentsFollowTheTasks() throws {
        let app = XCUIApplication()
        let url = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        // the main conversation: only it lists the Agents opened by hand
        app.launchArguments = ["-pairURL", url, "-tab", "assistant", "-dev.openChat", "",
                               "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001"]
        app.launchPinned()

        // In Progress lists the tasks going on (the widget fix among them).
        let progress = app.openActionTray("running")
        let tasks = app.trayTasks
        app.waitForRows(tasks, atLeast: 1)
        XCTAssertGreaterThanOrEqual(tasks.count, 1, app.debugDescription)
        sleep(2) // the conversation above settles
        try shot(app, "dock")

        // In Progress' All: the board lists them under Needs You / Working.
        let board = app.buttons["action-tray.board"]
        XCTAssertTrue(board.waitForExistence(timeout: 5), app.debugDescription)
        board.tap()
        let row = app.buttons.matching(identifier: "task-board.agent").firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 5), app.debugDescription)
        sleep(1)
        try shot(app, "board")
        app.buttons["关闭"].firstMatch.tap()
        XCTAssertTrue(progress.waitForExistence(timeout: 5))
        sleep(1)

        // The /orders agent was opened by hand and linked to a to-do made by hand: it is that to-do's one row in In
        // Progress, never a second row of its own (#3393, #3443); the same holds for the photo sync in Needs You.
        app.openActionTray("running")
        let orders = tasks.matching(NSPredicate(format: "label CONTAINS %@", "/orders"))
        app.waitForRows(orders, atLeast: 1)
        XCTAssertEqual(orders.count, 1, app.debugDescription)
        XCTAssertEqual(app.trayAgents.matching(NSPredicate(format: "label CONTAINS %@", "/orders")).count, 0, "the /orders agent is listed besides its to-do")
        XCTAssertTrue(orders.firstMatch.label.contains("工作中"), orders.firstMatch.label)
        try shot(app, "dock-scrolled")

        // A tap opens the agent's page in the task's sheet.
        orders.firstMatch.tap()
        XCTAssertTrue(app.buttons.matching(identifier: "chat.title").firstMatch.waitForExistence(timeout: 5), app.debugDescription)
        sleep(2)
        try shot(app, "sheet")

        // Full Screen: the sheet goes down and the agent's page is pushed onto the assistant page; back comes to the
        // conversation with the dock, the sheet does not rise again.
        let expand = app.buttons["task-detail.fullscreen"]
        XCTAssertTrue(expand.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertEqual(expand.label, "全屏查看")
        expand.tap()
        XCTAssertTrue(app.buttons["关闭"].waitForNonExistence(timeout: 5), "the sheet stayed up")
        XCTAssertFalse(expand.exists, "the full page still has the sheet's 全屏查看")
        sleep(2)
        try shot(app, "fullscreen")
        XCTAssertFalse(progress.isHittable, "the action bar shows through the full page:\n\(app.debugDescription)")
        let edge = app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5)) // the system's back swipe
        edge.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.85, dy: 0.5)))
        XCTAssertTrue(progress.waitForExistence(timeout: 5) && progress.isHittable, "back did not come to the conversation:\n\(app.debugDescription)")
        sleep(1)
        XCTAssertFalse(app.buttons["关闭"].exists, "the sheet rose again after back")
        try shot(app, "back")
    }
}

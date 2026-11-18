import XCTest

/// MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 MOCK_TASK_DECISION=1: a task-review decision in the Needs You tray.
final class DirectCompletionUITests: XCTestCase {
    @MainActor func testCompletingTheDecisionRemovesItWithoutAnotherAgentTurn() async throws {
        guard UIDevice.current.userInterfaceIdiom == .phone else { throw XCTSkip("the iPhone pending tray") }
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs the task decision mock hub")
        }
        continueAfterFailure = false
        _ = try await URLSession.shared.data(from: URL(string: hub + "/mock/reset")!)
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-dev.openChat", "",
                               "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001",
                               "-AppleLanguages", "(zh-Hans)", "-AppleLocale", "zh_CN"]
        app.launchPinned()
        let pending = app.buttons["action-bar.needs-you"]
        XCTAssertTrue(pending.waitForExistence(timeout: 30), app.debugDescription)
        pending.tap()
        let tray = app.otherElements["action-tray.needs-you"].firstMatch
        app.expandTrayRows()
        let complete = tray.buttons["brief.complete.todo_task_tests"]
        let recommendation = tray.buttons["brief.accept.todo_task_tests"]
        XCTAssertTrue(complete.waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertEqual(complete.label, "直接完成：补齐网络层的单元测试")
        XCTAssertTrue(recommendation.exists)
        let open = tray.buttons["打开：补齐网络层的单元测试"]
        XCTAssertTrue(open.exists)
        // the main button is never above the others: on their row's end when all fit, else on the line under them
        XCTAssertLessThanOrEqual(open.frame.minY, recommendation.frame.minY)
        XCTAssertLessThanOrEqual(complete.frame.minY, recommendation.frame.minY)
        XCTAssertTrue(tray.buttons["decision.later.todo_task_tests"].exists)
        XCTAssertTrue(tray.buttons["decision.dismiss.todo_task_tests"].exists)
        let before = XCTAttachment(screenshot: app.screenshot())
        before.name = "Decision before direct completion"
        before.lifetime = .keepAlways
        add(before)
        complete.tap()
        XCTAssertTrue(complete.waitForNonExistence(timeout: 15), "the completed decision stayed in Needs You")
        XCTAssertFalse(recommendation.exists, "the completed decision can still resume its Agent")
        XCTAssertFalse(tray.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "补齐网络层的单元测试")).firstMatch.exists,
                       "the finished task remains in Needs You as a task or an idle Agent")
        let (data, _) = try await URLSession.shared.data(from: URL(string: hub + "/api/v1/todos")!)
        let response = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        let todos = try XCTUnwrap(response["todos"] as? [[String: Any]])
        let todo = try XCTUnwrap(todos.first { $0["id"] as? String == "todo_task_tests" })
        XCTAssertNotNil(todo["done_at"])
        XCTAssertNil(todo["ask"])
    }

    /// Later puts the decision off for this run of the app; Don't handle is the hub's, and Undo brings it back.
    @MainActor func testLaterAndDontHandleTakeTheDecisionOutAndUndoBringsItBack() async throws {
        guard UIDevice.current.userInterfaceIdiom == .phone else { throw XCTSkip("the iPhone pending tray") }
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs the task decision mock hub")
        }
        continueAfterFailure = false
        _ = try await URLSession.shared.data(from: URL(string: hub + "/mock/reset")!)
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-dev.openChat", "",
                               "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001"]
        app.launchPinned()
        let needs = app.buttons["action-bar.needs-you"]
        XCTAssertTrue(needs.waitForExistence(timeout: 30), app.debugDescription)
        needs.tap()
        let tray = app.otherElements["action-tray.needs-you"].firstMatch
        app.expandTrayRows()
        let dismiss = tray.buttons["decision.dismiss.todo_task_tests"]
        XCTAssertTrue(dismiss.waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertGreaterThanOrEqual(dismiss.frame.height, 44, "a button under a finger's height")
        try shot(app, "decision-open")
        dismiss.tap()
        XCTAssertTrue(dismiss.waitForNonExistence(timeout: 10), "the dismissed decision stayed in Needs You")
        let undo = app.buttons["toast-action"]
        XCTAssertTrue(undo.waitForExistence(timeout: 5), app.debugDescription)
        try shot(app, "decision-undo-toast")
        undo.tap()
        app.expandTrayRows()
        XCTAssertTrue(dismiss.waitForExistence(timeout: 10), "Undo did not bring the decision back")
        let later = tray.buttons["decision.later.todo_task_tests"]
        later.tap()
        XCTAssertTrue(later.waitForNonExistence(timeout: 10), "Later left the decision in Needs You")
    }

    /// With PERSONA_SHOTS set, the screen goes there (PERSONA_SHOTS_APPEARANCE names the appearance).
    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        let appearance = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/\(name)-\(appearance).png"))
    }
}

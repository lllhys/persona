import XCTest

/// The task sheet closes on a rightward swipe from the leading edge (#5603). Against scripts/mock/mock_hub.py with
/// MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB). With PERSONA_SHOTS set it writes the opened sheet there
/// (PERSONA_SHOTS_APPEARANCE names the appearance).
final class TaskSheetEdgeSwipeUITests: XCTestCase {
    private var hub = ""
    private let main = "server/b2e4d6f8-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB)")
        }
        hub = h
        continueAfterFailure = false
    }

    @MainActor func testEdgeSwipeClosesTheTaskSheet() throws {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant", "-openAssistant", main,
                               "-showTaskDetail", "todo_task_widget"]
        app.launchPinned()

        let close = app.buttons.matching(NSPredicate(format: "label IN %@", ["Close", "关闭"])).firstMatch
        XCTAssertTrue(close.waitForExistence(timeout: 25), "no task sheet:\n\(app.debugDescription)")
        sleep(2)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let appearance = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
            try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/task-sheet-\(appearance).png"))
        }

        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5))
        start.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.6, dy: 0.5)))
        let gone = NSPredicate(format: "exists == false")
        expectation(for: gone, evaluatedWith: close)
        waitForExpectations(timeout: 10)
    }
}

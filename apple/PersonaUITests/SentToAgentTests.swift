import XCTest

/// What Persona sent to its tasks' agents in the main conversation (#1542): never the user's bubble. The hand-off sent with
/// a task's card is an exchange like any later message (the old per-task list that held it was removed with the task card,
/// docs/design/task-cards.md P7); a message uses a three-line exchange with Expand; the user's own word to a task stays the user's bubble. Against
/// scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_SENT=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB). With PERSONA_SHOTS
/// set it writes the folded and open states there (PERSONA_SHOTS_APPEARANCE names the simulator's appearance in the file).
final class SentToAgentTests: XCTestCase {
    private var hub = ""
    private let main = "server/b2e4d6f8-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_SENT=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB)")
        }
        hub = h
        continueAfterFailure = false
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        let env = ProcessInfo.processInfo.environment
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let appearance = env["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/sent-\(appearance)-\(name).png"))
    }

    @MainActor func testPersonasMessagesAreExchangesAndTheHandOffIsOneToo() throws {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant", "-openAssistant", main,
                               "-openChatsFresh", "YES", "-dev.openChat", "", "-AppleLanguages", "(zh-Hans)"]
        app.launchPinned()

        let any = app.descendants(matching: .any)
        func has(_ text: String) -> XCUIElement { any.matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch }
        let first = any.matching(identifier: "exchange.routed").firstMatch
        XCTAssertTrue(first.waitForExistence(timeout: 20), "no exchange of what was sent to a task:\n\(app.debugDescription)")
        sleep(1) // the list settles at the newest
        // No card lists a task's hand-off any more (the old task card did, docs/design/task-cards.md P7), and nothing in the
        // app applies `SentToAgents.takenIn`: the hand-off is drawn like any later message, as an exchange.
        XCTAssertTrue(has("常驻内存降下来").exists, "the hand-off is not in the chat:\n\(app.debugDescription)")
        // The rework to the widget fix stays an exchange, not the removed iOS baseline's one-line disclosure.
        XCTAssertTrue(has("Persona → @修小组件横屏崩溃").exists, app.debugDescription)
        let line = any.matching(identifier: "exchange.routed").allElementsBoundByIndex.last { $0.isHittable }
        XCTAssertNotNil(line, "the rework exchange is missing")
        let expand = line?.descendants(matching: .any)["exchange.expand"].firstMatch
        XCTAssertTrue(expand?.exists == true, "the long rework is not folded to three lines")
        XCTAssertTrue(has("先补重试和超时").exists, "the user's own word to a task is gone")
        try shot(app, "folded")

        expand?.tap()
        let collapse = line?.descendants(matching: .any)["exchange.collapse"].firstMatch
        XCTAssertTrue(collapse?.waitForExistence(timeout: 5) == true, "Expand did not open the rework")
        XCTAssertTrue(has("补一张横屏的截图贴进 PR").exists, "the open exchange is not whole")
        sleep(1)
        try shot(app, "line-open")
        collapse?.tap()
        XCTAssertTrue(expand?.waitForExistence(timeout: 5) == true, "the exchange did not fold")

        app.terminate()
    }
}

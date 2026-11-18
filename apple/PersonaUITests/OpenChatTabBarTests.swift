import XCTest

/// An Agents chat opened from the list takes the whole screen: the Shell switch goes away with the push, stays
/// away under and after the terminal, and comes back on the list. The iPhone shell's (an iPad opens the chat beside
/// the list, under no switch). The chat is a working one of the default assistant's, on the server: the list shows the
/// current assistant's Agents only (#5587), and on the mock hub the MacBook's are another assistant's. Needs scripts/mock/mock_hub.py:
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:<port> xcodebuild test … -only-testing:PersonaUITests/OpenChatTabBarTests
/// The flash itself only shows on film: run it under `xcrun simctl io <udid> recordVideo` and look at the frames
/// right after the tap (docs/operations/testing.md).
final class OpenChatTabBarTests: XCTestCase {
    @MainActor func testTheTabBarLeavesWithThePushAndComesBack() throws {
        continueAfterFailure = false
        guard UIDevice.current.userInterfaceIdiom == .phone else { throw XCTSkip("the iPhone shell's switch: an iPad opens the chat beside the list") }
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)") }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "chats", "-dev.openChat", "", "-tab", "dev", "-openAgentsFresh", "YES", "-probes", "YES"]
        app.launchPinned()
        // A fresh install asks for notifications over the list.
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons
            .matching(NSPredicate(format: "label IN %@", ["允许", "Allow"])).firstMatch
        if allow.waitForExistence(timeout: 8) { allow.tap() }

        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "排查 /orders 接口变慢")).firstMatch
        for _ in 0..<8 where !(row.exists && row.isHittable) {
            app.collectionViews.firstMatch.swipeUp(velocity: .slow)
        }
        XCTAssertTrue(row.waitForExistence(timeout: 15) && row.isHittable, "the agent's row")
        let tabs = app.buttons["shell-agents"].firstMatch
        XCTAssertTrue(tabs.isHittable, "no Shell switch on the list")
        row.tap()
        let more = app.buttons["更多"].firstMatch
        XCTAssertTrue(more.waitForExistence(timeout: 10), "the chat did not open")
        sleep(1)
        XCTAssertFalse(tabs.isHittable, "the Shell switch shows over the chat")

        // The terminal comes up over the page; closing it, the chat still has the whole screen. A folded header (the slim
        // bar) has no ···: a tap on its place opens it again.
        let unfold = app.buttons.matching(NSPredicate(format: "label ENDSWITH %@", "显示标题和标签")).firstMatch
        for _ in 0..<3 where !more.isHittable {
            if unfold.exists, unfold.isHittable { unfold.tap() } else { app.swipeDown() }
            sleep(1)
        }
        more.tap()
        let terminal = app.buttons["终端"].firstMatch
        XCTAssertTrue(terminal.waitForExistence(timeout: 5), "终端 in the ··· menu")
        terminal.tap()
        let close = app.descendants(matching: .any)["terminal-close"]
        XCTAssertTrue(close.waitForExistence(timeout: 10), "the terminal did not come up")
        let tips = app.buttons["知道了"]
        if tips.waitForExistence(timeout: 3) { tips.tap() }
        close.tap()
        sleep(1)
        XCTAssertFalse(tabs.isHittable, "the Shell switch came back after the terminal")

        // Back by the edge swipe: the list, with its Shell switch.
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5))
            .press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.85, dy: 0.5)))
        sleep(2)
        XCTAssertTrue(row.isHittable, "not back on the list")
        XCTAssertTrue(tabs.isHittable, "no Shell switch back on the list")
    }
}

import XCTest

/// Back from a chat opened at launch stays at the Agents list (#1868: a turn later the list opened the chat it had just
/// closed again), whether it was opened with `-openSession` or as the remembered chat (`-dev.openChat`, a launch
/// argument that cannot be removed). Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB).
final class ChatBackTests: XCTestCase {
    private let recipe = "macbook/a1f3c2d4-0000-4000-8000-000000000007" // 「菜谱网站加搜索」

    @MainActor func testBackStaysAtTheList() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        continueAfterFailure = false
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        for opened in [["-openSession", recipe], ["-dev.openChat", recipe]] {
            let app = XCUIApplication()
            app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-holdChromeOpen", "YES"] + opened
            app.launchPinned()
            let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.buttons["允许"]
            if allow.waitForExistence(timeout: 2) { allow.tap() }
            // At regular width (an iPad, iPhone Duo open) the chat opens beside the list, with no Back to take.
            guard app.windows.firstMatch.frame.width <= 600 else { throw XCTSkip("the chat is pushed over the list at compact width only") }
            let back = app.buttons.matching(NSPredicate(format: "label == '返回'")).firstMatch // the chat's own, by the title
            XCTAssertTrue(back.waitForExistence(timeout: 20), "\(opened[0]): the chat:\n\(app.debugDescription)")
            back.tap()
            sleep(4) // the list used to open the chat again a turn after going back
            XCTAssertFalse(back.exists, "\(opened[0]): back in the chat after 返回:\n\(app.debugDescription)")
            XCTAssertTrue(app.buttons["agents.filter.all"].exists, "\(opened[0]): the Agents list:\n\(app.debugDescription)")
            app.terminate()
        }
    }
}

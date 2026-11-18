import XCTest

/// #8467: a launch onto a chat leaves the Agents list under it unbuilt until it could be seen. Going back (‹ tapped, a
/// back swipe let go halfway and then carried through) must still find the list there, drawn during the pop, not a
/// blank page. Against scripts/mock/mock_hub.py with MOCK_AGENTS=1 (PERSONA_MOCK_HUB); PERSONA_SHOTS keeps the frames
/// taken mid-pop.
final class LaunchOntoChatListTests: XCTestCase {
    private static let chat = "macbook/a1f3c2d4-0000-4000-8000-000000000007"

    @MainActor func testBackFromALaunchedChatShowsTheList() throws {
        let app = try launch()
        let back = app.buttons.matching(NSPredicate(format: "label == '返回'")).firstMatch
        XCTAssertTrue(back.waitForExistence(timeout: 20), "the chat the launch opened:\n\(app.debugDescription)")
        back.tap()
        keep(XCUIScreen.main.screenshot(), "mid-pop") // the list sliding in, not an empty page
        XCTAssertTrue(app.buttons["agents.filter.all"].waitForExistence(timeout: 2), "the Agents list after ‹:\n\(app.debugDescription)")
        keep(XCUIScreen.main.screenshot(), "list")
    }

    @MainActor func testHalfSwipeThenBack() throws {
        let app = try launch()
        let back = app.buttons.matching(NSPredicate(format: "label == '返回'")).firstMatch
        XCTAssertTrue(back.waitForExistence(timeout: 20), "the chat the launch opened:\n\(app.debugDescription)")
        swipe(app, to: 0.4) // let go halfway: the chat stays
        XCTAssertTrue(back.waitForExistence(timeout: 3), "still the chat after a half swipe:\n\(app.debugDescription)")
        swipe(app, to: 0.9)
        XCTAssertTrue(app.buttons["agents.filter.all"].waitForExistence(timeout: 3), "the Agents list after the swipe:\n\(app.debugDescription)")
        keep(XCUIScreen.main.screenshot(), "after-swipe")
    }

    @MainActor private func launch() throws -> XCUIApplication {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-openSession", Self.chat]
        app.launchPinned()
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.buttons["允许"]
        if allow.waitForExistence(timeout: 2) { allow.tap() }
        return app
    }

    @MainActor private func swipe(_ app: XCUIApplication, to: CGFloat) {
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.005, dy: 0.5))
        start.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: to, dy: 0.5)),
                    withVelocity: .slow, thenHoldForDuration: 0.05)
    }

    private func keep(_ shot: XCUIScreenshot, _ name: String) {
        let a = XCTAttachment(screenshot: shot)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? shot.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("launch-onto-chat-\(name).png"))
        }
    }
}

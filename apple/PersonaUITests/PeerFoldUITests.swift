import XCTest

/// A long message another session sent an agent folds to three lines, as an exchange does, with
/// Expand; opened it reads in full with Collapse; a short one stays whole with no button (#2010). Against
/// scripts/mock/mock_hub.py with MOCK_PEER_FOLD=1 (PERSONA_MOCK_HUB). PERSONA_PEER_SCHEME: light or dark.
final class PeerFoldUITests: XCTestCase {
    private var app: XCUIApplication!
    private var hub = ""
    private let chat = "macbook/a1f3c2d4-0000-4000-8000-0000000c1e0a"
    private var scheme: String { ProcessInfo.processInfo.environment["PERSONA_PEER_SCHEME"] ?? "light" }

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_PEER_FOLD=1 (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
        app = XCUIApplication()
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "peer-\(name)-\(scheme)"
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor func testALongMessageFoldsAndOpens() throws {
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", chat, "-openChatsFresh", "YES", "-ui.scheme", scheme]
        app.launchPinned()
        let short = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "深色主题的对比度修好了")).firstMatch
        XCTAssertTrue(short.waitForExistence(timeout: 20), "the chat did not open:\n\(app.debugDescription)")
        acceptIconAlert(timeout: 3) // the scheme's icon
        sleep(1)
        shot("folded")

        // One button: the long message's. The short one has none.
        let expand = app.buttons.matching(identifier: "message.expand")
        XCTAssertEqual(expand.count, 1, "Expand under the long message only")
        // Folded, the long message's last lines are not drawn.
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "所有版本一起搜")).firstMatch.isHittable)
        expand.firstMatch.tap()
        let collapse = app.buttons["message.collapse"]
        XCTAssertTrue(collapse.waitForExistence(timeout: 5), "Expand did not open it")
        sleep(1)
        // The message's end and Collapse, a little further down.
        if !collapse.isHittable { app.swipeUp(velocity: .slow) }
        sleep(1)
        shot("expanded")
    }
}

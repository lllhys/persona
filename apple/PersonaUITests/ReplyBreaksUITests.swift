import XCTest

/// A reply written line by line reads line by line (#2088): a bold line and the steps right under it are separate,
/// steps numbered on from 5 are a list, and the sentence under a bold line starts on its own line. In an Agents chat
/// and in Persona's conversation (the new layout's box), against scripts/mock/mock_hub.py with MOCK_REPLY_BREAKS=1
/// (PERSONA_MOCK_HUB). PERSONA_BREAKS_SCHEME: light or dark.
final class ReplyBreaksUITests: XCTestCase {
    private var app: XCUIApplication!
    private var hub = ""
    private var scheme: String { ProcessInfo.processInfo.environment["PERSONA_BREAKS_SCHEME"] ?? "light" }

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_REPLY_BREAKS=1 (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
        app = XCUIApplication()
    }

    private var pair: String { "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock" }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "breaks-\(name)-\(scheme)"
        a.lifetime = .keepAlways
        add(a)
    }

    private func text(_ s: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
    }

    /// The step numbered 5 is not in the bold line's paragraph, and the last sentence starts a line of its own.
    @MainActor private func checkLines(_ chat: String) {
        let step = text("附上出错时间前后")
        XCTAssertTrue(step.waitForExistence(timeout: 20), "the reply did not show:\n\(app.debugDescription)")
        acceptIconAlert(timeout: 3) // the scheme's icon
        sleep(1)
        shot(chat)
        XCTAssertFalse(step.label.contains("更快"), "step 5 joined the bold line: \(step.label)")
        // The sentence is in its bold line's paragraph, on the next line: not after a space.
        let sentence = text("页面不再卡住").label
        XCTAssertFalse(sentence.contains("修好了 同样"), "the sentence joined its bold line: \(sentence)")
    }

    @MainActor func testAnAgentsChat() throws {
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-0000000c1e0b",
                               "-openChatsFresh", "YES", "-ui.scheme", scheme]
        app.launchPinned()
        checkLines("agents")
    }

    @MainActor func testPersonasConversation() throws {
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant",
                               "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001", "-openChatsFresh", "YES",
                               "-ui.scheme", scheme]
        app.launchPinned()
        checkLines("persona")
    }
}

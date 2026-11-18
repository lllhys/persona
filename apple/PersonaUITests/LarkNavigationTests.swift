import XCTest

/// A chat returns to the entry it came from, including after a cancelled edge swipe.
/// Film the same flows for bar-material continuity; an accessibility tree cannot prove that a blur stayed visible.
/// The iPhone's (#952: the shell's Messages and a chat pushed over the whole screen); an iPad has Messages as a tab and
/// pushes the chat in the column beside its list, away from the window's edge.
///
/// The Inbox lists the current assistant's items only (#5587), and on the default mock hub the group chat's item
/// (「周末徒步群」) is another assistant's (「工作」): its Inbox case needs a hub with one assistant, as InboxCollapsedSourceTests'
/// reused column does, and skips without it:
///   PORT=<port> MOCK_MANAGERS=0 python3 scripts/mock/mock_hub.py
///   TEST_RUNNER_PERSONA_INBOX_HUB=http://127.0.0.1:<port>
final class LarkNavigationTests: XCTestCase {
    @MainActor private func launch(inbox: String? = nil, hubKey: String = "PERSONA_MOCK_HUB") throws -> XCUIApplication {
        guard UIDevice.current.userInterfaceIdiom == .phone else { throw XCTSkip("the iPhone's pushed chats: an iPad pushes them beside the list") }
        guard let hub = ProcessInfo.processInfo.environment[hubKey] else {
            throw XCTSkip(hubKey == "PERSONA_MOCK_HUB" ? "needs mock Hub"
                          : "needs a mock hub with one assistant (\(hubKey), MOCK_MANAGERS=0): the group is another assistant's on the default one")
        }
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", inbox == nil ? "assistant" : "mail", "-demo.on", "NO",
                               "-integrations.lark.enabled", "YES",
                               "-inbox.kind", inbox.map { $0.contains("f3") ? "group" : "dm" } ?? "", "-inbox.source", ""]
            // the group chat is FYI: every section stacked (the section bar's All, #9053)
            + (inbox == nil ? [] : ["-showInboxSection", "all"])
        // Navigate through the actual inbox. Its debug deep link replaces the shell's path and skips that page.
        app.launchPinned()
        return app
    }

    @MainActor private func expandSource(_ app: XCUIApplication) {
        let source = app.buttons["inbox.detail.source"]
        XCTAssertTrue(source.waitForExistence(timeout: 20), "the original-source control did not appear")
        source.tapCenter()
    }

    @MainActor private func assertChat(_ app: XCUIApplication, title: String) {
        let nav = app.navigationBars[title]
        XCTAssertTrue(nav.waitForExistence(timeout: 20))
        XCTAssertTrue(app.textFields["lark.box"].waitForExistence(timeout: 15))
        XCTAssertTrue(nav.buttons["BackButton"].isHittable)
    }

    @MainActor private func cancelBack(_ app: XCUIApplication, title: String) {
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5))
            .press(forDuration: 0.1,
                   thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.18, dy: 0.5)),
                   withVelocity: .slow, thenHoldForDuration: 0.6)
        assertChat(app, title: title)
    }

    @MainActor private func back(_ app: XCUIApplication) {
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5))
            .press(forDuration: 0.1, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)))
    }

    @MainActor private func shot(_ app: XCUIApplication, name: String) {
        let shot = app.screenshot()
        let attachment = XCTAttachment(screenshot: shot)
        attachment.name = name; attachment.lifetime = .keepAlways; add(attachment)
        if let directory = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? shot.pngRepresentation.write(to: URL(fileURLWithPath: directory).appendingPathComponent(name + ".png"))
        }
    }

    @MainActor func testMessagesKeepsItsNavigationAfterCancelledAndCompletedBack() throws {
        let app = try launch()
        let entry = app.buttons["shell-feishu"].firstMatch
        XCTAssertTrue(entry.waitForExistence(timeout: 20))
        entry.tapCenter()
        for (feed, title) in [("f3", "周末徒步群"), ("f1", "韩梅梅")] {
            let row = app.buttons["feishu-chat-" + feed]
            XCTAssertTrue(row.waitForExistence(timeout: 15))
            row.tapCenter()
            assertChat(app, title: title)
            cancelBack(app, title: title)
            shot(app, name: "messages-shell-\(feed)")
            back(app)
            XCTAssertTrue(row.waitForExistence(timeout: 8))
            XCTAssertTrue(entry.isHittable, "the original entry returns with the list")
        }
        app.terminate()
    }

    /// The direct message: the default assistant's on every mock hub.
    @MainActor func testInboxKeepsItsNavigationAfterCancelledAndCompletedBack() throws {
        try inboxRoundTrip("lark:f1:100", title: "韩梅梅")
    }

    /// The group chat: 「工作」's on the default mock hub, so on a hub with one assistant.
    @MainActor func testInboxGroupKeepsItsNavigationAfterCancelledAndCompletedBack() throws {
        try inboxRoundTrip("lark:f3:300", title: "周末徒步群", hubKey: "PERSONA_INBOX_HUB")
    }

    @MainActor private func inboxRoundTrip(_ item: String, title: String, hubKey: String = "PERSONA_MOCK_HUB") throws {
        let app = try launch(inbox: item, hubKey: hubKey)
        XCTAssertTrue(app.navigationBars["收件箱"].waitForExistence(timeout: 20))
        let row = app.staticTexts[title].firstMatch
        for _ in 0..<5 where !row.isHittable { app.swipeUp() }
        XCTAssertTrue(row.waitForExistence(timeout: 10), "no \(title) in the Inbox:\n\(app.debugDescription)")
        row.tapCenter()
        expandSource(app)
        assertChat(app, title: title)
        cancelBack(app, title: title)
        shot(app, name: "inbox-shell-\(item.contains("f3") ? "group" : "dm")")
        back(app)
        XCTAssertTrue(app.navigationBars["收件箱"].waitForExistence(timeout: 8), "back returns to the inbox")
        app.terminate()
    }
}

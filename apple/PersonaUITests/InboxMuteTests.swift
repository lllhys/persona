import XCTest

/// Mute from an inbox row's long-press menu (docs/assistant/inbox.md「Muting」): the menu offers what the item can be muted by
/// (a GitHub item's repository and sender, a Feishu group's chat and sender, a mail's sender), muting files the item
/// and says so, and More › Muted lists the mutes. Against scripts/mock/mock_hub.py with MOCK_GITHUB_INBOX=1
/// MOCK_MUTES=1 (PERSONA_MOCK_HUB), which keeps the mutes at GET /api/v1/inbox/mutes.
final class InboxMuteTests: XCTestCase {
    @MainActor func testMuteFromARowsMenu() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        // the group chat and the shop's mail are FYI: every section stacked (the section bar's All, #9053)
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-inbox.kind", "", "-inbox.source", "", "-showInboxSection", "all"]
        app.launchPinned()
        func shot(_ name: String) {
            let a = XCTAttachment(screenshot: app.screenshot())
            a.name = "\(name) \(UITestStyle.name)"
            a.lifetime = .keepAlways
            add(a)
        }
        func text(_ s: String) -> XCUIElement {
            app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
        }
        func menuButton(_ s: String) -> XCUIElement {
            app.buttons.matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
        }
        /// Scrolls until el is on screen (BeautifulUI's taller cards push rows below the fold, where the list has
        /// not made them yet).
        func reveal(_ el: XCUIElement) {
            for _ in 0..<8 where !(el.exists && el.isHittable) {
                app.swipeUp()
            }
        }
        func closeMenu() {
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.06)).tap()
            sleep(1)
        }

        let github = text("Add retries to the example uploader")
        XCTAssertTrue(text("韩梅梅").waitForExistence(timeout: 20), "the inbox did not load")
        reveal(github)
        guard github.exists else {
            shot("inbox mute failed")
            XCTFail("no GitHub item:\n\(app.debugDescription)")
            return
        }
        shot("inbox mute list")

        let more = app.buttons["更多"].firstMatch
        XCTAssertTrue(more.waitForExistence(timeout: 5), "no 更多")
        more.tap()
        XCTAssertTrue(menuButton("已静音").waitForExistence(timeout: 5), "no 已静音 in 更多:\n\(app.debugDescription)")
        sleep(1)
        shot("inbox mute more")
        closeMenu()

        github.press(forDuration: 1.2)
        XCTAssertTrue(menuButton("静音这个仓库").waitForExistence(timeout: 5), "no 静音这个仓库:\n\(app.debugDescription)")
        XCTAssertTrue(menuButton("静音这个发件人").exists, "no 静音这个发件人")
        XCTAssertTrue(menuButton("已处理").exists, "no 已处理")
        sleep(1)
        shot("inbox mute menu github")
        closeMenu()

        let group = text("周末徒步群")
        reveal(group)
        group.press(forDuration: 1.2)
        XCTAssertTrue(menuButton("静音这个会话").waitForExistence(timeout: 5), "no 静音这个会话:\n\(app.debugDescription)")
        sleep(1)
        shot("inbox mute menu lark")
        closeMenu()

        let mail = text("你的订单已发货")
        reveal(mail)
        mail.press(forDuration: 1.2)
        let sender = menuButton("静音这个发件人")
        XCTAssertTrue(sender.waitForExistence(timeout: 5), "no 静音这个发件人 on mail:\n\(app.debugDescription)")
        sleep(1)
        shot("inbox mute menu mail")
        sender.tap()
        XCTAssertTrue(text("已静音「示例商城」").waitForExistence(timeout: 5), "no toast:\n\(app.debugDescription)")
        shot("inbox mute toast")

        let mutes = try String(contentsOf: URL(string: hub + "/api/v1/inbox/mutes")!, encoding: .utf8)
        XCTAssertTrue(mutes.contains("shop@example.com"), "the mute did not reach the hub: \(mutes)")

        app.terminate()
        app.launchArguments += ["-showInboxMutes", "YES"]
        app.launchPinned()
        XCTAssertTrue(text("示例商城").waitForExistence(timeout: 20), "no 已静音 list:\n\(app.debugDescription)")
        XCTAssertTrue(text("邮件 · 发件人").exists, "no source and kind")
        XCTAssertTrue(text("example-org/old-site").exists, "no earlier mute")
        sleep(1)
        shot("inbox mutes")
    }
}

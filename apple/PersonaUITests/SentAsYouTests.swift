import XCTest

/// Settings › Connectors › Records sent as you (#5278, #2522): the nested entry, the list with how each send
/// was let through (orange where the user did not decide), a row's details with the whole text, the chat that asked, Load
/// Earlier, the connector chips, and the empty / no database / no full access / older hub / unreachable states. Against
/// scripts/mock/mock_hub.py with MOCK_SENT=1 (scripts/mock/sent_ledger_mock.py: about seventy made-up rows, newest id 70):
///   PORT=<port> MOCK_SENT=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:<port> xcodebuild test … -only-testing:PersonaUITests/SentAsYouTests
/// Each test first puts the mock back to its rows (`GET /mock/sent?mode=1`), then switches it to another state where it needs one.
/// `TEST_RUNNER_PERSONA_SHOTS=<dir>` keeps the screenshots of the page, light and dark (`testScreenshots`).
final class SentAsYouTests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }

    /// The mock, back at its rows.
    private func freshMock() throws -> String {
        guard let hub = env["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_SENT=1 (PERSONA_MOCK_HUB)") }
        try setMode(hub, "1")
        return hub
    }

    /// `GET /mock/sent?mode=`: 1 (the rows), empty, nodb, forbidden or old.
    private func setMode(_ hub: String, _ mode: String) throws {
        let data = try Data(contentsOf: URL(string: hub + "/mock/sent?mode=" + mode)!)
        let said = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: String])
        XCTAssertEqual(said["mode"], mode)
    }

    @MainActor private func launch(_ hub: String, _ args: [String]) -> XCUIApplication {
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            for label in ["允许", "Allow"] where alert.buttons[label].exists { alert.buttons[label].tap(); return true }
            return false
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair] + args
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 3) { springboard.buttons[label].tap() }
        return app
    }

    /// The page itself, opened by `-openSentAsYou YES`.
    @MainActor private func openPage(_ hub: String, extra: [String] = []) -> XCUIApplication {
        launch(hub, ["-tab", "settings", "-openSentAsYou", "YES"] + extra)
    }

    @MainActor private func any(_ app: XCUIApplication, _ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    @MainActor private func text(_ app: XCUIApplication, containing s: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
    }

    /// Scrolls until `element` is on screen (a list draws only the rows near the screen).
    @MainActor private func reveal(_ app: XCUIApplication, _ element: XCUIElement, swipes: Int = 30) {
        for _ in 0..<swipes where !(element.exists && element.isHittable) { app.swipeUp() }
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let png = URL(fileURLWithPath: dir).appending(path: "\(name).png")
        try? app.screenshot().pngRepresentation.write(to: png)
        if env["PERSONA_SIMCTL_SHOTS"] == "1", name.hasPrefix("ios-") {
            let ready = png.appendingPathExtension("ready")
            let captured = png.appendingPathExtension("captured")
            try? FileManager.default.removeItem(at: captured)
            try? Data().write(to: ready)
            let receipt = expectation(for: NSPredicate { _, _ in FileManager.default.fileExists(atPath: captured.path) }, evaluatedWith: nil)
            wait(for: [receipt], timeout: 30)
            try? FileManager.default.removeItem(at: ready)
        }
    }

    // MARK: the entry

    /// Only Connectors is top-level; its bottom entry opens the existing ledger and Back returns to Connectors.
    @MainActor func testTheEntryComesAfterConnectors() throws {
        let app = launch(try freshMock(), ["-tab", "settings", "-dev.openChat", ""])
        let list = any(app, "settings-list")
        XCTAssertTrue(list.waitForExistence(timeout: 15))
        XCTAssertFalse(app.buttons["settings.sent"].exists)
        let connectors = app.buttons["settings.connectors"]
        for _ in 0..<12 where !connectors.isHittable { list.swipeUp() }
        XCTAssertTrue(connectors.isHittable, "no Connectors entry:\n\(app.debugDescription)")
        connectors.tap()
        let entry = app.buttons["connectors.sent"]
        reveal(app, entry, swipes: 12)
        XCTAssertTrue(entry.isHittable, "no nested ledger entry:\n\(app.debugDescription)")
        XCTAssertTrue(entry.label.contains("以你的名义发出的记录"), entry.label)
        let bot = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "飞书机器人")).firstMatch
        XCTAssertTrue(bot.exists)
        XCTAssertLessThan(bot.frame.maxY, entry.frame.minY, "the record entry must follow the connector list")
        entry.tap()
        XCTAssertTrue(app.buttons["sent.row.70"].waitForExistence(timeout: 15), "the page did not open:\n\(app.debugDescription)")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(any(app, "connectors-list").waitForExistence(timeout: 10))
        XCTAssertTrue(entry.exists, "Back must return to Connectors, not top-level Settings")
    }

    @MainActor func testLegacyLaunchReturnsToConnectors() throws {
        let app = openPage(try freshMock())
        XCTAssertTrue(app.buttons["sent.row.70"].waitForExistence(timeout: 15))
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(any(app, "connectors-list").waitForExistence(timeout: 10))
        reveal(app, app.buttons["connectors.sent"], swipes: 12)
        XCTAssertTrue(app.buttons["connectors.sent"].isHittable)
    }

    /// Demo Mode has no hub to read a ledger from: no row (the rest of Settings is there).
    @MainActor func testDemoModeHasNoRow() throws {
        _ = try freshMock()
        let app = XCUIApplication()
        app.launchArguments = ["-openWelcome", "YES", "-dev.openChat", ""]
        app.launchPinned()
        let demo = app.buttons["welcome.demo"]
        XCTAssertTrue(demo.waitForExistence(timeout: 15), "no 体验模式 on the pairing page")
        demo.tap()
        let drawer = app.buttons.matching(NSPredicate(format: "identifier IN %@", ["assistant-sidebar-button", "work-sidebar-button"]))
            .allElementsBoundByIndex.first(where: \.isHittable)
        XCTAssertNotNil(drawer, "the demo did not open:\n\(app.debugDescription)")
        drawer?.tapCenter()
        let settings = app.drawerSettingsButton
        XCTAssertTrue(settings.waitForExistence(timeout: 15), "the drawer has no Settings entry:\n\(app.debugDescription)")
        settings.tap()
        let leave = app.buttons["settings.leaveDemo"]
        XCTAssertTrue(leave.waitForExistence(timeout: 10), "Settings is not the demo's:\n\(app.debugDescription)")
        let connectors = app.buttons["settings.connectors"]
        reveal(app, connectors, swipes: 12)
        XCTAssertTrue(connectors.isHittable)
        connectors.tap()
        XCTAssertTrue(any(app, "connectors-list").waitForExistence(timeout: 10))
        for _ in 0..<8 { app.swipeUp() }
        XCTAssertFalse(app.buttons["connectors.sent"].exists, "Demo Mode has a ledger entry")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        // Demo Mode is remembered: leave it, so the tests after this one start from the pairing page
        for _ in 0..<12 where !(leave.exists && leave.isHittable) { app.swipeDown() } // it is at the top of the list
        XCTAssertTrue(leave.exists && leave.isHittable, "no 退出体验模式:\n\(app.debugDescription)")
        leave.tap()
        XCTAssertTrue(app.buttons["welcome.demo"].waitForExistence(timeout: 10), "the pairing page after leaving")
    }

    // MARK: the list

    @MainActor func testRowsSayHowEachSendWasLetThrough() throws {
        let app = openPage(try freshMock())
        let first = app.buttons["sent.row.70"]
        XCTAssertTrue(first.waitForExistence(timeout: 15), "no rows:\n\(app.debugDescription)")
        // a mail the user confirmed on a card: the connector, where it went, how it was let through
        XCTAssertTrue(first.label.contains("邮件"), first.label)
        XCTAssertTrue(first.label.contains("ann@example.com · Re: Design review moved to Tuesday"), first.label)
        XCTAssertTrue(first.label.contains("Tuesday afternoon works for me"), first.label)
        XCTAssertTrue(app.staticTexts["你在确认卡片上同意"].firstMatch.exists, "the card badge")
        // a GitHub comment a rule allowed, Lark's "always allow", the extension's own send, the assistant answering a card
        XCTAssertTrue(app.buttons["sent.row.69"].label.contains("example-org/app#412"))
        XCTAssertTrue(app.staticTexts["按规则"].firstMatch.exists)
        XCTAssertTrue(app.staticTexts["本对话始终允许"].firstMatch.exists)
        XCTAssertTrue(app.buttons["sent.row.68"].label.contains("飞书"))
        let auto = app.staticTexts["扩展自行发送 · auto_reply"].firstMatch
        for _ in 0..<4 where !auto.exists { app.swipeUp() }
        XCTAssertTrue(auto.exists, "the extension's own send:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["扩展自述"].firstMatch.exists, "marked as the extension's word")
        let assistant = app.staticTexts["助手代答"].firstMatch
        for _ in 0..<4 where !assistant.exists { app.swipeUp() }
        XCTAssertTrue(assistant.exists, "the card the assistant answered:\n\(app.debugDescription)")
        // a send with no text shows its one-line summary, and one with no chat says so
        let outOfOffice = app.buttons["sent.row.62"]
        reveal(app, outOfOffice, swipes: 10)
        XCTAssertTrue(outOfOffice.label.contains("Send an email to bob@example.com, subject: Out of office"), outOfOffice.label)
        XCTAssertTrue(app.staticTexts["无对话"].firstMatch.exists, "no 无对话")
    }

    /// A row opens every field and the whole text; Done closes it.
    @MainActor func testARowOpensItsDetails() throws {
        let app = openPage(try freshMock())
        let row = app.buttons["sent.row.70"]
        XCTAssertTrue(row.waitForExistence(timeout: 15), "no rows:\n\(app.debugDescription)")
        row.tap()
        let done = app.buttons["sent.detail.done"]
        XCTAssertTrue(done.waitForExistence(timeout: 10), "the details did not open:\n\(app.debugDescription)")
        XCTAssertTrue(text(app, containing: "mail_send").exists, "the action")
        XCTAssertTrue(text(app, containing: "ann@example.com").exists, "where it went")
        XCTAssertTrue(text(app, containing: "ix-demo-1").exists, "the card's id")
        shot(app, "sent-qa-detail")
        XCTAssertTrue(any(app, "sent.detail.text").exists)
        XCTAssertTrue(any(app, "sent.detail.text").label.contains("Tuesday afternoon works for me"), "the whole text")
        let copy = app.buttons["sent.detail.copy"]
        for _ in 0..<6 where !(copy.exists && copy.isHittable) { app.swipeUp() }
        XCTAssertTrue(copy.exists, "no 复制正文:\n\(app.debugDescription)")
        // the sheet itself says it was copied (the app's toast can sit under a sheet on the Mac), then goes back
        copy.tap()
        let copied = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "已复制")).firstMatch
        XCTAssertTrue(copied.waitForExistence(timeout: 2), "no 已复制 after copying:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "复制正文")).firstMatch.waitForExistence(timeout: 5),
                      "复制正文 did not come back")
        XCTAssertFalse(any(app, "sent.detail.cut").exists, "a short text is not marked cut")
        done.tap()
        XCTAssertTrue(done.waitForNonExistence(timeout: 5), "Done did not close the details")
        XCTAssertTrue(row.isHittable, "back on the list")
    }

    /// The chat link in the details closes them and opens the chat (the assistant's own chat here, which Persona shows).
    @MainActor func testTheDetailsOpenTheChatToo() throws {
        let app = openPage(try freshMock())
        let row = app.buttons["sent.row.70"] // a mail the assistant's chat asked for
        XCTAssertTrue(row.waitForExistence(timeout: 15), "no rows:\n\(app.debugDescription)")
        row.tap()
        let chat = app.buttons["sent.detail.chat"]
        for _ in 0..<4 where !(chat.exists && chat.isHittable) { app.swipeUp() }
        XCTAssertTrue(chat.exists, "no 打开对话 in the details:\n\(app.debugDescription)")
        chat.tap()
        XCTAssertTrue(app.buttons["sent.detail.done"].waitForNonExistence(timeout: 10), "the details stayed over the chat")
        XCTAssertTrue(any(app, "assistant-sidebar-button").waitForExistence(timeout: 20), "Persona's chat did not open:\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["sent.row.70"].exists, "still on the list")
    }

    /// A text of exactly 4000 characters is the most the ledger keeps: the details say it was cut.
    @MainActor func testATextAsLongAsTheLedgerKeepsSaysItWasCut() throws {
        let app = openPage(try freshMock())
        let row = app.buttons["sent.row.67"]
        reveal(app, row, swipes: 4)
        XCTAssertTrue(row.exists, "no long row:\n\(app.debugDescription)")
        row.tap()
        XCTAssertTrue(app.buttons["sent.detail.done"].waitForExistence(timeout: 10))
        let cut = text(app, containing: "个字处被截断")
        for _ in 0..<10 where !cut.exists { app.swipeUp() }
        XCTAssertTrue(cut.exists, "no note that the text was cut:\n\(app.debugDescription)")
        XCTAssertTrue(cut.label.contains("4,000") || cut.label.contains("4000"), cut.label)
        shot(app, "sent-qa-cut")
    }

    /// The chat that asked opens from the row, named by its connector and where it went.
    @MainActor func testTheChatThatAskedOpens() throws {
        let app = openPage(try freshMock())
        let link = app.buttons["sent.chat.69"] // a GitHub comment an agent's chat asked for
        XCTAssertTrue(link.waitForExistence(timeout: 15), "no chat link:\n\(app.debugDescription)")
        XCTAssertEqual(link.label, "打开对话：GitHub example-org/app#412")
        link.tap()
        XCTAssertTrue(app.buttons["更多"].firstMatch.waitForExistence(timeout: 20), "the chat did not open:\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["sent.row.69"].exists, "still on the list")
    }

    // MARK: Load Earlier and the chips

    @MainActor func testLoadEarlierAddsTheOlderRows() throws {
        let app = openPage(try freshMock())
        XCTAssertTrue(app.buttons["sent.row.70"].waitForExistence(timeout: 15), "no rows:\n\(app.debugDescription)")
        let more = app.buttons["sent.loadEarlier"]
        reveal(app, more, swipes: 40)
        XCTAssertTrue(more.isHittable, "no 加载更早 after fifty rows:\n\(app.debugDescription)")
        more.tap()
        let oldest = app.buttons["sent.row.1"]
        reveal(app, oldest, swipes: 30)
        XCTAssertTrue(oldest.exists, "the oldest row did not come:\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["sent.loadEarlier"].exists, "加载更早 stays after the last page")
    }

    @MainActor func testTheChipsFilterByConnector() throws {
        let app = openPage(try freshMock())
        XCTAssertTrue(app.buttons["sent.row.70"].waitForExistence(timeout: 15), "no rows:\n\(app.debugDescription)")
        for id in ["all", "lark", "mail", "github", "examplechat"] {
            XCTAssertTrue(app.buttons["sent.filter.\(id)"].exists, "no chip \(id):\n\(app.debugDescription)")
        }
        app.buttons["sent.filter.github"].tap()
        XCTAssertTrue(app.buttons["sent.row.70"].waitForNonExistence(timeout: 10), "a mail row stays under the GitHub chip")
        XCTAssertTrue(app.buttons["sent.row.69"].waitForExistence(timeout: 10), "no GitHub row")
        // the chosen chip again goes back to everything
        app.buttons["sent.filter.github"].tap()
        XCTAssertTrue(app.buttons["sent.row.70"].waitForExistence(timeout: 10), "All did not come back")
    }

    /// A chip over a connector with nothing recorded says so and has Show All.
    @MainActor func testAChipWithNothingRecordedHasShowAll() throws {
        let hub = try freshMock()
        let app = openPage(hub)
        XCTAssertTrue(app.buttons["sent.row.70"].waitForExistence(timeout: 15), "no rows:\n\(app.debugDescription)")
        try setMode(hub, "empty")
        app.buttons["sent.filter.mail"].tap()
        XCTAssertTrue(any(app, "sent.emptyFiltered").waitForExistence(timeout: 10), "no empty state under the chip:\n\(app.debugDescription)")
        XCTAssertEqual(any(app, "sent.emptyFiltered").label, "邮件 还没有记录")
        shot(app, "sent-qa-filtered-empty")
        XCTAssertTrue(app.buttons["sent.filter.mail"].exists, "the chip went with its rows")
        app.buttons["sent.showAll"].tap()
        XCTAssertTrue(any(app, "sent.empty").waitForExistence(timeout: 10), "Show All did not read everything again:\n\(app.debugDescription)")
    }

    // MARK: the states

    @MainActor func testAnEmptyLedgerSaysWhatIsAndIsNotRecorded() throws {
        let hub = try freshMock()
        try setMode(hub, "empty")
        let app = openPage(hub)
        XCTAssertTrue(any(app, "sent.empty").waitForExistence(timeout: 15), "no empty state:\n\(app.debugDescription)")
        XCTAssertTrue(text(app, containing: "还没有记录到以你名义发出的内容").exists)
        XCTAssertTrue(text(app, containing: "不会记：你自己在 App 里发的").exists, "what is not recorded")
        XCTAssertTrue(text(app, containing: "默认保留 90 天").exists)
        shot(app, "sent-qa-empty")
        XCTAssertFalse(app.buttons["sent.filter.all"].exists, "chips over an empty ledger")
    }

    @MainActor func testNoDatabaseIsSaidAsThat() throws {
        let hub = try freshMock()
        try setMode(hub, "nodb")
        let app = openPage(hub)
        let line = any(app, "sent.problem.noDatabase")
        XCTAssertTrue(line.waitForExistence(timeout: 15), "no 503 line:\n\(app.debugDescription)")
        XCTAssertTrue(line.label.contains("没有数据库"), line.label)
        shot(app, "sent-qa-nodb")
        XCTAssertFalse(app.buttons["sent.retry"].exists, "Retry for the hub's own answer")
    }

    @MainActor func testADeviceWithoutFullAccessSaysSo() throws {
        let hub = try freshMock()
        try setMode(hub, "forbidden")
        let app = openPage(hub)
        let line = any(app, "sent.problem.forbidden")
        XCTAssertTrue(line.waitForExistence(timeout: 15), "no 403 line:\n\(app.debugDescription)")
        XCTAssertTrue(line.label.contains("完整权限"), line.label)
        XCTAssertFalse(app.buttons["sent.retry"].exists)
    }

    @MainActor func testAnOlderHubSaysToUpdate() throws {
        let hub = try freshMock()
        try setMode(hub, "old")
        let app = openPage(hub)
        let line = any(app, "sent.problem.unsupported")
        XCTAssertTrue(line.waitForExistence(timeout: 15), "no older-hub line:\n\(app.debugDescription)")
        XCTAssertTrue(line.label.contains("升级 hub"), line.label)
        XCTAssertFalse(app.buttons["sent.retry"].exists)
    }

    /// Any other failure (here: nothing listens at the hub's address) has the usual words and Retry.
    @MainActor func testAnUnreachableHubHasRetry() throws {
        _ = try freshMock()
        let app = launch("http://127.0.0.1:1", ["-tab", "settings", "-openSentAsYou", "YES"])
        XCTAssertTrue(app.buttons["sent.retry"].waitForExistence(timeout: 20), "no 重试:\n\(app.debugDescription)")
        XCTAssertTrue(any(app, "sent.problem.error").exists, "the line over Retry")
        shot(app, "sent-qa-unreachable")
    }

    // MARK: screenshots

    /// The page in light and dark for the docs (`TEST_RUNNER_PERSONA_SHOTS`): ios-sent-as-you-light.png, ios-sent-as-you-dark.png.
    /// Light / dark is the app's own setting (`-ui.scheme`), which makes the system ask about the new home screen icon: OK first.
    @MainActor func testScreenshots() throws {
        let hub = try freshMock()
        guard env["PERSONA_SHOTS"] != nil else { throw XCTSkip("set PERSONA_SHOTS for the screenshots") }
        for look in ["light", "dark"] {
            let app = launch(hub, ["-tab", "settings", "-openConnectors", "YES", "-ui.scheme", look])
            let ok = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.buttons.firstMatch
            if ok.waitForExistence(timeout: 3) { ok.tap() }
            app.activate()
            XCTAssertTrue(any(app, "connectors-list").waitForExistence(timeout: 20))
            let entry = app.buttons["connectors.sent"]
            reveal(app, entry, swipes: 12)
            XCTAssertTrue(entry.isHittable)
            shot(app, "ios-connectors-entry-\(look)")
            entry.tapCenter()
            XCTAssertTrue(app.buttons["sent.row.70"].waitForExistence(timeout: 20), "no rows (\(look)):\n\(app.debugDescription)")
            XCTAssertTrue(app.buttons["sent.filter.all"].waitForExistence(timeout: 5), "no chips (\(look))")
            shot(app, "ios-sent-as-you-\(look)")
            app.terminate()
        }
    }
}

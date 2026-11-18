import XCTest

/// Explicit Agents selection is list-first, even after Assistant pushed an Agent or a link opened its detail.
/// Run against Fixtures/dispatch_hub.py with TEST_RUNNER_PERSONA_MOCK_HUB; screenshots contain only fictional data.
final class AgentsTabNavigationTests: XCTestCase {
    private var app: XCUIApplication!
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs Fixtures/dispatch_hub.py (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.name = "agents-tab-failure"
            shot.lifetime = .keepAlways
            add(shot)
            add(XCTAttachment(string: app.debugDescription))
        }
        app?.terminate()
        super.tearDown()
    }

    @MainActor private func launch() {
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-dev.openChat", "",
                               "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001",
                               "-openAgentsFresh", "YES", "-probes", "YES", "-holdChromeOpen", "YES", "-ui.scheme", "system"]
        if let size = ProcessInfo.processInfo.environment["PERSONA_UI_TEXT_SIZE"] {
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", size]
        }
        app.launchEnvironment["PERSONA_UI_TEST_SESSION"] = "1"
        app.launchPinned()
        XCTAssertTrue(app.buttons["action-bar.running"].waitForExistence(timeout: 25), app.debugDescription)
    }

    @MainActor private func openDispatch() {
        let card = app.buttons["dispatch.task.todo_task_widget"]
        for _ in 0..<8 where !(card.exists && card.isHittable) { app.swipeDown(velocity: .slow) }
        XCTAssertTrue(card.exists && card.isHittable, app.debugDescription)
        card.tap()
        let title = app.buttons["chat.title"].firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertEqual(title.value as? String, "macbook/a1f3c2d4-0000-4000-8000-000000000001")
        XCTAssertTrue(title.label.hasPrefix("给天气 App 加主屏幕小组件"), title.label)
    }

    @MainActor private func backToAssistant() {
        app.buttons["返回"].firstMatch.tap()
        let dock = app.buttons["action-bar.running"]
        let visible = NSPredicate(format: "exists == true AND hittable == true")
        XCTAssertEqual(XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: visible, object: dock)], timeout: 10), .completed,
                       "Back did not return to the original Assistant conversation")
    }

    @MainActor private func assertList(_ step: String) throws {
        let newAgent = app.buttons["agents-new"].firstMatch
        let visible = NSPredicate(format: "exists == true AND hittable == true")
        XCTAssertEqual(XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: visible, object: newAgent)], timeout: 10), .completed,
                       "Agents selection restored a detail instead of its list: \(step)\n\(app.debugDescription)")
        XCTAssertTrue(app.shellShown?.workRoot == true, "Agents has a retained navigation path: \(app.shellTrace)")
        XCTAssertFalse(app.buttons["chat.title"].firstMatch.isHittable, "An Agent detail is still in front")
        guard let directory = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        let appearance = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(directory)/agents-tab-\(appearance)-\(step).png"))
    }

    @MainActor func testNativeTabReselectionAndSwitchBackShowList() throws {
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-dev.openChat", "", "-openAgentsFresh", "YES",
                               "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000001",
                               "-holdChromeOpen", "YES", "-ui.scheme", "system"]
        app.launchEnvironment["PERSONA_UI_TEST_SESSION"] = "1"
        app.launchPinned()
        let persona = app.buttons["Persona"].firstMatch
        guard persona.waitForExistence(timeout: 10) else { throw XCTSkip("requires the iPad's native tabs") }
        let agents = app.buttons["Agents"].firstMatch
        XCTAssertTrue(agents.waitForExistence(timeout: 10), app.debugDescription)
        let title = app.buttons["chat.title"].firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 20), app.debugDescription)
        XCTAssertEqual(title.value as? String, "macbook/a1f3c2d4-0000-4000-8000-000000000001")
        agents.tap() // Already selected, while its detail is open.
        let noDetail = NSPredicate { _, _ in !title.exists || !title.isHittable }
        XCTAssertEqual(XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: noDetail, object: NSNull())], timeout: 10), .completed,
                       "Reselecting the native Agents tab retained its detail")
        for round in 1...3 {
            let row = app.buttons.matching(identifier: "agents.row").firstMatch
            XCTAssertTrue(row.waitForExistence(timeout: 10), app.debugDescription)
            row.tap()
            XCTAssertTrue(title.waitForExistence(timeout: 10), app.debugDescription)
            persona.tap()
            agents.tap()
            XCTAssertEqual(XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: noDetail, object: NSNull())], timeout: 10), .completed,
                           "Switching back to the native Agents tab retained its detail, round \(round)")
        }
        if let directory = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let appearance = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
            try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(directory)/agents-tab-ipad-\(appearance)-list.png"))
        }
    }

    @MainActor func testDispatchBackThenAgentsAlwaysShowsList() throws {
        launch()
        let finger = ShellFinger(app)
        for round in 1...5 {
            openDispatch()
            backToAssistant()
            finger.switchTo(agents: true, "Agents after dispatch Back, round \(round)", limit: 5)
            try assertList("round-\(round)")
            finger.switchTo(agents: false, "Assistant again", limit: 5)
        }
    }
}

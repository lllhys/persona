import XCTest

/// Machines › Add Computer (#4196, docs/assistant/onboarding.md 4.2): from no computer at all to one confirmed and online, and the list's
/// pending computer and error_code. Against scripts/mock/mock_scenarios.py, which restarts the mock hub with the
/// variables each test needs:
///   TEST_RUNNER_PERSONA_ADD_HUB=http://127.0.0.1:<port> TEST_RUNNER_PERSONA_ADD_CONTROL=http://127.0.0.1:<control>
/// TEST_RUNNER_PERSONA_SHOTS=<dir> also saves every screen there (light and dark) as PNG.
final class AddComputerTests: XCTestCase {
    private let env = ProcessInfo.processInfo.environment

    override func setUp() { continueAfterFailure = false }

    /// Restarts the mock hub with these MOCK_* variables (mock_scenarios.py waits until it answers).
    private func scenario(_ vars: String) throws {
        guard let control = env["PERSONA_ADD_CONTROL"] else { throw XCTSkip("needs mock_scenarios.py (PERSONA_ADD_CONTROL)") }
        let url = URL(string: control + "/scenario?env=" + vars.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed)!)!
        let done = expectation(description: "scenario")
        URLSession.shared.dataTask(with: url) { _, _, _ in done.fulfill() }.resume()
        wait(for: [done], timeout: 60)
    }

    @MainActor private func launch(_ args: [String], scheme: String) throws -> XCUIApplication {
        guard let hub = env["PERSONA_ADD_HUB"] else { throw XCTSkip("needs mock_scenarios.py (PERSONA_ADD_HUB)") }
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            for label in ["允许", "Allow", "好", "OK"] where alert.buttons[label].exists { alert.buttons[label].tap(); return true }
            return false
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-ui.scheme", scheme, "-dev.openChat", ""] + args
        app.launchPinned()
        dismissSystemAlerts(wait: 4)
        return app
    }

    /// The notification question and 「你已更改“Persona”的图标」 (the icon follows Light & Dark) are SpringBoard's.
    private func dismissSystemAlerts(wait: TimeInterval = 1) {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        var deadline = Date.now.addingTimeInterval(wait)
        while Date.now < deadline {
            let alert = springboard.alerts.firstMatch
            if alert.waitForExistence(timeout: 0.5) {
                for label in ["允许", "Allow", "好", "OK"] where alert.buttons[label].exists { alert.buttons[label].tap(); break }
                deadline = Date.now.addingTimeInterval(1.5) // another may follow
            }
        }
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        dismissSystemAlerts()
        let png = app.screenshot()
        let a = XCTAttachment(screenshot: png)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = env["PERSONA_SHOTS"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("add-computer-\(name).png"))
        }
    }

    private func text(_ app: XCUIApplication, _ s: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
    }

    private func element(_ app: XCUIApplication, _ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
    }

    /// 0 computers → Add Computer → the Mac's steps and the countdown → it joins → It's Mine → it is in the list, online.
    @MainActor func testFromNoComputerToOneConfirmed() throws {
        for scheme in ["light", "dark"] {
            try scenario("MOCK_READINESS=zero,MOCK_ADD_COMPUTER_JOIN=8")
            let app = try launch(["-tab", "settings", "-openMachines", "YES"], scheme: scheme)
            let add = app.buttons["添加电脑"].firstMatch
            XCTAssertTrue(text(app, "还没有电脑").waitForExistence(timeout: 20), "no empty state:\n\(app.debugDescription)")
            XCTAssertTrue(add.waitForExistence(timeout: 5))
            shot(app, "zero-\(scheme)")
            add.tap()

            let name = app.textFields["addComputer.name"]
            XCTAssertTrue(name.waitForExistence(timeout: 5), "no form:\n\(app.debugDescription)")
            XCTAssertFalse(app.buttons["addComputer.next"].isEnabled, "下一步 without a name")
            name.tap()
            name.typeText("小林的 MacBook")
            shot(app, "form-\(scheme)")
            app.buttons["addComputer.next"].tap()

            XCTAssertTrue(element(app, "addComputer.countdown").waitForExistence(timeout: 10), "no countdown:\n\(app.debugDescription)")
            XCTAssertTrue(text(app, "Persona Link").exists, "no Mac steps")
            XCTAssertTrue(app.buttons["addComputer.share"].exists && app.buttons["addComputer.copy"].exists)
            shot(app, "waiting-\(scheme)")

            let yes = app.buttons["addComputer.confirm"]
            XCTAssertTrue(yes.waitForExistence(timeout: 25), "it never joined:\n\(app.debugDescription)")
            XCTAssertTrue(text(app, "有一台电脑想加入：小林的 MacBook（macOS），是你的吗？").exists)
            shot(app, "joined-\(scheme)")
            yes.tap()

            XCTAssertTrue(element(app, "addComputer.confirmed").waitForExistence(timeout: 10), "not confirmed:\n\(app.debugDescription)")
            shot(app, "confirmed-\(scheme)")
            app.buttons["addComputer.close"].tap()

            XCTAssertTrue(text(app, "小林的 MacBook").waitForExistence(timeout: 10), "not in the list")
            XCTAssertTrue(text(app, "1/1 台在线").waitForExistence(timeout: 10), "not online:\n\(app.debugDescription)")
            XCTAssertFalse(text(app, "等你确认").exists, "still pending")
            shot(app, "full-\(scheme)")
            app.terminate()
        }
    }

    /// Linux: the one line that installs and pairs; the code expires, a new one starts the countdown again.
    @MainActor func testLinuxLineAndANewCodeAfterItExpires() throws {
        for scheme in ["light", "dark"] {
            try scenario("MOCK_READINESS=zero,MOCK_ADD_COMPUTER_JOIN=0,MOCK_PAIR_SECONDS=8")
            let app = try launch(["-showAddComputer", "YES"], scheme: scheme)
            let name = app.textFields["addComputer.name"]
            XCTAssertTrue(name.waitForExistence(timeout: 20), "no form:\n\(app.debugDescription)")
            app.segmentedControls["addComputer.os"].buttons["Linux"].tap()
            name.tap()
            name.typeText("家里的服务器")
            app.buttons["addComputer.next"].tap()
            let command = element(app, "addComputer.command")
            XCTAssertTrue(command.waitForExistence(timeout: 10), "no command:\n\(app.debugDescription)")
            XCTAssertTrue(command.label.hasPrefix("curl -fsSL "), "not the install line: \(command.label)")
            shot(app, "linux-\(scheme)")
            let renew = app.buttons["addComputer.renew"]
            XCTAssertTrue(renew.waitForExistence(timeout: 20), "it never expired:\n\(app.debugDescription)")
            shot(app, "expired-\(scheme)")
            renew.tap()
            let countdown = element(app, "addComputer.countdown")
            XCTAssertTrue(countdown.waitForExistence(timeout: 10), "no new code:\n\(app.debugDescription)")
            XCTAssertTrue(countdown.label.contains("0:0"), "the countdown did not start again: \(countdown.label)")
            app.terminate()
        }
    }

    /// The list: a computer waiting to be confirmed (Waiting for you, Remove) first, and one whose Persona Link is offline in words.
    @MainActor func testPendingAndErrorCodeInTheList() throws {
        for scheme in ["light", "dark"] {
            try scenario("MOCK_ADD_COMPUTER=seed")
            let app = try launch(["-tab", "settings", "-openMachines", "YES"], scheme: scheme)
            let pending = text(app, "等你确认")
            XCTAssertTrue(pending.waitForExistence(timeout: 20), "no pending computer:\n\(app.debugDescription)")
            XCTAssertTrue(app.buttons["machine.reject"].exists, "no 删掉 on the waiting computer")
            XCTAssertTrue(text(app, "还没连上过").exists, "never_connected not in words")
            shot(app, "pending-\(scheme)")
            let offline = text(app, "Persona Link 没在运行")
            for _ in 0..<4 where !offline.isHittable { app.swipeUp() }
            XCTAssertTrue(offline.exists, "link_offline not in words:\n\(app.debugDescription)")
            shot(app, "error-code-\(scheme)")
            app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Mac Studio")).firstMatch.tap()
            let details = element(app, "machine.problemDetails")
            XCTAssertTrue(details.waitForExistence(timeout: 10), "no 详情 on its page:\n\(app.debugDescription)")
            details.tap()
            XCTAssertTrue(text(app, "failed to get reader").waitForExistence(timeout: 5), "the hub's words are not under 详情")
            shot(app, "error-details-\(scheme)")
            app.terminate()
        }
    }

    /// Rejected: it goes from the list.
    @MainActor func testRejectedGoesFromTheList() throws {
        try scenario("MOCK_ADD_COMPUTER=seed")
        let app = try launch(["-tab", "settings", "-openMachines", "YES"], scheme: "light")
        let reject = app.buttons["machine.reject"]
        XCTAssertTrue(reject.waitForExistence(timeout: 20), "no pending computer:\n\(app.debugDescription)")
        reject.tap()
        XCTAssertTrue(text(app, "等你确认").waitForNonExistence(timeout: 10), "still listed")
        // Its card (the toast that says 「办公室的台式机」 was removed names it too).
        let card = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "办公室的台式机")).firstMatch
        XCTAssertTrue(card.waitForNonExistence(timeout: 10), "the rejected computer is still in the list")
    }

    /// Agents with no computer: as the machines page says it, with Add Computer.
    @MainActor func testAgentsWithNoComputer() throws {
        for scheme in ["light", "dark"] {
            try scenario("MOCK_READINESS=zero")
            let app = try launch(["-tab", "dev"], scheme: scheme)
            XCTAssertTrue(text(app, "还没有电脑").waitForExistence(timeout: 20), "no zero-computer state:\n\(app.debugDescription)")
            XCTAssertFalse(text(app, "herdr").exists)
            shot(app, "agents-zero-\(scheme)")
            app.buttons["添加电脑"].firstMatch.tap()
            XCTAssertTrue(app.textFields["addComputer.name"].waitForExistence(timeout: 10), "添加电脑 did not open:\n\(app.debugDescription)")
            app.terminate()
        }
    }
}

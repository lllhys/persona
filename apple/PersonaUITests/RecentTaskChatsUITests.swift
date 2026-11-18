import XCTest

/// Show Scheduled Task Chats (#8324), against scripts/mock/mock_hub.py with MOCK_AUTO_CONVERSATIONS=1 (PERSONA_AUTO_HUB).
/// Off (the default), the sidebar's Recent leaves the scheduled task's chat out; All Chats' ⋯ at the top left turns it
/// on, the hub keeps it, and Recent lists that chat by time, still five; Assistant Profile has the same switch, on, and
/// turns it off again. A hub without the setting (MOCK_RECENT_TASK_CHATS=absent) shows neither, as before.
/// SHOT_DIR keeps the screenshots (SHOT_SUFFIX names the look); PERSONA_NAVIGATION picks the layout (the iPad's Merged).
final class RecentTaskChatsUITests: XCTestCase {
    private var hub: String!
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_AUTO_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_AUTO_CONVERSATIONS=1 (PERSONA_AUTO_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func launch(_ extra: [String] = []) {
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        var args = ["-pairURL", pair, "-tab", "assistant", "-newAssistant", "YES", "-dev.openChat", ""] + extra
        if let layout = ProcessInfo.processInfo.environment["PERSONA_NAVIGATION"] { args += ["-app.navigation", layout] }
        app.launchArguments = args
        app.launchPinned()
    }

    /// The hub's assistant settings, after writing `body` when there is one.
    @discardableResult
    private func assistant(_ body: [String: Any]? = nil) throws -> [String: Any] {
        var req = URLRequest(url: URL(string: hub + "/api/v1/assistant")!)
        req.setValue("Bearer mock", forHTTPHeaderField: "Authorization")
        if let body {
            req.httpMethod = "PUT"
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        var out: Data?
        let done = expectation(description: "assistant")
        URLSession.shared.dataTask(with: req) { d, _, _ in out = d; done.fulfill() }.resume()
        wait(for: [done], timeout: 10)
        return (try? JSONSerialization.jsonObject(with: out ?? Data()) as? [String: Any]) ?? [:]
    }

    /// Waits until the hub holds `on`.
    private func hubHolds(_ on: Bool) throws {
        let end = Date().addingTimeInterval(10)
        while Date() < end {
            if try assistant()["recent_task_chats"] as? Bool == on { return }
            usleep(250_000)
        }
        XCTFail("the hub's recent_task_chats is not \(on)")
    }

    /// Opens the drawer on the phone (the iPad's column is there already) and waits for the newest chat in Recent.
    @MainActor private func showSidebar(timeout: TimeInterval = 20) {
        let menu = app.buttons["assistant-sidebar-button"].firstMatch
        let newest = conversation("1105 | 收件箱 | 回复韩梅梅")
        let end = Date().addingTimeInterval(timeout)
        while Date() < end && !menu.exists && !newest.exists { usleep(250_000) }
        if !(newest.exists && newest.isHittable) {
            XCTAssertTrue(menu.exists, "no sidebar button:\n\(app.debugDescription)")
            sleep(1)
            menu.tapCenter()
        }
        XCTAssertTrue(newest.waitForExistence(timeout: 10), "the newest chat is not in 最近:\n\(app.debugDescription)")
    }

    /// The All Chats row, scrolled to where it is at large text sizes.
    @MainActor private func allChatsRow() -> XCUIElement {
        let all = app.buttons["sidebar-all-chats"].firstMatch
        for _ in 0..<8 where !(all.exists && all.isHittable) { app.swipeUp(velocity: .slow) }
        XCTAssertTrue(all.exists, "no 全部对话 row:\n\(app.debugDescription)")
        return all
    }

    /// The sidebar back at its top, where Recent is.
    @MainActor private func sidebarTop() {
        let fold = app.buttons["sidebar-recent-fold"].firstMatch
        for _ in 0..<8 where !(fold.exists && fold.isHittable) { app.swipeDown(velocity: .slow) }
    }

    @MainActor private func conversation(_ prefix: String) -> XCUIElement {
        app.buttons.matching(identifier: "sidebar-conversation").matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).firstMatch
    }

    /// Scrolls the profile until `element` is on screen.
    @MainActor private func scroll(to element: XCUIElement) {
        for _ in 0..<12 where !(element.exists && element.isHittable) { app.swipeUp(velocity: .slow) }
    }

    @MainActor private func shot(_ name: String) {
        sleep(1)
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["SHOT_DIR"] {
            let suffix = ProcessInfo.processInfo.environment["SHOT_SUFFIX"].map { "-" + $0 } ?? ""
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/recent-\(name)\(suffix).png"))
        }
    }

    @MainActor func testShowScheduledTaskChats() throws {
        let hubHasIt = try assistant()["recent_task_chats"] != nil
        if hubHasIt { try assistant(["recent_task_chats": false]) } // every run starts from the default
        launch()
        showSidebar()
        let task = conversation("1105 | 定时 | 晨间简报")
        XCTAssertFalse(task.exists, "the scheduled task's chat is in 最近 while the switch is off (#6228)")
        shot("sidebar-off")
        allChatsRow().tap()
        XCTAssertTrue(app.buttons["auto-filter-all"].firstMatch.waitForExistence(timeout: 10), "All Chats did not open")
        let options = app.buttons["auto-display-options"].firstMatch
        let profileSwitch = app.switches["assistant-recent-task-chats"].firstMatch
        guard hubHasIt else {
            // A hub from before the setting: no ⋯, no switch, Recent as it was.
            XCTAssertFalse(options.exists, "All Chats has a ⋯ with a hub that has no such setting")
            shot("all-chats-older-hub")
            app.terminate()
            launch(["-openAssistantPage", "profile"])
            // where the switch would be: under Task Notifications, over Suggest the Next Message
            let quiet = app.switches["assistant-main-tasks-quiet"].firstMatch
            scroll(to: quiet)
            XCTAssertTrue(quiet.exists, "the profile did not open:\n\(app.debugDescription)")
            app.swipeUp(velocity: .slow)
            XCTAssertFalse(profileSwitch.exists, "Assistant Profile has the switch with a hub that has no such setting")
            shot("profile-older-hub")
            return
        }
        XCTAssertTrue(options.exists, "no ⋯ at All Chats' top left:\n\(app.debugDescription)")
        shot("all-chats")
        options.tap()
        let item = app.buttons.matching(NSPredicate(format: "label == %@", "显示定时任务的对话")).firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 5), "the ⋯ menu lacks 显示定时任务的对话:\n\(app.debugDescription)")
        shot("all-chats-menu")
        item.tap()
        XCTAssertTrue(item.waitForNonExistence(timeout: 5), "the menu stayed open")
        try hubHolds(true)
        app.navigationBars.buttons["sheet-close"].firstMatch.tap()
        showSidebar(timeout: 10)
        sidebarTop()
        // at the largest text sizes the fifth row is further down the drawer
        for _ in 0..<6 where !(task.exists && task.isHittable) { app.swipeUp(velocity: .slow) }
        XCTAssertTrue(task.waitForExistence(timeout: 10), "最近 did not list the scheduled task's chat once it was on:\n\(app.debugDescription)")
        XCTAssertEqual(app.buttons.matching(identifier: "sidebar-conversation").count, 5, "最近 lists five")
        shot("sidebar-on")

        // Assistant Profile has the same setting, on; turning it off there is the hub's too.
        app.terminate()
        launch(["-openAssistantPage", "profile"])
        scroll(to: profileSwitch)
        XCTAssertTrue(profileSwitch.exists, "Assistant Profile has no Show Scheduled Task Chats:\n\(app.debugDescription)")
        XCTAssertEqual(profileSwitch.value as? String, "1", "the profile's switch is not on")
        shot("profile-on")
        profileSwitch.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap() // the switch, not the label
        try hubHolds(false)
    }
}

import XCTest

/// Low-distraction mode's held pushes (#7782): in the main conversation the action bar's Held Pushes entry opens the
/// pushes the hub held today, P1 rows first and the P2 group folded until tapped; a row with a source opens it, and with
/// the mode off there is no entry. The tray opens above the bar's row at the bar's width, in the place Needs You's and
/// In Progress' open, one tray at a time, and the three entries stay on one row (#8223). Against
/// scripts/mock/mock_hub.py (PERSONA_MOCK_HUB), whose ln mode the tests switch with `/mock/ln` and put back after: two
/// P1 rows and one P2 row today (with the mode off as well), the first P1 row linking the /orders investigation to-do
/// (its `todo_id`; its agent is a session the assistant manager knows), and one P1 row of yesterday that never shows.
/// With PERSONA_SHOTS set they write
/// `held-pushes-<light|dark>-<entry|tray|p2-folded|p2-open|source|ln-off>.png` there (`p2-folded` only when the folded
/// P2 header is below the tray's fold in `tray`).
final class PushInterceptsUITests: XCTestCase {
    private var hub: String!
    /// The mock's ln mode before the test, put back after it.
    private var lnBefore = false

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
        lnBefore = try mock("ln")["on"] as? Bool ?? false
    }

    override func tearDownWithError() throws {
        if hub != nil { _ = try? mock("ln?on=\(lnBefore ? 1 : 0)") }
    }

    @MainActor func testTodaysHeldPushesOpenWithP2Folded() throws {
        XCTAssertEqual(try mock("ln?on=1")["on"] as? Bool, true, "the mock did not turn ln mode on")
        for scheme in ["light", "dark"] {
            let app = launch(scheme)
            let entry = app.buttons["action-bar.push-intercepts"]
            XCTAssertTrue(entry.waitForExistence(timeout: 20), "\(scheme): no Held Pushes entry:\n\(app.debugDescription)")
            // ln mode and the rows come in separate reads: wait until today's three are counted (yesterday's is not).
            let deadline = Date.now.addingTimeInterval(20)
            while !entry.label.contains("3") && Date.now < deadline { usleep(200_000) }
            XCTAssertTrue(entry.label.contains("3"), "\(scheme): \(entry.label)")
            sleep(2) // the conversation above settles
            assertOneRow(app, "\(scheme), closed")
            try shot(app, scheme, "entry")

            entry.tap()
            let tray = app.otherElements["action-tray.push-intercepts"].firstMatch
            XCTAssertTrue(tray.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(entry.isSelected, "\(scheme): Held Pushes is not marked open")
            // P1's two rows; P2's one row stays folded under its header.
            XCTAssertTrue(waitForRows(app, 2), "\(scheme): \(rowIDs(app))")
            let p2 = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "P2")).firstMatch
            XCTAssertTrue(p2.exists, "\(scheme): no P2 group:\n\(app.debugDescription)")
            let quiet = text(app, "后台检查已完成")
            XCTAssertFalse(quiet.exists, "\(scheme): the P2 row shows while folded")
            XCTAssertFalse(text(app, "昨天的记录不应出现").exists, "\(scheme): yesterday's held push is listed")
            // Only the row with a source is a button: the investigation's push opens its to-do (once the to-dos are in),
            // the other is read-only.
            let linked = app.buttons.matching(Self.row)
            let todos = Date.now.addingTimeInterval(10)
            while linked.count != 1 && Date.now < todos { usleep(200_000) }
            XCTAssertEqual(linked.count, 1, "\(scheme): \(rowIDs(app))")
            sleep(1)
            try shot(app, scheme, "tray")
            // Above the row at the bar's full width, not in the entry's column, and the entries stay on one row under it.
            let bar = app.descendants(matching: .any)["action-bar"].firstMatch
            XCTAssertTrue(bar.exists, "\(scheme): no action bar:\n\(app.debugDescription)")
            let trayFrame = tray.frame
            XCTAssertEqual(trayFrame.width, bar.frame.width, accuracy: 0.5,
                           "\(scheme): the tray is \(trayFrame.width) pt wide in a \(bar.frame.width) pt bar")
            XCTAssertEqual(trayFrame.minX, bar.frame.minX, accuracy: 0.5, "\(scheme): the tray starts \(trayFrame.minX - bar.frame.minX) pt into the bar")
            XCTAssertLessThan(trayFrame.maxY, entry.frame.minY, "\(scheme): the tray is not above the row")
            assertOneRow(app, "\(scheme), open")
            // One tray at a time, all in that place: Needs You's takes it and closes this one, and the other way round.
            let needs = app.buttons["action-bar.needs-you"]
            needs.tap()
            let needsTray = app.otherElements["action-tray.needs-you"].firstMatch
            XCTAssertTrue(needsTray.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(tray.waitForNonExistence(timeout: 5), "\(scheme): Held Pushes stayed open under Needs You")
            XCTAssertFalse(entry.isSelected, "\(scheme): Held Pushes is still marked open")
            XCTAssertEqual(needsTray.frame.minX, trayFrame.minX, accuracy: 0.5, "\(scheme): Needs You's tray is not where Held Pushes' was")
            XCTAssertEqual(needsTray.frame.width, trayFrame.width, accuracy: 0.5, "\(scheme): Needs You's tray is not as wide as Held Pushes'")
            entry.tap()
            XCTAssertTrue(tray.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(needsTray.waitForNonExistence(timeout: 5), "\(scheme): Needs You stayed open under Held Pushes")
            XCTAssertFalse(needs.isSelected, "\(scheme): Needs You is still marked open")
            XCTAssertTrue(waitForRows(app, 2), "\(scheme): \(rowIDs(app))")
            sleep(1)
            // The folded P2 header can be below the tray's fold: up into view for its own picture.
            let scroll = tray.scrollViews.firstMatch
            if p2.frame.maxY > scroll.frame.maxY {
                scroll.swipeUp(velocity: .slow)
                sleep(1)
                try shot(app, scheme, "p2-folded")
            }

            p2.tap()
            XCTAssertTrue(waitForRows(app, 3), "\(scheme): P2 did not open: \(rowIDs(app))")
            XCTAssertTrue(quiet.waitForExistence(timeout: 5), app.debugDescription)
            // The open group can start below the tray's fold: up into view, P1's rows above it.
            if quiet.frame.maxY > scroll.frame.maxY { scroll.swipeUp(velocity: .slow) }
            sleep(1)
            try shot(app, scheme, "p2-open")

            // The row with a source opens it: the /orders investigation to-do, its agent's chat in the to-do sheet.
            if !linked.firstMatch.isHittable { scroll.swipeDown(velocity: .slow) }
            linked.firstMatch.tap()
            XCTAssertTrue(text(app, "排查 /orders 接口变慢").waitForExistence(timeout: 10), "\(scheme): the investigation to-do did not open:\n\(app.debugDescription)")
            let hosted = app.buttons.matching(NSPredicate(format: "identifier == %@ AND value == %@", "chat.title",
                                                          "server/a1f3c2d4-0000-4000-8000-000000000004")).firstMatch
            XCTAssertTrue(hosted.waitForExistence(timeout: 10), "\(scheme): the to-do's agent page did not open:\n\(app.debugDescription)")
            XCTAssertFalse(text(app, "Agent 没开起来").exists, "\(scheme): the sheet fell back to the agent-not-started empty state")
            XCTAssertTrue(tray.waitForNonExistence(timeout: 5), "\(scheme): the tray stayed open under the to-do")
            sleep(1)
            try shot(app, scheme, "source")
            app.terminate()
        }
    }

    @MainActor func testNoEntryWithLnModeOff() throws {
        XCTAssertEqual(try mock("ln?on=0")["on"] as? Bool, false, "the mock did not turn ln mode off")
        for scheme in ["light", "dark"] {
            let reads = try lnReads()
            let app = launch(scheme)
            XCTAssertTrue(app.buttons["action-bar.running"].waitForExistence(timeout: 20), "\(scheme): no action bar:\n\(app.debugDescription)")
            // The entry is gone once the app has read the mode, not just before it has, though held pushes are there.
            let deadline = Date.now.addingTimeInterval(20)
            while try lnReads() <= reads && Date.now < deadline { usleep(200_000) }
            XCTAssertGreaterThan(try lnReads(), reads, "\(scheme): the app never read ln mode")
            sleep(2)
            XCTAssertFalse(app.buttons["action-bar.push-intercepts"].exists, "\(scheme): Held Pushes shows with ln mode off")
            try shot(app, scheme, "ln-off")
            app.terminate()
        }
    }

    /// A held push's row (`push-intercepts.row.<id>`).
    private static var row: NSPredicate { NSPredicate(format: "identifier BEGINSWITH %@", "push-intercepts.row.") }

    /// Needs You and In Progress on Held Pushes' row, as AssistantActionBarUITests checks the first two (`when` names the
    /// moment in the failure).
    @MainActor private func assertOneRow(_ app: XCUIApplication, _ when: String, file: StaticString = #filePath, line: UInt = #line) {
        let held = app.buttons["action-bar.push-intercepts"].frame
        for id in ["action-bar.needs-you", "action-bar.running"] {
            XCTAssertEqual(app.buttons[id].frame.midY, held.midY, accuracy: 0.5,
                           "\(when): \(id) is \(app.buttons[id].frame.midY - held.midY) pt off Held Pushes' row", file: file, line: line)
        }
    }

    /// The main conversation, where the entry is, in `scheme` (`light` / `dark`).
    @MainActor private func launch(_ scheme: String) -> XCUIApplication {
        let app = XCUIApplication()
        let url = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", url, "-tab", "assistant", "-dev.openChat", "", "-ui.scheme", scheme,
                               "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001"]
        app.launchEnvironment["PERSONA_UI_TEST_SESSION"] = "1" // the chat title's value names the open session
        app.launchPinned()
        closeSystemAlert(timeout: 3)
        return app
    }

    /// The app icon follows the color scheme, and the system says so in an alert over the page: closed, so it is not in a
    /// screenshot or under a tap. A permission request is left alone (docs/operations/testing.md, "Mock-data screenshots").
    @MainActor private func closeSystemAlert(timeout: TimeInterval) {
        let board = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        guard board.alerts.firstMatch.waitForExistence(timeout: timeout) else { return }
        for label in ["OK", "好"] where board.alerts.buttons[label].exists { // i18n-ignore: the system's own button
            board.alerts.buttons[label].tap(); sleep(1); return
        }
    }

    /// What reads `label` (a row without a source is its texts, one with a source a button that reads them all).
    @MainActor private func text(_ app: XCUIApplication, _ label: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
    }

    /// The held pushes the open tray lists, by id (a row without a source may carry its id on each of its texts).
    @MainActor private func rowIDs(_ app: XCUIApplication) -> Set<String> {
        Set(app.descendants(matching: .any).matching(Self.row).allElementsBoundByIndex.map(\.identifier))
    }

    @MainActor private func waitForRows(_ app: XCUIApplication, _ count: Int, timeout: TimeInterval = 10) -> Bool {
        let deadline = Date.now.addingTimeInterval(timeout)
        while rowIDs(app).count != count && Date.now < deadline { usleep(300_000) }
        return rowIDs(app).count == count
    }

    /// `GET /mock/<path>` on the mock hub, as JSON.
    private func mock(_ path: String) throws -> [String: Any] {
        let data = try Data(contentsOf: URL(string: hub + "/mock/" + path)!)
        return try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
    }

    /// How many times the app has read ln mode from the mock.
    private func lnReads() throws -> Int {
        (try mock("hits")["hits"] as? [String: Any])?["GET /api/v1/push/ln-mode"] as? Int ?? 0
    }

    @MainActor private func shot(_ app: XCUIApplication, _ scheme: String, _ name: String) throws {
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        closeSystemAlert(timeout: 1)
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/held-pushes-\(scheme)-\(name).png"))
    }
}

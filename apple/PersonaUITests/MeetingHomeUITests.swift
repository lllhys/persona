import XCTest

/// The Meetings home (#8731 v2, docs/apple/recording.md「Meeting recordings」) against a mock hub (PERSONA_MOCK_HUB) with the
/// meeting conversation on: today's calendar meetings above the quick start (`-showUpcomingSample YES` stands in for a
/// calendar), Persona invited and removed again on the quick start, and the page's options menu. Start is never tapped
/// (it would open the microphone). zh-Hans. PERSONA_SHOTS=<dir> saves home-<appearance>-<name>.png.
final class MeetingHomeUITests: XCTestCase {
    override func setUpWithError() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { return }
        try post(hub, "/mock/meeting_conversation", ["on": true, "room": "ok", "end_seconds": 180])
        try post(hub, "/mock/meeting_attendance", ["on": true, "default": "off"])
    }

    @MainActor func testUpcomingMeetingsInviteAndOptions() throws {
        let hub = try mockHub()
        // The shots' appearance (PERSONA_SHOTS_APPEARANCE), set on the device rather than left to the last run.
        XCUIDevice.shared.appearance = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] == "dark" ? .dark : .light
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair(hub), "-tab", "assistant", "-openShellPage", "recordings", "-showUpcomingSample", "YES"]
        app.launchPinned()
        let start = app.buttons["recording-meeting-start"]
        XCTAssertTrue(start.waitForExistence(timeout: 15), app.debugDescription)
        // Today's two sample meetings, the first on now; each has its Start.
        let rows = app.descendants(matching: .any).matching(identifier: "meeting-upcoming")
        XCTAssertTrue(waitFor { rows.count == 2 }, "upcoming rows: \(rows.count) \(app.debugDescription)")
        XCTAssertTrue(rows.element(boundBy: 0).label.contains("产品周会") && rows.element(boundBy: 0).label.contains("进行中"), rows.element(boundBy: 0).label)
        XCTAssertTrue(rows.element(boundBy: 1).label.contains("设计评审"), rows.element(boundBy: 1).label)
        XCTAssertEqual(start.label, "开始会议")
        try shot(app, "upcoming")

        // Only you until Persona is invited (the hub's default is off); invited, it answers on screen; removed, gone.
        let invite = element(app, "meeting-invite-persona")
        XCTAssertTrue(invite.waitForExistence(timeout: 15), app.debugDescription)
        invite.tap()
        let persona = element(app, "meeting-persona-invited")
        XCTAssertTrue(persona.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(persona.label.contains("屏幕回答"), persona.label)
        try shot(app, "invited")
        persona.tap()
        let remove = app.buttons["移出会议"].firstMatch
        XCTAssertTrue(remove.waitForExistence(timeout: 5), app.debugDescription)
        remove.tap()
        XCTAssertTrue(invite.waitForExistence(timeout: 5), "Invite again once Persona is removed")

        // Private, language (and the microphone with a headset) in the page's options.
        let options = app.buttons["meeting-options"].firstMatch
        XCTAssertTrue(options.waitForExistence(timeout: 5), app.debugDescription)
        options.tap()
        XCTAssertTrue(element(app, "meeting-private-start").waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(app.buttons["语言"].firstMatch.exists || app.staticTexts["语言"].firstMatch.exists, app.debugDescription)
        try shot(app, "options")
    }

    // MARK: Helpers

    @MainActor private func element(_ app: XCUIApplication, _ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
    }

    @MainActor private func waitFor(_ seconds: TimeInterval = 10, _ ok: () -> Bool) -> Bool {
        let deadline = Date.now.addingTimeInterval(seconds)
        while Date.now < deadline {
            if ok() { return true }
            usleep(250_000)
        }
        return ok()
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let appearance = env["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/home-\(appearance)-\(name).png"))
    }

    private func mockHub() throws -> String {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("需要独立 mock hub") }
        return hub
    }

    private func pair(_ hub: String) -> String {
        "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
    }

    private func post(_ hub: String, _ path: String, _ body: [String: Any]) throws {
        var request = URLRequest(url: URL(string: hub + path)!)
        request.httpMethod = "POST"
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        let done = expectation(description: path)
        URLSession.shared.dataTask(with: request) { _, _, _ in done.fulfill() }.resume()
        wait(for: [done], timeout: 5)
    }
}

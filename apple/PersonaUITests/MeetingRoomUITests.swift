import XCTest

/// The meeting room (#8731 v2 B1, docs/apple/recording.md「Meeting recordings」) against a mock hub (PERSONA_MOCK_HUB) with the
/// meeting conversation on: a made-up room meeting Persona listens to (`-showMeetingSample room-listening`) fills the
/// screen; its pages swipe by name (live, highlights, map); the speaker makes Persona answer out loud and back; Persona
/// leaves and is invited again; End asks first. zh-Hans. PERSONA_SHOTS=<dir> saves room-<appearance>-<name>.png.
final class MeetingRoomUITests: XCTestCase {
    override func setUpWithError() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { return }
        try post(hub, "/mock/meeting_conversation", ["on": true, "room": "ok", "end_seconds": 180])
        try post(hub, "/mock/meeting_attendance", ["on": true, "default": "off"])
    }

    @MainActor func testTheRoomsPagesSpeakerInviteAndEnd() throws {
        let hub = try mockHub()
        XCUIDevice.shared.appearance = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] == "dark" ? .dark : .light
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair(hub), "-tab", "assistant", "-openShellPage", "recordings", "-showMeetingSample", "room-listening"]
        app.launchPinned()
        XCTAssertTrue(element(app, "meeting-thread-header").waitForExistence(timeout: 20), "no meeting room:\n\(app.debugDescription)")
        XCTAssertTrue(element(app, "recording-live-hide").exists, "no way out of the room")
        let persona = element(app, "meeting-room-persona")
        XCTAssertTrue(persona.waitForExistence(timeout: 10), "Persona isn't shown in the room: \(app.debugDescription)")
        XCTAssertTrue(persona.label.contains("屏幕回答"), persona.label)
        try shot(app, "live")

        // The pages, by name.
        for (tab, name) in [("highlights", "highlights"), ("map", "map"), ("live", "back")] {
            let button = app.buttons["meeting-room-tab-\(tab)"].firstMatch
            XCTAssertTrue(button.waitForExistence(timeout: 5), app.debugDescription)
            button.tap()
            XCTAssertTrue(waitFor(5) { button.isSelected }, "\(tab) not picked")
            if name != "back" { try shot(app, name) }
        }

        // Out loud, and back on screen.
        let speaker = app.buttons["meeting-room-speaker"].firstMatch
        XCTAssertTrue(speaker.waitForExistence(timeout: 5), app.debugDescription)
        speaker.tap()
        XCTAssertTrue(waitFor(10) { persona.label.contains("出声回答") }, "not out loud: \(persona.label)")
        try shot(app, "aloud")
        speaker.tap()
        XCTAssertTrue(waitFor(10) { persona.label.contains("屏幕回答") }, "not back on screen: \(persona.label)")

        // Persona leaves (the room keeps its notes, #8851) and is invited again.
        let inviteButton = app.buttons["meeting-room-invite-button"].firstMatch
        inviteButton.tap()
        let invite = element(app, "meeting-room-invite")
        XCTAssertTrue(invite.waitForExistence(timeout: 10), "Persona still in: \(app.debugDescription)")
        XCTAssertFalse(speaker.isEnabled, "the speaker without Persona")
        // The thread says it left where it happened (#8731), after the header's joined row.
        XCTAssertTrue(element(app, "meeting-thread-left").waitForExistence(timeout: 10), "no left row: \(app.debugDescription)")
        try shot(app, "alone")
        invite.tap()
        XCTAssertTrue(persona.waitForExistence(timeout: 10), "Persona not back: \(app.debugDescription)")
        // Joined again, after the left row (the header's joined row may be on the thread's earlier page by now).
        let left = element(app, "meeting-thread-left")
        XCTAssertTrue(waitFor(10) {
            app.descendants(matching: .any).matching(identifier: "meeting-thread-joined").allElementsBoundByIndex
                .contains { $0.frame.minY > left.frame.minY }
        }, "no joined row for the invite: \(app.debugDescription)")

        // End asks first; a tap outside the question keeps the meeting going (the dialog shows no Cancel on iOS 26).
        let end = app.buttons["recording-stop"].firstMatch
        XCTAssertTrue(end.exists, app.debugDescription)
        end.tap()
        let confirm = app.buttons["结束并生成纪要"].firstMatch
        XCTAssertTrue(confirm.waitForExistence(timeout: 5), app.debugDescription)
        try shot(app, "end")
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3)).tap()
        XCTAssertTrue(waitFor(5) { !confirm.exists }, "the question stays")
        XCTAssertTrue(element(app, "meeting-thread-header").exists, "the room left after Keep Going")
    }

    /// The B2 experiment (#8731): Settings › Meetings ends with an Experimental switch, off until turned on, for echo
    /// cancellation with the speaker. Turned back off at the end: the app's settings outlive the test.
    @MainActor func testTheEchoExperimentIsASwitchOffUntilTurnedOn() throws {
        let hub = try mockHub()
        XCUIDevice.shared.appearance = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] == "dark" ? .dark : .light
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair(hub), "-tab", "settings", "-openMeetingSettings", "YES"]
        app.launchPinned()
        let echo = app.switches["settings-meeting-echo-speaker"].firstMatch
        XCTAssertTrue(element(app, "settings-meeting-invite").waitForExistence(timeout: 20), app.debugDescription)
        for _ in 0..<4 where !echo.isHittable { app.swipeUp() }
        XCTAssertTrue(echo.waitForExistence(timeout: 5), "no experiment: \(app.debugDescription)")
        XCTAssertEqual(echo.value as? String, "0", "the experiment is off until turned on")
        try shot(app, "settings")
        echo.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)).tap()
        XCTAssertTrue(waitFor(5) { echo.value as? String == "1" }, "not turned on")
        echo.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)).tap()
        XCTAssertTrue(waitFor(5) { echo.value as? String == "0" }, "not turned off again")
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
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/room-\(appearance)-\(name).png"))
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

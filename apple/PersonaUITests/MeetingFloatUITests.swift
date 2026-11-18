import XCTest

/// The floating meeting window (#8731 V5, docs/design/DESIGN_SYSTEM.md "The floating meeting window") against a mock hub
/// (PERSONA_MOCK_HUB): the made-up room meeting (`-showMeetingSample room-listening`) put away with the room's collapse,
/// the window over the app with the meeting's name, its time and who speaks (`-showFloatSpeaker` pins one for the
/// pictures); dragged, it settles on the left; pulled past the side, it docks as a bar, and the bar brings it back; a tap
/// brings the room back. zh-Hans. PERSONA_SHOTS=<dir> saves float-<appearance>-<name>.png.
final class MeetingFloatUITests: XCTestCase {
    override func setUpWithError() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { return }
        try post(hub, "/mock/meeting_conversation", ["on": true, "room": "ok", "end_seconds": 180])
    }

    @MainActor func testTheWindowOverTheAppDocksAndOpensTheRoom() throws {
        let app = try collapsed(speaker: "someone")
        let window = element(app, "meeting-float")
        XCTAssertTrue(element(app, "meeting-float-someone").exists, "not someone speaking:\n\(app.debugDescription)")
        try shot(app, "someone")

        // Dragged to the left: it settles on the left side.
        let start = window.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        start.press(forDuration: 0.1, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.45)))
        XCTAssertTrue(waitFor(5) { window.frame.midX < app.frame.midX }, "not on the left: \(window.frame)")
        try shot(app, "left")

        // Pulled past the side: a bar; the bar brings it back. Once the snap has settled, a slow, held drag to just inside
        // the screen's edge: the card's middle there puts half of it past the side (a finger can't go further). A touch
        // ending on the edge itself was lost, and a quick drag read as a tap on the card, which opened the room.
        sleep(1)
        let left = window.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        left.press(forDuration: 0.4, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.03, dy: 0.45)),
                   withVelocity: .slow, thenHoldForDuration: 0.3)
        let dock = element(app, "meeting-float-dock")
        XCTAssertTrue(dock.waitForExistence(timeout: 5), "no bar:\n\(app.debugDescription)")
        try shot(app, "dock")
        dock.tap()
        XCTAssertTrue(window.waitForExistence(timeout: 5), "the bar didn't bring it back")

        // A tap: the room again, and the window gone under it.
        window.tap()
        XCTAssertTrue(element(app, "meeting-thread-header").waitForExistence(timeout: 15), "no room:\n\(app.debugDescription)")
    }

    /// Persona answering, you (a headset) and quiet, for the pictures.
    @MainActor func testTheOtherSpeakers() throws {
        for speaker in ["persona", "you", "quiet"] {
            let app = try collapsed(speaker: speaker)
            XCTAssertTrue(element(app, "meeting-float-\(speaker)").exists, "not \(speaker):\n\(app.debugDescription)")
            try shot(app, speaker)
            app.terminate()
        }
    }

    // MARK: Helpers

    /// The meeting room put away (its collapse) with `speaker` pinned: the window shows.
    @MainActor private func collapsed(speaker: String) throws -> XCUIApplication {
        let hub = try mockHub()
        XCUIDevice.shared.appearance = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] == "dark" ? .dark : .light
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair(hub), "-tab", "assistant", "-openShellPage", "recordings",
                               "-showMeetingSample", "room-listening", "-showFloatSpeaker", speaker]
        app.launchPinned()
        let hide = element(app, "recording-live-hide")
        XCTAssertTrue(hide.waitForExistence(timeout: 25), "no meeting room:\n\(app.debugDescription)")
        hide.tap()
        XCTAssertTrue(element(app, "meeting-float").waitForExistence(timeout: 10), "no floating window:\n\(app.debugDescription)")
        return app
    }

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
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/float-\(appearance)-\(name).png"))
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

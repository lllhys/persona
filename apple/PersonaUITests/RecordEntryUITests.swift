import XCTest

/// One Record entry on the iPhone (#5243, docs/apple/recording.md「Meeting recordings」), against a mock hub (PERSONA_MOCK_HUB)
/// with the meeting conversation on: the Recordings page has one prominent Record button and a Meeting / Note choice under
/// it (Note hides Persona's options), the drawer's row is Recordings and opens the page (#5517), and a running recording's
/// page menu turns it into a note and back (the mock's `GET /mock/recordings` shows each upload's `kind`). A made-up meeting
/// stands in for the microphone (`-showMeetingSample room-listening`); Record itself is never tapped (it would open the
/// microphone). The app runs in zh-Hans. PERSONA_SHOTS=<dir> saves record-<appearance>-<name>.png
/// (PERSONA_SHOTS_APPEARANCE names the appearance).
final class RecordEntryUITests: XCTestCase {
    private let live = "00000000-0000-0000-0000-000000000002"

    override func setUpWithError() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { return }
        try post(hub, "/mock/recording_minutes", ["mode": "pending"])
        try post(hub, "/mock/meeting_conversation", ["on": true, "room": "ok", "end_seconds": 180])
        try post(hub, "/mock/meeting_attendance", ["on": true, "default": "listener"])
    }

    /// The shared mock goes back to how it starts (the features on, attending's default off).
    override func tearDownWithError() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { return }
        try post(hub, "/mock/meeting_conversation", ["on": true, "room": "ok", "end_seconds": 180])
        try post(hub, "/mock/meeting_attendance", ["on": true, "default": "off"])
    }

    /// One start button (no Start Recording beside it) and, on the quick start, Meeting | Note: Meeting by default with
    /// who is in it (Invite Persona, #8731); Note hides that and leaves the same button.
    @MainActor func testOneRecordButtonAndTheMeetingNoteChoice() throws {
        let hub = try mockHub()
        let app = launch(hub, ["-openShellPage", "recordings"])
        let record = app.buttons["recording-meeting-start"]
        XCTAssertTrue(record.waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertEqual(record.label, "开始会议")
        XCTAssertFalse(app.buttons["recording-start"].exists, "one start button, no second one")
        let kind = element(app, "recording-kind")
        reveal(kind, in: app)
        XCTAssertTrue(kind.waitForExistence(timeout: 10), app.debugDescription)
        let meetingSegment = kind.buttons["会议"].firstMatch
        XCTAssertTrue(waitFor { meetingSegment.isSelected }, "Meeting by default: \(app.debugDescription)")
        // The hub's default here is Listen, so Persona is already in it (#8731: Invite Persona Automatically on).
        let role = element(app, "meeting-persona-invited")
        reveal(role, in: app)
        XCTAssertTrue(role.waitForExistence(timeout: 15), "a meeting shows who is in it: \(app.debugDescription)")
        scrollToTop(app)
        try shot(app, "start")

        reveal(kind, in: app)
        let note = kind.buttons["笔记"].firstMatch
        XCTAssertTrue(note.waitForExistence(timeout: 5), app.debugDescription)
        note.tap()
        XCTAssertTrue(waitFor { note.isSelected }, "Note picked: \(app.debugDescription)")
        XCTAssertTrue(waitFor(5) { !role.exists }, "a note hides who is in it")
        XCTAssertFalse(element(app, "meeting-private-start").exists)
        XCTAssertFalse(app.buttons["meeting-attendee"].exists)
        XCTAssertTrue(element(app, "recording-kind-detail").exists, "what a note is, under the choice")
        XCTAssertFalse(app.buttons["recording-start"].exists)
        scrollToTop(app)
        XCTAssertTrue(record.isHittable, "the same button records the note")
        XCTAssertEqual(record.label, "开始笔记") // it says what it starts (#8731)
        try shot(app, "start-note")

        reveal(kind, in: app)
        meetingSegment.tap()
        reveal(role, in: app)
        XCTAssertTrue(role.waitForExistence(timeout: 5), "Meeting shows who is in it again")
    }

    /// The drawer's row is Recordings, a place like the rows under it (#5517): a tap opens the page with its Record
    /// button and records nothing (no start under way, no microphone prompt, nothing new on the hub). Record is the page's.
    @MainActor func testTheDrawerOpensRecordings() throws {
        let hub = try mockHub()
        let before = recordingIDs(hub)
        // Opened with its button, as a finger does: a drawer opened by the launch argument can come up without its rows.
        let app = launch(hub, ["-newAssistant", "YES"])
        let menu = app.buttons["assistant-sidebar-button"].firstMatch
        XCTAssertTrue(menu.waitForExistence(timeout: 20), app.debugDescription)
        sleep(1)
        menu.tapCenter()
        let row = app.buttons["sidebar-meeting-recording"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertTrue(row.label.contains("会议"), row.label)
        XCTAssertFalse(row.label.contains("会议中") || row.label.contains("录音中"), "the place, Meetings (#8731), not a running one: \(row.label)")
        try shot(app, "drawer")

        row.tap()
        let record = app.buttons["recording-meeting-start"]
        XCTAssertTrue(record.waitForExistence(timeout: 15), "the row opens Meetings: \(app.debugDescription)")
        XCTAssertEqual(record.label, "开始会议")
        sleep(3)
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        XCTAssertFalse(springboard.alerts.firstMatch.exists, "no microphone prompt: the row starts nothing")
        XCTAssertTrue(record.isEnabled, "no start under way (Record is disabled while one starts)")
        XCTAssertFalse(app.buttons["recording-stop"].exists, "nothing recording")
        XCTAssertEqual(recordingIDs(hub), before, "the hub got a recording")
        try shot(app, "drawer-opens")
    }

    /// A room meeting being recorded: its page menu turns it into a note (the hub gets `kind: note` without the room, and
    /// the sheet shows the live view), and the live view's menu makes it a meeting again (`kind: meeting` with the room
    /// again, and the meeting page).
    @MainActor func testARunningRecordingSwitchesBetweenMeetingAndNote() throws {
        let hub = try mockHub()
        let app = launch(hub, ["-openShellPage", "recordings", "-showMeetingSample", "room-listening", "-openChatsFresh", "YES"])
        XCTAssertTrue(element(app, "meeting-thread-header").waitForExistence(timeout: 20), "no meeting page:\n\(app.debugDescription)")
        XCTAssertTrue(waitFor(20) { self.kind(hub) == "meeting" && self.room(hub) != nil }, "the meeting never reached the hub")

        let menu = element(app, "meeting-page-menu")
        XCTAssertTrue(menu.waitForExistence(timeout: 10), app.debugDescription)
        menu.tapCenter()
        let note = app.buttons["笔记"].firstMatch
        // At the largest text sizes the menu scrolls: Meeting / Note is its last section.
        if !note.waitForExistence(timeout: 3) {
            for _ in 0..<4 where !note.exists { app.swipeUp(velocity: .slow) }
        }
        XCTAssertTrue(note.waitForExistence(timeout: 5), "no Note in the page menu: \(app.debugDescription)")
        try shot(app, "live-menu")
        note.tap()
        XCTAssertTrue(waitFor(20) { self.kind(hub) == "note" && self.room(hub) == nil }, "the hub still has a meeting: \(String(describing: kind(hub)))")
        let liveMenu = element(app, "recording-page-menu")
        XCTAssertTrue(liveMenu.waitForExistence(timeout: 10), "a note's sheet is the live view:\n\(app.debugDescription)")
        XCTAssertFalse(element(app, "meeting-thread-header").exists, "no meeting page for a note")
        XCTAssertFalse(element(app, "meeting-attending").exists, "no Persona for a note")
        try shot(app, "live-note")

        liveMenu.tapCenter()
        let meeting = app.buttons["会议"].firstMatch
        XCTAssertTrue(meeting.waitForExistence(timeout: 5), "no Meeting in the live view's menu: \(app.debugDescription)")
        try shot(app, "live-note-menu")
        meeting.tap()
        XCTAssertTrue(waitFor(20) { self.kind(hub) == "meeting" && self.room(hub)?["attendance"] as? String == "listener" },
                      "not a meeting again on the hub: \(String(describing: kind(hub))) \(String(describing: room(hub)))")
        XCTAssertTrue(element(app, "meeting-thread-header").waitForExistence(timeout: 10), "a meeting again: its page:\n\(app.debugDescription)")
    }

    // MARK: Helpers

    @MainActor private func launch(_ hub: String, _ args: [String]) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair(hub), "-tab", "assistant"] + args
        app.launchPinned()
        return app
    }

    @MainActor private func element(_ app: XCUIApplication, _ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
    }

    @MainActor private func text(_ e: XCUIElement) -> String { "\(e.label) \(e.value ?? "")" }

    /// At the largest text sizes the options are below the fold.
    @MainActor private func reveal(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<5 where !(element.exists && element.isHittable) { app.swipeUp() }
    }

    @MainActor private func scrollToTop(_ app: XCUIApplication) {
        let record = app.buttons["recording-meeting-start"]
        for _ in 0..<5 where !(record.exists && record.isHittable) { app.swipeDown() }
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
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/record-\(appearance)-\(name).png"))
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

    /// The made-up meeting's last upload as the mock stored it.
    private func upload(_ hub: String) -> [String: Any]? {
        guard let data = try? Data(contentsOf: URL(string: hub + "/mock/recordings")!),
              let all = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        return (all["recordings"] as? [String: Any])?[live] as? [String: Any]
    }

    private func kind(_ hub: String) -> String? { upload(hub)?["kind"] as? String }

    /// Every recording the mock has been sent.
    private func recordingIDs(_ hub: String) -> Set<String> {
        guard let data = try? Data(contentsOf: URL(string: hub + "/mock/recordings")!),
              let all = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [] }
        return Set(((all["recordings"] as? [String: Any]) ?? [:]).keys)
    }

    private func room(_ hub: String) -> [String: Any]? { upload(hub)?["room"] as? [String: Any] }
}

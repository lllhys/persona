import XCTest

/// Realtime voice in the meeting on the iPhone (#3862 phase 4; contract docs/assistant/meeting-minutes.md「Phase 4: realtime
/// voice in the meeting」, the phone's side docs/apple/recording.md「Realtime voice (phase 4)」), end to end against a mock
/// hub started with `MOCK_VOICE=1` (PERSONA_MOCK_HUB): its voice extension lists `meeting`, and its meeting socket
/// (`scripts/mock/voice_meeting_mock.py`, read back with `GET /mock/voice_meeting`) checks the start against its own room,
/// counts the audio inside and outside Talk spans, and answers a span in the room's thread. A made-up meeting stands in for
/// the microphone (`-showMeetingSample room-taking-part`): the recorder's tap gets canned audio, so a held Talk has
/// something to send. Every test starts from a fresh mock (`POST /mock/recording_minutes`, the meeting features on, the
/// voice log reset). The app runs in zh-Hans: elements are found by their identifiers, never their words.
/// PERSONA_SHOTS=<dir> saves talk-<appearance>-<name>.png (PERSONA_SHOTS_APPEARANCE names the appearance).
final class MeetingTalkUITests: XCTestCase {
    private let live = "00000000-0000-0000-0000-000000000002"

    override func setUpWithError() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { return }
        try post(hub, "/mock/recording_minutes", ["mode": "pending"])
        try post(hub, "/mock/meeting_conversation", ["on": true, "room": "ok", "end_seconds": 180])
        try post(hub, "/mock/meeting_attendance", ["on": true, "default": "participant", "answer": "ok", "kb_read_only": false])
        _ = try? json(hub, "/mock/voice_meeting?meeting=on&reset=1")
    }

    override func tearDownWithError() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { return }
        try post(hub, "/mock/meeting_conversation", ["on": true, "room": "ok", "end_seconds": 180])
        try post(hub, "/mock/meeting_attendance", ["on": true, "default": "off"])
        _ = try? json(hub, "/mock/voice_meeting?meeting=on")
    }

    // MARK: The meeting call

    /// Taking part, with the room ready and the extension listing `meeting`: a meeting call starts (mode, recording,
    /// the room as its target); held Talk sends `talk on`, the span's audio and `talk off`, and nothing outside the span;
    /// the call's answer shows in the room's thread.
    @MainActor func testHeldTalkGoesToTheMeetingCall() throws {
        let hub = try mockHub()
        let app = try meetingPage(hub)
        let call = try XCTUnwrap(waitForCall(hub), "no meeting call: \(String(describing: try? voice(hub)))")
        let start = try XCTUnwrap(call["start"] as? [String: Any])
        XCTAssertEqual(start["mode"] as? String, "meeting")
        XCTAssertEqual(start["protocol"] as? Int, 2)
        XCTAssertEqual(start["recording_id"] as? String, live)
        let theRoom = try XCTUnwrap(try room(hub, live))
        let target = try XCTUnwrap(start["target"] as? [String: Any])
        XCTAssertEqual(target["machine_id"] as? String, theRoom["machine_id"] as? String)
        XCTAssertEqual(target["session_id"] as? String, theRoom["session_id"] as? String, "bound to the meeting's room")
        XCTAssertNotNil(start["echo_cancellation"] as? Bool, "the start says whether echo cancellation runs")
        XCTAssertEqual((call["refused"] as? [Any])?.count ?? 0, 0, "the extension refused the call: \(call)")

        let talk = element(app, "meeting-talk")
        XCTAssertTrue(talk.waitForExistence(timeout: 15), "no Talk:\n\(app.debugDescription)")
        // A while with the call up and no span: the meeting's audio goes nowhere.
        sleep(3)
        try shot(app, "page")
        let answers = app.descendants(matching: .any).matching(identifier: "meeting-thread-answer")
        let before = answers.count
        // The Wording is on screen before the first hold: only then may that span go to the voice service.
        let privacy = element(app, "meeting-talk-privacy")
        XCTAssertTrue(privacy.exists && privacy.label.contains("语音服务"), "Talk's first-use note isn't showing:\n\(app.debugDescription)")
        talk.press(forDuration: 3)

        XCTAssertTrue(waitFor(15) { ((try? self.voice(hub))?["talk_off"] as? Int ?? 0) >= 1 }, "no talk off: \(String(describing: try? voice(hub)))")
        let after = try voice(hub)
        XCTAssertEqual(after["talk_on"] as? Int, 1, "one span")
        XCTAssertGreaterThan(after["audio_in"] as? Int ?? 0, 0, "the span's audio never reached the call")
        XCTAssertEqual(after["audio_out"] as? Int, 0, "audio went outside the span")
        XCTAssertTrue(waitFor(30) { answers.count > before }, "the call's answer never reached the room's thread:\n\(app.debugDescription)")
        // While the call's voice says it, Stop Speaking shows over the bar; then the thread settles on the answer.
        let stopSpeaking = element(app, "meeting-page-stop-speaking")
        if stopSpeaking.waitForExistence(timeout: 3) { try shot(app, "speaking") }
        XCTAssertTrue(waitFor(15) { !stopSpeaking.exists }, "Stop Speaking stayed after the voice ended")
        sleep(1)
        if answers.count > before { reveal(answers.element(boundBy: answers.count - 1), in: app) }
        try shot(app, "answered")
        XCTAssertEqual((try voice(hub))["audio_out"] as? Int, 0, "nothing of the meeting after the span either")
        // The note was shown as the span went to the voice service: it doesn't show again.
        XCTAssertFalse(privacy.exists, "the first-use note stayed: \(privacy.label)")
    }

    /// Talk held, as the page shows it (`room-talk-held` holds it for ten seconds once the call is up and the first-use
    /// note has been on screen under Talk, so the owner has been told), then the Lock Screen's Talk: a tap opens a talk
    /// window, which the Lock Screen says ("Listening to you…"), and Done ends it. A Done tapped once the window has
    /// closed (by Done, or on its own after 2 s of silence) opens no other span, and no audio ever reaches the call
    /// outside a span.
    @MainActor func testTalkHeldAndTheLockScreensTalkWindow() throws {
        let hub = try mockHub()
        let app = try meetingPage(hub, sample: "room-talk-held")
        XCTAssertNotNil(waitForCall(hub), "no meeting call")
        let caption = element(app, "meeting-ask-caption")
        XCTAssertTrue(waitFor(30) { ((try? self.voice(hub))?["talking"] as? Bool) == true }, "Talk was never held")
        XCTAssertTrue(caption.waitForExistence(timeout: 5), "nothing says Persona hears:\n\(app.debugDescription)")
        try shot(app, "held")
        XCTAssertTrue(waitFor(20) { ((try? self.voice(hub))?["talk_off"] as? Int ?? 0) >= 1 }, "the held span never ended")

        // The Lock Screen: Talk leads the activity's buttons; a tap opens a window.
        XCUIDevice.shared.perform(NSSelectorFromString("pressLockButton"))
        sleep(2)
        XCUIDevice.shared.perform(NSSelectorFromString("pressLockButton"))
        sleep(3)
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["Allow", "允许", "Always Allow", "始终允许"] {
            let b = springboard.buttons[label].firstMatch
            if b.exists { b.tap(); sleep(2) }
        }
        let talk = springboard.buttons["说话"].firstMatch
        let found = talk.waitForExistence(timeout: 10)
        try screen("lock-before")
        if found {
            talk.tap()
            let listening = springboard.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "正在听你说")).firstMatch
            let open = listening.waitForExistence(timeout: 10)
            try screen("lock")
            XCTAssertTrue(open, "the Lock Screen never said Persona listens:\n\(springboard.debugDescription)")
            XCTAssertTrue(waitFor(10) { ((try? self.voice(hub))?["spans"] as? Int ?? 0) >= 2 }, "the window opened no span on the call")
            // Done while the window is known to be open (the canned audio's quiet may close it first).
            let done = springboard.buttons["说完"].firstMatch
            if done.waitForExistence(timeout: 5), ((try? voice(hub))?["talking"] as? Bool) == true { done.tap() }
            XCTAssertTrue(waitFor(15) { ((try? self.voice(hub))?["talk_off"] as? Int ?? 0) >= 2 }, "the window never closed its span")
            // The window is closed: a Done the Lock Screen may still show says so with its tap, and opens nothing.
            if done.exists { done.tap() }
            sleep(4)
            let end = try voice(hub)
            XCTAssertEqual(end["spans"] as? Int, 2, "a Done after the window closed opened another span: \(end)")
            XCTAssertEqual(end["talking"] as? Bool, false, "a span stayed open: \(end)")
            XCTAssertEqual(end["audio_out"] as? Int, 0, "audio reached the call outside its spans: \(end)")
            XCTAssertGreaterThan(end["audio_in"] as? Int ?? 0, 0, "the spans' audio never reached the call")
        }
        // Unlock (swipe up; no passcode on the simulator): a locked simulator hangs the next test run.
        let bottom = springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.99))
        bottom.press(forDuration: 0.1, thenDragTo: springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3)))
        sleep(1)
        XCTAssertTrue(found, "no Talk on the Lock Screen:\n\(springboard.debugDescription)")
    }

    // MARK: Without a call

    /// A tap (#9658) opens Talk as the page's talk window: it stays on through the sample's quiet stretch (2 s of quiet after
    /// 3 s of hum, which ends the Lock Screen's window), says to tap when done, and the next tap ends the span: one
    /// `talk on`, one `talk off`, no audio outside it.
    @MainActor func testATappedTalkStaysOnUntilTheNextTap() throws {
        let hub = try mockHub()
        let app = try meetingPage(hub)
        XCTAssertNotNil(waitForCall(hub), "no meeting call")
        let talk = element(app, "meeting-talk")
        XCTAssertTrue(talk.waitForExistence(timeout: 15), "no Talk:\n\(app.debugDescription)")
        // The first span goes to the call only once the Wording has been on screen for a moment.
        XCTAssertTrue(element(app, "meeting-talk-privacy").waitForExistence(timeout: 10), "no first-use note")
        sleep(2)
        talk.tap()
        XCTAssertTrue(waitFor(10) { ((try? self.voice(hub))?["talking"] as? Bool) == true }, "a tap did not turn Talk on")
        sleep(7)
        XCTAssertEqual((try voice(hub))["talking"] as? Bool, true, "a tapped Talk ended on a pause")
        XCTAssertEqual((try voice(hub))["talk_off"] as? Int ?? 0, 0, "a tapped Talk ended on a pause")
        let caption = element(app, "meeting-ask-caption")
        XCTAssertTrue(caption.exists, "nothing says Persona hears:\n\(app.debugDescription)")
        try shot(app, "tapped")
        talk.tap()
        XCTAssertTrue(waitFor(15) { ((try? self.voice(hub))?["talk_off"] as? Int ?? 0) >= 1 }, "the next tap did not end Talk")
        let after = try voice(hub)
        XCTAssertEqual(after["talk_on"] as? Int, 1, "one span")
        XCTAssertEqual(after["talking"] as? Bool, false)
        XCTAssertEqual(after["audio_out"] as? Int, 0, "audio went outside the span")
    }

    /// A tapped Talk left on turns itself off at the minute and the caption says why (#9658); the span still goes.
    @MainActor func testATappedTalkTurnsOffAtTheMinuteAndSaysWhy() throws {
        let hub = try mockHub()
        let app = try meetingPage(hub)
        XCTAssertNotNil(waitForCall(hub), "no meeting call")
        let talk = element(app, "meeting-talk")
        XCTAssertTrue(talk.waitForExistence(timeout: 15), "no Talk:\n\(app.debugDescription)")
        XCTAssertTrue(element(app, "meeting-talk-privacy").waitForExistence(timeout: 10), "no first-use note")
        sleep(2)
        talk.tap()
        XCTAssertTrue(waitFor(10) { ((try? self.voice(hub))?["talking"] as? Bool) == true }, "a tap did not turn Talk on")
        XCTAssertTrue(waitFor(75) { ((try? self.voice(hub))?["talk_off"] as? Int ?? 0) >= 1 }, "Talk did not turn off at the minute")
        let caption = element(app, "meeting-ask-caption")
        XCTAssertTrue(waitFor(3) { caption.exists && caption.label.contains("已关闭") }, "the caption doesn't say why Talk stopped: \(caption.label)")
        try shot(app, "turned-off")
        XCTAssertEqual((try voice(hub))["audio_out"] as? Int, 0, "audio went outside the span")
    }

    /// A listener has no call: held Talk cuts the segment, the phone transcribes the span (made up in the sample) and asks
    /// the room with `…/ask` (`spoken: true`, its `question_source` `spoken`); nothing reaches the voice socket.
    @MainActor func testAListenersTalkIsAskedThroughAsk() throws {
        let hub = try mockHub()
        let app = try meetingPage(hub, sample: "room-listening")
        let talk = element(app, "meeting-talk")
        XCTAssertTrue(talk.waitForExistence(timeout: 15), "no Talk for a listener:\n\(app.debugDescription)")
        talk.press(forDuration: 2)
        XCTAssertTrue(waitFor(30) { (try? self.asks(hub, self.live))?.contains { $0["question_source"] as? String == "spoken" } == true },
                      "no spoken ask: \(String(describing: try? asks(hub, live)))")
        let asked = try XCTUnwrap(try asks(hub, live).last { $0["question_source"] as? String == "spoken" })
        XCTAssertEqual(asked["question"] as? String, "上次评审定的导出上限是多少？", "the span's words")
        let calls = try voice(hub)
        XCTAssertEqual(calls["calls"] as? Int ?? 0, 0, "a listener has no meeting call")
        XCTAssertEqual(calls["audio_in"] as? Int ?? 0, 0, "a listener's Talk sent audio")
        XCTAssertEqual(calls["audio_out"] as? Int ?? 0, 0)
        // The answer's row settles in the thread (it moves from the ask's card to the room's messages).
        sleep(3)
        try shot(app, "listener-asked")
    }

    // MARK: Privacy

    /// The start says what Talk sends where it sends it (taking part, an extension with `meeting`), and today's words
    /// where it doesn't (an extension from before phase 4).
    @MainActor func testTheStartSaysWhatTalkSends() throws {
        let hub = try mockHub()
        let app = launch(hub)
        let line = element(app, "recording-start-privacy")
        if !line.waitForExistence(timeout: 15) { reveal(line, in: app) }
        XCTAssertTrue(waitFor(20) { line.exists && line.label.contains("说话") }, "no Talk in the start's privacy line: \(line.label)")
        try shot(app, "start")
        app.terminate()

        _ = try json(hub, "/mock/voice_meeting?meeting=off")
        let older = launch(hub)
        let again = element(older, "recording-start-privacy")
        if !again.waitForExistence(timeout: 15) { reveal(again, in: older) }
        XCTAssertTrue(again.waitForExistence(timeout: 10), older.debugDescription)
        sleep(3)
        XCTAssertFalse(again.label.contains("说话"), "Talk sends nothing without meeting calls: \(again.label)")
    }

    // MARK: Helpers: the app

    @MainActor private func launch(_ hub: String, _ args: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        // `-openChatsFresh`: the mock makes the room under the same id for the same made-up recording in every test.
        app.launchArguments = ["-pairURL", pair(hub), "-tab", "assistant", "-openShellPage", "recordings", "-openChatsFresh", "YES"] + args
        app.launchPinned()
        return app
    }

    /// The made-up meeting's page, once the hub's room for it is up.
    @MainActor private func meetingPage(_ hub: String, sample: String = "room-taking-part") throws -> XCUIApplication {
        let app = launch(hub, ["-showMeetingSample", sample])
        XCTAssertTrue(element(app, "meeting-thread-header").waitForExistence(timeout: 20), "no meeting page:\n\(app.debugDescription)")
        XCTAssertTrue(waitFor(20) { (try? self.room(hub, self.live))?["state"] as? String == "ready" }, "the room never came up")
        return app
    }

    /// The mock's meeting-call log once a call has started (`calls` at least 1), or nil after 30 s.
    private func waitForCall(_ hub: String) -> [String: Any]? {
        var log: [String: Any]?
        _ = waitFor(30) {
            log = try? self.voice(hub)
            return (log?["calls"] as? Int ?? 0) >= 1 && log?["start"] is [String: Any]
        }
        return (log?["calls"] as? Int ?? 0) >= 1 ? log : nil
    }

    @MainActor private func element(_ app: XCUIApplication, _ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
    }

    @MainActor private func reveal(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<5 where !(element.exists && element.isHittable) {
            app.swipeUp()
        }
    }

    private func waitFor(_ seconds: TimeInterval = 10, _ ok: () -> Bool) -> Bool {
        let deadline = Date.now.addingTimeInterval(seconds)
        while Date.now < deadline {
            if ok() { return true }
            usleep(250_000)
        }
        return ok()
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        try save(app.screenshot().pngRepresentation, name)
    }

    /// The whole screen (the Lock Screen and the island are the springboard's).
    @MainActor private func screen(_ name: String) throws {
        let image = XCUIScreen.main.screenshot()
        let a = XCTAttachment(screenshot: image)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        try save(image.pngRepresentation, name)
    }

    private func save(_ png: Data, _ name: String) throws {
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let appearance = env["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
        try png.write(to: URL(fileURLWithPath: "\(dir)/talk-\(appearance)-\(name).png"))
    }

    // MARK: Helpers: the mock hub

    private func mockHub() throws -> String {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("需要独立 mock hub") }
        // A mock without MOCK_VOICE has no meeting socket: nothing here would hold.
        guard (try? json(hub, "/mock/voice_meeting"))?["meeting"] != nil else { throw XCTSkip("需要 MOCK_VOICE=1 的 mock hub") }
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

    private func json(_ hub: String, _ path: String) throws -> [String: Any] {
        let data = try Data(contentsOf: URL(string: hub + path)!)
        return try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
    }

    /// What the app sent to meeting calls (`GET /mock/voice_meeting`): `calls`, the newest `start`, `talk_on` / `talk_off`,
    /// `spans`, `talking`, `audio_in` / `audio_out` (frames inside and outside spans), `refused`.
    private func voice(_ hub: String) throws -> [String: Any] {
        try json(hub, "/mock/voice_meeting")
    }

    private func room(_ hub: String, _ id: String) throws -> [String: Any]? {
        ((try json(hub, "/mock/meeting_conversation")["meetings"] as? [String: Any])?[id] as? [String: Any])?["room"] as? [String: Any]
    }

    private func asks(_ hub: String, _ id: String) throws -> [[String: Any]] {
        let live = try json(hub, "/mock/meeting_attendance")["live"] as? [String: Any]
        return (live?[id] as? [String: Any])?["asks"] as? [[String: Any]] ?? []
    }
}

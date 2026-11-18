import XCTest

/// A meeting recorded for minutes with the app in the background, transcribed on the hub (#9108), end to end against the
/// mock hub's hub mode (PERSONA_MOCK_HUB; docs/operations/testing.md "Meeting transcription") with the simulator's own
/// microphone, not a made-up meeting. On a phone, iOS killed a recording's app in the background for the CPU a chat row's
/// path detection used, and the recording ended at a segment boundary while the Live Activity counted on.
///
/// Opt-in, since it runs as long as it says: PERSONA_LONG_RECORDING_MINUTES (the minutes in the background, at least 10
/// for the acceptance run) and a microphone the app may use (`xcrun simctl privacy <udid> grant microphone <bundle id>`).
/// Every 30 seconds in the background the audio the mock got must have grown, and one line per check goes to the log
/// (`long-recording: …`), so a runner sampling the app's CPU beside it can line the two up.
final class MeetingBackgroundRecordingUITests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor func testAHubModeMeetingRecordsOnInTheBackground() throws {
        let env = ProcessInfo.processInfo.environment
        guard let minutes = env["PERSONA_LONG_RECORDING_MINUTES"].flatMap(Double.init), minutes > 0 else {
            throw XCTSkip("PERSONA_LONG_RECORDING_MINUTES unset: the long background recording is opt-in")
        }
        guard let hub = env["PERSONA_MOCK_HUB"] else { throw XCTSkip("需要独立 mock hub") } // i18n-ignore
        try post(hub, "/mock/recording_minutes", ["mode": "pending"])
        try post(hub, "/mock/meeting_conversation", ["on": true, "room": "ok", "end_seconds": 180])
        // A room meeting Persona listens in, as on the phone that lost its recordings.
        try post(hub, "/mock/meeting_attendance", ["on": true, "default": "listener", "answer": "ok", "kb_read_only": false])
        try post(hub, "/mock/meeting_transcription", ["on": true, "ready": true, "upstream": "ok", "fatal": "", "line_seconds": 4,
                                                       "final_seconds": 2])
        try put(hub, "/api/v1/assistant", ["meeting_transcription_default": "hub", "meeting_glossary": [String](),
                                           "meeting_transcription_extension": ""])

        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair(hub), "-tab", "assistant", "-openShellPage", "recordings"]
        addUIInterruptionMonitor(withDescription: "microphone") { alert in
            for name in ["允许", "Allow", "好", "OK"] where alert.buttons[name].exists { // i18n-ignore: the system's buttons
                alert.buttons[name].tap()
                return true
            }
            return false
        }
        app.launchPinned()
        let record = app.buttons["recording-meeting-start"].firstMatch
        XCTAssertTrue(record.waitForExistence(timeout: 20), app.debugDescription)
        record.tap()
        app.tap() // lets the interruption monitor answer a microphone prompt, if one came

        // Recording, in hub mode: the socket is up and audio reaches the mock.
        var id = ""
        XCTAssertTrue(waitFor(60) {
            guard let (rid, r) = (try? self.recordings(hub))?.first(where: { ($0.value["socket"] as? Bool) == true }),
                  (r["bytes"] as? Int ?? 0) > 0 else { return false }
            id = rid
            return true
        }, "no hub-mode audio reached the mock: \((try? self.recordings(hub)) ?? [:])")
        print("long-recording: recording \(id) streams; to the background for \(minutes) min")

        XCUIDevice.shared.press(.home)
        let away = Date.now
        var last = try bytes(hub, id)
        while Date.now.timeIntervalSince(away) < minutes * 60 {
            sleep(30)
            let now = try bytes(hub, id)
            let at = Int(Date.now.timeIntervalSince(away))
            print("long-recording: t=\(at)s bytes=\(now) audio=\(now / 32_000)s")
            XCTAssertGreaterThan(now, last, "audio stopped reaching the hub \(at) s into the background")
            last = now
        }

        // Back in front: still recording; then Stop, and the mock has the whole meeting.
        app.activate()
        XCTAssertTrue(waitFor(30) { ((try? self.bytes(hub, id)) ?? 0) > last }, "the recording didn't go on after coming back")
        // The live sheet's, the meeting page's or the Recordings page's Stop, whichever is in front.
        let stop = app.buttons["recording-stop"].firstMatch
        XCTAssertTrue(stop.waitForExistence(timeout: 20), app.debugDescription)
        stop.tap()
        confirmStop(app)
        let total = try bytes(hub, id)
        print("long-recording: stopped with \(total / 32_000) s of audio on the hub")
        XCTAssertGreaterThanOrEqual(Double(total) / 32_000, minutes * 60, "the hub has less audio than the time in the background")
    }

    // MARK: Helpers

    /// Stop asks first: its confirmation's Stop Recording.
    @MainActor private func confirmStop(_ app: XCUIApplication) {
        let named = app.buttons.matching(NSPredicate(format: "label IN %@", ["结束并生成纪要", "停止录音"])).firstMatch // the meeting room's End (#8731), else Stop Recording
        if named.waitForExistence(timeout: 5) { named.tap(); return }
        let dialog = app.sheets.firstMatch.exists ? app.sheets.firstMatch : app.alerts.firstMatch
        if dialog.exists { dialog.buttons.element(boundBy: 0).tap() }
    }

    /// The mock's hub-mode recordings (`GET /mock/meeting_transcription`).
    private func recordings(_ hub: String) throws -> [String: [String: Any]] {
        let data = try Data(contentsOf: URL(string: hub + "/mock/meeting_transcription")!)
        let all = try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
        return all["recordings"] as? [String: [String: Any]] ?? [:]
    }

    /// The audio bytes the mock got for recording `id` (PCM16 at 16 kHz: 32 000 a second).
    private func bytes(_ hub: String, _ id: String) throws -> Int { try recordings(hub)[id]?["bytes"] as? Int ?? 0 }

    private func waitFor(_ seconds: TimeInterval = 10, _ ok: () -> Bool) -> Bool {
        let deadline = Date.now.addingTimeInterval(seconds)
        while Date.now < deadline {
            if ok() { return true }
            usleep(500_000)
        }
        return ok()
    }

    private func pair(_ hub: String) -> String {
        "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
    }

    /// One of the mock's switches.
    private func post(_ hub: String, _ path: String, _ body: [String: Any]) throws {
        var request = URLRequest(url: URL(string: hub + path)!)
        request.httpMethod = "POST"
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        let done = expectation(description: path)
        URLSession.shared.dataTask(with: request) { _, _, _ in done.fulfill() }.resume()
        wait(for: [done], timeout: 5)
    }

    /// `PUT` on the hub, as the app would (the mock takes any full-credentials device).
    private func put(_ hub: String, _ path: String, _ body: [String: Any]) throws {
        var request = URLRequest(url: URL(string: hub + path)!)
        request.httpMethod = "PUT"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer mock", forHTTPHeaderField: "Authorization")
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        let done = expectation(description: path)
        URLSession.shared.dataTask(with: request) { _, _, _ in done.fulfill() }.resume()
        wait(for: [done], timeout: 5)
    }
}

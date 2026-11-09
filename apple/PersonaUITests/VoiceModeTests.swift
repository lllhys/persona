import XCTest

/// The voice mode in the main conversation and an agent's chat (docs/assistant/voice.md 6.2, 6.8, #3508), against
/// scripts/mock/mock_hub.py with MOCK_VOICE=1 (scripts/mock/voice_mock.py):
///   MOCK_VOICE=1 PORT=<port> python3 scripts/mock/mock_hub.py
///   TEST_RUNNER_PERSONA_VOICE_HUB=http://127.0.0.1:<port>
///   TEST_RUNNER_PERSONA_SHOTS=<dir>                 screenshots, named by style, appearance, page, text size, state
///   TEST_RUNNER_PERSONA_VOICE_APPEARANCE=light|dark  for the names only (`simctl ui <udid> appearance` sets it)
///   TEST_RUNNER_PERSONA_UI_STYLE=beautiful           the BeautifulUI style (`launchPinned`)
/// The matrix tests open each `-showVoiceSample` state (no socket, no microphone); `testOpenVoiceWalksTheScript` takes
/// the real path (`-openVoice silent`: the real socket and bar, no audio, which a simulator may not have).
final class VoiceModeTests: XCTestCase {
    private static let agent = ["-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000001"]
    private static let main = ["-app.tab", "assistant", "-tab", "assistant", "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001"]
    /// The states the screenshot matrix covers (docs/assistant/voice.md 6.8), each with what proves it is on screen.
    private static let samples: [(name: String, shows: String)] = [
        ("listening", "voice-bar"), ("working", "voice-line-stop"), ("asking", "voice-asking"), ("approval", "voice-bar"),
        ("refused", "voice-line-clear"), ("plan_feedback", "voice-line-plan"), ("paused", "voice-bar"), ("speaking", "voice-bar"),
        ("ended", "voice-continue"), ("setup", "去设置"),
    ]

    private var hub: String {
        get throws {
            guard let hub = ProcessInfo.processInfo.environment["PERSONA_VOICE_HUB"] else {
                throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_VOICE=1 (PERSONA_VOICE_HUB)")
            }
            return hub
        }
    }

    private func launch(_ args: [String], large: Bool = false) throws -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + (try hub).addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-AppleLanguages", "(zh-Hans)",
                               "-UIPreferredContentSizeCategoryName", large ? "UICTContentSizeCategoryAccessibilityXL" : "UICTContentSizeCategoryL"] + args
        app.launchPinned()
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let look = [UITestStyle.name, env["PERSONA_VOICE_APPEARANCE"] ?? "light"].joined(separator: "-")
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir + "/voice-\(look)-\(name).png"))
    }

    private func element(_ app: XCUIApplication, _ id: String) -> XCUIElement {
        app.descendants(matching: .any)[id].firstMatch
    }

    @MainActor private func matrix(page: String, args: [String], large: Bool) throws {
        for (sample, shows) in Self.samples {
            try XCTContext.runActivity(named: "\(page) \(large ? "AX" : "standard") \(sample)") { _ in
                let app = try launch(args + ["-showVoiceSample", sample], large: large)
                // At large text sizes Send Back as Feedback does not fit beside anything: it is in ⋯. The main conversation's mock
                // session is idle, so its working sample (a run at work) has no Stop to offer.
                let shown = large && sample == "plan_feedback" ? "voice-more" : page == "main" && sample == "working" ? "voice-line" : shows
                let mark = sample == "setup" ? app.buttons[shown].firstMatch : element(app, shown)
                XCTAssertTrue(mark.waitForExistence(timeout: 25), "\(sample): no \(shown)\n\(app.debugDescription)")
                if sample != "setup", sample != "ended" {
                    XCTAssertTrue(element(app, "voice-end").exists, "\(sample): no 结束")
                    XCTAssertTrue(element(app, "voice-tail").waitForExistence(timeout: 10), "\(sample): no captions in the thread")
                }
                Thread.sleep(forTimeInterval: 1.5) // the orb and the line settle
                shot(app, "\(page)-\(large ? "ax" : "std")-\(sample)")
                app.terminate()
            }
        }
    }

    @MainActor func testAgentStandard() throws { try matrix(page: "agent", args: Self.agent, large: false) }
    @MainActor func testAgentLargeText() throws { try matrix(page: "agent", args: Self.agent, large: true) }
    @MainActor func testMainStandard() throws { try matrix(page: "main", args: Self.main, large: false) }
    @MainActor func testMainLargeText() throws { try matrix(page: "main", args: Self.main, large: true) }

    private func log(_ hub: String) throws -> [String: Any] {
        let data = try Data(contentsOf: URL(string: hub + "/mock/voice2")!)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    /// The real path against the mock's script (docs/archive/assistant/voice-plan.md ("Acceptance", #3508)): connected →
    /// handed over → run → paused → asked → refused, Clear and Send → done → Stop; then the background, another page and
    /// leaving each end the call.
    @MainActor func testOpenVoiceWalksTheScript() throws {
        let hub = try hub
        let app = try launch(Self.agent + ["-openVoice", "silent"])
        // A simulator that never answered the microphone prompt shows it as the call starts (xcodebuild reinstalls the
        // app, which drops a `simctl privacy` grant), and the call waits behind it: allow it.
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons
            .matching(NSPredicate(format: "label IN %@", ["Allow", "允许"])).firstMatch
        if allow.waitForExistence(timeout: 5) { allow.tap() }
        let status = element(app, "voice-status")
        XCTAssertTrue(status.waitForExistence(timeout: 30), "no voice bar:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["帮我把登录页那个失败的测试修一下"].waitForExistence(timeout: 15), "no caption in the thread")
        shot(app, "walk-1-handed")
        let starts = try XCTUnwrap(log(hub)["starts"] as? [[String: Any]])
        XCTAssertEqual(starts.last?["protocol"] as? Int, 2)
        XCTAssertEqual((starts.last?["target"] as? [String: String])?["machine_id"], "macbook")
        XCTAssertTrue(app.buttons["voice-line-stop"].waitForExistence(timeout: 10), "no 停止 Claude while it works")
        XCTAssertTrue(app.staticTexts["语音已暂停"].waitForExistence(timeout: 10), "the upstream never paused")
        shot(app, "walk-2-paused")
        XCTAssertTrue(element(app, "voice-asking").waitForExistence(timeout: 10), "the asked card has no mark")
        shot(app, "walk-3-asking")
        let clear = app.buttons["voice-line-clear"]
        XCTAssertTrue(clear.waitForExistence(timeout: 20), "no 清空并发送 for the computer's draft")
        shot(app, "walk-4-refused")
        clear.tap()
        let deadline = Date.now.addingTimeInterval(5)
        var resends: [[String: Any]] = []
        while Date.now < deadline, resends.isEmpty {
            resends = (try log(hub)["resends"] as? [[String: Any]]) ?? []
            if resends.isEmpty { Thread.sleep(forTimeInterval: 0.2) }
        }
        XCTAssertEqual(resends.last?["clear_draft"] as? String, "fix it", "清空并发送 did not send the clearable text back")
        XCTAssertTrue(app.staticTexts["登录页的测试修好了，改了 Login.tsx 和 session.go 两处。"].firstMatch.waitForExistence(timeout: 20),
                      "the result was not read out")
        shot(app, "walk-5-done")
        element(app, "voice-end").tap()
        XCTAssertTrue(element(app, "voice-bar").waitForNonExistence(timeout: 5), "结束 left the bar")
        XCTAssertFalse(element(app, "voice-ended").exists, "结束 by the user leaves no 继续")
        let voice = element(app, "shell-composer-voice")
        XCTAssertTrue(voice.waitForExistence(timeout: 5), "the composer's voice button is not back")

        // The background ends the phone's call; Continue stays.
        voice.tap()
        XCTAssertTrue(status.waitForExistence(timeout: 15))
        XCUIDevice.shared.press(.home)
        Thread.sleep(forTimeInterval: 2)
        app.activate()
        XCTAssertTrue(element(app, "voice-ended").waitForExistence(timeout: 10), "no 「语音已结束 · 继续」 after the background")
        shot(app, "walk-6-background")
        element(app, "voice-continue").tap()
        XCTAssertTrue(status.waitForExistence(timeout: 15), "继续 did not pick the call up")

        // Leaving the page ends the call, and its socket.
        let closes = try XCTUnwrap(log(hub)["closes"] as? Int)
        let back = app.buttons.matching(NSPredicate(format: "label == '返回'")).firstMatch
        XCTAssertTrue(back.waitForExistence(timeout: 5))
        back.tap()
        let gone = Date.now.addingTimeInterval(5)
        while Date.now < gone, (try log(hub)["closes"] as? Int) == closes { Thread.sleep(forTimeInterval: 0.2) }
        XCTAssertEqual(try log(hub)["closes"] as? Int, closes + 1, "the call outlived its page")
    }
}

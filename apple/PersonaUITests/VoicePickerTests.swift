import XCTest

/// Choosing the voice (#2411, docs/assistant/voice.md 5.7, 6.2, 6.7): the call's ⋯ › Voice with the list, while it loads,
/// after it failed and once Retry got it, a pick that keeps the call; and the Voice row on the voice extension's page.
/// Against scripts/mock/mock_hub.py with MOCK_VOICE=1, whose /voices fails or waits on demand (`/mock/voice2?voices=`):
///   MOCK_VOICE=1 PORT=<port> python3 scripts/mock/mock_hub.py
///   TEST_RUNNER_PERSONA_VOICE_HUB=http://127.0.0.1:<port>
///   TEST_RUNNER_PERSONA_SHOTS=<dir>                    screenshots, named by style, appearance and state
///   TEST_RUNNER_PERSONA_VOICE_APPEARANCE=light|dark     for the names only (`simctl ui <udid> appearance` sets it)
final class VoicePickerTests: XCTestCase {
    private static let agent = ["-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000001"]

    override func setUp() {
        continueAfterFailure = false
    }

    override func tearDown() {
        if let hub = try? hub { _ = try? mock(hub, voices: "ok") }
    }

    private var hub: String {
        get throws {
            guard let hub = ProcessInfo.processInfo.environment["PERSONA_VOICE_HUB"] else {
                throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_VOICE=1 (PERSONA_VOICE_HUB)")
            }
            return hub
        }
    }

    /// How the mock's /voices answers from now on; returns what /mock/voice2 says.
    @discardableResult private func mock(_ hub: String, voices: String) throws -> [String: Any] {
        let data = try Data(contentsOf: URL(string: hub + "/mock/voice2?voices=" + voices)!)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    private func launch(_ args: [String]) throws -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + (try hub).addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-AppleLanguages", "(zh-Hans)", "-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryL"] + args
        app.launchPinned()
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let look = [UITestStyle.name, env["PERSONA_VOICE_APPEARANCE"] ?? "light"].joined(separator: "-")
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir + "/picker-\(look)-\(name).png"))
    }

    private func element(_ app: XCUIApplication, _ id: String) -> XCUIElement {
        app.descendants(matching: .any)[id].firstMatch
    }

    /// The menu's Voice item, whatever it says now.
    private func voiceItem(_ app: XCUIApplication, _ words: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", words)).firstMatch
    }

    private func containing(_ app: XCUIApplication, _ words: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", words)).firstMatch
    }

    @MainActor private func openMore(_ app: XCUIApplication) {
        let more = element(app, "voice-more")
        XCTAssertTrue(more.waitForExistence(timeout: 30), "no voice bar:\n\(app.debugDescription)")
        more.tap()
    }

    /// ⋯ opened and its Voice item ready to tap: a call's bar redraws its line as the call goes on, which closes the
    /// menu, so it opens it again until the item stays.
    @MainActor private func steadyVoiceItem(_ app: XCUIApplication, _ words: String) -> XCUIElement? {
        for _ in 0..<4 {
            openMore(app)
            let item = voiceItem(app, words)
            if item.waitForExistence(timeout: 3) {
                Thread.sleep(forTimeInterval: 0.5)
                if item.exists, item.isHittable { return item }
            }
            if voiceItem(app, "音色").exists { closeMenu(app) }
        }
        return nil
    }

    /// The voice starts the mock has had so far (each with the voice it asked for).
    private func starts(_ hub: String) -> [[String: Any]] {
        (try? mock(hub, voices: "ok")["starts"] as? [[String: Any]]) ?? []
    }

    /// The system's microphone prompt, the first time a call starts on the simulator.
    @MainActor private func allowMicrophone() {
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons
            .matching(NSPredicate(format: "label IN %@", ["Allow", "允许"])).firstMatch
        if allow.waitForExistence(timeout: 5) { allow.tap() }
    }

    @MainActor private func closeMenu(_ app: XCUIApplication) {
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.2)).tap()
        Thread.sleep(forTimeInterval: 0.6)
    }

    /// ⋯ › Voice in a call: the list with the current voice in its title; while it loads; after it failed, and Retry.
    @MainActor func testTheCallsVoiceMenu() throws {
        let hub = try hub
        try mock(hub, voices: "ok")
        var app = try launch(Self.agent + ["-showVoiceSample", "listening"])
        XCTAssertTrue(element(app, "voice-tail").waitForExistence(timeout: 25))
        Thread.sleep(forTimeInterval: 1)
        openMore(app)
        let current = voiceItem(app, "音色：温柔")
        XCTAssertTrue(current.waitForExistence(timeout: 5), "the current voice is not named:\n\(app.debugDescription)")
        Thread.sleep(forTimeInterval: 0.6)
        shot(app, "menu-list")
        current.tap()
        XCTAssertTrue(app.buttons["沉稳"].waitForExistence(timeout: 5), "no voices in the submenu:\n\(app.debugDescription)")
        Thread.sleep(forTimeInterval: 0.6)
        shot(app, "menu-voices")
        app.terminate()

        // While it loads.
        try mock(hub, voices: "slow")
        app = try launch(Self.agent + ["-showVoiceSample", "listening"])
        openMore(app)
        XCTAssertTrue(voiceItem(app, "音色").waitForExistence(timeout: 5))
        XCTAssertTrue(containing(app, "正在读取音色").waitForExistence(timeout: 3), "no Loading voices…:\n\(app.debugDescription)")
        Thread.sleep(forTimeInterval: 0.6)
        shot(app, "menu-loading")
        app.terminate()

        // Failed: it says so, asks again by itself (the mock counts), and Retry gets the list once the hub answers.
        try mock(hub, voices: "fail")
        app = try launch(Self.agent + ["-showVoiceSample", "listening"])
        XCTAssertTrue(element(app, "voice-tail").waitForExistence(timeout: 25))
        let before = try XCTUnwrap(try mock(hub, voices: "fail")["voices"] as? Int)
        Thread.sleep(forTimeInterval: 3) // past the first try by itself (2 s)
        let after = try XCTUnwrap(try mock(hub, voices: "fail")["voices"] as? Int)
        XCTAssertGreaterThan(after, before, "a failed list is not asked again by itself")
        openMore(app)
        let failed = voiceItem(app, "音色")
        XCTAssertTrue(failed.waitForExistence(timeout: 5))
        XCTAssertTrue(containing(app, "列表没读到").waitForExistence(timeout: 5), "no Couldn't load · Retry:\n\(app.debugDescription)")
        Thread.sleep(forTimeInterval: 0.6)
        shot(app, "menu-failed")
        try mock(hub, voices: "ok")
        containing(app, "列表没读到").tap()
        Thread.sleep(forTimeInterval: 1.5)
        openMore(app)
        XCTAssertTrue(voiceItem(app, "音色：温柔").waitForExistence(timeout: 5), "Retry did not get the list:\n\(app.debugDescription)")
        closeMenu(app)
        app.terminate()
    }

    /// Picking another voice in a real call keeps the call: a new connection starts with it, and the menu names it.
    @MainActor func testPickingAVoiceKeepsTheCall() throws {
        let hub = try hub
        try mock(hub, voices: "ok")
        let app = try launch(Self.agent + ["-openVoice", "silent"])
        allowMicrophone()
        let status = element(app, "voice-status")
        XCTAssertTrue(status.waitForExistence(timeout: 30), "no voice bar:\n\(app.debugDescription)")
        // The script's question holds the bar's line still for a while (the menu closes when the line's layout changes).
        XCTAssertTrue(element(app, "voice-asking").waitForExistence(timeout: 20), "the script never asked")
        let first = starts(hub).count
        let current = try XCTUnwrap(steadyVoiceItem(app, "音色："), "no Voice item:\n\(app.debugDescription)")
        // Another voice than the one it has (an earlier run may have kept one on the device).
        let (name, id) = current.label.contains("沉稳") ? ("元气", "lively") : ("沉稳", "calm")
        current.tap()
        let other = app.buttons[name]
        XCTAssertTrue(other.waitForExistence(timeout: 5), "no voices in the submenu:\n\(app.debugDescription)")
        other.tap()
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline, starts(hub).count == first { Thread.sleep(forTimeInterval: 0.2) }
        XCTAssertEqual(starts(hub).last?["voice"] as? String, id, "the new connection did not start with the voice picked")
        XCTAssertTrue(status.waitForExistence(timeout: 10), "the call ended")
        XCTAssertFalse(element(app, "voice-ended").exists, "the call ended")
        XCTAssertNotNil(steadyVoiceItem(app, "音色：" + name), "the menu does not name the new voice:\n\(app.debugDescription)")
        closeMenu(app)
        element(app, "voice-end").tap()
        app.terminate()
    }

    /// Settings › Extensions › the voice extension: the Voice row with the current voice, its list, a pick; it says when
    /// the list could not be read, and Retry gets it.
    @MainActor func testTheSettingsVoiceRow() throws {
        let hub = try hub
        try mock(hub, voices: "ok")
        var app = try launch(["-tab", "settings", "-openExtension", "voice"])
        let row = element(app, "voice-timbre-setting")
        XCTAssertTrue(row.waitForExistence(timeout: 30), "no Voice row:\n\(app.debugDescription)")
        // The list came (a Picker; not still Loading voices…): the row names the current voice (picked earlier, or the default).
        XCTAssertTrue(app.buttons["voice-timbre-setting"].waitForExistence(timeout: 10), "the list did not come:\n\(app.debugDescription)")
        Thread.sleep(forTimeInterval: 1)
        shot(app, "settings-row")
        row.tap()
        let lively = app.buttons["元气"]
        XCTAssertTrue(lively.waitForExistence(timeout: 5), "no voices to pick:\n\(app.debugDescription)")
        Thread.sleep(forTimeInterval: 0.6)
        shot(app, "settings-list")
        lively.tap()
        XCTAssertTrue(containing(app, "元气").waitForExistence(timeout: 5), "the pick does not show")
        app.terminate()
        // Kept on the device: the next launch shows it, and a call would start with it.
        app = try launch(["-tab", "settings", "-openExtension", "voice"])
        XCTAssertTrue(element(app, "voice-timbre-setting").waitForExistence(timeout: 30))
        XCTAssertTrue(containing(app, "元气").waitForExistence(timeout: 10), "the pick was not kept")
        app.terminate()
        // The next call starts with it.
        let first = starts(hub).count
        app = try launch(Self.agent + ["-openVoice", "silent"])
        allowMicrophone()
        let deadline = Date.now.addingTimeInterval(30)
        while Date.now < deadline, starts(hub).count == first { Thread.sleep(forTimeInterval: 0.3) }
        XCTAssertEqual(starts(hub).last?["voice"] as? String, "lively", "the next call did not start with the voice picked in Settings")
        app.terminate()

        try mock(hub, voices: "fail")
        app = try launch(["-tab", "settings", "-openExtension", "voice"])
        let retry = element(app, "voice-timbre-retry")
        XCTAssertTrue(retry.waitForExistence(timeout: 30), "no Retry:\n\(app.debugDescription)")
        Thread.sleep(forTimeInterval: 1)
        shot(app, "settings-failed")
        try mock(hub, voices: "ok")
        retry.tap()
        XCTAssertTrue(containing(app, "元气").waitForExistence(timeout: 10), "Retry did not get the list")
        app.terminate()
    }
}

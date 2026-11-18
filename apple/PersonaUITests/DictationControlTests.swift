import XCTest

/// The Dictate to Persona control opens a new conversation with the composer's dictation already running. The DEBUG
/// argument stands in for its foreground intent; the sample stands in for the simulator's missing microphone. A later
/// launch without the argument stays quiet. Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB).
final class DictationControlTests: XCTestCase {
    private let words = "dictation control sample"
    private var app: XCUIApplication!
    private var hub = ""

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.name = "failure"
            shot.lifetime = .keepAlways
            add(shot)
        }
        app?.terminate()
        super.tearDown()
    }

    @MainActor func testControlOpensNewConversationDictatingOnce() throws {
        let largest = ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"]
        for (name, scheme, textSize) in [("light", "light", [String]()), ("dark", "dark", [String]()),
                                         ("light-largest", "light", largest), ("dark-largest", "dark", largest)] {
            let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
            let base = ["-pairURL", pair, "-tab", "assistant", "-probes", "YES", "-ui.scheme", scheme, "-showDictationSample", words] + textSize
            app = XCUIApplication()
            app.launchArguments = base + ["-openDictationControl", "YES"]
            app.launchPinned()
            LiveActivityShots.allowNotifications()

            assertNewConversation()
            let mic = app.buttons["shell-composer-mic"].firstMatch
            let cancel = app.buttons["dictation-bar-cancel"].firstMatch
            XCTAssertTrue(mic.waitForExistence(timeout: 10), "no composer mic")
            XCTAssertTrue(wait { mic.label == "停止听写" && cancel.isHittable }, "dictation did not start:\n\(app.shellTrace)\n\(app.debugDescription)")
            XCTAssertTrue(app.buttons["dictation-bar-stop"].firstMatch.isHittable)
            XCTAssertTrue(app.buttons["dictation-bar-send"].firstMatch.isHittable)
            dismissIconNote()
            shot("dictation-control-\(name)")

            app.terminate()
            app = XCUIApplication()
            app.launchArguments = base
            app.launchPinned()
            LiveActivityShots.allowNotifications()
            assertNewConversation()
            let secondMic = app.buttons["shell-composer-mic"].firstMatch
            let secondCancel = app.buttons["dictation-bar-cancel"].firstMatch
            XCTAssertTrue(secondMic.waitForExistence(timeout: 10), "no composer mic on the second launch")
            XCTAssertFalse(secondCancel.waitForExistence(timeout: 2), "a later launch started dictation again")
            XCTAssertNotEqual(secondMic.label, "停止听写")
            XCTAssertFalse(secondCancel.isHittable)
        }
    }

    @MainActor private func assertNewConversation() {
        let field = app.descendants(matching: .any)["shell-composer-field"].firstMatch
        let dictating = app.buttons["dictation-bar-cancel"].firstMatch
        XCTAssertTrue(wait(30) { field.exists || dictating.exists }, "no starter composer:\n\(app.debugDescription)")
        let greeting = app.staticTexts.matching(NSPredicate(format: "label IN %@", ["早上好", "中午好", "下午好", "晚上好"])).firstMatch
        XCTAssertTrue(greeting.waitForExistence(timeout: 5), "the control did not open a new conversation:\n\(app.debugDescription)")
    }

    @MainActor private func dismissIconNote() {
        let board = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let alert = board.alerts.firstMatch
        guard alert.waitForExistence(timeout: 2) else { return }
        for label in ["OK", "好"] where alert.buttons[label].exists { // i18n-ignore: the system's own button
            alert.buttons[label].tap()
            sleep(1)
            return
        }
    }

    @MainActor private func wait(_ timeout: TimeInterval = 8, _ condition: @escaping () -> Bool) -> Bool {
        let deadline = Date.now.addingTimeInterval(timeout)
        repeat {
            if condition() { return true }
            usleep(200_000)
        } while Date.now < deadline
        return false
    }

    @MainActor private func shot(_ name: String) {
        guard let directory = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        let image = app.screenshot()
        let attachment = XCTAttachment(screenshot: image)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        try? image.pngRepresentation.write(to: URL(fileURLWithPath: directory).appending(path: name + ".png"))
    }
}

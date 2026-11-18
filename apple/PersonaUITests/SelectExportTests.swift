import XCTest

/// Select Messages, Include Questions, Copy and Export Image (#9468), on scripts/mock/mock_hub.py with MOCK_EXPORT=1:
/// long-press the second reply › Select Messages (the row under the bar): selecting, that reply picked, Include
/// Questions offered and on, its question marked as going with it. Off and on again. Export Image: the preview draws a
/// page and Share is on (the share sheet itself is the system's: never opened here). Title and Date off draws it again.
/// Close, Copy: a toast, and selecting ends.
///   TEST_RUNNER_PERSONA_MOCK_URL=http://127.0.0.1:<port> xcodebuild test-without-building … -only-testing:PersonaUITests/SelectExportTests
/// TEST_RUNNER_PERSONA_SHOTS=<dir> saves each step, light and dark (TEST_RUNNER_PERSONA_SCHEMES="light dark" by default).
final class SelectExportTests: XCTestCase {
    private let session = "macbook/a1f3c2d4-0000-4000-8000-000000009468"
    /// The reply's last line, pressed; its row while selecting is named by its first words.
    private let reply = "要我现在提交吗"
    private let replyRow = "会多一点"
    private let question = "改成 15 分钟会更费电吗"
    private var hub = ""

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let mock = ProcessInfo.processInfo.environment["PERSONA_MOCK_URL"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_EXPORT=1 (PERSONA_MOCK_URL)")
        }
        hub = mock
    }

    @MainActor private func launch(_ scheme: String) -> XCUIApplication {
        let url = hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? hub
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", "persona://pair?url=\(url)&token=mock", "-tab", "dev", "-openSession", session,
                               "-ui.scheme", scheme, "-openChatsFresh", "YES", "-openDraftsFresh", "YES"]
        app.launchPinned()
        return app
    }

    /// An element's centre in the window: rows of the flipped chat list report flipped frames.
    @MainActor private func centre(of element: XCUIElement, in app: XCUIApplication) -> XCUICoordinate {
        let window = app.windows.firstMatch.frame
        var f = element.frame
        if !window.contains(CGPoint(x: f.midX, y: f.midY)) { f.origin.y = window.height - f.maxY }
        return app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: f.midX, dy: f.midY))
    }

    /// A message's row while selecting, found by its words.
    @MainActor private func row(_ message: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@ AND label CONTAINS %@", "chat.pick.", message)).firstMatch
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let png = app.screenshot().pngRepresentation
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
        let a = XCTAttachment(data: png, uniformTypeIdentifier: "public.png")
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor private func waitFor(_ predicate: String, _ element: XCUIElement, timeout: TimeInterval = 10) -> Bool {
        XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: predicate), object: element)], timeout: timeout) == .completed
    }

    @MainActor func testSelectFromTheBarIncludeQuestionsExportAndCopy() {
        let schemes = (ProcessInfo.processInfo.environment["PERSONA_SCHEMES"] ?? "light dark").split(separator: " ").map(String.init)
        for scheme in schemes {
            let app = launch(scheme)

            // Long-press the reply: the bar ends in a row of its own, Select Messages.
            let text = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", reply)).firstMatch
            XCTAssertTrue(text.waitForExistence(timeout: 20), "「\(reply)」 is not on screen:\n\(app.debugDescription)")
            sleep(2) // let the list settle at the newest message
            centre(of: text, in: app).press(forDuration: 0.6)
            let select = app.buttons["chat.selectMessages"]
            XCTAssertTrue(select.waitForExistence(timeout: 5), "no Select Messages under the bar:\n\(app.debugDescription)")
            XCTAssertTrue(app.buttons["转发"].exists && app.buttons["复制"].exists, "the bar's actions are not all there")
            XCTAssertGreaterThan(select.frame.minY, app.buttons["转发"].frame.maxY, "Select Messages is not under the actions")
            sleep(1)
            shot(app, "x1-longpress-\(scheme)")
            select.tap()

            // Selecting, the reply picked; Include Questions offered and on: its question goes with it.
            let questions = app.buttons["chat.pickQuestions"]
            XCTAssertTrue(questions.waitForExistence(timeout: 5), "Include Questions is not offered:\n\(app.debugDescription)")
            XCTAssertTrue(row(replyRow, in: app).isSelected, "the pressed reply is not picked")
            XCTAssertFalse(row(question, in: app).isSelected, "its question is picked, not brought")
            XCTAssertEqual(row(question, in: app).value as? String, "随回答一起", "its question is not marked as going with it")
            XCTAssertTrue(app.staticTexts["chat.pickCount"].label.contains("1"))
            for id in ["chat.pickForward", "chat.pickCopy", "chat.pickExport"] {
                XCTAssertTrue(app.buttons[id].exists && app.buttons[id].isEnabled, "\(id) is not on")
            }
            sleep(1)
            shot(app, "x2-selecting-\(scheme)")

            // Off: nothing brought; on again.
            questions.tap()
            XCTAssertTrue(waitFor("value == '关'", questions), "Include Questions did not turn off: \(String(describing: questions.value))")
            XCTAssertNotEqual(row(question, in: app).value as? String, "随回答一起")
            sleep(1)
            shot(app, "x3-questions-off-\(scheme)")
            questions.tap()
            XCTAssertTrue(waitFor("value == '开'", questions), "Include Questions did not turn on again")

            // Export Image: the preview draws a page; Share is on.
            app.buttons["chat.pickExport"].tap()
            let page = app.descendants(matching: .any)["export.page.1"]
            XCTAssertTrue(page.waitForExistence(timeout: 30), "no page in the preview:\n\(app.debugDescription)")
            let share = app.descendants(matching: .any)["export.share"]
            XCTAssertTrue(waitFor("isEnabled == true", share, timeout: 30), "Share stays off: the picture never finished")
            XCTAssertTrue(app.switches["export.questions"].exists, "the preview has no Include Questions")
            sleep(2)
            shot(app, "x4-preview-\(scheme)")

            // Title and Date off: drawn again, Share on again.
            // The switch at the row's trailing end: a tap on its words does not turn it.
            app.switches["export.heading"].coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
            XCTAssertTrue(waitFor("isEnabled == true", share, timeout: 30), "Share stays off after Title and Date")
            app.buttons["export.close"].tap()
            XCTAssertTrue(app.buttons["chat.pickCopy"].waitForExistence(timeout: 5), "selecting ended with the preview")

            // Copy: a toast, and selecting ends.
            app.buttons["chat.pickCopy"].tap()
            XCTAssertTrue(app.staticTexts["已拷贝"].waitForExistence(timeout: 10), "no Copied toast:\n\(app.debugDescription)")
            XCTAssertTrue(waitFor("exists == false", app.buttons["chat.pickCopy"], timeout: 5), "still selecting after Copy")
            app.terminate()
        }
    }
}

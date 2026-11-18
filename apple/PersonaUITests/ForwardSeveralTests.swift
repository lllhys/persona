import XCTest

/// Forwarding several at once (#3838): long-press a reply › Forward › Select Messages…, the reply already picked; Cancel
/// leaves the chat as it was. Again, nothing picked turns Forward off; pick the question too, Forward 2 › an Agent on
/// another computer: one bar for both over its box, a note, Send, and the hub gets two blocks in the chat's order, which
/// the bubble shows as one card. Runs against scripts/mock/mock_hub.py with MOCK_ECHO_SENT=1:
///   TEST_RUNNER_PERSONA_MOCK_URL=http://127.0.0.1:18760 xcodebuild test … -only-testing:PersonaUITests/ForwardSeveralTests
/// TEST_RUNNER_PERSONA_SHOTS=<dir> saves each step, light and dark (TEST_RUNNER_PERSONA_SCHEMES="light dark" by default).
final class ForwardSeveralTests: XCTestCase {
    private let answer = "PR #42 看完了"
    private let question = "看一下 PR #42"
    private let review = "macbook/a1f3c2d4-0000-4000-8000-000000000006"
    private let blog = "a1f3c2d4-0000-4000-8000-000000000003"
    private var hub = ""

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let mock = ProcessInfo.processInfo.environment["PERSONA_MOCK_URL"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_URL)")
        }
        hub = mock
    }

    @MainActor private func launch(_ scheme: String) -> XCUIApplication {
        let url = hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? hub
        let app = XCUIApplication()
        // Fresh chats and boxes: a forward cached by an earlier run would sit where the steps tap, and one a failed run
        // left over a box would stand in for the same message forwarded again.
        app.launchArguments = ["-pairURL", "persona://pair?url=\(url)&token=mock", "-tab", "dev", "-openSession", review,
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

    @MainActor private func openForward(on message: String, in app: XCUIApplication) {
        let text = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", message)).firstMatch
        XCTAssertTrue(text.waitForExistence(timeout: 20), "「\(message)」 is not on screen:\n\(app.debugDescription)")
        sleep(2) // let the list settle at the newest message
        centre(of: text, in: app).press(forDuration: 0.6)
        let forward = app.buttons["转发"]
        XCTAssertTrue(forward.waitForExistence(timeout: 3), "no Forward in the bar")
        forward.tap()
        XCTAssertTrue(app.navigationBars["转发到"].waitForExistence(timeout: 5), "Forward To did not open")
    }

    /// A message's row while selecting, found by its words.
    @MainActor private func row(_ message: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@ AND label CONTAINS %@", "chat.pick.", message)).firstMatch
    }

    @MainActor private func toggle(_ message: String, in app: XCUIApplication) {
        let r = row(message, in: app)
        XCTAssertTrue(r.waitForExistence(timeout: 5), "「\(message)」 has no mark:\n\(app.debugDescription)")
        centre(of: r, in: app).tap()
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

    private func sent() -> [String] {
        struct Sent: Decodable { struct Body: Decodable { var text: String? }; var sent: [Body] }
        guard let data = try? Data(contentsOf: URL(string: hub + "/mock/sent")!),
              let s = try? JSONDecoder().decode(Sent.self, from: data) else { return [] }
        return s.sent.compactMap(\.text)
    }

    @MainActor private func type(_ note: String, in app: XCUIApplication) {
        let fields = app.descendants(matching: .any).matching(identifier: "shell-composer-field")
        XCTAssertTrue(fields.firstMatch.waitForExistence(timeout: 10), "no composer:\n\(app.debugDescription)")
        let focused = fields.matching(NSPredicate(format: "hasKeyboardFocus == true")).firstMatch
        if focused.waitForExistence(timeout: 5) {
            focused.typeText(note)
            return
        }
        let field = fields.allElementsBoundByIndex.last { $0.isHittable } ?? fields.firstMatch
        field.tap()
        field.typeText(note)
    }

    @MainActor func testForwardSeveralMessages() {
        let schemes = (ProcessInfo.processInfo.environment["PERSONA_SCHEMES"] ?? "light dark").split(separator: " ").map(String.init)
        for scheme in schemes {
            let app = launch(scheme)
            // Forward To offers Select Messages… under Share as Image.
            openForward(on: answer, in: app)
            let select = app.buttons["forward.selectMessages"]
            XCTAssertTrue(select.exists && app.buttons["forward.shareImage"].exists, "no Select Messages… beside Share as Image")
            sleep(1)
            shot(app, "s1-picker-\(scheme)")
            select.tap()

            // Selecting, the pressed reply picked; Cancel leaves the chat as it was.
            let forward = app.buttons["chat.pickForward"]
            XCTAssertTrue(forward.waitForExistence(timeout: 5), "selecting did not start")
            XCTAssertTrue(row(answer, in: app).waitForExistence(timeout: 5) && row(answer, in: app).isSelected, "the pressed reply is not picked")
            XCTAssertFalse(row(question, in: app).isSelected)
            app.buttons["chat.pickCancel"].tap()
            XCTAssertTrue(app.descendants(matching: .any)["shell-composer-field"].waitForExistence(timeout: 5), "the box did not come back")
            XCTAssertFalse(forward.exists || row(answer, in: app).exists, "still selecting after Cancel")

            // Again: with nothing picked Forward is off; the question and the reply, Forward 2.
            openForward(on: answer, in: app)
            select.tap()
            XCTAssertTrue(forward.waitForExistence(timeout: 5))
            toggle(answer, in: app)
            XCTAssertFalse(forward.isEnabled, "Forward is on with nothing picked")
            toggle(answer, in: app)
            toggle(question, in: app)
            XCTAssertTrue(row(question, in: app).isSelected && row(answer, in: app).isSelected, "both are not picked")
            // The count is beside Cancel since the bar has Forward · Copy · Export Image (#9468).
            let count = app.staticTexts["chat.pickCount"]
            XCTAssertTrue(forward.isEnabled && count.label.contains("2"), "the bar does not say 2: \(count.label)")
            sleep(1)
            shot(app, "s2-selecting-\(scheme)")
            forward.tap()

            // Forward To with both: no Share as Image, no Select Messages….
            XCTAssertTrue(app.navigationBars["转发到"].waitForExistence(timeout: 5), "Forward To did not open for both")
            XCTAssertFalse(app.buttons["forward.shareImage"].exists || app.buttons["forward.selectMessages"].exists)
            sleep(1)
            shot(app, "s3-picker-two-\(scheme)")
            app.searchFields.firstMatch.tap()
            app.searchFields.firstMatch.typeText("Astro")
            let target = app.buttons["forward.session." + blog]
            XCTAssertTrue(target.waitForExistence(timeout: 5), "search did not find the blog's Agent")
            target.tap()

            // One bar for both over the target's box, then the note.
            XCTAssertTrue(app.descendants(matching: .any)["composer.forwardRoute"].firstMatch.waitForExistence(timeout: 10),
                          "the forward is not over the target's box")
            XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "2 条消息")).firstMatch.exists,
                          "the bar does not say two messages")
            // One route line, so one bar: the chat above may hold earlier forwards' bubbles (ForwardMessageTests).
            XCTAssertEqual(app.descendants(matching: .any).matching(identifier: "composer.forwardRoute").count, 1,
                           "a bar of its own for one of them")
            let note = scheme == "dark" ? "两条一起看 dark" : "两条一起看"
            type(note, in: app)
            sleep(1)
            shot(app, "s4-composer-\(scheme)")
            app.buttons.matching(NSPredicate(format: "identifier == %@ OR label == %@", "shell-composer-send", "发送")).firstMatch.tap()

            // Two blocks, the question's first, then the note; one card in the bubble.
            let deadline = Date.now.addingTimeInterval(10)
            func found() -> String? { sent().first { $0.hasSuffix(note) } }
            while found() == nil, Date.now < deadline { usleep(250_000) }
            let text = found() ?? ""
            XCTAssertEqual(text.components(separatedBy: #"kind="forward""#).count - 1, 2, "not two blocks: \(text)")
            let q = text.range(of: question), a = text.range(of: answer)
            XCTAssertTrue(q != nil && a != nil && q!.lowerBound < a!.lowerBound, "not in the chat's order: \(text)")
            XCTAssertTrue(app.descendants(matching: .any)["bubble.forwardGroup"].firstMatch.waitForExistence(timeout: 10),
                          "the sent bubble has no card for both")
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.02, dy: 0.35)).tap() // the keyboard away
            sleep(2)
            shot(app, "s5-sent-\(scheme)")
            app.terminate()
        }
    }
}

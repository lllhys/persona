import XCTest

/// MCP forms and links on the iPhone (#2322) against scripts/mock/mock_hub.py, in the PR review chat:
/// - `MOCK_ELICITATION=1` (TEST_RUNNER_PERSONA_ELICITATION_HUB=http://127.0.0.1:<port>): the app's hello declares the cards
///   with the platform and version its requests carry; the form keeps its server's order, blocks Submit on a bad value
///   and sends every kind of field; the link's Continue waits until Open Link showed the link in the app's link card.
/// - `MOCK_ELICITATION=unsupported` (TEST_RUNNER_PERSONA_ELICITATION_UNSUPPORTED_HUB): a form the app can't draw says to
///   hand it back and still declines; the link cancels.
/// TEST_RUNNER_PERSONA_SHOTS=<dir> also saves the screens (TEST_RUNNER_PERSONA_SHOTS_APPEARANCE names them light / dark).
final class ElicitationCardTests: XCTestCase {
    private let review = "macbook/a1f3c2d4-0000-4000-8000-000000000006"
    private var app: XCUIApplication!

    private func hub(_ name: String) throws -> String {
        guard let hub = ProcessInfo.processInfo.environment[name] else { throw XCTSkip("needs scripts/mock/mock_hub.py (\(name))") }
        return hub
    }

    private func launch(_ hub: String) {
        continueAfterFailure = false
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", review]
        app.launchPinned()
        allowNotifications()
    }

    /// The system's notification question, answered: while an alert covers the app it isn't active, and its socket
    /// (which brings the cards) waits.
    private func allowNotifications() {
        let alert = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.firstMatch
        guard alert.waitForExistence(timeout: 5) else { return }
        for label in ["允许", "Allow"] where alert.buttons[label].exists {
            alert.buttons[label].tap()
            return
        }
    }

    private func mock(_ hub: String, _ path: String) -> [String: Any] {
        guard let data = try? Data(contentsOf: URL(string: hub + path)!),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [:] }
        return obj
    }

    /// What the app answered for a card, the last time.
    private func resolution(_ hub: String, _ id: String) -> [String: Any]? {
        let all = mock(hub, "/mock/resolved")["resolved"] as? [[String: Any]] ?? []
        return all.last { $0["id"] as? String == id }?["resolution"] as? [String: Any]
    }

    private func waitForResolution(_ hub: String, _ id: String) -> [String: Any]? {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline {
            if let r = resolution(hub, id) { return r }
            usleep(200_000)
        }
        return nil
    }

    private func anything(_ label: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
    }

    /// Scrolls the pending cards (and the card's own form) until `element` can be tapped, towards wherever it is.
    private func reveal(_ element: XCUIElement) {
        let cards = app.scrollViews["pending-cards"]
        let area = cards.exists ? cards : app.windows.firstMatch
        for _ in 0..<10 where !element.isHittable {
            if element.frame.midY > area.frame.midY { area.swipeUp(velocity: .slow) } else { area.swipeDown(velocity: .slow) }
        }
    }

    private func card(_ id: String) -> XCUIElement { app.descendants(matching: .any)["elicitation." + id] }

    @MainActor func testFormAndLink() throws {
        let hub = try hub("PERSONA_ELICITATION_HUB")
        launch(hub)

        let form = anything("MCP 表单等你填写")
        XCTAssertTrue(form.waitForExistence(timeout: 60), app.debugDescription)
        XCTAssertTrue(anything("MCP 链接等你确认").exists)
        // The hello asked for these cards with what this app's requests say about it.
        let hello = mock(hub, "/mock/hello")
        XCTAssertTrue((hello["features"] as? [String] ?? []).contains("elicitation"), "\(hello)")
        XCTAssertEqual(hello["platform"] as? String, "ios-app")
        let platform = hello["platform"] as? String ?? "", version = hello["version"] as? String ?? ""
        XCTAssertFalse(version.isEmpty)
        XCTAssertEqual(hello["request_client"] as? String, platform + "/" + version)

        // Who asks, and the fields in the server's order.
        XCTAssertTrue(anything("来自子 Agent · worker").exists)
        XCTAssertTrue(anything("来自 MCP 服务器 deploy-mcp（配置来源：project）").exists)
        let env = anything("部署环境（必填）"), token = anything("部署口令（选填）")
        XCTAssertTrue(env.exists && anything("副本数（必填）").exists)
        shot("elicitation-form")
        let submit = app.buttons["elicitation.submit"]
        XCTAssertFalse(submit.isEnabled, "the environment is required")

        // A bad value blocks Submit and says why; finished, it goes.
        app.buttons["elicitation.choice.env.0"].tap()
        let owner = app.textFields["elicitation.field.owner"]
        reveal(owner)
        owner.tap()
        owner.typeText("nope")
        let error = app.staticTexts["elicitation.error.owner"]
        XCTAssertTrue(error.waitForExistence(timeout: 5))
        XCTAssertEqual(error.label, "请检查填写内容和格式")
        reveal(submit)
        XCTAssertFalse(submit.isEnabled, "a bad e-mail address blocks Submit")
        shot("elicitation-form-error")
        reveal(owner)
        owner.typeText("@example.com")
        XCTAssertTrue(error.waitForNonExistence(timeout: 5))
        XCTAssertTrue(token.exists)
        XCTAssertLessThan(env.frame.minY, token.frame.minY, "the server's order: the environment before the password")

        let secret = app.secureTextFields["elicitation.field.token"]
        reveal(secret)
        secret.tap()
        secret.typeText("s3cret")
        let notify = app.segmentedControls["elicitation.field.notify"].buttons["是"]
        reveal(notify)
        notify.tap()
        let east = app.buttons["elicitation.choice.regions.0"], north = app.buttons["elicitation.choice.regions.2"]
        reveal(east)
        east.tap()
        reveal(north)
        north.tap()
        reveal(submit)
        XCTAssertTrue(submit.isEnabled)
        reveal(east)
        shot("elicitation-form-filled")
        reveal(submit)
        submit.tap()

        let sent = try XCTUnwrap(waitForResolution(hub, "ix_elicit_form"))
        XCTAssertEqual(sent["action"] as? String, "accept")
        let content = try XCTUnwrap(sent["content"] as? [String: Any])
        XCTAssertEqual(content["env"] as? String, "staging")
        XCTAssertEqual(content["replicas"] as? Int, 2, "the default, as a number")
        XCTAssertEqual(content["notify"] as? Bool, true)
        XCTAssertEqual(content["regions"] as? [String], ["east", "north"])
        XCTAssertEqual(content["owner"] as? String, "nope@example.com")
        XCTAssertEqual(content["token"] as? String, "s3cret")
        XCTAssertTrue(form.waitForNonExistence(timeout: 10))

        // The link: Continue only after Open Link showed it in the link card.
        let open = app.buttons["elicitation.open"], go = app.buttons["elicitation.continue"]
        XCTAssertTrue(open.waitForExistence(timeout: 10))
        XCTAssertTrue(anything("https://example.com/oauth/authorize?state=demo").exists, "the whole link")
        XCTAssertTrue(anything("example.com").exists)
        XCTAssertFalse(go.isEnabled)
        shot("elicitation-link")
        open.tap()
        let close = app.buttons["link-close"]
        XCTAssertTrue(close.waitForExistence(timeout: 10), "the link opens in the app's link card")
        shot("elicitation-link-sheet")
        close.tap()
        XCTAssertTrue(app.descendants(matching: .any)["elicitation.opened"].waitForExistence(timeout: 10))
        XCTAssertTrue(go.isEnabled)
        shot("elicitation-link-opened")
        go.tap()
        let accepted = try XCTUnwrap(waitForResolution(hub, "ix_elicit_url"))
        XCTAssertEqual(accepted["action"] as? String, "accept")
        XCTAssertNil(accepted["content"])
        XCTAssertTrue(open.waitForNonExistence(timeout: 10))
    }

    @MainActor func testUnsupportedFormDeclinesAndLinkCancels() throws {
        let hub = try hub("PERSONA_ELICITATION_UNSUPPORTED_HUB")
        launch(hub)

        let note = anything("这个表单里有 App 还不能显示的项目")
        XCTAssertTrue(note.waitForExistence(timeout: 60), app.debugDescription)
        let form = card("ix_elicit_form")
        XCTAssertFalse(form.buttons["elicitation.submit"].exists)
        let handBack = form.buttons["elicitation.handback"]
        XCTAssertTrue(handBack.exists)
        reveal(handBack)
        shot("elicitation-unsupported")
        form.buttons["elicitation.decline"].tap()
        XCTAssertEqual(waitForResolution(hub, "ix_elicit_form")?["action"] as? String, "decline")
        XCTAssertNil(resolution(hub, "ix_elicit_url"), "only the form's Decline")
        XCTAssertTrue(note.waitForNonExistence(timeout: 10))

        let cancel = card("ix_elicit_url").buttons["elicitation.cancel"]
        XCTAssertTrue(cancel.waitForExistence(timeout: 10))
        reveal(cancel)
        cancel.tap()
        XCTAssertEqual(waitForResolution(hub, "ix_elicit_url")?["action"] as? String, "cancel")
    }

    private func shot(_ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let look = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"].map { "-" + $0 } ?? ""
            let device = UIDevice.current.userInterfaceIdiom == .pad ? "-ipad" : ""
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + device + look + ".png"))
        }
    }
}

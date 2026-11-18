import XCTest

/// Every field for a token, an API key or another service's secret: typed in beside an ordinary field (what a login
/// form looks like to AutoFill), its page goes away, and iOS does not offer to save it as a password (#1598, after
/// #1512 for the plugin pages). Each test also checks the secret still reached the hub.
///
/// Against scripts/mock/mock_hub.py with `MOCK_EXTENSIONS=1 MOCK_BOT=off MOCK_SCREEN_SECRET=1`:
/// `TEST_RUNNER_PERSONA_SECRETS_HUB=http://127.0.0.1:<port>`; `TEST_RUNNER_PERSONA_SHOTS=<dir>` keeps the screenshots, named by
/// `TEST_RUNNER_PERSONA_SECRETS_APPEARANCE` (light / dark) and the style.
final class SecretFieldTests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var hub = ""

    @MainActor private func launch(_ args: [String]) throws -> XCUIApplication {
        guard let hub = env["PERSONA_SECRETS_HUB"] else {
            throw XCTSkip("needs mock_hub.py with MOCK_EXTENSIONS=1 MOCK_BOT=off MOCK_SCREEN_SECRET=1 (PERSONA_SECRETS_HUB)")
        }
        self.hub = hub
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            for label in ["允许", "Allow"] where alert.buttons[label].exists { alert.buttons[label].tap(); return true }
            return false
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair] + args
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 3) { springboard.buttons[label].tap() }
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let look = env["PERSONA_SECRETS_APPEARANCE"] ?? "light"
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "secret-\(name)-\(UITestStyle.name)-\(look).png"))
    }

    /// The hidden field: by its identifier, or by what it says when empty.
    @MainActor private func secret(_ app: XCUIApplication, _ id: String, _ placeholder: String) -> XCUIElement {
        let byID = app.secureTextFields[id]
        if byID.waitForExistence(timeout: 2) { return byID }
        return app.secureTextFields.matching(NSPredicate(format: "placeholderValue == %@", placeholder)).firstMatch
    }

    @MainActor private func field(_ app: XCUIApplication, _ placeholder: String) -> XCUIElement {
        app.textFields.matching(NSPredicate(format: "placeholderValue == %@ OR label == %@", placeholder, placeholder)).firstMatch
    }

    /// Types into a field. An ordinary field is left with return (the keyboard goes, so the next field is in view); a
    /// hidden one keeps the keyboard up, and the page goes with it still up: the hardest case (#1512).
    @MainActor private func type(_ el: XCUIElement, _ text: String, in app: XCUIApplication, submit: Bool = true) {
        XCTAssertTrue(el.waitForExistence(timeout: 5), "no field:\n\(app.debugDescription)")
        // A short drag in the page, not a swipe: a swipe with the keyboard up can take a sheet down.
        let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.45)), to = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.25))
        for _ in 0..<4 where !el.isHittable { from.press(forDuration: 0.05, thenDragTo: to) }
        el.tap()
        el.typeText(submit ? text + "\n" : text)
    }

    private func get(_ path: String) -> Any? {
        guard let data = try? Data(contentsOf: URL(string: hub + path)!) else { return nil }
        return try? JSONSerialization.jsonObject(with: data)
    }

    /// Settings › Model › Add Provider, filled in by hand: name, address, key, model; saved, the sheet closes.
    @MainActor func testALauncherKey() throws {
        let app = try launch(["-tab", "settings", "-openModelLaunchers", "YES", "-openLauncherEditor", "YES", "-dev.openChat", ""])
        let name = "jv_key\(Int.random(in: 1000...9999))"
        XCTAssertTrue(app.navigationBars["新增供应商"].waitForExistence(timeout: 15), "no 新增供应商:\n\(app.debugDescription)")
        type(field(app, "名字，比如 jv_strong"), name, in: app)
        type(field(app, "https://…"), "https://gateway.example.com", in: app)
        type(secret(app, "launcher.editor.key", "API Key"), "sk-example-\(Int.random(in: 1000...9999))", in: app, submit: false)
        type(field(app, "默认模型"), "claude-opus-5-5", in: app)
        shot(app, "launcher")
        app.navigationBars["新增供应商"].buttons["launcher-save"].tap()
        XCTAssertTrue(app.navigationBars["新增供应商"].waitForNonExistence(timeout: 10), "the sheet stays:\n\(app.debugDescription)")
        PasswordPrompt.assertNone(app, "a launcher's API key")
        let launchers = (get("/api/v1/launchers") as? [String: Any])?["launchers"] as? [[String: Any]] ?? []
        XCTAssertEqual(launchers.first { $0["name"] as? String == name }?["has_key"] as? Bool, true, "the key did not reach the hub")
    }

    /// Add Provider › From Extension: a plugin's provider, a name and its key; saved, the sheet closes.
    @MainActor func testAPresetKey() throws {
        let app = try launch(["-tab", "settings", "-openModelLaunchers", "YES", "-openLauncherEditor", "preset", "-dev.openChat", ""])
        let preset = app.buttons["launcher.preset.example-gateway/team"]
        XCTAssertTrue(preset.waitForExistence(timeout: 15), "no preset:\n\(app.debugDescription)")
        preset.tap()
        let name = "jv_team\(Int.random(in: 1000...9999))"
        let nameField = app.textFields["launcher.preset.name"]
        type(nameField, name, in: app)
        type(secret(app, "launcher.preset.key", "API Key"), "sk-example-\(Int.random(in: 1000...9999))", in: app, submit: false)
        shot(app, "preset")
        app.navigationBars["新增供应商"].buttons["launcher-save"].tap()
        XCTAssertTrue(app.navigationBars["新增供应商"].waitForNonExistence(timeout: 10), "the sheet stays:\n\(app.debugDescription)")
        PasswordPrompt.assertNone(app, "a plugin provider's API key")
        let launchers = (get("/api/v1/launchers") as? [String: Any])?["launchers"] as? [[String: Any]] ?? []
        XCTAssertEqual(launchers.first { $0["name"] as? String == name }?["has_key"] as? Bool, true, "the key did not reach the hub")
    }

    /// Settings › Lark Bot on a fresh hub: App ID and App Secret, Verify and Save, then back.
    @MainActor func testTheFeishuBotSecret() throws {
        let app = try launch(["-tab", "settings", "-openConnectorBot", "YES", "-dev.openChat", ""])
        let appID = field(app, "App ID")
        XCTAssertTrue(appID.waitForExistence(timeout: 15), "no App ID:\n\(app.debugDescription)")
        type(appID, "cli_example\(Int.random(in: 1000...9999))", in: app)
        type(secret(app, "bot.appSecret", "App Secret"), "example-secret-\(Int.random(in: 1000...9999))", in: app, submit: false)
        shot(app, "bot")
        app.buttons["验证并保存"].tap()
        let deadline = Date().addingTimeInterval(10)
        var saved = false
        while Date() < deadline, !saved {
            saved = (get("/api/v1/bots/feishu") as? [String: Any])?["has_secret"] as? Bool == true
            if !saved { Thread.sleep(forTimeInterval: 0.3) }
        }
        XCTAssertTrue(saved, "the secret did not reach the hub")
        app.navigationBars.buttons.firstMatch.tap()
        PasswordPrompt.assertNone(app, "the Feishu bot's App Secret")
    }

    /// Settings › Mail › Add (Other IMAP): address and password, Connect, the sheet closes.
    @MainActor func testAMailPassword() throws {
        let app = try launch(["-tab", "settings", "-addMail", "YES", "-dev.openChat", ""])
        let imap = app.buttons["其他 IMAP"]
        XCTAssertTrue(imap.waitForExistence(timeout: 15), "no add-mail sheet:\n\(app.debugDescription)")
        imap.tap()
        let email = "me\(Int.random(in: 1000...9999))@example.com"
        type(field(app, "邮箱地址"), email, in: app)
        type(secret(app, "mail.password", "密码 / 授权码"), "example-code", in: app, submit: false)
        shot(app, "mail")
        app.buttons["连接"].tap()
        XCTAssertTrue(imap.waitForNonExistence(timeout: 10), "the sheet stays:\n\(app.debugDescription)")
        PasswordPrompt.assertNone(app, "a mail account's password")
        let accounts = (get("/mock/mail") as? [String: Any])?["accounts_added"] as? [[String: Any]] ?? []
        XCTAssertTrue(accounts.contains { $0["email"] as? String == email }, "the account was not added: \(accounts)")
    }

    /// A terminal asking for a password (sudo): typed on the card, sent, then back to the list.
    @MainActor func testATerminalPassword() throws {
        let app = try launch(["-tab", "dev", "-app.tab", "chats",                               "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000006"])
        let field = secret(app, "terminal.secret", "输入密码（不会保存）")
        XCTAssertTrue(field.waitForExistence(timeout: 20), "no password field:\n\(app.debugDescription)")
        let password = "example-\(Int.random(in: 1000...9999))"
        type(field, password, in: app, submit: false)
        shot(app, "terminal")
        app.buttons["terminal.send"].firstMatch.tap()
        let deadline = Date().addingTimeInterval(10)
        var sent = false
        while Date() < deadline, !sent {
            let rows = (get("/mock/resolved") as? [String: Any])?["resolved"] as? [[String: Any]] ?? []
            sent = rows.contains { $0["id"] as? String == "scr_macbook_review" && ($0["resolution"] as? [String: Any])?["text"] as? String == password }
            if !sent { Thread.sleep(forTimeInterval: 0.3) }
        }
        XCTAssertTrue(sent, "the password did not reach the hub")
        // Answered, the card and its field go.
        XCTAssertTrue(field.waitForNonExistence(timeout: 10), "the card stays:\n\(app.debugDescription)")
        PasswordPrompt.assertNone(app, "a terminal's password")
    }
}

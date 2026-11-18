import XCTest

/// Control Computer on a Mac's page (docs/extensions/computer-use.md), against scripts/mock/mock_hub.py with `MOCK_COMPUTER=<state>`
/// (TEST_RUNNER_PERSONA_COMPUTER_HUB=http://127.0.0.1:<port>, TEST_RUNNER_PERSONA_COMPUTER_STATE=<state>): the section
/// says the state, and its switch turns it off and on; not installed, it installs and asks for the permissions. PERSONA_SHOTS
/// saves the screenshots there too.
final class ComputerSectionTests: XCTestCase {
    private var state: String { ProcessInfo.processInfo.environment["PERSONA_COMPUTER_STATE"] ?? "ready" }

    /// The page, scrolled to the section: its switch and its state line.
    @MainActor private func open() throws -> (XCUIApplication, toggle: XCUIElement, status: XCUIElement) {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_COMPUTER_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_COMPUTER (PERSONA_COMPUTER_HUB)")
        }
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=" + UITestToken.device
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-openMachineDetail", "macbook", "-openScreenFresh", "YES"]
        // TEST_RUNNER_PERSONA_SCHEME=light|dark draws in it, for the shots.
        if let scheme = ProcessInfo.processInfo.environment["PERSONA_SCHEME"] { app.launchArguments += ["-ui.scheme", scheme] }
        // TEST_RUNNER_PERSONA_TEXT_SIZE=XXXL (a UIContentSizeCategory suffix) draws at that text size.
        if let size = ProcessInfo.processInfo.environment["PERSONA_TEXT_SIZE"] {
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategory\(size)"]
        }
        app.launchPinned()
        let toggle = app.switches["computer-switch"].firstMatch
        XCTAssertTrue(toggle.waitForExistence(timeout: 30), "no 操控电脑 switch:\n\(app.debugDescription)")
        for _ in 0..<6 where !toggle.isHittable || toggle.frame.maxY > app.frame.height * 0.5 { app.swipeUp() }
        usleep(800_000) // the page coasts on after a swipe, and a tap then only stops it
        return (app, toggle, app.descendants(matching: .any)["computer-status"].firstMatch)
    }

    /// Not installed (#1183): Install Cua Driver shows the node's steps, then the grant waits on the Mac's dialogs, then it
    /// can be operated. MOCK_COMPUTER=missing.
    @MainActor func testInstallThenGrant() throws {
        guard state == "missing" else { throw XCTSkip("MOCK_COMPUTER=missing") }
        let (app, _, status) = try open()
        let install = app.buttons["computer-install"].firstMatch
        XCTAssertTrue(install.waitForExistence(timeout: 10) && status.label.contains("还没有装"), "\(status.label)")
        shot(app, "computer-missing")
        install.tap()
        XCTAssertTrue(waitFor(status, label: "下载"), "the node's steps: \(status.label)")
        XCTAssertTrue(app.buttons["computer-cancel"].exists, "an install can be stopped")
        shot(app, "computer-installing")
        XCTAssertTrue(waitFor(status, label: "弹出了授权窗口", timeout: 30), "then the grant: \(status.label)")
        shot(app, "computer-granting")
        XCTAssertTrue(waitFor(status, label: "现在可以操控", timeout: 30), "allowed on the Mac: \(status.label)")
        shot(app, "computer-granted")
    }

    /// VNC (#1184): the hub's live screen page in a web view, which reaches the Mac's Screen Sharing through the port
    /// relay and asks for its sign-in. Needs the mock to serve the built page (MOCK_WEB=web/dist) and to relay to a real
    /// Screen Sharing on this Mac's 5900 (TEST_RUNNER_PERSONA_COMPUTER_SCREEN=1).
    @MainActor func testTheLiveScreenAsksForTheMacsSignIn() throws {
        guard state == "ready", ProcessInfo.processInfo.environment["PERSONA_COMPUTER_SCREEN"] == "1" else {
            throw XCTSkip("MOCK_COMPUTER=ready with MOCK_WEB and a Screen Sharing on 5900 (PERSONA_COMPUTER_SCREEN=1)")
        }
        let (app, _, _) = try open()
        let look = app.buttons["computer-screen"].firstMatch
        XCTAssertTrue(look.waitForExistence(timeout: 10), "no VNC:\n\(app.debugDescription)")
        look.tap()
        let signIn = app.staticTexts["登录这台 Mac 的屏幕共享"].firstMatch
        XCTAssertTrue(signIn.waitForExistence(timeout: 30), "the Mac's Screen Sharing did not ask to sign in:\n\(app.debugDescription)")
        XCTAssertTrue(app.textFields["vnc-account"].exists && app.secureTextFields["vnc-password"].exists)
        shot(app, "computer-screen-signin")
        app.descendants(matching: .any)["screen-close"].firstMatch.tap() // the drawer's handle (#1343)
        XCTAssertTrue(look.waitForExistence(timeout: 10), "back on the machine page")
    }

    /// In the chat, under the assistant's View Computer / Control Computer: VNC for the Mac it operated (MOCK_COMPUTER=ready has a
    /// conversation where it did 6 × 7 on MacBook).
    @MainActor func testTheChatOffersTheLiveScreen() throws {
        guard state == "ready", let hub = ProcessInfo.processInfo.environment["PERSONA_COMPUTER_HUB"] else {
            throw XCTSkip("MOCK_COMPUTER=ready (PERSONA_COMPUTER_HUB)")
        }
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=" + UITestToken.device
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant",
                               "-openAssistant", "server/b2e4d6f8-0000-4000-8000-0000000000c1", "-openAllWork", "YES",
                               "-showToolDetails", "YES", "-showRunsOpen", "YES"] // BeautifulUI folds the two into a run of chips
        app.launchPinned()
        let look = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "看电脑")).firstMatch
        XCTAssertTrue(look.waitForExistence(timeout: 30), "no 看电脑 row:\n\(app.debugDescription)")
        XCTAssertTrue(look.label.contains("MacBook · Calculator"), "the Mac's name, not its id: \(look.label)")
        // The screenshot under it decoded (a placeholder has no such label).
        let picture = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "图片，点按查看大图")).firstMatch
        XCTAssertTrue(picture.waitForExistence(timeout: 10), "no screenshot under 看电脑:\n\(app.debugDescription)")
        let screen = app.buttons["tool-screen"].firstMatch
        XCTAssertTrue(screen.waitForExistence(timeout: 10), "no VNC under the tool:\n\(app.debugDescription)")
        for _ in 0..<4 where !screen.isHittable { screen.frame.minY < app.frame.midY ? app.swipeDown() : app.swipeUp() }
        shot(app, "computer-chat-tool")
    }

    /// The apps always allowed for the assistant on this Mac (#1507; MOCK_COMPUTER_APPS=1, PERSONA_COMPUTER_APPS=1):
    /// listed under VNC, each taken back with Remove (asked about again next time).
    @MainActor func testAllowedAppsCanBeTakenBack() throws {
        guard state == "ready", ProcessInfo.processInfo.environment["PERSONA_COMPUTER_APPS"] == "1" else {
            throw XCTSkip("MOCK_COMPUTER=ready MOCK_COMPUTER_APPS=1 (PERSONA_COMPUTER_APPS=1)")
        }
        let (app, _, status) = try open()
        XCTAssertTrue(status.waitForExistence(timeout: 10))
        let calc = app.descendants(matching: .any)["computer-app-com.apple.calculator"].firstMatch
        XCTAssertTrue(calc.waitForExistence(timeout: 10), "the allowed apps:\n\(app.debugDescription)")
        XCTAssertTrue(calc.label.contains("计算器") || calc.staticTexts["计算器"].exists, calc.debugDescription)
        let screen = app.descendants(matching: .any)["computer-app-screen"].firstMatch
        XCTAssertTrue(screen.exists, "整个屏幕 too")
        for _ in 0..<4 where !calc.isHittable { app.swipeUp() }
        shot(app, "computer-apps")
        calc.buttons["computer-app-remove"].firstMatch.tap()
        XCTAssertTrue(waitGone(calc), "taken back")
        XCTAssertTrue(screen.exists, "the other stays")
    }

    /// Don't Auto-Lock While On (#1507 b2): off by default; turning it on asks first (Cancel leaves it off, Open turns it on),
    /// turning it off does not ask. MOCK_COMPUTER=ready.
    @MainActor func testNoAutoLockAsksFirst() throws {
        guard state == "ready" else { throw XCTSkip("MOCK_COMPUTER=ready") }
        let (app, _, status) = try open()
        XCTAssertTrue(status.waitForExistence(timeout: 10))
        let noLock = app.switches["computer-no-lock"].firstMatch
        XCTAssertTrue(noLock.waitForExistence(timeout: 10), "the switch:\n\(app.debugDescription)")
        for _ in 0..<4 where !noLock.isHittable { app.swipeUp() }
        usleep(800_000)
        XCTAssertEqual(noLock.value as? String, "0", "off by default")
        let flip = { noLock.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap() }
        let turnOn = app.buttons["打开"].firstMatch
        flip()
        XCTAssertTrue(turnOn.waitForExistence(timeout: 5), "asked first:\n\(app.debugDescription)")
        let warning = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "坐在它前面的人能直接用它")).firstMatch
        XCTAssertTrue(warning.exists, "what it means:\n\(app.debugDescription)")
        shot(app, "computer-no-lock-ask")
        cancelDialog(app)
        XCTAssertTrue(waitGone(turnOn))
        XCTAssertEqual(noLock.value as? String, "0", "取消: still off")
        flip()
        XCTAssertTrue(turnOn.waitForExistence(timeout: 5))
        turnOn.tap()
        XCTAssertTrue(waitValue(noLock, "1"), "on")
        shot(app, "computer-no-lock-on")
        flip()
        XCTAssertTrue(waitValue(noLock, "0"), "off")
        XCTAssertFalse(turnOn.exists, "turning it off does not ask")
    }

    /// x264 for the live screen (#8859): shown only on a Mac whose node found ffmpeg with libx264 (MOCK_SCREEN_X264=1,
    /// TEST_RUNNER_PERSONA_SCREEN_X264=1), off by default; turning it on asks first and says it is GPL software Persona
    /// doesn't include, turning it off does not ask. Light and dark shots with TEST_RUNNER_PERSONA_SCHEME. MOCK_COMPUTER=ready.
    @MainActor func testX264AsksFirst() throws {
        guard state == "ready" else { throw XCTSkip("MOCK_COMPUTER=ready") }
        guard ProcessInfo.processInfo.environment["PERSONA_SCREEN_X264"] == "1" else {
            throw XCTSkip("needs MOCK_SCREEN_X264=1 and TEST_RUNNER_PERSONA_SCREEN_X264=1")
        }
        let (app, _, status) = try open()
        for source in [app, XCUIApplication(bundleIdentifier: "com.apple.springboard")] { // the icon follows the look
            let alert = source.alerts.containing(NSPredicate(format: "label CONTAINS[c] 'icon' OR label CONTAINS '图标'")).firstMatch
            if alert.exists, alert.buttons.firstMatch.exists { alert.buttons.firstMatch.tap() }
        }
        XCTAssertTrue(status.waitForExistence(timeout: 10))
        let x264 = app.switches["computer-x264"].firstMatch
        XCTAssertTrue(x264.waitForExistence(timeout: 10), "the switch:\n\(app.debugDescription)")
        // Short drags until the row sits in the middle of the screen: a whole swipe carries the page past it.
        let height = app.frame.height
        for _ in 0..<12 where x264.frame.minY < height * 0.25 || x264.frame.maxY > height * 0.7 {
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.6))
            let to = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: x264.frame.maxY > height * 0.7 ? 0.4 : 0.8))
            from.press(forDuration: 0.05, thenDragTo: to)
            usleep(500_000)
        }
        usleep(800_000)
        XCTAssertEqual(x264.value as? String, "0", "off by default")
        let flip = { x264.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap() }
        let turnOn = app.buttons["打开"].firstMatch
        let env = ProcessInfo.processInfo.environment
        let scheme = (env["PERSONA_SCHEME"].map { "-" + $0 } ?? "") + (env["PERSONA_TEXT_SIZE"].map { "-" + $0.lowercased() } ?? "")
        shot(app, "computer-x264-off\(scheme)")
        flip()
        XCTAssertTrue(turnOn.waitForExistence(timeout: 5), "asked first:\n\(app.debugDescription)")
        let warning = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Persona 不带它")).firstMatch
        XCTAssertTrue(warning.exists, "what it means:\n\(app.debugDescription)")
        shot(app, "computer-x264-ask\(scheme)")
        cancelDialog(app)
        XCTAssertTrue(waitGone(turnOn))
        XCTAssertEqual(x264.value as? String, "0", "取消: still off")
        flip()
        XCTAssertTrue(turnOn.waitForExistence(timeout: 5))
        turnOn.tap()
        XCTAssertTrue(waitValue(x264, "1"), "on")
        shot(app, "computer-x264-on\(scheme)")
        flip()
        XCTAssertTrue(waitValue(x264, "0"), "off")
        XCTAssertFalse(turnOn.exists, "turning it off does not ask")
    }

    /// Cancel the dialog: its button where it has one, else a tap outside it (iOS 26 shows it as a popover by the switch,
    /// with no Cancel).
    @MainActor private func cancelDialog(_ app: XCUIApplication) {
        let cancel = app.buttons["取消"].firstMatch
        let outside = app.otherElements["PopoverDismissRegion"].firstMatch
        if cancel.exists { cancel.tap() } else if outside.exists { outside.tap() } else {
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.04, dy: 0.5)).tap()
        }
    }

    private func waitValue(_ e: XCUIElement, _ value: String) -> Bool {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline { if e.value as? String == value { return true }; usleep(250_000) }
        return false
    }

    @MainActor func testAnInstallCanBeCancelled() throws {
        guard state == "missing" else { throw XCTSkip("MOCK_COMPUTER=missing") }
        let (app, _, status) = try open()
        let install = app.buttons["computer-install"].firstMatch
        XCTAssertTrue(install.waitForExistence(timeout: 10))
        install.tap()
        let cancel = app.buttons["computer-cancel"].firstMatch
        XCTAssertTrue(cancel.waitForExistence(timeout: 10))
        cancel.tap()
        XCTAssertTrue(install.waitForExistence(timeout: 10) && waitFor(status, label: "还没有装"), "back to 安装: \(status.label)")
    }

    @MainActor func testTheSwitchAndTheState() throws {
        guard state != "missing" else { throw XCTSkip("MOCK_COMPUTER=missing is the install's") }
        let (app, toggle, status) = try open()
        if state == "off" {
            XCTAssertEqual(toggle.value as? String, "0")
            XCTAssertFalse(status.exists, "nothing to say until it is on")
            shot(app, "computer-off")
            return
        }
        XCTAssertEqual(toggle.value as? String, "1")
        let says = ["ready": "现在可以操控 · Cua Driver 0.38.0", "locked": "锁屏", "permissions": "还需要一项权限", "both": "还需要两项权限"][state] ?? ""
        XCTAssertTrue(status.waitForExistence(timeout: 10) && status.label.contains(says), "\(status.label)")
        if state == "permissions" || state == "both" { // #1280: steps for what is missing, what is allowed checked
            XCTAssertTrue(app.buttons["computer-grant"].exists && app.buttons["computer-recheck"].exists, "去授权 and 再检查一次")
            let accessibility = app.descendants(matching: .any)["computer-step-accessibility"].firstMatch
            let screen = app.descendants(matching: .any)["computer-step-screen"].firstMatch
            XCTAssertEqual(accessibility.value as? String, state == "both" ? "待做" : "已完成")
            XCTAssertEqual(screen.value as? String, "待做")
            XCTAssertTrue(screen.label.contains("录屏与系统录音") && screen.label.contains("Cua Driver"), screen.label)
            if state == "both" { XCTAssertTrue(accessibility.label.contains("系统设置 › 隐私与安全性 › 辅助功能"), accessibility.label) }
        }
        if state == "locked" { // Unlock opens its live screen, where the lock screen takes the password
            XCTAssertTrue(app.buttons["computer-unlock"].exists, "解锁:\n\(app.debugDescription)")
        }
        shot(app, "computer-\(state)")

        toggle.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(waitFor(toggle, value: "0"), "turned off")
        XCTAssertTrue(waitGone(status), "its state goes with it")
        toggle.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(waitFor(toggle, value: "1") && status.waitForExistence(timeout: 10), "on again")
    }

    private func waitFor(_ e: XCUIElement, value: String) -> Bool {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline { if e.value as? String == value { return true }; usleep(200_000) }
        return false
    }

    private func waitFor(_ e: XCUIElement, label: String, timeout: TimeInterval = 10) -> Bool {
        let deadline = Date.now.addingTimeInterval(timeout)
        while Date.now < deadline { if e.exists, e.label.contains(label) { return true }; usleep(200_000) }
        return false
    }

    private func waitGone(_ e: XCUIElement) -> Bool {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline { if !e.exists { return true }; usleep(200_000) }
        return false
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let look = ProcessInfo.processInfo.environment["PERSONA_LOOK"].map { "-" + $0 } ?? ""
        let full = "\(name)-\(UITestStyle.name)\(look)"
        let png = app.screenshot()
        let attachment = XCTAttachment(screenshot: png)
        attachment.name = full
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(full + ".png"))
        }
    }
}

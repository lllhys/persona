import XCTest

/// The to-do's page at the text sizes up to xxxLarge (#6151), against the mock hub: its two bottom buttons
/// (`StackingButtonRow`) sit side by side while both labels fit whole and stack, Hand to Agent first, each across the
/// page, when one doesn't: on the iPhone 17 Pro from xxLarge in English and Japanese; Chinese stays side by side.
/// (At the accessibility sizes the interface stops growing at xxxLarge, #6448.) PERSONA_SHOTS=<dir> saves the page as
/// it opens, `page-<language>-<size>.png`, for the before / after pixel comparison. PERSONA_SHOOT says which:
/// `zh:L,XL,XXL,XXXL;en:L,XXXL` (default `zh:L,XXXL`). PERSONA_BEFORE (the app built from main) shoots the same and
/// checks nothing.
final class TodoPageTextSizeTests: XCTestCase {
    private var app: XCUIApplication!

    private static let sizes = ["L": "UICTContentSizeCategoryL", "XL": "UICTContentSizeCategoryXL",
                                "XXL": "UICTContentSizeCategoryXXL", "XXXL": "UICTContentSizeCategoryXXXL"]
    private static let languages = ["zh": ("(zh-Hans)", "zh_CN"), "en": ("(en)", "en_US"), "ja": ("(ja)", "ja_JP")]

    @MainActor func testTodoPageAtTheTextSizes() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        continueAfterFailure = true
        let env = ProcessInfo.processInfo.environment
        let before = env["PERSONA_BEFORE"] != nil
        for part in (env["PERSONA_SHOOT"] ?? "zh:L,XXXL").split(separator: ";") {
            let pair = part.split(separator: ":").map(String.init)
            let language = pair[0]
            for size in pair[1].split(separator: ",").map(String.init) {
                page(hub, language, size, check: !before)
            }
        }
    }

    /// The bottom bar in the page's three states (#9008): with an Agent at work and a stage, with nothing yet, and done
    /// (Reopen alone). Its buttons are the Sidebar layout's foot's pills (`floatingPill`), as tall as its New Chat (46 pt,
    /// not the large control size's 61), and on a wide page (an iPad) the row is no wider than `Theme.Size.readable`,
    /// centered. `bar-<state>.png` in PERSONA_SHOTS;
    /// PERSONA_LANDSCAPE=1 turns the device first; PERSONA_BEFORE shoots and checks nothing.
    @MainActor func testBottomBarInEachState() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        continueAfterFailure = true
        let env = ProcessInfo.processInfo.environment
        XCUIDevice.shared.orientation = env["PERSONA_LANDSCAPE"] == "1" ? .landscapeLeft : .portrait
        for (state, name) in [("working", "天气 App 发 1.3 版"), ("open", "给 NAS 配自动备份"), ("done", "修复登录页在 Safari 上的样式问题")] {
            launch(hub, "zh", "L", ["-openTodoTitled", name])
            XCTAssertTrue(app.navigationBars.staticTexts[name].firstMatch.waitForExistence(timeout: 60), "\(state): the to-do's page")
            let done = app.buttons["todo.toggleDone"].firstMatch
            XCTAssertTrue(done.waitForExistence(timeout: 5), "\(state): the bottom bar")
            shot("bar-\(state)")
            guard env["PERSONA_BEFORE"] == nil else { continue }
            let buttons = [done] + (state == "done" ? [] : [app.buttons["todo.handToAgent"].firstMatch])
            for b in buttons {
                XCTAssertEqual(b.frame.height, 46, accuracy: 2, "\(state): \(b.identifier) 46 pt tall \(b.frame)")
            }
            let row = buttons.map(\.frame).reduce(buttons[0].frame) { $0.union($1) }
            XCTAssertLessThanOrEqual(row.width, 561, "\(state): the row is no wider than a readable page \(row)")
        }
    }

    /// The mock's 「给 NAS 配自动备份」, known by its title in the bar.
    @MainActor private func page(_ hub: String, _ language: String, _ size: String, check: Bool) {
        let name = "给 NAS 配自动备份"
        launch(hub, language, size, ["-openTodoTitled", name])
        let title = app.navigationBars.staticTexts[name].firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 60), "\(language) \(size): the to-do's page")
        shot("page-\(language)-\(size)")
        guard check else { return }
        let screen = app.windows.firstMatch.frame
        let hand = app.buttons["todo.handToAgent"].firstMatch, done = app.buttons["todo.toggleDone"].firstMatch
        XCTAssertTrue(hand.waitForExistence(timeout: 5) && done.exists, "\(language) \(size): the bottom buttons")
        // Measured on the iPhone 17 Pro: "Hand to Agent" and 「エージェントへ」 stop fitting half the bar at xxLarge.
        let stacked = language != "zh" && ["XXL", "XXXL"].contains(size)
        if stacked {
            for (button, what) in [(hand, "Hand to Agent"), (done, "Done")] {
                XCTAssertGreaterThan(button.frame.width, screen.width * 0.8, "\(language) \(size): \(what) across the page \(button.frame)")
            }
            XCTAssertLessThanOrEqual(hand.frame.maxY, done.frame.minY + 1, "\(language) \(size): Hand to Agent above Done")
        } else {
            XCTAssertLessThan(hand.frame.width, screen.width * 0.55, "\(language) \(size): Hand to Agent beside Done \(hand.frame)")
            XCTAssertEqual(hand.frame.midY, done.frame.midY, accuracy: 1, "\(language) \(size): one row")
            XCTAssertLessThan(hand.frame.maxX, done.frame.minX, "\(language) \(size): Hand to Agent first")
        }
    }

    @MainActor private func launch(_ hub: String, _ language: String, _ size: String, _ extra: [String]) {
        app?.terminate()
        let dark = ProcessInfo.processInfo.environment["PERSONA_APPEARANCE"] == "dark"
        XCUIDevice.shared.appearance = dark ? .dark : .light
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let (languages, locale) = Self.languages[language]!
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-openAssistantPage", "todos",
                               "-AppleLanguages", languages, "-AppleLocale", locale,
                               "-UIPreferredContentSizeCategoryName", Self.sizes[size]!,
                               // The app's own appearance setting wins over the device's: name the one asked for.
                               "-ui.scheme", dark ? "dark" : "light"] + extra
        app.launchPinned()
        dismissSystemAlerts()
    }

    /// SpringBoard's notification question, which a fresh simulator asks on some launches over the page.
    @MainActor private func dismissSystemAlerts() {
        let alert = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.firstMatch
        if alert.waitForExistence(timeout: 2) {
            for label in ["允许", "Allow", "好", "OK"] where alert.buttons[label].exists { alert.buttons[label].tap(); break }
        }
    }

    /// The screen once two shots in a row are the same, so a view still settling is not counted as a difference.
    @MainActor private func shot(_ name: String) {
        var last = XCUIScreen.main.screenshot()
        for _ in 0..<8 {
            usleep(300_000)
            let next = XCUIScreen.main.screenshot()
            if next.pngRepresentation == last.pngRepresentation { break }
            last = next
        }
        let a = XCTAttachment(screenshot: last)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
            try? last.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }
}

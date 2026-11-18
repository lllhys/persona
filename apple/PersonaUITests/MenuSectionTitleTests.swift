import XCTest

/// The section titles of the composer's model menus (#4322): over a running agent's model and effort
/// (`SessionModesMenu`) and over a new conversation's (`StarterModesMenu`). An iOS menu draws no section header over an
/// inline picker (docs/lessons/ios.md, "A `Menu` draws no section header over an inline picker"): a section with other
/// items keeps its title over those, below the picker's options, and a section of only a picker loses it. So each title
/// must stand above its own options, not only be on screen. The menus open upward and scroll; the test scrolls each one
/// until both titles have shown, and shoots it (light and dark). Runs on a mock hub:
///   PORT=18931 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18931 TEST_RUNNER_PERSONA_SHOTS=<dir> xcodebuild test …
/// The test runs in zh-Hans: 「模型」 is "Model", 「思考强度」 "Thinking Effort".
final class MenuSectionTitleTests: XCTestCase {
    private var hub = ""
    private var app: XCUIApplication!
    /// An idle Claude agent in herdr: its menu has the model list and the effort levels.
    private static let idle = "macbook/a1f3c2d4-0000-4000-8000-000000000006"
    /// The rows of each section, by their labels: the mock's models for Claude Code, its launchers, the effort levels.
    private static var models: NSPredicate {
        NSPredicate(format: "label IN %@", ["Default (recommended)", "Opus 5.5", "Sonnet 5.5", "Haiku 5.5", "Fable 5.1", "glm-5.3",
                                            "deepseek-v4", "qwen-4-coder"])
    }
    private static var launchers: NSPredicate { NSPredicate(format: "label BEGINSWITH 'cc_' OR label BEGINSWITH 'claude' OR label BEGINSWITH 'jv_'") }
    private static var efforts: NSPredicate { // i18n-ignore: zh-Hans labels
        NSPredicate(format: "label MATCHES %@", "^(自动|低|中|高|很高|最高|跟随启动器)([,，\\n][\\s\\S]*)?$") // i18n-ignore: zh-Hans
    }

    override func setUpWithError() throws {
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        hub = h
        continueAfterFailure = true // every menu is shot even when an earlier one misses a title
    }

    @MainActor func testARunningAgentsMenuTitlesItsSections() {
        for scheme in ["light", "dark"] {
            open(["-openSession", Self.idle], scheme: scheme)
            openMenu()
            checkTitles([("模型", Self.models), ("思考强度", Self.efforts)], shot: "agent-\(scheme)") // i18n-ignore: zh-Hans
        }
    }

    @MainActor func testANewConversationsMenuTitlesItsSections() {
        for scheme in ["light", "dark"] {
            open(["-tab", "assistant", "-app.tab", "assistant", "-newAssistant", "YES"], scheme: scheme)
            openMenu()
            checkTitles([("模型", Self.launchers), ("思考强度", Self.efforts)], shot: "new-\(scheme)") // i18n-ignore: zh-Hans
        }
    }

    /// Opens the page and puts the keyboard in its field: the shell composer shows its model menu only while open.
    @MainActor private func open(_ extra: [String], scheme: String) {
        app?.terminate()
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-ui.scheme", scheme] + extra
        app.launchPinned()
        LiveActivityShots.allowNotifications()
        HomeScreen.dismissIconNote()
        let field = app.descendants(matching: .any)["shell-composer-field"].firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 20), "no composer:\n\(app.debugDescription)")
        field.tapCenter()
    }

    @MainActor private func openMenu() {
        let menu = app.descendants(matching: .any)["shell-composer-modes"].firstMatch
        XCTAssertTrue(menu.waitForExistence(timeout: 20), "no composer model menu:\n\(app.debugDescription)")
        menu.tap()
        sleep(1) // the menu has finished opening
    }

    /// Each title on screen above one of its own rows, somewhere while the menu scrolls from its bottom (where it opens,
    /// next to the button) to its top. Shots: where it opens, and where the last title came into view.
    @MainActor private func checkTitles(_ sections: [(title: String, rows: NSPredicate)], shot name: String) {
        let menu = app.collectionViews.containing(sections[0].rows).firstMatch
        guard menu.waitForExistence(timeout: 5) else {
            XCTFail("\(name): no menu with its rows:\n\(app.debugDescription)")
            return
        }
        shot(name)
        var placed = Set<String>()
        let top = menu.frame.minY // where the menu's visible part starts; its frame runs as long as its rows
        let from = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: menu.frame.midX, dy: top + 30))
        let to = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: menu.frame.midX, dy: top + 230))
        for step in 0..<7 {
            for s in sections where above(s.title, s.rows, in: menu, from: top) { placed.insert(s.title) }
            print("\(name) step \(step): placed \(placed.sorted())")
            if placed.count == sections.count || step == 6 { break }
            from.press(forDuration: 0.05, thenDragTo: to) // the rows above come down into view
            sleep(1)
        }
        shot(name + "-top")
        for s in sections where !placed.contains(s.title) {
            XCTFail("\(name): the menu's \(s.title) title, over its rows; the menu's elements:\n\(menu.debugDescription)")
        }
    }

    /// The title is on screen as a line of the menu, with at least one of its rows below it. A line scrolled out of the
    /// menu's top can still be in the tree with its frame (a dark-mode iPhone run passed with the title out of view): it
    /// counts only from `top` down, so the shot after it shows the title.
    @MainActor private func above(_ title: String, _ rows: NSPredicate, in menu: XCUIElement, from top: CGFloat) -> Bool {
        let line = menu.staticTexts.matching(NSPredicate(format: "label == %@", title)).firstMatch
        guard line.exists else { return false }
        print("\(title): line at \(line.frame.minY), the menu's top at \(top)")
        guard line.frame.minY >= top - 1 else { return false }
        let notText = NSPredicate(format: "elementType != %d", XCUIElement.ElementType.staticText.rawValue)
        let below = menu.descendants(matching: .any).matching(NSCompoundPredicate(andPredicateWithSubpredicates: [rows, notText]))
        return below.allElementsBoundByIndex.contains { $0.frame.minY >= line.frame.maxY - 1 }
    }

    @MainActor private func shot(_ name: String) {
        let device = UIDevice.current.userInterfaceIdiom == .pad ? "ipad" : "iphone"
        let screen = XCUIScreen.main.screenshot() // the whole screen: the menu over the page
        let a = XCTAttachment(screenshot: screen)
        a.name = "\(device)-\(name)"
        a.lifetime = .keepAlways
        add(a)
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        try? screen.pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/menu-titles-\(device)-\(name).png"))
    }
}

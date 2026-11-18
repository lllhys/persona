import XCTest

/// A new Persona conversation opens on the welcome (`AssistantWelcome`: a greeting, what Persona can reach, things to ask),
/// in the shell, from launch and from the sidebar's New Chat. The shell once showed nothing but the
/// composer there (2026-11-02, the user: 「之前设计的挺好的助手空状态，现在咋全空白了？」). Runs in the BeautifulUI
/// style too (`TEST_RUNNER_PERSONA_UI_STYLE=beautiful`).
final class AssistantWelcomeTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = false
    }

    /// `textSize`: a content size category to launch in (`UICTContentSizeCategory…`), else the simulator's.
    @MainActor private func launch(newConversation: Bool, needsYou: Bool = false, textSize: String? = nil) {
        app = XCUIApplication()
        // `-showHomeState empty`: nothing needs the user, the case that was blank. Without it, the demo's agents waiting
        // and inbox items to answer.
        app.launchArguments = ["-demo.on", "YES", "-app.tab", "assistant",
                               "-dev.openChat", ""] + (needsYou ? [] : ["-showHomeState", "empty"])
            // Otherwise the demo's main conversation (the last run may have left a new one open).
            + (newConversation ? ["-newAssistant", "YES"] : ["-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001"])
            + (textSize.map { ["-UIPreferredContentSizeCategoryName", $0] } ?? [])
        app.launchPinned()
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "\(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
    }

    /// The welcome's parts are there and on screen, below the top bar and above the composer.
    @MainActor private func assertWelcome(_ layout: String) {
        let welcome = app.descendants(matching: .any)["assistant-welcome"].firstMatch
        XCTAssertTrue(welcome.waitForExistence(timeout: 15), "\(layout): no welcome on the new conversation")
        shot("\(layout) welcome")
        let window = app.windows.firstMatch.frame
        let greeting = app.staticTexts.matching(NSPredicate(format: "label IN %@", ["早上好", "中午好", "下午好", "晚上好"])).firstMatch
        XCTAssertTrue(greeting.exists, "\(layout): no greeting")
        let reach = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "我能看你的电脑")).firstMatch
        XCTAssertTrue(reach.exists, "\(layout): no line on what Persona can reach")
        let ask = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "今天的安排")).firstMatch
        XCTAssertTrue(ask.exists, "\(layout): no things to ask")
        let field = app.textViews.firstMatch.exists ? app.textViews.firstMatch : app.textFields.firstMatch
        for (what, e) in [("greeting", greeting), ("reach", reach), ("first suggestion", ask)] {
            XCTAssertTrue(e.isHittable, "\(layout): the \(what) is not visible")
            let f = e.frame
            XCTAssertTrue(f.width > 20 && f.height > 10, "\(layout): the \(what) is \(f.size)")
            XCTAssertTrue(window.contains(f), "\(layout): the \(what) is off screen at \(f)")
            if field.exists { XCTAssertLessThan(f.maxY, field.frame.minY, "\(layout): the \(what) is under the composer") }
        }
    }

    @MainActor func testTheShellsNewConversation() {
        launch(newConversation: true)
        assertWelcome("shell")
    }

    /// What needs the user over the composer stays below the welcome's first row of things to ask: on a short screen
    /// (iPhone Duo's outer one, an SE) fewer rows, the rest in "N more" (#283: three rows and the composer took half the
    /// outer screen and cut the cards in two). Typing does not change how many.
    @MainActor func testNeedsYouLeavesTheFirstRowToAsk() {
        launch(newConversation: true, needsYou: true)
        let needs = app.descendants(matching: .any)["shell-needs-you"].firstMatch
        XCTAssertTrue(needs.waitForExistence(timeout: 15), "nothing needs the user over the composer")
        sleep(1)
        shot("needs you")
        let ask = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "今天的安排")).firstMatch
        XCTAssertTrue(ask.exists, "no things to ask")
        XCTAssertLessThanOrEqual(ask.frame.maxY, needs.frame.minY, "需要你 covers the first row of things to ask")
        let rows = { self.app.buttons.matching(NSPredicate(format: "identifier IN %@", ["shell-needs-agent", "shell-needs-inbox"])).count }
        let shown = rows()
        XCTAssertGreaterThan(shown, 0, "not one row of what needs the user")
        let more = app.buttons["shell-needs-more"].firstMatch
        let rest = more.exists ? more.label : nil
        app.descendants(matching: .any)["shell-composer-field"].firstMatch.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "no keyboard")
        sleep(1)
        shot("needs you while typing")
        XCTAssertEqual(rows(), shown, "typing changed how many rows of 需要你 show")
        XCTAssertEqual(more.exists ? more.label : nil, rest, "typing changed what 还有 N 件 says")
    }

    /// At the largest text size the greeting and what Persona can reach fill most of the page, and the welcome's first
    /// things to ask are below it: Needs You keeps one row, and the greeting stays in view above it (#4073: the cards'
    /// lazy grid never made the ones below the page, so their end was never measured, and three rows and "N more"
    /// covered the welcome).
    ///
    /// Nothing asks for the page's elements until it has settled, and the rows are counted at the first look: building the
    /// accessibility tree makes the lazy grid make every card, which measured the fold and hid the bug from a test that
    /// waited for Needs You first (it passed without the fix).
    @MainActor func testNeedsYouAtTheLargestTextSize() {
        launch(newConversation: true, needsYou: true, textSize: "UICTContentSizeCategoryAccessibilityXXXL")
        sleep(6)
        let rows = app.buttons.matching(NSPredicate(format: "identifier IN %@", ["shell-needs-agent", "shell-needs-inbox"])).count
        shot("needs you at the largest text size")
        let needs = app.descendants(matching: .any)["shell-needs-you"].firstMatch
        XCTAssertTrue(needs.waitForExistence(timeout: 15), "nothing needs the user over the composer")
        let ask = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "今天的安排")).firstMatch
        if !(ask.exists && ask.isHittable && ask.frame.maxY <= needs.frame.minY) {
            XCTAssertEqual(rows, 1, "the first thing to ask is out of view, and 需要你 shows \(rows) rows")
        }
        let greeting = app.staticTexts.matching(NSPredicate(format: "label IN %@", ["早上好", "中午好", "下午好", "晚上好"])).firstMatch
        XCTAssertTrue(greeting.exists && greeting.isHittable, "no greeting in view")
        XCTAssertLessThanOrEqual(greeting.frame.maxY, needs.frame.minY, "需要你 covers the greeting")
    }

    /// From a conversation, the sidebar's New Chat opens the same welcome.
    @MainActor func testNewConversationFromTheSidebar() {
        launch(newConversation: false)
        let menu = app.buttons["assistant-sidebar-button"].firstMatch
        XCTAssertTrue(menu.waitForExistence(timeout: 15), "no sidebar button")
        XCTAssertFalse(app.descendants(matching: .any)["assistant-welcome"].exists, "opened on the welcome, not a conversation")
        sleep(1)
        menu.tapCenter()
        // The pill's own id is under the sidebar's (`assistant-sidebar`): by its label, not the top bar's New Chat.
        let new = app.buttons.matching(NSPredicate(format: "label == %@ AND identifier != %@", "新对话", "assistant-new")).firstMatch
        XCTAssertTrue(new.waitForExistence(timeout: 5), "the sidebar did not open")
        new.tap()
        assertWelcome("sidebar")
    }
}

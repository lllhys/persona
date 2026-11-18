import XCTest

/// The home screen icon's long-press menu (#3199, `QuickAction`): each item opens its page; the chat opened last is on it
/// (#2191), nearest the icon, then Record, New Chat and Inbox, and Agents no longer is. Record is never tapped (it
/// records).
final class QuickActionsTests: XCTestCase {
    private var app: XCUIApplication!
    /// Where the icon was on the home screen when the menu last opened.
    private var iconFrame = CGRect.zero
    /// A demo Agent's chat (Persona/Resources/Demo.json), its title (the menu's) and its first message (the open chat's;
    /// its header names the folder, not the title).
    private static let review = "macbook/a1f3c2d4-0000-4000-8000-000000000006"
    private static let reviewTitle = "检查 PR #42 的改动"
    private static let reviewMessage = "看一下 PR #42"
    /// The menu's Start Meeting (#2949; Record a Meeting until #5243, Record until #8731).
    private static let recordTitle = "开始会议"
    /// Another demo Agent's chat, its title and the start of its first message.
    private static let widget = "macbook/a1f3c2d4-0000-4000-8000-000000000001"
    private static let widgetTitle = "给天气 App 加主屏幕小组件"
    private static let widgetMessage = "给天气 App 加一个主屏幕小组件"

    override func setUp() {
        continueAfterFailure = false
    }

    /// `opening`: a chat opened at launch (it goes on the menu), else Persona's.
    @MainActor private func launch(opening session: String? = nil) {
        app = XCUIApplication()
        // not with -newAssistant, which puts Persona in front after -openSession opened the chat
        app.launchArguments = ["-demo.on", "YES", "-probes", "YES"]
            + (session.map { ["-openSession", $0] } ?? ["-newAssistant", "YES", "-dev.openChat", "", "-tab", "assistant"])
        app.launchPinned()
        if session == nil {
            XCTAssertTrue(app.buttons["assistant-sidebar-button"].firstMatch.waitForExistence(timeout: 15), "no shell")
        }
    }

    /// Home, then the icon's long-press menu: SpringBoard, the menu on it. The alert of an icon that switched with the
    /// look (a test before launched with `-ui.scheme`) is tapped away first: up over the app it takes the Home press, and
    /// closing it then shows the app again, so Home is pressed once more (#9004).
    @MainActor private func openMenu() -> XCUIApplication {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        HomeScreen.dismissIconNote()
        XCUIDevice.shared.press(.home)
        if HomeScreen.dismissIconNote() { XCUIDevice.shared.press(.home) }
        let icon = springboard.icons["Persona"].firstMatch
        XCTAssertTrue(icon.waitForExistence(timeout: 10), "no Persona icon on the home screen")
        iconFrame = icon.frame
        icon.press(forDuration: 1.5)
        return springboard
    }

    /// A menu item by its title: a recent conversation's reads "<title>, <computer> · <folder>", its title starts it.
    @MainActor private func item(_ springboard: XCUIApplication, _ title: String) -> XCUIElement {
        springboard.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
    }

    /// The icon's menu: `title` tapped; `present` must be on the menu too, `absent` must not.
    @MainActor private func pick(_ title: String, present: String? = nil, absent: String? = nil) {
        let springboard = openMenu()
        let found = item(springboard, title)
        XCTAssertTrue(found.waitForExistence(timeout: 5), "no \(title) in the icon's menu")
        if let present { XCTAssertTrue(item(springboard, present).exists, "no \(present) in the icon's menu") }
        if let absent { XCTAssertFalse(springboard.buttons[absent].exists, "\(absent) is still in the icon's menu") }
        let shot = XCTAttachment(screenshot: springboard.screenshot())
        shot.name = "menu"; shot.lifetime = .keepAlways; add(shot)
        found.tap()
        XCTAssertTrue(app.wait(for: .runningForeground, timeout: 10), "the app did not come back")
    }

    @MainActor func testEachItemOpensItsPage() {
        launch(opening: Self.review)
        XCTAssertTrue(app.staticTexts[Self.reviewMessage].firstMatch.waitForExistence(timeout: 20), "the chat did not open")
        pick(Self.reviewTitle, present: Self.recordTitle, absent: "Agents")
        XCTAssertTrue(app.staticTexts[Self.reviewMessage].firstMatch.waitForExistence(timeout: 10), "the recent chat did not open")
        pick("收件箱")
        // from an Agent's chat the inbox takes longer to come than from the shell's root
        XCTAssertTrue(app.navigationBars["收件箱"].firstMatch.waitForExistence(timeout: 10), "the inbox did not open")
        pick("新对话")
        XCTAssertTrue(app.waitAgentsDrawn(false), "助手 is not in front: \(app.shellTrace)")
        XCTAssertTrue(app.navigationBars["收件箱"].firstMatch.waitForNonExistence(timeout: 5), "the inbox stayed over 助手: \(app.shellTrace)")
        XCTAssertTrue(app.descendants(matching: .any)["shell-composer-field"].firstMatch.waitForExistence(timeout: 5), "no composer")
    }

    /// Only the chat opened last, nearest the icon, then Record, New Chat and Inbox (#2191; the system shows four): two
    /// chats opened in turn, then the menu (its screenshot is attached, "menu order"). The system lists the menu outwards
    /// from the icon: below an icon high on the screen, above one lower down (the same list from the bottom), so the
    /// order is each item's distance from the icon, not its height (#9004).
    @MainActor func testMenuListsTheLastOpened() {
        launch(opening: Self.widget)
        let first = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", Self.widgetMessage)).firstMatch
        XCTAssertTrue(first.waitForExistence(timeout: 20), "the first chat did not open")
        // on the menu at once; by the time it is checked the list is saved (a second after), so it outlives the app
        var springboard = openMenu()
        XCTAssertTrue(item(springboard, Self.widgetTitle).waitForExistence(timeout: 5), "the first chat is not on the menu")
        XCUIDevice.shared.press(.home)
        app.terminate()
        launch(opening: Self.review)
        XCTAssertTrue(app.staticTexts[Self.reviewMessage].firstMatch.waitForExistence(timeout: 20), "the chat did not open")
        springboard = openMenu()
        let titles = [Self.reviewTitle, Self.recordTitle, "新对话", "收件箱"]
        XCTAssertTrue(item(springboard, titles[0]).waitForExistence(timeout: 5), "the chat opened last is not on the menu")
        XCTAssertFalse(item(springboard, Self.widgetTitle).exists, "the chat opened before it is still on the menu")
        let fromIcon = { (title: String) in abs(self.item(springboard, title).frame.midY - self.iconFrame.midY) }
        for (nearer, farther) in zip(titles, titles.dropFirst()) {
            XCTAssertTrue(item(springboard, farther).exists, "no \(farther) in the icon's menu")
            XCTAssertLessThan(fromIcon(nearer), fromIcon(farther), "\(nearer) is not nearer the icon than \(farther)")
        }
        let shot = XCTAttachment(screenshot: springboard.screenshot())
        shot.name = "menu order"; shot.lifetime = .keepAlways; add(shot)
        XCUIDevice.shared.press(.home)
    }
}

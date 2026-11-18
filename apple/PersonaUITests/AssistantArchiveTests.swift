import XCTest

/// Archiving Persona's conversations (docs/assistant/assistant.md「Managing its own conversations」): a conversation in the sidebar swipes left to
/// Archive (the open drawer leaves the drag to the row), it shows up under Archived, and a swipe there brings it back. In
/// the shell, on demo data.
final class AssistantArchiveTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor private func launch() {
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-newAssistant", "YES",
                               "-app.tab", "assistant", "-dev.openChat", ""]
        app.launchPinned()
        let menu = app.buttons["assistant-sidebar-button"].firstMatch
        XCTAssertTrue(menu.waitForExistence(timeout: 15), "no sidebar button")
        sleep(1)
        menu.tapCenter()
        XCTAssertTrue(conversation("帮我约周五").waitForExistence(timeout: 5), "the sidebar did not open")
        sleep(1)
    }

    @MainActor private func conversation(_ title: String) -> XCUIElement {
        app.buttons.matching(identifier: "sidebar-conversation").matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor private func archiveAndBringBack(_ layout: String) {
        let row = conversation("帮我约周五")
        row.swipeLeft()
        let archive = app.buttons["归档"].firstMatch
        XCTAssertTrue(archive.waitForExistence(timeout: 3), "\(layout): the swipe did not show 归档 (the drawer took it?)")
        shot("\(layout) swiped")
        archive.tap()
        sleep(1)
        shot("\(layout) after 归档")
        XCTAssertTrue(row.waitForNonExistence(timeout: 5), "\(layout): still in the list")
        // Still open: its first row is still there. (The demo's other conversations belong to scheduled tasks, so since
        // #3264 / #3566 they are in Auto Chats or under Scheduled Tasks, not Recent, which may be empty now; #5197.)
        XCTAssertTrue(app.buttons["sidebar-main"].firstMatch.exists, "\(layout): the drawer closed or the list went")

        let entry = app.buttons["sidebar-archived"].firstMatch
        XCTAssertTrue(entry.waitForExistence(timeout: 3), "\(layout): no 已归档 entry")
        XCTAssertTrue(entry.label.contains("1"), "\(layout): \(entry.label)")
        entry.tap()
        let archived = app.buttons.matching(identifier: "archived-conversation").firstMatch
        XCTAssertTrue(archived.waitForExistence(timeout: 5), "\(layout): not under 已归档")
        XCTAssertTrue(archived.label.contains("帮我约周五"), archived.label)
        shot("\(layout) archived list")
        archived.swipeLeft()
        let back = app.buttons["取消归档"].firstMatch
        XCTAssertTrue(back.waitForExistence(timeout: 3), "\(layout): no 取消归档")
        back.tap()
        XCTAssertTrue(archived.waitForNonExistence(timeout: 5), "\(layout): still archived")
        XCTAssertTrue(app.staticTexts["没有归档的对话"].waitForExistence(timeout: 3), "\(layout): no empty state")
        shot("\(layout) archived empty")
        app.buttons["sheet-close"].firstMatch.tap()
        XCTAssertTrue(conversation("帮我约周五").waitForExistence(timeout: 5), "\(layout): not back in the list")
        XCTAssertFalse(app.buttons["sidebar-archived"].exists, "\(layout): 已归档 with nothing in it")
    }

    @MainActor func testArchiveInTheShell() {
        launch()
        archiveAndBringBack("shell")
        // A drag to the left off the rows still closes the drawer.
        let w = app.windows.firstMatch
        w.coordinate(withNormalizedOffset: CGVector(dx: 0.6, dy: 0.12))
            .press(forDuration: 0.05, thenDragTo: w.coordinate(withNormalizedOffset: CGVector(dx: 0.05, dy: 0.12)))
        XCTAssertTrue(conversation("帮我约周五").waitForNonExistence(timeout: 3), "the drawer did not close")
    }

    /// The main conversation has no Archive. In the shell it is the drawer's first row, Persona, above Inbox and once
    /// only: not again under Pin or Recent (#2004).
    @MainActor func testTheMainConversationStays() {
        launch()
        let main = app.buttons["sidebar-main"].firstMatch
        XCTAssertTrue(main.waitForExistence(timeout: 5), "no Persona row at the top of the drawer")
        XCTAssertFalse(conversation("Persona").exists, "the main conversation is under 置顶 too")
        XCTAssertEqual(app.buttons.matching(identifier: "sidebar-main").count, 1, "the main conversation shows more than once")
        let inbox = app.buttons["sidebar-inbox"].firstMatch
        XCTAssertTrue(inbox.exists && main.frame.maxY <= inbox.frame.minY, "Persona is not above 收件箱")
        sleep(1) // the drawer's slide
        shot("shell drawer")
        main.swipeLeft()
        sleep(1)
        XCTAssertFalse(app.buttons["归档"].exists, "the main conversation offered 归档")
    }
}

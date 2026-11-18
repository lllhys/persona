import XCTest

/// iPad's Inbox selection keeps Select All at the top leading, beside Back or the hub picker, rather than the
/// iPhone shell's one-leading-button bottom action bar. This is a real selection flow, not place-stack coverage.
final class IPadInboxSelectionTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom != .pad, "iPad's non-shell Inbox selection toolbar")
    }

    @MainActor func testSelectAllStaysAtTheTopLeadingAndDoneRestoresTheInbox() throws {
        app = XCUIApplication()
        let hub = try XCTUnwrap(ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"])
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-dev.openChat", "", "-inbox.kind", "", "-inbox.source", ""]
        app.launchPinned()
        acceptIconAlert()

        let select = app.buttons["inbox.select"]
        XCTAssertTrue(select.waitForExistence(timeout: 20), "the Inbox did not open:\n\(app.debugDescription)")
        select.tap()

        let all = app.buttons["inbox.selectAll"]
        XCTAssertTrue(all.waitForExistence(timeout: 10), "Select did not enter selection")
        let bar = app.navigationBars["收件箱"]
        XCTAssertTrue(bar.waitForExistence(timeout: 5))
        XCTAssertTrue(all.isHittable, "Select All is not directly accessible:\n\(app.debugDescription)")
        let leading = bar.buttons.matching(identifier: "inbox.selectAll").allElementsBoundByIndex
        XCTAssertEqual(leading.count, 1, "Select All must remain one leading toolbar item")
        let frame = try XCTUnwrap(leading.first?.frame)
        let done = app.buttons["inbox.selectDone"]
        print("IPAD-SELECTION-FRAMES bar=\(bar.frame) selectAll=\(frame) done=\(done.frame)")
        XCTAssertLessThan(frame.midX, done.frame.midX, "Select All must remain to Done's leading side")
        XCTAssertLessThan(frame.midY, app.windows.firstMatch.frame.height * 0.4, "Select All moved into the bottom action bar")
        shot("select-all-leading")

        all.tap()
        let deselect = app.buttons.matching(identifier: "inbox.selectAll")
            .matching(NSPredicate(format: "label == '取消全选' OR label == 'Deselect All'")).firstMatch
        XCTAssertTrue(deselect.waitForExistence(timeout: 5), "Select All did not become Deselect All")
        deselect.tap()
        let selectAllAgain = app.buttons.matching(identifier: "inbox.selectAll")
            .matching(NSPredicate(format: "label == '全选' OR label == 'Select All'")).firstMatch
        XCTAssertTrue(selectAllAgain.waitForExistence(timeout: 5), "Deselect All did not restore the Select All label")
        let zero = app.staticTexts.matching(NSPredicate(format: "label == '已选 0 项' OR label == '0 Selected'")).firstMatch
        XCTAssertTrue(zero.waitForExistence(timeout: 5), "Deselect All did not clear the picked count:\n\(app.debugDescription)")
        shot("deselected-zero")
        app.buttons["inbox.selectDone"].tap()
        XCTAssertTrue(select.waitForExistence(timeout: 5), "Done did not restore the ordinary Inbox toolbar")
        XCTAssertFalse(all.exists, "Done left the selection toolbar in place")
        shot("done-restores-inbox")
    }

    @MainActor private func shot(_ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir + "/ipad-inbox-" + name + ".png"))
    }
}

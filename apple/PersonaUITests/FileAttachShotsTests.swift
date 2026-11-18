import XCTest

/// Screenshots of files sent with a message as cards (#8361, docs/protocol/protocol.md「Sending files」), against
/// scripts/mock/mock_hub.py with MOCK_FILE_ATTACH=1 (a sent message with eight files, and an older one whose file is
/// gone), MOCK_UPLOAD_FAIL=logs-1114 (that upload fails) and a slow MOCK_UPLOAD_KBPS, given as PERSONA_FILE_SHOTS_HUB
/// (TEST_RUNNER_PERSONA_FILE_SHOTS_HUB=http://127.0.0.1:<port>). PERSONA_SHOTS=<dir> keeps the shots, named with
/// PERSONA_APPEARANCE and PERSONA_SIZE (a UIContentSizeCategory name; unset: the default size). Mock data only.
final class FileAttachShotsTests: XCTestCase {
    private let widget = "macbook/a1f3c2d4-0000-4000-8000-000000000001"
    private static let picked = "q3-report.pdf,budget-2026.xlsx,sales-by-region.xlsx,meeting-notes.docx,logs-1114.zip,contract-draft.pdf,季度汇总.xlsx,screenshots.zip"
    private static let typed = "帮我对比这几份季度数据，重点看第三季度的支出" // i18n-ignore: the made-up message the shots show

    override func setUpWithError() throws {
        continueAfterFailure = true
        guard ProcessInfo.processInfo.environment["PERSONA_FILE_SHOTS_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_FILE_ATTACH=1 (PERSONA_FILE_SHOTS_HUB)")
        }
    }

    /// The sent message with eight files: three cards and the rest folded, opened, a file in the viewer and its
    /// download menu.
    @MainActor func testSentCards() throws {
        let app = launch([])
        let more = app.buttons["file-cards-more"].firstMatch
        XCTAssertTrue(more.waitForExistence(timeout: 30), "no folded cards:\n\(app.debugDescription)")
        sleep(2) // the gone file's look-up
        shot(app, "1-sent")
        // The older message's file the computer no longer has, a little further up.
        let gone = app.buttons["file-card-gone"].firstMatch
        XCTAssertTrue(gone.exists, "no card for the file that is gone:\n\(app.debugDescription)")
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
            .press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.55)))
        sleep(1)
        shot(app, "1b-gone")
        for _ in 0..<4 { app.swipeUp(velocity: .fast) }
        sleep(1)
        more.tap()
        sleep(1)
        shot(app, "2-expanded")
        let card = app.buttons.matching(NSPredicate(format: "identifier == 'file-card' AND label CONTAINS 'q3-report.pdf'")).firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 10), "no card for the PDF:\n\(app.debugDescription)")
        card.tap()
        let download = app.buttons["file-download"].firstMatch
        XCTAssertTrue(download.waitForExistence(timeout: 15), "the viewer did not open:\n\(app.debugDescription)")
        sleep(2) // the PDF draws
        shot(app, "3-viewer")
        download.press(forDuration: 1.2)
        sleep(1)
        shot(app, "4-download-menu")
    }

    /// Eight files picked while typing: on their way three at a time, one failing, then ready; the words stay as
    /// typed; sent, they are cards over the bubble.
    @MainActor func testComposerCards() throws {
        let app = launch(["-openUploadDemo", Self.picked])
        let field = app.descendants(matching: .any)["shell-composer-field"].firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 30), "no composer:\n\(app.debugDescription)")
        for _ in 0..<4 where !app.keyboards.firstMatch.exists { // the box opens up as it takes the focus
            field.tap()
            _ = app.keyboards.firstMatch.waitForExistence(timeout: 3)
        }
        // A draft an earlier run left in this chat goes first (drafts are kept per chat).
        let left = field.value as? String ?? ""
        if !left.isEmpty, left != field.placeholderValue {
            field.coordinate(withNormalizedOffset: CGVector(dx: 0.98, dy: 0.95)).tap()
            field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: left.count + 8))
        }
        field.typeText(Self.typed)
        let busy = app.descendants(matching: .any)["file-attachment-busy"].firstMatch
        XCTAssertTrue(busy.waitForExistence(timeout: 30), "no card on its way:\n\(app.debugDescription)")
        sleep(2)
        shot(app, "5-uploading")
        XCTAssertTrue(wait(timeout: 400) { !busy.exists }, "the uploads did not end")
        let hide = app.buttons["shell-composer-dismiss-keyboard"].firstMatch
        if hide.exists { hide.tap() }
        sleep(1)
        XCTAssertEqual(field.value as? String, Self.typed, "the words changed")
        let strip = app.scrollViews["file-attachments"].firstMatch
        let failed = app.descendants(matching: .any)["file-attachment-failed"].firstMatch
        XCTAssertTrue(failed.exists, "no failed card (MOCK_UPLOAD_FAIL)")
        for _ in 0..<3 where !failed.isHittable { strip.swipeLeft(velocity: .slow) } // the failed one is fifth
        sleep(1)
        shot(app, "6-one-failed")
        if failed.exists {
            app.buttons["移除 logs-1114.zip"].firstMatch.tap() // i18n-ignore: the zh-Hans label the tests run in
            sleep(1)
        }
        for _ in 0..<3 { strip.swipeRight(velocity: .fast) }
        sleep(1)
        shot(app, "7-ready")
        let send = app.buttons.matching(NSPredicate(format: "identifier == %@ AND enabled == true", "shell-composer-send")).firstMatch
        XCTAssertTrue(send.waitForExistence(timeout: 10), "no send button:\n\(app.debugDescription)")
        send.tap()
        XCTAssertTrue(app.buttons["file-cards-more"].firstMatch.waitForExistence(timeout: 15), "no cards over the sent bubble")
        sleep(2)
        shot(app, "8-after-send")
    }

    @MainActor private func launch(_ extra: [String]) -> XCUIApplication {
        let env = ProcessInfo.processInfo.environment
        let hub = env["PERSONA_FILE_SHOTS_HUB"] ?? ""
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", widget, "-openChatsFresh", "YES"] + extra
        if let size = env["PERSONA_SIZE"], !size.isEmpty { app.launchArguments += ["-UIPreferredContentSizeCategoryName", size] }
        app.launchPinned()
        return app
    }

    @MainActor private func wait(timeout: TimeInterval, _ ok: () -> Bool) -> Bool {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if ok() { return true }
            RunLoop.current.run(until: Date().addingTimeInterval(0.5))
        }
        return ok()
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let look = env["PERSONA_APPEARANCE"] ?? "light"
        let size = (env["PERSONA_SIZE"] ?? "").isEmpty ? "default" : "xxxl"
        try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "files-\(name)-\(look)-\(size).png"))
    }
}

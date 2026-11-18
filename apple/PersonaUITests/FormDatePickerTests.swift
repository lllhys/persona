import XCTest

/// The to-do date pickers at the text sizes (#6108, `FormDatePicker`), against the mock hub: the to-do's page (Due,
/// Remind At), "Change Due Date…" and New To-Do. At the accessibility sizes every picker fits the screen's width (the
/// compact picker's label and ends were cut off there). PERSONA_SHOTS=<dir> saves each screen and each picker,
/// `<screen>-<language>-<size>.png` and `<picker>-<language>-<size>.png`, for the before / after pixel comparison.
/// PERSONA_SHOOT says which: `zh:L,XL,XXL,XXXL,AX5;en:L,AX5` (default `zh:L,AX5`). PERSONA_BEFORE (the app built from
/// main) shoots the same and checks nothing.
final class FormDatePickerTests: XCTestCase {
    private var app: XCUIApplication!

    private static let sizes = ["L": "UICTContentSizeCategoryL", "XL": "UICTContentSizeCategoryXL",
                                "XXL": "UICTContentSizeCategoryXXL", "XXXL": "UICTContentSizeCategoryXXXL",
                                "AX5": "UICTContentSizeCategoryAccessibilityXXXL"]
    private static let languages = ["zh": ("(zh-Hans)", "zh_CN"), "en": ("(en)", "en_US"), "ja": ("(ja)", "ja_JP")]
    /// The page's "+" and a sheet's Cancel, as each language says them (the mock's to-dos are in Chinese in all three).
    private static let newToDo = ["zh": "新建待办", "en": "New To-Do", "ja": "新しいToDo"]
    private static let cancel = ["zh": "取消", "en": "Cancel", "ja": "キャンセル"]

    @MainActor func testDatePickersAtTheTextSizes() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        continueAfterFailure = true
        let env = ProcessInfo.processInfo.environment
        let before = env["PERSONA_BEFORE"] != nil
        for part in (env["PERSONA_SHOOT"] ?? "zh:L,AX5").split(separator: ";") {
            let pair = part.split(separator: ":").map(String.init)
            let language = pair[0]
            for size in pair[1].split(separator: ",").map(String.init) {
                let check = !before && size.hasPrefix("AX")
                page(hub, language, size, check: check)
                sheets(hub, language, size, check: check)
            }
        }
    }

    /// The to-do's page with a due day and a reminder (the mock's 「给 NAS 配自动备份」): the screen as it opens, then each
    /// picker brought into view. The page is known by its title in the bar: at the largest sizes its header can fill the
    /// screen, and the plan's rows below it may not exist until scrolled to.
    @MainActor private func page(_ hub: String, _ language: String, _ size: String, check: Bool) {
        let name = "给 NAS 配自动备份"
        launch(hub, language, size, ["-openTodoTitled", name])
        let title = app.navigationBars.staticTexts[name].firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 60), "\(language) \(size): the to-do's page")
        shot("page-\(language)-\(size)")
        for id in ["todo.plan.dueDate", "todo.plan.remindAt"] {
            let picker = app.descendants(matching: .any)[id].firstMatch
            bringIntoView(picker)
            XCTAssertTrue(picker.exists, "\(language) \(size): \(id)")
            let what = id.split(separator: ".").last!
            row(picker, "\(what)-\(language)-\(size)")
            // Below the first screen at the accessibility sizes: the screen there too, with the picker's label.
            if size.hasPrefix("AX") { shot("page-\(what)-\(language)-\(size)") }
            if check { fits(picker, "\(language) \(size) \(id)") }
        }
    }

    /// "Change Due Date…" (`-openRescheduleTitled`) over the month, then New To-Do from the page's "+".
    @MainActor private func sheets(_ hub: String, _ language: String, _ size: String, check: Bool) {
        launch(hub, language, size, ["-openRescheduleTitled", "检查 PR #42"])
        let reschedule = app.descendants(matching: .any)["todo.calendar.rescheduleDue"].firstMatch
        XCTAssertTrue(reschedule.waitForExistence(timeout: 60), "\(language) \(size): Change Due Date")
        shot("reschedule-\(language)-\(size)")
        row(reschedule, "rescheduleDue-\(language)-\(size)")
        if check { fits(reschedule, "\(language) \(size) Change Due Date") }
        app.buttons[Self.cancel[language]!].firstMatch.tap()
        XCTAssertTrue(wait { !reschedule.exists }, "\(language) \(size): Change Due Date closes")

        app.buttons[Self.newToDo[language]!].firstMatch.tap()
        let title = app.textFields["todo.calendar.newTitle"]
        XCTAssertTrue(title.waitForExistence(timeout: 10), "\(language) \(size): New To-Do")
        // The keyboard down (Return with no title adds nothing), so no caret blinks in the screenshot.
        title.typeText("\n")
        XCTAssertTrue(wait { self.app.keyboards.count == 0 }, "\(language) \(size): the keyboard goes down")
        let due = app.descendants(matching: .any)["todo.calendar.newDue"].firstMatch
        XCTAssertTrue(due.waitForExistence(timeout: 10))
        shot("new-\(language)-\(size)")
        row(due, "newDue-\(language)-\(size)")
        if check { fits(due, "\(language) \(size) New To-Do") }
    }

    @MainActor private func launch(_ hub: String, _ language: String, _ size: String, _ extra: [String]) {
        app?.terminate()
        XCUIDevice.shared.appearance = ProcessInfo.processInfo.environment["PERSONA_APPEARANCE"] == "dark" ? .dark : .light
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let (languages, locale) = Self.languages[language]!
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-openAssistantPage", "todos",
                               "-todos.view", "calendar", "-todos.calendar.mode", "month",
                               "-AppleLanguages", languages, "-AppleLocale", locale,
                               "-UIPreferredContentSizeCategoryName", Self.sizes[size]!] + extra
        app.launchPinned()
    }

    /// The picker within the screen's width, both ends.
    @MainActor private func fits(_ picker: XCUIElement, _ what: String) {
        let screen = app.windows.firstMatch.frame
        XCTAssertGreaterThanOrEqual(picker.frame.minX, screen.minX - 1, "\(what): starts off the screen \(picker.frame)")
        XCTAssertLessThanOrEqual(picker.frame.maxX, screen.maxX + 1, "\(what): ends off the screen \(picker.frame)")
    }

    /// Up the form until the picker is wholly on the screen (a row out of sight may not exist yet). Dragged by the
    /// form's margin at the screen's leading edge, never across a wheel (that would turn it), and held at the end so it
    /// stops where it was let go.
    @MainActor private func bringIntoView(_ picker: XCUIElement) {
        let screen = app.windows.firstMatch.frame
        _ = picker.waitForExistence(timeout: 3)
        for _ in 0..<10 {
            if picker.exists, picker.frame.minY >= screen.minY + screen.height * 0.12,
               picker.frame.maxY <= screen.maxY - screen.height * 0.08 { return }
            let from = app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.02, dy: 0.7))
            from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: 0, dy: -screen.height * 0.3)),
                       withVelocity: .slow, thenHoldForDuration: 0.3)
        }
    }

    @MainActor private func wait(_ check: () -> Bool) -> Bool {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline {
            if check() { return true }
            usleep(200_000)
        }
        return false
    }

    /// The screen once two shots in a row are the same: the glass behind a sheet that has just opened can still change
    /// by a shade, which a pixel comparison would count.
    @MainActor private func shot(_ name: String) {
        var last = XCUIScreen.main.screenshot()
        for _ in 0..<8 {
            usleep(300_000)
            let next = XCUIScreen.main.screenshot()
            if next.pngRepresentation == last.pngRepresentation { break }
            last = next
        }
        save(last, name)
    }

    @MainActor private func row(_ element: XCUIElement, _ name: String) {
        save(element.screenshot(), name)
    }

    @MainActor private func save(_ s: XCUIScreenshot, _ name: String) {
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }
}

import XCTest

/// The attach panel's photo grid takes a tap where each cell is drawn (#3702), and its caption is typed in sight (#3704). A thumbnail filling its square is laid out
/// past it (a landscape photo, wider than tall), and the cell to its right, drawn over it, took the taps on its ring: on
/// the open iPhone Duo the ring did nothing, a tap a little to its left picked the photo.
///
/// A phone (the panel is the iPhone's; the iPad keeps the + menu), with landscape photos first in the library and Photos
/// access (the runner: `xcrun simctl addmedia`, `xcrun simctl privacy … grant photos`). TEST_RUNNER_PERSONA_ORIENTATION=landscape
/// turns it on its side (the open Duo). Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB).
final class AttachGridTests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
        guard UIDevice.current.userInterfaceIdiom == .phone else { throw XCTSkip("the attach panel is a phone's") }
    }

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
        super.tearDown()
    }

    /// The ring in each cell's corner picks that cell's photo, and leaves it again.
    @MainActor func testEachRingPicksItsOwnPhoto() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)") }
        XCUIDevice.shared.orientation = ProcessInfo.processInfo.environment["PERSONA_ORIENTATION"] == "landscape" ? .landscapeLeft : .portrait
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-app.tab", "assistant"]
        app.launchPinned()
        let plus = app.buttons.matching(NSPredicate(format: "(identifier == %@ OR label IN %@) AND enabled == true", "shell-composer-add", ["添加", "Add"])).firstMatch
        XCTAssertTrue(plus.waitForExistence(timeout: 30), "no + in the composer:\n\(app.debugDescription)")
        plus.tap()
        let cells = app.buttons.matching(identifier: "attach-photo")
        XCTAssertTrue(cells.firstMatch.waitForExistence(timeout: 15), "no photos in the panel:\n\(app.debugDescription)")
        sleep(2) // the panel's spring, and the thumbnails
        shot(app, "grid")
        // The ring: `Theme.Attach.badge` (29) inset by `badgeInset` (3) from the cell's top trailing corner. In the cell's
        // own coordinates, which hold on its side too.
        for i in 0..<min(cells.count, 4) {
            let cell = cells.element(boundBy: i), side = cell.frame.width
            let ring = cell.coordinate(withNormalizedOffset: CGVector(dx: (side - 17.5) / side, dy: 17.5 / side))
            ring.tap()
            XCTAssertEqual(cell.value as? String, "已选，第 1 张", "cell \(i)'s ring did not pick it; picked: \(picked(cells))")
            if i == 0 { shot(app, "picked") }
            ring.tap()
            XCTAssertEqual(cell.value as? String ?? "", "", "cell \(i)'s ring did not leave it; picked: \(picked(cells))")
        }
    }

    /// A photo picked, its caption is typed above the keyboard, Send beside it (#3704): on the open iPhone Duo on its side
    /// the keyboard takes most of the window, and the field went under it, nothing showing what was typed.
    @MainActor func testTheCaptionStaysAboveTheKeyboard() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)") }
        XCUIDevice.shared.orientation = ProcessInfo.processInfo.environment["PERSONA_ORIENTATION"] == "landscape" ? .landscapeLeft : .portrait
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-app.tab", "assistant"]
        app.launchPinned()
        let plus = app.buttons.matching(NSPredicate(format: "(identifier == %@ OR label IN %@) AND enabled == true", "shell-composer-add", ["添加", "Add"])).firstMatch
        XCTAssertTrue(plus.waitForExistence(timeout: 30), "no + in the composer:\n\(app.debugDescription)")
        plus.tap()
        let cells = app.buttons.matching(identifier: "attach-photo")
        XCTAssertTrue(cells.firstMatch.waitForExistence(timeout: 15), "no photos in the panel:\n\(app.debugDescription)")
        sleep(2)
        let first = cells.firstMatch, side = first.frame.width
        first.coordinate(withNormalizedOffset: CGVector(dx: (side - 17.5) / side, dy: 17.5 / side)).tap()
        let caption = app.descendants(matching: .any)["attach-caption"].firstMatch
        XCTAssertTrue(caption.waitForExistence(timeout: 5), "no caption field after a photo was picked:\n\(app.debugDescription)")
        caption.tap()
        let keyboard = app.keyboards.firstMatch
        XCTAssertTrue(keyboard.waitForExistence(timeout: 5), "no keyboard")
        sleep(1)
        caption.typeText("看看这张图")
        sleep(1)
        shot(app, "caption")
        let send = app.buttons["attach-send"].firstMatch
        XCTAssertLessThanOrEqual(caption.frame.maxY, keyboard.frame.minY + 1, "the caption field is under the keyboard: \(caption.frame), the keyboard \(keyboard.frame)")
        XCTAssertLessThanOrEqual(send.frame.maxY, keyboard.frame.minY + 1, "Send is under the keyboard: \(send.frame), the keyboard \(keyboard.frame)")
        XCTAssertGreaterThanOrEqual(caption.frame.minY, app.windows.firstMatch.frame.minY, "the caption field went over the top: \(caption.frame)")
        XCTAssertTrue(caption.isHittable, "the caption field can't be reached")
        // The caption is the conversation's draft, which the app keeps: what was typed ends it.
        XCTAssertTrue((caption.value as? String ?? "").hasSuffix("看看这张图"), "what was typed is not in the field: \(caption.value ?? "")")
        // Leave the draft empty for the tests after this one (AttachMenuTests' Mention reads it).
        caption.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.9)).tap()
        caption.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: (caption.value as? String ?? "").count + 2))
        sleep(2) // drafts are saved a moment after the last change
    }

    /// The panel is a half-height system sheet with an even grid (#4312): it opens at the medium detent with the chat
    /// above it, the camera is one cell the size of every photo, every cell is square, and the grid scrolls to its last
    /// row clear of the tab bar. Needs 30 or so pictures in the library. `TEST_RUNNER_PERSONA_SHOTS_ONLY=1` takes the
    /// screenshots without the checks (a build from before the change).
    @MainActor func testHalfSheetEvenGrid() throws {
        let check = ProcessInfo.processInfo.environment["PERSONA_SHOTS_ONLY"] != "1"
        let (app, cells) = try openPanel()
        shot(app, "1-open")
        let window = app.windows.firstMatch.frame
        if check {
            let sheet = app.descendants(matching: .any)["attach-sheet"].firstMatch
            XCTAssertGreaterThan(sheet.frame.minY, window.height * 0.3, "the sheet does not open at half height: \(sheet.frame)")
            let camera = app.buttons["attach-camera"].firstMatch
            XCTAssertTrue(camera.exists, "no camera cell")
            let first = cells.element(boundBy: 0).frame
            XCTAssertEqual(camera.frame.width, first.width, accuracy: 0.5, "the camera cell is wider than a photo")
            XCTAssertEqual(camera.frame.height, first.height, accuracy: 0.5, "the camera cell is taller than a photo: \(camera.frame) vs \(first)")
            for i in 0..<cells.count {
                let f = cells.element(boundBy: i).frame
                XCTAssertEqual(f.width, f.height, accuracy: 0.5, "cell \(i) is not square: \(f)")
                XCTAssertEqual(f.width, first.width, accuracy: 0.5, "cell \(i) is not the size of the others: \(f)")
            }
        }
        // To the end: swipe until the last cell on screen stays where it is.
        let grid = app.scrollViews["attach-grid"].firstMatch
        var last = CGRect.null
        for _ in 0..<12 {
            grid.swipeUp(velocity: .fast)
            let now = cells.allElementsBoundByIndex.last?.frame ?? .null
            if now == last { break }
            last = now
        }
        sleep(1)
        shot(app, "2-bottom")
        if check {
            let bar = app.buttons["attach-tab-photos"].firstMatch.frame
            XCTAssertLessThanOrEqual(last.maxY, bar.minY + 1, "the last row stays under the tab bar: \(last), the bar \(bar)")
            XCTAssertLessThanOrEqual(last.maxY, window.maxY, "the last row is cut off: \(last)")
        }
    }

    /// A photo picked in the half-height sheet, the caption is typed right away above the keyboard (#4312).
    @MainActor func testCaptionTypedAboveTheKeyboard() throws {
        let check = ProcessInfo.processInfo.environment["PERSONA_SHOTS_ONLY"] != "1"
        let (app, cells) = try openPanel()
        ring(cells.element(boundBy: 0)).tap()
        let caption = app.descendants(matching: .any)["attach-caption"].firstMatch
        XCTAssertTrue(caption.waitForExistence(timeout: 5), "no caption field after a photo was picked:\n\(app.debugDescription)")
        caption.tap()
        let keyboard = app.keyboards.firstMatch
        XCTAssertTrue(keyboard.waitForExistence(timeout: 10), "no keyboard")
        sleep(1)
        // ASCII: typing Chinese through the simulator's English keyboard stalls its keyboard queue.
        caption.typeText("Look at this one")
        sleep(1)
        shot(app, "3-keyboard")
        if check {
            XCTAssertLessThanOrEqual(caption.frame.maxY, keyboard.frame.minY + 1, "the caption field is under the keyboard: \(caption.frame), the keyboard \(keyboard.frame)")
            XCTAssertTrue(caption.isHittable, "the caption field can't be reached")
            // The caption is the conversation's draft, which the app keeps: what was typed is in it.
            XCTAssertTrue((caption.value as? String ?? "").contains("Look at this one"), "what was typed is not in the field: \(caption.value ?? "")")
        }
        // Leave the draft empty for the tests after this one.
        caption.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.9)).tap()
        caption.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: (caption.value as? String ?? "").count + 2))
        sleep(2) // drafts are saved a moment after the last change
    }

    /// Opened while typing, the composer's keyboard goes down and stays down after the sheet is swiped away; what was
    /// picked lands in the composer once, and the next opening starts with nothing picked (#4312).
    @MainActor func testSwipeDownKeepsThePicksAndTheKeyboardDown() throws {
        let (app, cells) = try openPanel(typingFirst: true)
        ring(cells.element(boundBy: 0)).tap()
        XCTAssertEqual(cells.element(boundBy: 0).value as? String, "已选，第 1 张", "the first photo was not picked")
        let sheet = app.descendants(matching: .any)["attach-sheet"].firstMatch
        sheet.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.02))
            .press(forDuration: 0.05, thenDragTo: app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.98)))
        XCTAssertTrue(sheet.waitForNonExistence(timeout: 5), "the sheet did not go on a swipe down")
        sleep(1)
        XCTAssertFalse(app.keyboards.firstMatch.exists, "the composer's keyboard came back after the sheet went")
        let removes = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ OR label BEGINSWITH %@", "移除图片", "Remove Image"))
        XCTAssertTrue(removes.firstMatch.waitForExistence(timeout: 10), "the picked photo is not in the composer:\n\(app.debugDescription)")
        XCTAssertEqual(removes.count, 1, "the picked photo went into the composer \(removes.count) times")
        shot(app, "4-after-swipe")
        app.buttons["shell-composer-add"].firstMatch.tap()
        XCTAssertTrue(cells.firstMatch.waitForExistence(timeout: 15), "the panel did not open again")
        sleep(1)
        let picked = (0..<cells.count).filter { !((cells.element(boundBy: $0).value as? String) ?? "").isEmpty }
        XCTAssertEqual(picked, [], "the next opening starts with photos picked")
        // Leave the composer as it was: close the panel and take the photo out.
        app.buttons["attach-close"].firstMatch.tap()
        _ = app.descendants(matching: .any)["attach-sheet"].firstMatch.waitForNonExistence(timeout: 5)
        removes.firstMatch.tap()
        sleep(2)
    }

    /// Launches against the mock hub with the camera cell shown, opens the panel and gives it the photo library.
    /// `typingFirst`: the composer's field has the keyboard when + is tapped.
    @MainActor private func openPanel(typingFirst: Bool = false) throws -> (XCUIApplication, XCUIElementQuery) {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)") }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-app.tab", "assistant", "-showAttachCamera", "YES"]
        app.launchPinned()
        let plus = app.buttons.matching(NSPredicate(format: "(identifier == %@ OR label IN %@) AND enabled == true", "shell-composer-add", ["添加", "Add"])).firstMatch
        XCTAssertTrue(plus.waitForExistence(timeout: 30), "no + in the composer:\n\(app.debugDescription)")
        if typingFirst {
            let field = app.descendants(matching: .any)["shell-composer-field"].firstMatch
            XCTAssertTrue(field.waitForExistence(timeout: 10), "no composer field")
            field.tap()
            XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 10), "no keyboard for the composer")
        }
        // A fresh install asks for the library on the first +: give it all (the runner's grant does not always hold, and
        // the default handler would deny it).
        addUIInterruptionMonitor(withDescription: "photos access") { alert in
            let allow = alert.buttons.matching(NSPredicate(format: "label IN %@", ["允许完全访问", "Allow Full Access"])).firstMatch
            guard allow.exists else { return false }
            allow.tap()
            return true
        }
        plus.tap()
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons
            .matching(NSPredicate(format: "label IN %@", ["允许完全访问", "Allow Full Access"])).firstMatch
        if allow.waitForExistence(timeout: 5) { allow.tap() }
        let cells = app.buttons.matching(identifier: "attach-photo")
        XCTAssertTrue(cells.firstMatch.waitForExistence(timeout: 15), "no photos in the panel:\n\(app.debugDescription)")
        sleep(2) // the sheet's spring, and the thumbnails
        return (app, cells)
    }

    /// A cell's ring, which picks (a tap on the picture opens the editor, #4963).
    @MainActor private func ring(_ cell: XCUIElement) -> XCUICoordinate {
        let side = cell.frame.width
        return cell.coordinate(withNormalizedOffset: CGVector(dx: (side - 17.5) / side, dy: 17.5 / side))
    }

    /// The cells that read as picked, by their place in the grid.
    @MainActor private func picked(_ cells: XCUIElementQuery) -> [Int] {
        (0..<cells.count).filter { !((cells.element(boundBy: $0).value as? String) ?? "").isEmpty }
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        XCTContext.runActivity(named: "duo-shot attach-\(name)") { _ in
            if let hold = ProcessInfo.processInfo.environment["PERSONA_HOLD"].flatMap(UInt32.init) { sleep(hold) }
        }
    }
}

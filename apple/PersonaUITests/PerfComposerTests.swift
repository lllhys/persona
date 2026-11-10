import XCTest

/// The shell composer over the long main conversation (#2660's composer): the card opening and closing with the
/// keyboard, typing until it grows, send and the mic swapping, and the + panel with and without the keyboard.
/// Mock: the `assistant` suite's (`scripts/app/perf-test.sh composer`). With `-perfCounts YES` each run's
/// `frames.json` also lists the view bodies evaluated, which shows how far a keystroke redraws.
///
/// Inside a measured block the test only taps, drags and types at places found beforehand, and waits by sleeping:
/// an element query snapshots the whole accessibility tree of the long conversation on the app's main thread
/// (100 ms and more on a simulator), and those frames would be counted as the composer's.
final class PerfComposerTests: PerfTestCase {
    @MainActor private func openMain() {
        launch(["-dev.openChat", "", "-app.tab", "assistant", "-tab", "assistant", "-openAssistant", PerfAssistantTests.main,
                "-holdChromeOpen", "YES", "-perfCounts", "YES"])
        see(app.buttons["assistant-sidebar-button"], "the main conversation", timeout: 30)
        see(field, "the composer")
        sleep(2)
    }

    @MainActor private var field: XCUIElement { any("shell-composer-field") }

    /// A point on screen, resolved now; tapping it later needs no element query.
    @MainActor private func point(_ e: XCUIElement) -> XCUICoordinate {
        let f = e.frame
        return app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: f.midX, dy: f.midY))
    }

    /// The conversation dragged down a little: the keyboard follows it away (`ComposerKeyboardTests`).
    @MainActor private func dismissKeyboard() {
        let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
        from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: 0, dy: 160)), withVelocity: .default,
                   thenHoldForDuration: 0.1)
    }

    @MainActor private func erase(_ n: Int) {
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: n))
    }

    /// The photo-library prompt the panel's first open can raise, answered before anything is measured.
    @MainActor private func answerPhotoPrompt() {
        let alerts = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts
        for label in ["允许完全访问", "Allow Full Access", "允许", "Allow"] {
            let b = alerts.buttons[label]
            if b.waitForExistence(timeout: 1) { b.tap(); return }
        }
    }

    /// Opens the panel once from `plus` (answering the library prompt), and returns where its close button is.
    @MainActor private func warmPanel(_ plus: XCUICoordinate) -> XCUICoordinate {
        plus.tap()
        answerPhotoPrompt()
        see(any("attach-sheet"), "the + panel")
        sleep(2)
        let close = point(app.buttons["attach-close"].firstMatch)
        close.tap()
        _ = wait(any("attach-sheet"), shown: false)
        sleep(2)
        return close
    }

    /// Tapped into, the one line opens into the card with the keyboard; dragged away, it folds back.
    @MainActor func testComposerFocusAndBlur() throws {
        openMain()
        let tap = point(field)
        measureFrames(metrics()) {
            tap.tap()
            sleep(2)
            dismissKeyboard()
            sleep(2)
        }
    }

    /// A person's pace into the open card, over five lines (the box grows) and back to empty.
    @MainActor func testComposerTypingGrows() throws {
        openMain()
        field.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "no software keyboard")
        sleep(1)
        let text = "看一下这个页面为什么会卡，顺便测一下输入的延迟\n第二行\n第三行比较长一点，会折到下一行去看看高度怎么变\n第四行\n第五行"
        measureFrames(metrics()) {
            field.typeText(text)
            erase(text.count + 2)
        }
    }

    /// The same, typed in plain letters: the keyboard's own work for Chinese text (its tokenizer) stays out of it.
    @MainActor func testComposerTypingLatin() throws {
        openMain()
        field.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "no software keyboard")
        sleep(1)
        let text = "why does this page stutter while typing, and how long does a key take to show up on screen\nsecond line\nthird"
        measureFrames(metrics()) {
            field.typeText(text)
            erase(text.count + 2)
        }
    }

    /// One character in and out: send grows in over the mic's place and goes again.
    @MainActor func testComposerSendSwap() throws {
        openMain()
        field.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "no software keyboard")
        sleep(1)
        measureFrames(metrics()) {
            for _ in 0..<3 {
                field.typeText("a")
                usleep(800_000)
                erase(1)
                usleep(800_000)
            }
        }
    }

    /// The + panel opened from the one line and closed again (no keyboard).
    @MainActor func testComposerPlusPanel() throws {
        openMain()
        let plus = point(app.buttons["shell-composer-add"].firstMatch)
        let close = warmPanel(plus)
        measureFrames(metrics()) {
            plus.tap()
            sleep(2)
            close.tap()
            sleep(2)
        }
    }

    /// + with the keyboard up: the keyboard goes first, then the panel (Telegram's hand-off), and back.
    @MainActor func testComposerPlusOverKeyboard() throws {
        openMain()
        field.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "no software keyboard")
        sleep(1)
        let plus = point(app.buttons["shell-composer-add"].firstMatch) // the card's row, over the keyboard
        let close = warmPanel(plus) // closed, the keyboard stays down (Telegram's): the field is the one line again
        let tap = point(field)
        measureFrames(metrics()) {
            tap.tap()
            sleep(2)
            plus.tap()
            sleep(2)
            close.tap()
            sleep(2)
        }
    }
}

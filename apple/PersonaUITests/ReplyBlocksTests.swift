import XCTest
import PersonaWire

/// A reply's code blocks, inline code and quotes as Telegram draws them (#2984): a code block's Copy takes the whole
/// block (and scrolls sideways when a line runs past it), a tap on inline code takes the span, and a long quote folds and opens. What was copied is read back by
/// pasting it into the composer (the runner may not read the app's pasteboard).
///   PORT=18950 MOCK_REPLY_BLOCKS=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18950 xcodebuild test … -only-testing:PersonaUITests/ReplyBlocksTests
final class ReplyBlocksTests: XCTestCase {
    private var app: XCUIApplication!

    private var composer: XCUIElement { app.descendants(matching: .any)["shell-composer-field"].firstMatch }

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_REPLY_BLOCKS=1 (PERSONA_MOCK_HUB)")
        }
    }

    /// The chat with the reply, as it loads (`extra`: more launch arguments, a text size).
    @MainActor private func open(_ extra: [String] = []) {
        let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"]!
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-0000005e1ec9",
                               "-openChatsFresh", "YES"] + extra
        app.launchPinned()
        let last = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "有问题再找我")).firstMatch
        XCTAssertTrue(last.waitForExistence(timeout: 20), "the reply did not show:\n\(app.debugDescription)")
        acceptIconAlert(timeout: 2)
    }

    private func menuItem(_ label: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "(elementType == %d OR elementType == %d) AND label == %@",
            XCUIElement.ElementType.menuItem.rawValue, XCUIElement.ElementType.button.rawValue, label)).firstMatch
    }

    @MainActor private func toast(_ words: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", words)).firstMatch
    }

    /// The composer emptied: the cursor put at its end (its bottom right: a long paste is several lines), then
    /// deleted back. A paste left in it would stay as a draft for the next test. A tall draft does not always take the
    /// cursor at its end (a run once kept two lines of one and pasted between them): looked at again until it is empty.
    @MainActor private func empty() {
        let field = composer
        for _ in 0..<3 {
            let text = field.value as? String ?? ""
            guard !text.isEmpty, text != field.placeholderValue else { return }
            field.coordinate(withNormalizedOffset: CGVector(dx: 0.97, dy: 0.92)).tap()
            field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: text.count + 8))
        }
    }

    /// What the app put on the pasteboard, pasted into the empty composer from its edit menu (a paste the user taps
    /// asks nothing), then deleted again.
    @MainActor private func pasted() -> String {
        let field = composer
        XCTAssertTrue(field.waitForExistence(timeout: 5), "no composer:\n\(app.debugDescription)")
        field.tap()
        empty()
        let paste = menuItem("粘贴")
        for _ in 0..<2 where !paste.exists { // the menu does not always come up on the press after the deleting
            field.press(forDuration: 1.0)
            _ = paste.waitForExistence(timeout: 3)
        }
        XCTAssertTrue(paste.exists, "no 粘贴 in the composer's edit menu:\n\(app.debugDescription)")
        paste.tap()
        sleep(1)
        let got = composer.value as? String ?? ""
        empty()
        sleep(2) // drafts are saved a second after the last change (`Debouncer`): the next test's launch kept the paste
        return got
    }

    @MainActor func testACodeBlocksCopyTakesTheWholeBlock() throws {
        open()
        let copy = app.buttons.matching(identifier: "code-block-copy").firstMatch
        XCTAssertTrue(copy.waitForExistence(timeout: 5), "no Copy on the code block:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["ts"].exists, "the fence's language is not in the block's bar")
        XCTAssertEqual(app.buttons.matching(identifier: "code-block-copy").count, 2, "the fence without a language has no Copy")
        copy.tap()
        XCTAssertTrue(toast("已拷贝代码").waitForExistence(timeout: 3), "no Code copied notice")
        XCTAssertEqual(pasted(), "export async function clear(page: string) {\n  await caches.delete(`page:${page}`)\n  return reload(page)\n}")
    }

    /// At the largest text size a code line runs past its block's edge (code does not wrap): the block scrolls sideways
    /// to show the rest, a left swipe on it moving the line left.
    @MainActor func testACodeBlockScrollsSidewaysAtTheLargestText() throws {
        open(["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"])
        let text = "rm -rf .cache && npm run build"
        let line = app.staticTexts.matching(NSPredicate(format: "label == %@", text)).firstMatch
        XCTAssertTrue(line.waitForExistence(timeout: 5), "no short code block:\n\(app.debugDescription)")
        // The chat opens at its end, this block above the quotes: dragged down until it is on screen.
        for _ in 0..<6 where !line.isHittable {
            app.scrollViews.firstMatch.swipeDown(velocity: .slow)
        }
        XCTAssertTrue(line.isHittable, "the short code block did not come on screen:\n\(app.debugDescription)")
        // Its own scroll view: the innermost of those holding the line (the chat's holds it too).
        let holders = app.scrollViews.containing(NSPredicate(format: "label == %@", text))
        XCTAssertGreaterThan(holders.count, 1, "the code block is not a scroll view of its own:\n\(app.debugDescription)")
        let block = holders.element(boundBy: holders.count - 1)
        let start = line.frame.minX
        block.swipeLeft(velocity: .slow)
        sleep(1) // settled: a block with nothing to scroll would have bounced back
        XCTAssertLessThan(line.frame.minX, start - 20, "the code block did not scroll sideways")
    }

    @MainActor func testATapOnInlineCodeCopiesIt() throws {
        open()
        let code = app.links.matching(NSPredicate(format: "label CONTAINS %@", "npm run build")).firstMatch
        XCTAssertTrue(code.waitForExistence(timeout: 5), "inline code is not tappable:\n\(app.debugDescription)")
        // The chat can open with the composer focused (an earlier test's draft) and this paragraph under the top bar,
        // where a tap goes to the bar: drag the chat down until the code is clear of it.
        let bar = app.buttons["chat.compactTitle"]
        for _ in 0..<3 where bar.exists && code.frame.minY < bar.frame.maxY + 8 {
            app.scrollViews.firstMatch.swipeDown(velocity: .slow)
        }
        code.tap()
        XCTAssertTrue(toast("已拷贝").waitForExistence(timeout: 3), "no Copied notice")
        XCTAssertEqual(pasted(), "npm run build")
    }

    @MainActor func testALongQuoteFoldsAndOpens() throws {
        open()
        let quotes = app.descendants(matching: .any).matching(identifier: "reply-quote")
        XCTAssertTrue(quotes.firstMatch.waitForExistence(timeout: 5))
        XCTAssertEqual(quotes.count, 2)
        // A container's frame takes in the lines clipped away, and the chat holds its bottom still while the quote
        // grows: how tall it shows is from its words' top to its chevron.
        let chevron = app.buttons.matching(identifier: "quote-fold").firstMatch
        let words = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "上线前请确认")).firstMatch
        XCTAssertTrue(chevron.exists && words.exists, "the long quote is not folded")
        XCTAssertEqual(chevron.label, "展开")
        func shown() -> CGFloat { chevron.frame.maxY - words.frame.minY }
        let folded = shown()
        app.buttons["展开"].firstMatch.tap()
        XCTAssertTrue(app.buttons["收起"].firstMatch.waitForExistence(timeout: 3), "no 收起 on the opened quote")
        sleep(1) // it opens with the layout animation
        XCTAssertGreaterThan(shown(), folded + 20, "opened, the quote did not grow")
        chevron.tap()
        XCTAssertTrue(app.buttons["展开"].firstMatch.waitForExistence(timeout: 3), "folded again, no 展开")
        sleep(1)
        XCTAssertLessThan(abs(shown() - folded), 2, "folded again, not as before")
    }
}

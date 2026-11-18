import XCTest

/// Changed documents and pictures in the Git tab compare their versions (#1299) instead of 「没有文本改动」.
///   PORT=18785 MOCK_AGENTS=1 MOCK_FILES=1 MOCK_COMPARE=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18785 xcodebuild test … -only-testing:PersonaUITests/CompareFileTests
final class CompareFileTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_AGENTS=1 MOCK_FILES=1 MOCK_COMPARE=1 (PERSONA_MOCK_HUB)")
        }
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "compare \(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor private func row(_ name: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label CONTAINS %@", name)).firstMatch
    }

    @MainActor func testDocumentsAndPicturesCompare() {
        let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"]!
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000001", "-openPane", "git"]
        app.launchPinned()
        let docx = row("季度报告.docx")
        XCTAssertTrue(docx.waitForExistence(timeout: 25), "the changed Word file is not in the Git tab")
        docx.tap()
        XCTAssertTrue(app.descendants(matching: .any)["compare-text"].waitForExistence(timeout: 20), "the Word file was not compared")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "+10.2%")).firstMatch.exists, "the new text is missing")
        sleep(1)
        shot("docx")
        app.buttons["sheet-close"].firstMatch.tap()
        let deck = row("季度回顾.pptx")
        XCTAssertTrue(deck.waitForExistence(timeout: 10))
        deck.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "幻灯片")).firstMatch.waitForExistence(timeout: 20),
                      "the deck's changes are not under their slide")
        sleep(1)
        shot("pptx")
        app.buttons["sheet-close"].firstMatch.tap()
        let picture = row("banner.png")
        XCTAssertTrue(picture.waitForExistence(timeout: 10))
        picture.tap()
        XCTAssertTrue(app.descendants(matching: .any)["compare-image"].waitForExistence(timeout: 20), "the picture was not compared")
        sleep(1)
        shot("image")
        app.buttons["滑块"].firstMatch.tap()
        sleep(1)
        shot("image-slider")
    }
}

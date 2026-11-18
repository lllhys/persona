import XCTest

/// Regression evidence for the six-line iOS composer and manual draft scrolling (#5650).
final class ComposerDraftBehaviorTests: XCTestCase {
    private var app: XCUIApplication!
    private var appearance: String { ProcessInfo.processInfo.environment["PERSONA_APPEARANCE"] ?? "light" }
    private var field: XCUIElement { element("shell-composer-field") }
    private func element(_ id: String) -> XCUIElement { app.descendants(matching: .any).matching(identifier: id).firstMatch }

    override func tearDown() {
        app?.terminate()
        super.tearDown()
    }

    @MainActor func testSixLineGrowthManualScrollAndExpandedLayout() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs the isolated composer mock (PERSONA_MOCK_HUB)")
        }
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-AppleLanguages", "(zh-Hans)"]
        app.launchPinned()
        XCTAssertTrue(field.waitForExistence(timeout: 20), app.debugDescription)
        app.terminate()
        app.launchArguments += ["-app.tab", "assistant", "-tab", "assistant", "-probes", "YES", "-holdChromeOpen", "YES",
                                "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001"]
        if ProcessInfo.processInfo.environment["PERSONA_LARGE_TYPE"] == "1" {
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"]
        }
        app.launchPinned()
        XCTAssertTrue(field.waitForExistence(timeout: 20), app.debugDescription)
        field.coordinate(withNormalizedOffset: CGVector(dx: 0.25, dy: 0.75)).tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5))
        clearDraft()
        if ProcessInfo.processInfo.environment["PERSONA_LARGE_TYPE"] == "1" {
            // Physical room is smaller at the largest type size; verify the controls and text, not six visible rows.
            field.typeText("1\n2\n3")
            capture("large-type-compact")
            verifyExpandedLayout()
            clearDraft()
            return
        }

        var heights: [CGFloat] = []
        for line in 1...6 {
            field.typeText((line == 1 ? "" : "\n") + "Draft line \(line)")
            pause(1)
            heights.append(field.frame.height)
            capture("line-\(line)")
        }
        for index in 1..<heights.count {
            XCTAssertGreaterThan(heights[index], heights[index - 1] + 5, "line \(index + 1) did not grow: \(heights)")
        }
        XCTAssertEqual(readLines()?.maximumLines, 6)
        field.typeText((7...14).map { "\nDraft line \($0)" }.joined())
        pause(1)
        XCTAssertEqual(field.frame.height, heights.last!, accuracy: 2, "the composer exceeded six lines")
        capture("capped-14-lines")
        print("COMPOSER_RESULT: \(appearance) heights=\(heights), capped=\(field.frame.height)")

        // A drag in the text area, not a tap (which would move the cursor). The caret stays on the last line.
        let start = field.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.15))
        let end = field.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.85))
        for _ in 0..<4 {
            start.press(forDuration: 0.05, thenDragTo: end, withVelocity: .slow, thenHoldForDuration: 0.1)
            pause(0.5)
            if let lines = readLines(), lines.offset <= 1 { break }
        }
        pause(1)
        let before = try XCTUnwrap(readLines())
        XCTAssertLessThanOrEqual(before.offset, 1, "the manual drag did not reach the first line: \(before)")
        capture("scroll-first-t0")
        let started = Date()
        // The DEBUG line probe keeps sampling while the person's reading position stays put.
        for _ in 0..<6 {
            pause(1)
            let current = try XCTUnwrap(readLines())
            XCTAssertEqual(current.offset, before.offset, accuracy: 1, "the draft jumped back without input")
        }
        capture("scroll-first-t6")
        print("COMPOSER_RESULT: \(appearance) manual-scroll offset=\(before.offset)->\(readLines()?.offset ?? -999), hold=\(Date().timeIntervalSince(started))s")

        field.typeText("!")
        pause(1)
        let afterInput = try XCTUnwrap(readLines())
        XCTAssertGreaterThan(afterInput.offset, before.offset + 10, "typing did not reveal the caret at the end")
        capture("typing-follows-caret")

        verifyExpandedLayout()
        clearDraft()
    }

    @MainActor private func verifyExpandedLayout() {
        element("shell-composer-expand").tap()
        let page = element("composer-page-field")
        XCTAssertTrue(page.waitForExistence(timeout: 5))
        let hide = element("composer-page-dismiss-keyboard")
        let collapse = element("composer-page-collapse")
        XCTAssertTrue(hide.exists)
        XCTAssertTrue(collapse.exists)
        XCTAssertGreaterThanOrEqual(page.frame.minY, max(hide.frame.maxY, collapse.frame.maxY), "text overlaps the controls")
        XCTAssertGreaterThan(collapse.frame.midX, app.frame.midX)
        capture("expanded")
        let draft = page.value as? String
        hide.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 5))
        XCTAssertEqual(page.value as? String, draft)
        capture("expanded-keyboard-hidden")
        collapse.tap()
        XCTAssertTrue(page.waitForNonExistence(timeout: 5))
        XCTAssertEqual(field.value as? String, draft)
        capture("collapsed")
    }

    private struct Lines: Decodable {
        var count: Int
        var offset: Double
        var visible: Double
        var maximumLines: Int
        var cutTop: Bool
        var cutFoot: Bool
    }

    @MainActor private func readLines() -> Lines? { app.probe("composer-lines", as: Lines.self) }

    @MainActor private func clearDraft() {
        guard let text = field.value as? String,
              text.hasPrefix("Draft") || text.hasPrefix("1\n") || text == "1" else { return }
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: text.count + 2))
        pause(1)
    }

    private func pause(_ seconds: TimeInterval) { Thread.sleep(forTimeInterval: seconds) }

    @MainActor private func capture(_ state: String) {
        let name = "composer-\(appearance)-\(state)"
        XCTContext.runActivity(named: name) { _ in
            let attachment = XCTAttachment(screenshot: app.screenshot())
            attachment.name = name
            attachment.lifetime = .keepAlways
            add(attachment)
            // The host captures the actual simulator through simctl while this state remains visible.
            print("COMPOSER_CAPTURE: \(name)")
            if let directory = ProcessInfo.processInfo.environment["PERSONA_CAPTURE_DIR"] {
                try? Data().write(to: URL(fileURLWithPath: directory).appending(path: name + ".ready"))
            }
            pause(3)
        }
    }
}

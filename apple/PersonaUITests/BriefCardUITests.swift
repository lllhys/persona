import XCTest

/// A routine's facts card, the `brief` feed item (#2320), against scripts/mock/mock_hub.py with MOCK_TASKS=1
/// MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB): the mock's item is the main conversation's newest line. On an iPhone the done
/// and failed rows fold into their totals until the heading is tapped; Accept and Handled turn into what happened, and a
/// waiting card's Open goes to the chat it came from. With PERSONA_SHOTS set it writes the card there
/// (PERSONA_SHOTS_APPEARANCE names the appearance).
final class BriefCardUITests: XCTestCase {
    private let main = "server/b2e4d6f8-0000-4000-8000-000000000001"

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        let env = ProcessInfo.processInfo.environment
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let appearance = env["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/brief-card-\(appearance)-\(name).png"))
    }

    /// Drags the chat until `element` sits clear of the top bar and the composer: the card is taller than the screen, and a
    /// tap on a part of it scrolled under the bars lands on the bars (an element there still counts as hittable).
    @MainActor private func reveal(_ element: XCUIElement, in app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
        let window = app.windows.firstMatch.frame
        let band = (window.minY + window.height * 0.2)...(window.minY + window.height * 0.75)
        let middle = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        for _ in 0..<12 where !band.contains(element.frame.midY) {
            // Above the band: pull the chat down; below it: push it up. Slow, and held at the end, so it doesn't fling.
            let to = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: element.frame.midY < band.lowerBound ? 0.75 : 0.25))
            middle.press(forDuration: 0.1, thenDragTo: to, withVelocity: .slow, thenHoldForDuration: 0.2)
        }
        XCTAssertTrue(band.contains(element.frame.midY) && element.isHittable, "can't bring \(element) into view: \(element.frame)",
                      file: file, line: line)
    }

    @MainActor func testTheCardFoldsAndItsButtonsSayWhatHappened() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB)")
        }
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant", "-openAssistant", main]
        app.launchPinned()
        // The notification question from SpringBoard covers the card on a fresh install.
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let allow = springboard.buttons.matching(NSPredicate(format: "label IN %@", ["Allow", "允许"])).firstMatch
        if allow.waitForExistence(timeout: 3) { allow.tap() }

        let card = app.descendants(matching: .any)["brief.card"].firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 30), "no brief card:\n\(app.debugDescription)")

        // Folded: the heading carries the totals, the rows and "and 5 more" wait behind it.
        let past = app.buttons["brief.past"]
        XCTAssertTrue(past.waitForExistence(timeout: 10), "the past section does not fold on an iPhone")
        XCTAssertEqual(past.value as? String, "已收起")
        let more = app.staticTexts["还有 5 项"]
        let done = app.buttons["brief.done.todo_task_blog"]
        XCTAssertFalse(more.exists)
        XCTAssertFalse(done.exists)
        try shot(app, "folded")

        reveal(past, in: app)
        past.tap()
        XCTAssertTrue(more.waitForExistence(timeout: 5), "unfolded, no \"and 5 more\":\n\(app.debugDescription)")
        XCTAssertTrue(done.exists)
        XCTAssertEqual(past.value as? String, "已展开")
        try shot(app, "unfolded")

        // A deployed to-do's Accept, then a decision's Take recommendation beside it.
        let accept = app.buttons["brief.accept.todo_task_release"]
        XCTAssertTrue(accept.waitForExistence(timeout: 5))
        let take = app.buttons["brief.accept.todo_task_cache"]
        XCTAssertTrue(take.exists)
        XCTAssertTrue(take.label.contains("按推荐来"), take.label)
        reveal(accept, in: app)
        accept.tap()
        let accepted = app.staticTexts["brief.outcome.todo_task_release"]
        XCTAssertTrue(accepted.waitForExistence(timeout: 10), "Accept did not turn into what happened")
        XCTAssertEqual(accepted.label, "已验收")
        XCTAssertFalse(accept.exists)

        // A question that can wait: Handled.
        let handled = app.buttons["brief.later.lt_mock_1"]
        XCTAssertTrue(handled.waitForExistence(timeout: 5))
        reveal(handled, in: app)
        handled.tap()
        let marked = app.staticTexts["brief.outcome.lt_mock_1"]
        XCTAssertTrue(marked.waitForExistence(timeout: 10))
        XCTAssertEqual(marked.label, "已标记为处理过")
        try shot(app, "answered")

        // The card from no chat has no Open; the one that names its chat opens it.
        XCTAssertFalse(app.buttons["打开：需要你批准"].exists)
        let open = app.buttons["打开：Mock 用协议注入还是保留单例？"]
        XCTAssertTrue(open.waitForExistence(timeout: 5))
        reveal(open, in: app)
        open.tap()
        // The Agents page comes up on that chat (the mock's Agent is at work, so its composer queues). The Persona page stays
        // mounted under it: the card is covered, not gone, and still counts as hittable.
        let queued = app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue == %@", "补充说明（会排队发送）")).firstMatch
        XCTAssertTrue(queued.waitForExistence(timeout: 10), "Open did not go to the card's chat")
        try shot(app, "opened")
    }

    /// In regular width (an iPad) every row shows, as on the web: nothing folds.
    @MainActor func testRegularWidthShowsEveryRow() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB)")
        }
        guard UIDevice.current.userInterfaceIdiom == .pad else { throw XCTSkip("an iPhone folds the rows (the test above)") }
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant", "-openAssistant", main]
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let allow = springboard.buttons.matching(NSPredicate(format: "label IN %@", ["Allow", "允许"])).firstMatch
        if allow.waitForExistence(timeout: 3) { allow.tap() }

        let card = app.descendants(matching: .any)["brief.card"].firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 30), "no brief card:\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["brief.past"].exists, "the past section folds in regular width")
        XCTAssertTrue(app.buttons["brief.done.todo_task_blog"].exists)
        XCTAssertTrue(app.staticTexts["还有 5 项"].exists)
        try shot(app, "ipad")
    }
}

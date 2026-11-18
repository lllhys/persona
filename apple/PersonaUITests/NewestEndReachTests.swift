import XCTest

/// Persona's main conversation, with the capsule row over the composer and a long newest answer (#7382, #1942): opened
/// again at a place far up in its history (`ChatView.landEntry`) and scrolled back to the newest end by finger, the
/// newest line is laid out where the list starts, so the answer's last line rests above the capsules and a drag released
/// there does not spring back to a place that hides it. Before the fix SwiftUI kept the newest rows where its estimates
/// had put them (`ChatView.mendMisplacedStart`): thousands of points off, the answer's end out of reach.
///   PORT=18841 MOCK_LONG_ASSISTANT=1 MOCK_TASKS=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18841 xcodebuild test … -only-testing:PersonaUITests/NewestEndReachTests
final class NewestEndReachTests: XCTestCase {
    private var hub = ""
    private let persona = "server/b2e4d6f8-0000-4000-8000-000000000001"
    /// The answer's last line.
    private let marker = "结尾标记"
    /// From the newest message to the capsules at rest (`ChatView.newestGap`).
    private let endGap: CGFloat = 48
    /// Where the newest line starts in the list's content: the rows' spacing past the end sentinel (`Theme.Space.l`).
    private let newestTop = 16

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_LONG_ASSISTANT=1 MOCK_TASKS=1 (PERSONA_MOCK_HUB)")
        }
        hub = h
        _ = try? Data(contentsOf: URL(string: hub + "/mock/reset")!)
        _ = try? Data(contentsOf: URL(string: hub + "/mock/paging?on=1")!) // older pages load as the list goes up, as on a hub
    }

    override func tearDownWithError() throws {
        _ = try? Data(contentsOf: URL(string: hub + "/mock/paging?on=0")!)
        _ = try? Data(contentsOf: URL(string: hub + "/mock/reset")!)
    }

    /// About two screens of made-up Markdown, bold subheadings and lists, long enough to be drawn as several rows
    /// (`AnswerParts.minimumBytes`), and a last line to find.
    private var longAnswer: String {
        var parts: [String] = []
        for n in 1...10 {
            parts.append("**第 \(n) 部分：示例小标题**")
            parts.append((1...4).map { "- 第 \(n).\($0) 条：这是一段虚构的说明文字，用来把回复撑到大约两屏高，内容本身没有意义。" }.joined(separator: "\n"))
            parts.append("这一部分的小结：示例文字再写一行，让段落之间的高度和真实回复差不多，方便看清滚动的位置。")
        }
        parts.append("\(marker)：以上就是全部内容。")
        return parts.joined(separator: "\n\n")
    }

    @MainActor func testTheNewestAnswerEndIsReachableAfterOpeningFarUp() throws {
        var app = launch(fresh: true)
        XCTAssertTrue(app.otherElements["action-bar"].firstMatch.waitForExistence(timeout: 25), "no action bar:\n\(app.debugDescription)")
        _ = waitRows(app, 15) { $0["newest"] as? Bool == true && $0["dragging"] as? Bool == false }
        var c = URLComponents(string: hub + "/mock/say")!
        c.queryItems = [URLQueryItem(name: "user", value: "帮我整理一下这周的示例事项"), URLQueryItem(name: "text", value: longAnswer)]
        _ = try Data(contentsOf: c.url!)
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", marker)).firstMatch.waitForExistence(timeout: 20),
                      "the answer did not come")
        _ = waitRows(app, 10, atEnd)
        try checkEnd(app, "arrived")

        // Far up into the history (older pages load), then the app is left there and opened again: it lands there.
        for _ in 0..<8 { app.swipeDown(velocity: .fast) }
        _ = waitRows(app, 10, resting)
        XCUIDevice.shared.press(.home)
        XCTAssertTrue(app.wait(for: .runningBackground, timeout: 10), "the app did not go to the background")
        app.terminate()
        app = launch(fresh: false)
        XCTAssertTrue(app.otherElements["action-bar"].firstMatch.waitForExistence(timeout: 25), "no action bar after opening again")
        let landed = waitRows(app, 15) { $0["newest"] as? Bool == false && $0["dragging"] as? Bool == false }
        XCTAssertEqual(landed["newest"] as? Bool, false, "opened again at the newest end, not at the place far up: \(landed)")
        shot(app, "landed")

        // Back to the newest end by finger only (no ↓), a flick at a time, each glide left to end, until the answer's last
        // line is on screen or the list says it is at its end.
        let end = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", marker)).firstMatch
        let window = app.windows.firstMatch.frame
        for _ in 0..<40 {
            if rows(app)["newest"] as? Bool == true || (end.exists && end.frame.minY > window.minY && end.frame.maxY < window.maxY) { break }
            app.swipeUp(velocity: .fast)
            _ = waitRows(app, 5, resting)
        }
        // At rest there, a list whose newest end was placed by estimate is built again (`ChatView.mendMisplacedStart`).
        _ = waitRows(app, 10, atEnd)
        try checkEnd(app, "flicked back")
    }

    /// At rest at the newest end: the newest line laid out where the list starts, the list at its end, the answer's last
    /// line above the capsules by the room the end has; and after a drag up past the end, released, the same.
    @MainActor private func checkEnd(_ app: XCUIApplication, _ name: String) throws {
        let r = rows(app)
        shot(app, name)
        XCTAssertEqual(r["newestY"] as? Int, newestTop, "\(name): the newest line is not where the list starts: \(r)")
        try answerEndClear(app, name)
        let list = try XCTUnwrap(app.scrollViews.allElementsBoundByIndex.max { $0.frame.height < $1.frame.height })
        let start = list.coordinate(withNormalizedOffset: CGVector(dx: 0.4, dy: 0.7))
        start.press(forDuration: 0.05, thenDragTo: start.withOffset(CGVector(dx: 0, dy: -300)), withVelocity: .slow, thenHoldForDuration: 0.2)
        let released = waitRows(app, 6, atEnd)
        shot(app, "\(name)-dragged")
        XCTAssertEqual(released["listGap"] as? Int, 0, "\(name): released, the list is not at its newest end: \(released)")
        try answerEndClear(app, "\(name), dragged")
    }

    /// No finger on the list and no glide.
    private func resting(_ r: [String: Any]) -> Bool { r["dragging"] as? Bool == false }

    /// At rest at the newest end, its newest line where the list starts.
    private func atEnd(_ r: [String: Any]) -> Bool {
        resting(r) && r["listGap"] as? Int == 0 && r["newestY"] as? Int == newestTop
    }

    /// The scroll probe once `ok` holds, polled until `timeout`; the last reading either way (the caller asserts).
    @MainActor private func waitRows(_ app: XCUIApplication, _ timeout: TimeInterval, _ ok: ([String: Any]) -> Bool) -> [String: Any] {
        let deadline = Date().addingTimeInterval(timeout)
        var r = rows(app)
        while !ok(r), Date() < deadline {
            RunLoop.current.run(until: Date().addingTimeInterval(0.3))
            r = rows(app)
        }
        return r
    }

    @MainActor private func answerEndClear(_ app: XCUIApplication, _ name: String) throws {
        let bar = app.otherElements["action-bar"].firstMatch.frame.minY
        let end = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", marker)).firstMatch
        XCTAssertTrue(end.exists && !end.frame.isEmpty, "\(name): the answer's last line is not drawn: \(rows(app))")
        XCTAssertGreaterThanOrEqual(bar - end.frame.maxY, endGap - 1,
                                    "\(name): the answer's last line ends \(Int(end.frame.maxY)), the capsules start \(Int(bar)): \(rows(app))")
    }

    @MainActor private func launch(fresh: Bool) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-dev.openChat", "", "-openAssistant", persona,
                               "-probes", "YES", "-ui.scheme", "light"] + (fresh ? ["-openChatsFresh", "YES"] : [])
        app.launchPinned()
        return app
    }

    @MainActor private func rows(_ app: XCUIApplication) -> [String: Any] {
        app.probeEnvelope("chat-rows", page: "assistant")?["v"] as? [String: Any] ?? [:]
    }

    /// An attachment, and a file in PERSONA_SHOTS when set.
    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let file = "reach-" + name.replacingOccurrences(of: " ", with: "-")
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = file
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/\(file).png"))
        }
    }
}

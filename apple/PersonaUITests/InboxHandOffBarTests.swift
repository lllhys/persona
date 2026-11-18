import XCTest

/// The inbox event page's Hand to AI bar (#2055): a chip hands the item over in one tap behind a toast whose Undo sends
/// nothing; left alone it goes to the main chat once. The target capsule is a menu, its choice is remembered across
/// launches, and the bar rides up with the keyboard, the content above it. Against scripts/mock/mock_hub.py with
/// MOCK_INBOX_EVENTS=1 (`TEST_RUNNER_PERSONA_MOCK_HUB`); the New Agent start case also uses MOCK_SEND_DELAY_MS=6000 to
/// distinguish the local bubble from hub delivery. `TEST_RUNNER_PERSONA_SHOTS` saves the screenshots,
/// `TEST_RUNNER_PERSONA_SHOTS_APPEARANCE` only names them.
final class InboxHandOffBarTests: XCTestCase {
    private static let item = "lark:oc_demo1:100"

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor func testAChipHandsOverInOneTapAndUndoSendsNothing() throws {
        let hub = try mockHub()
        let app = open(hub)
        let chip = app.buttons["handoff-chip-summarize"]
        XCTAssertTrue(chip.waitForExistence(timeout: 20), "no chips:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["handoff-target"].exists, "no target capsule")
        XCTAssertTrue(app.textFields["handoff-field"].exists, "no field")
        XCTAssertFalse(app.buttons["交给 AI"].exists, "the old Hand to AI button is still on the page")
        shot(app, "bar")
        let before = handedOff(hub)

        chip.tap()
        let undo = app.buttons["toast-secondary"]
        XCTAssertTrue(undo.waitForExistence(timeout: 3), "no toast with Undo:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["toast-action"].exists, "no View on the toast")
        shot(app, "chip-sent")
        undo.tap()
        XCTAssertTrue(chip.waitForExistence(timeout: 3), "the chips did not come back after Undo")
        sleep(6)
        XCTAssertEqual(handedOff(hub), before, "Undo still handed the item over")

        chip.tap()
        XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label BEGINSWITH %@", "已交给")).firstMatch
            .waitForExistence(timeout: 15), "the hand-off never went:\n\(app.debugDescription)")
        shot(app, "chip-handed")
        XCTAssertEqual(handedOff(hub), before + 1, "the chip did not hand the item to the main chat once")
    }

    @MainActor func testTheTargetIsAMenuAndIsRemembered() throws {
        let hub = try mockHub()
        var app = open(hub)
        let target = app.buttons["handoff-target"]
        XCTAssertTrue(target.waitForExistence(timeout: 20), "no target capsule:\n\(app.debugDescription)")
        if target.value as? String != "主对话" {
            target.tap()
            app.buttons["主对话"].firstMatch.tap()
        }
        target.tap()
        let newChat = app.buttons["新对话"].firstMatch
        XCTAssertTrue(newChat.waitForExistence(timeout: 3), "no menu:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["主对话"].firstMatch.exists, "the menu has no main chat")
        sleep(1)
        shot(app, "target-menu")
        newChat.tap()
        XCTAssertEqual(target.value as? String, "新对话")

        app.terminate()
        app = open(hub)
        let again = app.buttons["handoff-target"]
        XCTAssertTrue(again.waitForExistence(timeout: 20))
        XCTAssertEqual(again.value as? String, "新对话", "the target was not remembered")
        again.tap()
        app.buttons["主对话"].firstMatch.tap()
        XCTAssertEqual(again.value as? String, "主对话")
    }

    /// Per source (#2121; needs MOCK_EVENT_MUTES=1 too): an item handed to New Chat makes New Chat the next pick for
    /// items from the same sender, marked Last time, even after the bar's own last target went back to the main chat
    /// on another source's item.
    @MainActor func testASourceRemembersItsTarget() throws {
        let hub = try mockHub()
        var app = open(hub, item: "mail:a1:ci:77")
        var target = app.buttons["handoff-target"]
        XCTAssertTrue(target.waitForExistence(timeout: 20), "no target capsule:\n\(app.debugDescription)")
        target.tap()
        app.buttons["新对话"].firstMatch.tap()
        app.buttons["handoff-chip-summarize"].tap()
        XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "已交给新对话")).firstMatch
            .waitForExistence(timeout: 15), "the hand-off did not go:\n\(app.debugDescription)")
        // The sent toast (with Undo) fades into the handed one: let it finish, or the shot catches both.
        let undoGone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: app.buttons["toast-secondary"])
        _ = XCTWaiter.wait(for: [undoGone], timeout: 5)
        sleep(1) // the fade itself
        shot(app, "source-handed")

        app.terminate()
        app = open(hub, item: "lark:oc_demo2:200") // another source: nothing remembered, the bar's own default
        target = app.buttons["handoff-target"]
        XCTAssertTrue(target.waitForExistence(timeout: 20))
        XCTAssertFalse((target.value as? String ?? "").contains("上次"), "another source's target came along")
        target.tap()
        app.buttons["主对话"].firstMatch.tap()

        app.terminate()
        app = open(hub, item: "mail:a1:notice:110") // the same sender
        target = app.buttons["handoff-target"]
        XCTAssertTrue(target.waitForExistence(timeout: 20))
        let value = target.value as? String ?? ""
        XCTAssertTrue(value.contains("新对话") && value.contains("上次"), "the sender's last target was not preselected: \(value)")
        shot(app, "source-last-time")
        target.tap()
        sleep(1)
        shot(app, "source-menu")
    }

    /// New Agent… (#5933): first of the menu's Agents, it opens New Agent with the item over the first message and the
    /// typed ask in it; nothing goes before Start, and Cancel comes back to the bar with the ask still in the field.
    @MainActor func testNewAgentOpensThePrefilledPageAndCancelKeepsTheAsk() throws {
        let hub = try mockHub()
        let app = open(hub)
        let field = app.textFields["handoff-field"]
        XCTAssertTrue(field.waitForExistence(timeout: 20), "no field:\n\(app.debugDescription)")
        let before = handedOff(hub)
        field.tap()
        field.typeText("先看看这个人之前说过什么")
        let target = app.buttons["handoff-target"]
        target.tap()
        let newAgent = app.buttons["新建 Agent…"].firstMatch
        XCTAssertTrue(newAgent.waitForExistence(timeout: 3), "no New Agent… in the menu:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["主对话"].firstMatch.exists, "the menu lost Main Chat")
        sleep(1)
        shot(app, "new-agent-menu")
        newAgent.tap()

        XCTAssertTrue(app.buttons["取消"].firstMatch.waitForExistence(timeout: 10), "New Agent did not open:\n\(app.debugDescription)")
        // The card and First Message sit below the computer, folder and model rows.
        let card = app.descendants(matching: .any)["newChat.forward"]
        for _ in 0..<6 where !card.exists { app.swipeUp() }
        XCTAssertTrue(card.waitForExistence(timeout: 5), "no item card on New Agent:\n\(app.debugDescription)")
        let prompt = app.descendants(matching: .any).matching(identifier: "newChat.prompt").firstMatch
        XCTAssertTrue((prompt.value as? String)?.contains("先看看这个人之前说过什么") == true, "the ask is not the first message: \(String(describing: prompt.value))")
        sleep(1)
        shot(app, "new-agent-page")
        app.buttons["取消"].firstMatch.tap()

        XCTAssertTrue(field.waitForExistence(timeout: 5), "not back on the item")
        XCTAssertEqual(field.value as? String, "先看看这个人之前说过什么", "Cancel lost the ask")
        XCTAssertEqual(target.value as? String, "主对话", "the target moved off Main Chat")
        XCTAssertEqual(handedOff(hub), before, "something was handed over before Start")
    }

    /// An ordinary New Agent start still opens its chat on the local first message; recording waits for the delayed
    /// hub delivery, which this case observes through the mock's send log without inspecting the record request.
    @MainActor func testNewAgentStartOpensOnFirstMessageBeforeHubDelivery() throws {
        let hub = try mockHub()
        let app = open(hub, more: ["-openChatsFresh", "YES", "-newChat.machine", "macbook",
                                  "-cwd.macbook", "/Users/me/code/weather", "-launcher.macbook", ""])
        let ask = "New Agent hand-off \(UUID().uuidString)"
        let startsBefore = mockEntries(hub, "started", key: "started").count
        let field = app.textFields["handoff-field"]
        XCTAssertTrue(field.waitForExistence(timeout: 20), "no field:\n\(app.debugDescription)")
        field.tap()
        field.typeText(ask)
        app.buttons["handoff-target"].tap()
        let newAgent = app.buttons["新建 Agent…"].firstMatch
        XCTAssertTrue(newAgent.waitForExistence(timeout: 3), "no New Agent… in the menu:\n\(app.debugDescription)")
        newAgent.tap()

        let start = app.buttons["开始对话"].firstMatch
        XCTAssertTrue(start.waitForExistence(timeout: 15), "New Agent did not open:\n\(app.debugDescription)")
        for _ in 0..<40 where !start.isEnabled { usleep(250_000) }
        XCTAssertTrue(start.isEnabled, "New Agent is not ready:\n\(app.debugDescription)")
        start.tap()

        let bubble = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", ask)).firstMatch
        XCTAssertTrue(bubble.waitForExistence(timeout: 4), "the chat did not open on the local first message:\n\(app.debugDescription)")
        XCTAssertTrue(bubble.isHittable, "the first message is still behind New Agent")
        XCTAssertTrue(app.buttons["chat.title"].exists, "the new Agent chat is not open")
        shot(app, "new-agent-local-first-message")

        let started = mockEntries(hub, "started", key: "started")
        XCTAssertEqual(started.count, startsBefore + 1, "New Agent did not start once")
        let body = try XCTUnwrap(started.last)
        XCTAssertEqual(body["async"] as? Bool, true, "the ordinary start must answer before the Agent is ready")
        XCTAssertNil(body["prompt"], "the ordinary start must send its first message separately")
        XCTAssertFalse(delivered(hub, ask: ask), "the local bubble waited for hub delivery, or the mock lacks MOCK_SEND_DELAY_MS=6000")

        let deadline = Date().addingTimeInterval(12)
        while !delivered(hub, ask: ask), Date() < deadline { usleep(200_000) }
        XCTAssertTrue(delivered(hub, ask: ask), "the first message never reached the hub")
    }

    @MainActor func testTheBarRidesUpWithTheKeyboard() throws {
        let hub = try mockHub()
        let app = open(hub)
        let field = app.textFields["handoff-field"]
        XCTAssertTrue(field.waitForExistence(timeout: 20), "no field:\n\(app.debugDescription)")
        let down = field.frame.maxY
        field.tap()
        let keyboard = app.keyboards.firstMatch
        XCTAssertTrue(keyboard.waitForExistence(timeout: 5), "no software keyboard")
        field.typeText("先看看这个人之前说过什么")
        sleep(1)
        XCTAssertLessThanOrEqual(field.frame.maxY, keyboard.frame.minY + 1, "the keyboard covers the field")
        XCTAssertLessThan(field.frame.maxY, down, "the bar did not move up")
        XCTAssertLessThanOrEqual(app.buttons["handoff-chip-summarize"].frame.maxY, field.frame.minY, "the chips are not above the field")
        shot(app, "keyboard")
    }

    // MARK: -

    private func mockHub() throws -> String {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_INBOX_EVENTS=1 (PERSONA_MOCK_HUB)")
        }
        return hub
    }

    @MainActor private func open(_ hub: String, item: String = InboxHandOffBarTests.item, more: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-inbox.kind", "", "-inbox.source", "",
                               "-openInboxItem", item] + more
        app.launchPinned()
        acceptIconAlert()
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let png = app.screenshot().pngRepresentation
        let a = XCTAttachment(data: png, uniformTypeIdentifier: "public.png")
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        let env = ProcessInfo.processInfo.environment
        if let dir = env["PERSONA_SHOTS"] {
            let look = env["PERSONA_SHOTS_APPEARANCE"].map { "-" + $0 } ?? ""
            try? png.write(to: URL(fileURLWithPath: dir).appending(path: "handoff-\(name)-\(UITestStyle.name)\(look).png"))
        }
    }

    /// Whether the mock accepted this case's unique first message (`/mock/sent_to`).
    private func delivered(_ hub: String, ask: String) -> Bool {
        mockEntries(hub, "sent_to", key: "sent").contains { ($0["text"] as? String)?.contains(ask) == true }
    }

    private func mockEntries(_ hub: String, _ endpoint: String, key: String) -> [[String: Any]] {
        guard let url = URL(string: hub + "/mock/" + endpoint) else { return [] }
        var answer: [[String: Any]] = []
        let done = expectation(description: endpoint)
        URLSession.shared.dataTask(with: url) { data, _, _ in
            if let data, let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                answer = obj[key] as? [[String: Any]] ?? []
            }
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 10)
        return answer
    }

    /// How many hand-offs reached the mock's main chat (`/mock/sent_to`, the ones with an inbox block).
    private func handedOff(_ hub: String) -> Int {
        guard let url = URL(string: hub + "/mock/sent_to") else { return 0 }
        var count = 0
        let done = expectation(description: "sent_to")
        URLSession.shared.dataTask(with: url) { data, _, _ in
            if let data, let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               let sent = obj["sent"] as? [[String: Any]] {
                count = sent.filter { ($0["text"] as? String)?.contains("收件箱事件") == true }.count
            }
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 10)
        return count
    }
}

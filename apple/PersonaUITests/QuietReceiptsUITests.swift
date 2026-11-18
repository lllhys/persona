import XCTest

/// A task's card comes once in the assistant's chat, and how it goes on is its row (#8237, docs/assistant/unified-entry.md
/// "Receipts"). Against scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_FINISHED=1 MOCK_TASK_CHECK=1 MOCK_TASKS_CUT=1
/// MOCK_MANAGERS=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB; one manager, so the MacBook's agents are this page's, and no
/// task's question card over the chat): the main chat draws no
/// receipt, the settings task's hand-off card says why it did not start and the release task's that it was cut off;
/// In Progress lists the two finished tasks under Just Finished, uncounted; Needs You's rows carry Continue (the release
/// task, cut off) and Retry and Remove (the settings task). Each look in PERSONA_LOOKS (`light:L,dark:L,light:XXXL,dark:XXXL`
/// by default) is checked, and PERSONA_SHOTS=<dir> keeps each screen as `<screen>-<appearance>-<size>.png`.
final class QuietReceiptsUITests: XCTestCase {
    private var app: XCUIApplication!
    private var hub = ""

    private static let sizes = ["L": "UICTContentSizeCategoryL", "XXXL": "UICTContentSizeCategoryXXXL"]

    override func setUpWithError() throws {
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_FINISHED=1 MOCK_TASK_CHECK=1 MOCK_TASKS_CUT=1 MOCK_MANAGERS=1 MOCK_TASKS_NO_ASKS=1 (PERSONA_MOCK_HUB)")
        }
        hub = h
        continueAfterFailure = true
    }

    override func tearDown() {
        app?.terminate()
        XCUIDevice.shared.appearance = .light
        super.tearDown()
    }

    @MainActor func testOneCardPerTaskAndTheRowsSayTheRest() {
        let looks = ProcessInfo.processInfo.environment["PERSONA_LOOKS"] ?? "light:L,dark:L,light:XXXL,dark:XXXL"
        for look in looks.split(separator: ",") {
            let parts = look.split(separator: ":").map(String.init)
            let (appearance, size) = (parts[0], parts[1])
            chat(appearance, size)
            inProgress(appearance, size)
            needsYou(appearance, size)
        }
    }

    /// The main chat, up from its bottom to the hand-off cards: the settings task's says why it did not start, the
    /// release task's that it was cut off, and no receipt is drawn near them (the settings task's failed one, the release
    /// task's interrupted one and the finished tasks' are in the feed).
    @MainActor private func chat(_ appearance: String, _ size: String) {
        launch(appearance, size, [])
        XCTAssertTrue(app.descendants(matching: .any)["shell-composer-field"].firstMatch.waitForExistence(timeout: 60),
                      "\(appearance) \(size): the main chat")
        sleep(2) // the feed's items and the chat's last rows settle
        for (id, name) in [("dispatch.failure", "chat-failed"), ("dispatch.interrupted", "chat-interrupted")] {
            let card = app.descendants(matching: .any)[id].firstMatch
            for _ in 0..<14 where !(card.exists && card.isHittable) { app.swipeDown(velocity: .slow); sleep(1) }
            XCTAssertTrue(card.exists && card.isHittable, "\(appearance) \(size): \(id) on its hand-off card:\n\(app.debugDescription)")
            if id == "dispatch.failure" {
                XCTAssertTrue(card.label.contains("和这台电脑的连接断了"), "\(appearance) \(size): the card's reason in plain words: \(card.label)")
            }
            // clear of the top bar, which the chat runs under: a little further down, held so it stops there
            let window = app.windows.firstMatch.frame
            for _ in 0..<3 where card.exists && card.frame.minY < window.minY + window.height * 0.3 {
                let from = app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.45))
                from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: 0, dy: window.height * 0.2)),
                           withVelocity: .slow, thenHoldForDuration: 0.3)
            }
            for receipt in ["补了两条测试", "发布说明分好了三组", "模型服务出错，中断了", "rpc: connection closed"] {
                XCTAssertFalse(labelled(receipt).exists, "\(appearance) \(size): a receipt is drawn in the chat: \(receipt)")
            }
            shot("\(name)-\(appearance)-\(size)")
        }
    }

    /// In Progress: the open tasks, then Just Finished with the two finished ones, which the entry does not count.
    @MainActor private func inProgress(_ appearance: String, _ size: String) {
        launch(appearance, size, ["-showActionTray", "running"])
        let group = app.descendants(matching: .any)["action-tray.just-finished"].firstMatch
        XCTAssertTrue(group.waitForExistence(timeout: 60), "\(appearance) \(size): Just Finished under In Progress:\n\(app.debugDescription)")
        sleep(2)
        shot("in-progress-\(appearance)-\(size)")
        let finished = app.buttons.matching(identifier: "action-tray.finished")
        reveal(finished.element(boundBy: max(0, finished.count - 1)), in: "action-tray.running")
        for (title, line) in [("修复登录页闪退", "改好了：登录页不再闪退，PR #43 已合并"), ("整理本周发布说明", "写好了：7 个 PR 分成新功能、修复和其他三组")] {
            let row = finished.matching(NSPredicate(format: "label CONTAINS %@", title)).firstMatch
            XCTAssertTrue(row.exists, "\(appearance) \(size): \(title) under Just Finished")
            XCTAssertTrue(row.label.contains(line), "\(appearance) \(size): \(title) says what it did: \(row.label)")
        }
        shot("in-progress-finished-\(appearance)-\(size)")
    }

    /// Needs You: the cut-off release task with Continue, the settings task that did not start with Retry and Remove.
    @MainActor private func needsYou(_ appearance: String, _ size: String) {
        launch(appearance, size, ["-showActionTray", "needs-you"])
        let resume = app.buttons["action-tray.continue"].firstMatch
        XCTAssertTrue(resume.waitForExistence(timeout: 60), "\(appearance) \(size): Continue on the interrupted row:\n\(app.debugDescription)")
        sleep(2)
        shot("needs-you-\(appearance)-\(size)")
        let retry = app.buttons["action-tray.retry"].firstMatch
        reveal(retry, in: "action-tray.needs-you")
        XCTAssertTrue(retry.exists, "\(appearance) \(size): Retry on the failed start's row")
        // why in plain words; the raw error ("rpc: connection closed") is only in the task's details
        XCTAssertTrue(labelled("和这台电脑的连接断了").exists, "\(appearance) \(size): the failed start's reason in plain words")
        XCTAssertFalse(labelled("rpc: connection closed").exists, "\(appearance) \(size): the raw error on the row")
        XCTAssertTrue(app.buttons["action-tray.remove"].firstMatch.exists, "\(appearance) \(size): Remove on the failed start's row")
        shot("needs-you-failed-\(appearance)-\(size)")
    }

    // MARK: -

    @MainActor private func launch(_ appearance: String, _ size: String, _ extra: [String]) {
        app?.terminate()
        XCUIDevice.shared.appearance = appearance == "dark" ? .dark : .light
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        // The default manager's Persona page: the mock's hub has more than one, and its tasks are the default's.
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-app.tab", "assistant", "-openAssistantManager", "assistant",
                               "-ui.scheme", appearance, "-AppleLanguages", "(zh-Hans)", "-AppleLocale", "zh_CN",
                               "-UIPreferredContentSizeCategoryName", Self.sizes[size] ?? Self.sizes["L"]!] + extra
        app.launchPinned()
        dismissIconNotice()
    }

    /// The app follows its icon to the look it was launched in, and the system says so in an alert over the screen
    /// (docs/operations/testing.md): tapped away, so the screenshots show the app.
    @MainActor private func dismissIconNotice() {
        let notice = NSPredicate(format: "label CONTAINS[c] 'icon' OR label CONTAINS '图标'")
        for _ in 0..<10 {
            for source in [app!, XCUIApplication(bundleIdentifier: "com.apple.springboard")] {
                let alert = source.alerts.containing(notice).firstMatch
                if alert.exists, alert.buttons.firstMatch.exists {
                    alert.buttons.firstMatch.tap()
                    return
                }
            }
            usleep(500_000)
        }
    }

    /// Up the tray's list until the element is on screen (rows past the tray's cap scroll inside it).
    @MainActor private func reveal(_ element: XCUIElement, in tray: String) {
        let list = app.descendants(matching: .any)[tray].firstMatch
        for _ in 0..<6 where !(element.exists && element.isHittable) && list.exists {
            let from = list.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.8))
            from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: 0, dy: -list.frame.height * 0.5)),
                       withVelocity: .slow, thenHoldForDuration: 0.3)
        }
    }

    @MainActor private func labelled(_ text: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    @MainActor private func shot(_ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }
}

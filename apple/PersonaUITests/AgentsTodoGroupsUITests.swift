import XCTest

/// By To-do's groups of Agents on no to-do (#6458): Started Manually and Unassigned start folded below the to-dos, say
/// what they hold (their header's spoken words, and behind an ⓘ a tap opens), and an open, empty group says it in its
/// place. Against the mock hub:
///   `PORT=<port> MOCK_MANUAL_HERDR=1 python3 scripts/mock/mock_hub.py` (TEST_RUNNER_PERSONA_TODO_GROUPS_HUB), and
///   `PORT=<port> python3 scripts/mock/mock_hub.py` (TEST_RUNNER_PERSONA_TODO_GROUPS_EMPTY_HUB: nothing opened by hand).
/// TEST_RUNNER_PERSONA_SHOTS=<dir> saves the screens (TEST_RUNNER_PERSONA_STYLE names them, e.g. light / dark;
/// TEST_RUNNER_PERSONA_SCHEME=light|dark draws in it). TEST_RUNNER_PERSONA_TODO_GROUPS_BEFORE=1 only takes the
/// screenshots, for a build from before this change.
final class AgentsTodoGroupsUITests: XCTestCase {
    private var app: XCUIApplication!
    private let env = ProcessInfo.processInfo.environment
    private var before: Bool { env["PERSONA_TODO_GROUPS_BEFORE"] == "1" }

    /// The groups' names in the language the tests run in (zh-Hans), and in English.
    private static let manual = ["手动开的", "Started Manually"]
    private static let unassigned = ["未归属", "Unassigned"]

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.name = "failure"
            shot.lifetime = .keepAlways
            add(shot)
            let tree = XCTAttachment(string: app.debugDescription)
            tree.name = "view tree"
            tree.lifetime = .keepAlways
            add(tree)
        }
        super.tearDown()
    }

    private func launch(_ hub: String) {
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "chats", "-dev.openChat", "", "-tab", "dev", "-dev.grouping", "todo",
                               "-openAgentsFresh", "YES"]
        if let scheme = env["PERSONA_SCHEME"] { app.launchArguments += ["-ui.scheme", scheme] }
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            for label in ["允许", "Allow", "好", "OK"] where alert.buttons[label].exists {
                alert.buttons[label].tap()
                return true
            }
            return false
        }
        app.launchPinned()
    }

    /// A group's header (FoldButton: its name, folded or not, its count and what it holds, as one element).
    private func header(_ names: [String]) -> XCUIElement {
        app.buttons.matching(identifier: "agents.section")
            .matching(NSPredicate(format: "label BEGINSWITH %@ OR label BEGINSWITH %@", names[0], names[1])).firstMatch
    }

    /// Scrolls the list until the group's header is on screen.
    @discardableResult
    private func reveal(_ names: [String]) -> XCUIElement {
        let h = header(names)
        for _ in 0..<10 where !(h.exists && h.isHittable) { app.swipeUp(velocity: .slow) }
        return h
    }

    private func save(_ name: String) {
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let style = env["PERSONA_STYLE"].map { "-" + $0 } ?? ""
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("ios-\(name)\(style).png"))
    }

    func testGroupsFoldAndSayWhatTheyHold() throws {
        guard let hub = env["PERSONA_TODO_GROUPS_HUB"] else { throw XCTSkip("needs the mock hub (PERSONA_TODO_GROUPS_HUB, MOCK_MANUAL_HERDR=1)") }
        launch(hub)
        let manual = reveal(Self.manual)
        XCTAssertTrue(manual.waitForExistence(timeout: 20), "Started Manually")
        if before {
            // before #6458 Started Manually opened by default: show Unassigned beside it too
            reveal(Self.unassigned)
            save("todo-groups")
            return
        }
        XCTAssertTrue(manual.label.contains("已折叠") || manual.label.contains("Collapsed"), "folded by default: \(manual.label)")
        XCTAssertTrue(manual.label.contains("终端") || manual.label.contains("terminal"), "says what it holds: \(manual.label)")
        XCTAssertFalse(manual.label.localizedCaseInsensitiveContains("herdr"), "no engine name: \(manual.label)")
        // listed while empty, folded below the to-dos too
        let unassigned = reveal(Self.unassigned)
        XCTAssertTrue(unassigned.exists, "Unassigned is listed")
        save("todo-groups")

        // the ⓘ's words, in a popover. The header speaks as one element (its words included), so the ⓘ is tapped where it
        // sits: just after the title, in zh-Hans at the default text size (a tap elsewhere on the header folds it).
        reveal(Self.manual)
        manual.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 94, dy: manual.frame.height / 2)).tap()
        let words = app.staticTexts.matching(identifier: "infoTip.text").firstMatch
        XCTAssertTrue(words.waitForExistence(timeout: 10), "the ⓘ's popover")
        XCTAssertTrue(words.label.contains("终端") || words.label.contains("terminal"))
        save("todo-groups-info")
    }

    func testAnEmptyGroupSaysWhatWouldBeThere() throws {
        guard let hub = env["PERSONA_TODO_GROUPS_EMPTY_HUB"] else { throw XCTSkip("needs the mock hub without MOCK_MANUAL_HERDR (PERSONA_TODO_GROUPS_EMPTY_HUB)") }
        launch(hub)
        if before {
            // before #6458 an empty group was not listed at all
            reveal(Self.unassigned)
            app.swipeUp(velocity: .slow)
            save("todo-groups-empty")
            return
        }
        let manual = reveal(Self.manual)
        XCTAssertTrue(manual.waitForExistence(timeout: 20), "Started Manually is listed while empty")
        manual.tap()
        let empty = app.staticTexts.matching(identifier: "agents.groupEmpty").firstMatch
        // the header may sit at the screen's foot: its row opens below it
        if !empty.waitForExistence(timeout: 2) { app.swipeUp(velocity: .slow) }
        XCTAssertTrue(empty.waitForExistence(timeout: 5), "an open, empty group says what it would hold")
        XCTAssertTrue(empty.label.contains("终端") || empty.label.contains("terminal"))
        save("todo-groups-empty")
    }
}

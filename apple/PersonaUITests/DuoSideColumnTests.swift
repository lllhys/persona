import XCTest

/// iPhone Duo with the system's bars down the side (#9837): everything in the side column stands apart. The tab bar, the
/// page's own buttons the system puts there (Back, an Agent chat's terminal / Git / Files / ⋯, Lark Settings, a search at
/// the foot) and the rail's face and action never lie over one another; where a page's buttons would reach the tab bar,
/// the trailing ones are only in ⋯, which stays. Checked on an Agent's chat, Messages, a to-do and the main chat, also at
/// the largest text size.
///
/// The posture is set before the run (scripts' display tool or Xcode's Device Hub, as `DuoTabSwitchTests`); skips where
/// the bars run across the top (the open Duo upright). Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB), the Separate
/// layout. TEST_RUNNER_PERSONA_HOLD=<seconds> holds each page, named in an activity `duo-column <page>`, for
/// `simctl io --display=<1 outer, 3 inner> screenshot`.
final class DuoSideColumnTests: XCTestCase {
    private static let agent = "macbook/a1f3c2d4-0000-4000-8000-000000000002"
    private static let tabItems: Set<String> = ["shell-assistant", "shell-agents", "shell-feishu"]
    /// The column: the side's safe-area inset on both displays.
    private static let column: CGFloat = 84

    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard UIDevice.current.userInterfaceIdiom == .phone else { throw XCTSkip("iPhone Duo only") }
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        app = XCUIApplication()
    }

    private func launch(_ extra: [String]) {
        let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] ?? ""
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-dev.openChat", "", "-openSplitFresh", "YES", "-integrations.lark.enabled", "YES"] + extra
        app.launchPinned()
    }

    @MainActor private func element(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    /// The page on screen, the tab bar down the side; skips where it is not.
    @MainActor private func open(_ extra: [String], until id: String? = nil, label: String? = nil) throws {
        launch(extra)
        let agents = element("shell-agents"), assistant = element("shell-assistant")
        XCTAssertTrue(app.windows.firstMatch.waitForExistence(timeout: 20))
        if let id { XCTAssertTrue(element(id).waitForExistence(timeout: 20), "\(id) is not on screen:\n\(app.debugDescription)") }
        if let label {
            let shown = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
            XCTAssertTrue(shown.waitForExistence(timeout: 20), "\(label) is not on screen:\n\(app.debugDescription)")
        }
        sleep(2) // the system's bar items come in with the page
        if app.windows.firstMatch.frame.width > 600, app.windows.firstMatch.frame.width < 900 {
            throw XCTSkip("the bars run across the top: the open Duo upright")
        }
        if assistant.exists, agents.exists, abs(assistant.frame.midX - agents.frame.midX) >= 2 {
            throw XCTSkip("the switch runs across the top here: not an iPhone Duo")
        }
    }

    @MainActor private func hold(_ name: String) {
        XCTContext.runActivity(named: "duo-column \(name)") { _ in
            if let hold = ProcessInfo.processInfo.environment["PERSONA_HOLD"].flatMap(UInt32.init) { sleep(hold) }
        }
    }

    /// The column's buttons as drawn: the tab bar (its items and all under them or its container) as its one capsule
    /// (their 56 pt and the capsule's padding), the system's 38 pt items as their 48 pt glass, the rail's buttons as they
    /// are; a button reported again inside itself (the system's button and its inner one) as one.
    @MainActor private func columnItems() throws -> [(name: String, frame: CGRect)] {
        let window = app.windows.firstMatch.frame
        let edge = window.maxX - Self.column
        var buttons: [(name: String, frame: CGRect, tab: Bool)] = []
        func walk(_ s: XCUIElementSnapshot, inTab: Bool) {
            let f = s.frame
            let tab = inTab || Self.tabItems.contains(s.identifier) || s.identifier == "tab-switch" || s.elementType == .tabBar
            if s.elementType == .button, f.width > 0, f.height > 0, f.minX >= edge - 1, f.maxX <= window.maxX + 1 {
                buttons.append((s.identifier.isEmpty ? s.label : s.identifier, f, tab))
            }
            for child in s.children { walk(child, inTab: tab) }
        }
        walk(try app.snapshot(), inTab: false)
        // The tab bar's items, and what has no name of its own within them (the shell's switch as one more button).
        let items = buttons.filter(\.tab).reduce(CGRect.null) { $0.union($1.frame) }
        var found: [(name: String, frame: CGRect)] = []
        for b in buttons where !b.tab && !(b.name.isEmpty && items.insetBy(dx: -1, dy: -1).contains(b.frame)) {
            if let i = found.firstIndex(where: { $0.frame.contains(b.frame) || b.frame.contains($0.frame) }) {
                found[i].frame = found[i].frame.union(b.frame)
                if found[i].name.isEmpty { found[i].name = b.name }
            } else {
                found.append((b.name, b.frame))
            }
        }
        var drawn = found.map { ($0.name, $0.frame.width < 44 ? $0.frame.insetBy(dx: -5, dy: -5) : $0.frame) }
        if !items.isNull { drawn.append(("tab bar", items.insetBy(dx: -4, dy: -8))) }
        return drawn
    }

    /// No two of the column's items lie over one another (touching is fine).
    @MainActor private func assertApart(_ page: String, file: StaticString = #filePath, line: UInt = #line) throws -> [String: CGRect] {
        let items = try columnItems()
        for (i, a) in items.enumerated() {
            for b in items[(i + 1)...] {
                let overlap = a.frame.intersection(b.frame)
                XCTAssertFalse(!overlap.isNull && overlap.width > 0.5 && overlap.height > 0.5,
                               "\(page): \(a.name) \(a.frame) lies over \(b.name) \(b.frame) in \(items)", file: file, line: line)
            }
        }
        return Dictionary(items.map { ($0.name, $0.frame) }, uniquingKeysWith: { $0.union($1) })
    }

    /// An Agent's chat: its buttons in the column stand clear of the tab bar, the ones without room only in ⋯, which stays.
    @MainActor func testAnAgentsChat() throws {
        for big in [false, true] {
            try open(["-openSession", Self.agent] + (big ? ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXL"] : []),
                     label: "补齐网络层的单元测试") // i18n-ignore: the mock hub's session title
            let items = try assertApart(big ? "Agent chat, largest text" : "Agent chat")
            let more = items.keys.first { $0 == "更多" || $0 == "More" } // i18n-ignore: the system's ⋯
            XCTAssertNotNil(more, "⋯ is not in the column: \(items.keys.sorted())")
            if items["tab bar"] != nil, let more {
                XCTAssertLessThan(items[more]!.maxY, items["tab bar"]!.minY, "⋯ is not over the tab bar")
            }
            hold(big ? "agent-chat-big" : "agent-chat")
            app.terminate()
        }
    }

    /// Messages: the face, the system's search at the foot and Lark Settings at the top stand apart from each other and
    /// from the tab bar.
    @MainActor func testMessages() throws {
        for big in [false, true] {
            try open(["-tab", "feishu", "-app.tab", "feishu"] + (big ? ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXL"] : []),
                     until: "feishu-conversations")
            let items = try assertApart(big ? "Messages, largest text" : "Messages")
            XCTAssertNotNil(items["tab bar"], "no tab bar in the column: \(items.keys.sorted())")
            hold(big ? "messages-big" : "messages")
            app.terminate()
        }
    }

    /// A to-do and the main chat: Back and ⋯, the face and New Chat, apart from the tab bar.
    @MainActor func testToDoAndMainChat() throws {
        try open(["-tab", "assistant", "-app.tab", "assistant", "-openShellPage", "todos", "-openTodoTitled", "NAS"], label: "NAS")
        _ = try assertApart("to-do")
        hold("todo")
        app.terminate()
        try open(["-tab", "assistant", "-app.tab", "assistant"], until: "shell-assistant")
        let items = try assertApart("main chat")
        XCTAssertNotNil(items["tab bar"], "no tab bar in the column: \(items.keys.sorted())")
        hold("assistant")
    }
}

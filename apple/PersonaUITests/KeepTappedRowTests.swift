import XCTest

/// Opening or closing something in a chat keeps the row tapped where it was on screen (#3757, docs/design/DESIGN_SYSTEM.md
/// 4.7.1): measured by the app itself, frame by frame for 3 s from the tap (`UnfoldTrace`), since XCUITest's frames of rows
/// in the flipped list are wrong. In the middle and at the oldest end the row stays within 2 pt; at the newest end,
/// following, a row opened rises to the top bar at most while the newest end stays on screen, and one closed takes the
/// list back to the newest end, smoothly both. Prints `KEEP <case> <what>: moved … end … step … gap … top … ys …` per tap.
/// On an iPhone and on an iPad alike (there the chat beside the sidebar: #8619).
///   Persona's main conversation: PORT=… MOCK_LONG_ASSISTANT=1 MOCK_FOLD_DELAY=2 python3 scripts/mock/mock_hub.py
///   (`PERSONA_FOLDS_HUB`, else `PERSONA_MOCK_HUB`): sixty finished turns, each one's work folded by the hub and its
///   steps fetched when it is opened, 2 s later: after a hold of the usual 1.5 s is over, which let the steps push the
///   header away (TestFlight 224); sooner, the usual hold covered them, and the test passed without the fix.
///   An Agent's chat at its newest end: PORT=… MOCK_LONG_HISTORY=1 MOCK_AGENTS=1 python3 scripts/mock/mock_hub.py
///   (`PERSONA_MOCK_HUB`), as `UnfoldTests`; in the middle and at its oldest end, where that one has no work line:
///   PORT=… MOCK_TOOL_TURN=1 python3 scripts/mock/mock_hub.py (`PERSONA_TOOL_TURN_HUB`, as `ToolFoldTests`), twelve
///   finished exchanges with a work line each before a running turn.
final class KeepTappedRowTests: XCTestCase {
    private var app: XCUIApplication!
    private var page = "assistant"
    private static let main = "server/b2e4d6f8-0000-4000-8000-000000000001"
    private static let widget = "macbook/a1f3c2d4-0000-4000-8000-000000000001" // long history, a work line in the last turn
    private static let toolTurn = "macbook/a1f3c2d4-0000-4000-8000-0000000000f5" // a work line in every turn
    /// Where the open part of the screen ends (window y): `overComposer` above the composer's field, set at launch.
    private var openBottom: CGFloat = 0
    /// The footer under the list (the composer, and over it an Agent's plan pill or Persona's capsules) is as tall on
    /// every device, so the open part ends this far above the composer's field, not at a share of the window: 0.72 of
    /// an iPad's taller window left out the newest work line, as far above the composer there as on a phone (#8619).
    /// 166 pt is where 0.72 of an iPhone 17 Pro's window ended.
    private static let overComposer: CGFloat = 166

    private struct Result: Decodable {
        var what: String
        var moved: Int
        var end: Int
        var step: Int?
        var ys: [Int?]
        var gap: Int?
        var top: Int?
        /// Where the row was at the tap (window y).
        var from: Int?
        /// Its top as UIKit draws it (`DrawnProbe`), beside `ys`, what SwiftUI reports.
        var drawn: [Int?]?

        /// Frame by frame, where it was drawn where the app can tell, else where SwiftUI reported it (`UnfoldTrace`).
        var path: [Int?] { (drawn ?? []).contains { $0 != nil } ? drawn ?? [] : ys }
        /// Where it was last seen (window y).
        var last: Int? { path.compactMap { $0 }.last }
        /// The furthest it went down from where it was.
        var down: Int { from.map { s in path.compactMap { $0 }.map { $0 - s }.max() ?? 0 } ?? 0 }
    }

    override func setUpWithError() throws {
        continueAfterFailure = true
    }

    private func launch(_ args: [String], page: String) {
        self.page = page
        app.launchArguments = ["-probes", "YES", "-unfoldTrace", "YES", "-scrollTrace", "YES", "-holdChromeOpen", "YES"] + args
        app.launchPinned()
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons
            .matching(NSPredicate(format: "label IN %@", ["Allow", "允许"])).firstMatch
        if allow.waitForExistence(timeout: 6) { allow.tap() }
        XCTAssertTrue(app.buttons["work-head"].firstMatch.waitForExistence(timeout: 30), "the conversation's work lines show")
        // The chat measures itself at rest 6 s after it shows (`UnfoldTrace.idle`): left alone until that is in.
        sleep(8)
        let field = app.descendants(matching: .any)["shell-composer-field"].firstMatch
        XCTAssertTrue(field.exists, "the composer shows")
        openBottom = field.frame.minY - Self.overComposer
    }

    private func pair(_ hub: String) -> String {
        "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
    }

    private func launchPersona() throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["PERSONA_FOLDS_HUB"] ?? env["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs MOCK_LONG_ASSISTANT=1 MOCK_FOLD_DELAY=2 (PERSONA_FOLDS_HUB)")
        }
        app = XCUIApplication()
        launch(["-pairURL", pair(hub), "-dev.openChat", "", "-app.tab", "assistant", "-tab", "assistant", "-openAssistant", Self.main],
               page: "assistant")
    }

    private func launchAgent() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs MOCK_LONG_HISTORY=1 MOCK_AGENTS=1 (PERSONA_MOCK_HUB)")
        }
        app = XCUIApplication()
        launch(["-pairURL", pair(hub), "-openChatsFresh", "YES", "-app.tab", "chats", "-tab", "dev", "-dev.openChat", Self.widget],
               page: "agents")
    }

    /// An Agent's chat with a work line in every turn (`MOCK_TOOL_TURN=1`), the turns before the running one finished.
    private func launchAgentTurns() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_TOOL_TURN_HUB"] else {
            throw XCTSkip("needs MOCK_TOOL_TURN=1 (PERSONA_TOOL_TURN_HUB)")
        }
        app = XCUIApplication()
        launch(["-pairURL", pair(hub), "-openChatsFresh", "YES", "-app.tab", "chats", "-tab", "dev", "-dev.openChat", Self.toolTurn],
               page: "agents")
    }

    private func seq() -> Int { app.probeSeq("unfold", page: page) ?? 0 }

    private func results() -> [Result] { app.probe("unfold", as: [Result].self, page: page) ?? [] }

    /// The work line headers in the open part of the screen, by where they are: the headers of the mocks' turns read
    /// alike, and a query bound by index finds another one once the list has changed. Hittable is not enough: one under
    /// the composer or the top bar counts as hittable, and the tap went to the composer. The open part: under the top bar,
    /// down to `openBottom`.
    private func heads() -> [XCUIElement] {
        let h = app.windows.firstMatch.frame.height
        return app.buttons.matching(identifier: "work-head").allElementsBoundByIndex
            .filter { $0.isHittable && $0.frame.midY > h * 0.15 && $0.frame.midY < openBottom }
    }

    private func point(_ e: XCUIElement) -> XCUICoordinate {
        let f = e.frame
        return app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: f.midX, dy: f.midY))
    }

    /// A header in the middle third of the screen, the list moved a little at a time until one is there (the mock's
    /// turns are long: a screen may hold one header, or none); the lowest one (the newest on screen); the highest one.
    private func middleHeader() -> XCUICoordinate? {
        let h = app.windows.firstMatch.frame.height
        for _ in 0..<12 {
            if let head = heads().first(where: { $0.frame.midY > h * 0.33 && $0.frame.midY < h * 0.62 }) { return point(head) }
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.4))
            from.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.55)))
            sleep(1)
        }
        return nil
    }
    private func lowestHeader() -> XCUICoordinate? { heads().max(by: { $0.frame.midY < $1.frame.midY }).map(point) }
    private func highestHeader() -> XCUICoordinate? { heads().min(by: { $0.frame.midY < $1.frame.midY }).map(point) }

    /// A work line header is still under `point`.
    private func headerAt(_ point: XCUICoordinate) -> Bool {
        let p = point.screenPoint
        return heads().contains { $0.frame.contains(p) }
    }

    /// Taps `point` and waits for the app's measurement of it, through what the tap opens arriving from the hub too.
    private func tap(_ label: String, _ point: XCUICoordinate) -> Result? {
        let before = seq()
        point.tap()
        // Nothing asked of the app while it measures: every query of its accessibility tree costs it frames.
        Thread.sleep(forTimeInterval: 3.6) // `UnfoldTrace.window`
        guard seq() != before, let r = results().last else { XCTFail("KEEP \(label): the tap measured nothing"); return nil }
        // In steps: Xcode 26.0 can't type-check it as one expression in time.
        let ys: String = r.ys.map { $0.map(String.init) ?? "-" }.joined(separator: " ")
        let drawn: String = (r.drawn ?? []).map { $0.map(String.init) ?? "-" }.joined(separator: " ")
        print("KEEP \(label) \(r.what): moved \(r.moved) end \(r.end) step \(r.step ?? -1) gap \(r.gap ?? -1) top \(r.top ?? -1) ys \(ys) drawn \(drawn)")
        return r
    }

    /// In the middle or at the oldest end: the tapped row stayed where it was, within 2 pt, the whole time.
    private func still(_ label: String, _ point: XCUICoordinate) {
        guard let r = tap(label, point) else { return }
        XCTAssertLessThanOrEqual(r.moved, 2, "\(label) \(r.what): the tapped row moved \(r.moved) pt")
        XCTAssertTrue(headerAt(point), "\(label) \(r.what): the tapped row is still under the finger")
        sleep(1)
    }

    /// At the newest end, following, opened: the row only went up, smoothly, and no higher than the top bar; it ended
    /// with the newest end still on screen, or kept just under the top bar (rule B).
    private func rises(_ label: String, _ point: XCUICoordinate) {
        guard let r = tap(label, point), let last = r.last, let top = r.top, let gap = r.gap else {
            return XCTFail("\(label): no measurement of where the row went")
        }
        XCTAssertLessThanOrEqual(r.down, 2, "\(label) \(r.what): the row went \(r.down) pt down")
        XCTAssertLessThanOrEqual(r.step ?? .max, 160, "\(label) \(r.what): the row jumped \(r.step ?? -1) pt in one frame")
        XCTAssertGreaterThanOrEqual(last, top - 3, "\(label) \(r.what): the row rose past the top bar (\(last) < \(top))")
        XCTAssertTrue(gap <= 2 || last <= top + 3, "\(label) \(r.what): the newest end went off (\(gap) pt) before the row reached the top bar (\(last), bar \(top))")
        sleep(1)
    }

    /// At the newest end, following, closed: the list came back to the newest end, smoothly.
    private func followsBack(_ label: String, _ point: XCUICoordinate) {
        guard let r = tap(label, point), let gap = r.gap else { return XCTFail("\(label): no measurement of the list") }
        XCTAssertLessThanOrEqual(gap, 2, "\(label) \(r.what): the list stayed \(gap) pt short of the newest end")
        XCTAssertLessThanOrEqual(r.step ?? .max, 160, "\(label) \(r.what): the row jumped \(r.step ?? -1) pt in one frame")
        sleep(1)
    }

    /// Back to the oldest end: swiped until what the list shows stops changing (each swipe may load an older page): where
    /// its work line headers are, and which messages it holds (`chat.user.…`, `chat.answer.…`), since a screen there may
    /// hold no header. Twice in a row unchanged: a page still loading leaves it unchanged once. By where things were
    /// alone, an iPad took a page's end for the oldest end (#8619): the mocks' turns are drawn alike, and there each swipe
    /// stopped at the end of what was loaded with the headers where the page before had left them, a point or two apart;
    /// the first texts on screen, read with them, were the top bar's, the composer's and the sidebar's, which never move.
    @MainActor private func toOldest() {
        var was: [String] = [], same = 0
        for _ in 0..<80 {
            app.swipeDown(velocity: .fast)
            sleep(1)
            let now = shown()
            same = !now.isEmpty && now == was ? same + 1 : 0
            if same >= 2 { return }
            was = now
        }
        XCTFail("the oldest end was not reached")
    }

    /// What the list shows, from one snapshot of the app: each work line header's top, and each message's id.
    @MainActor private func shown() -> [String] {
        guard let root = try? app.snapshot() else { return [] }
        var out: [String] = []
        func visit(_ s: XCUIElementSnapshot) {
            if s.identifier == "work-head" {
                out.append("head \(Int(s.frame.minY))")
            } else if s.identifier.hasPrefix("chat.user.") || s.identifier.hasPrefix("chat.answer.") {
                out.append(s.identifier)
            }
            s.children.forEach(visit)
        }
        visit(root)
        return out.sorted()
    }

    // MARK: Persona's main conversation

    /// Reading back: a turn's folded work opened, its steps fetched from the hub a moment later, then folded again. The
    /// list fell to another message when the steps came in (TestFlight 224).
    @MainActor func testPersonaInTheMiddle() throws {
        try launchPersona()
        // Back a few screens: the flipped list goes older downwards.
        for _ in 0..<3 { app.swipeDown() }
        sleep(3)
        guard let work = middleHeader() else { return XCTFail("no work line on screen after scrolling back") }
        still("persona middle", work)
        still("persona middle", work)
    }

    @MainActor func testPersonaAtTheNewestEnd() throws {
        try launchPersona()
        guard let work = lowestHeader() else { return XCTFail("no work line on screen at the newest end") }
        rises("persona newest", work)
        // Folded from where it rose to.
        guard let risen = lowestHeader() else { return XCTFail("the opened work line left the screen") }
        followsBack("persona newest", risen)
    }

    @MainActor func testPersonaAtTheOldestEnd() throws {
        try launchPersona()
        toOldest()
        sleep(2)
        guard let work = highestHeader() else { return XCTFail("no work line on screen at the oldest end") }
        still("persona oldest", work)
        still("persona oldest", work)
    }

    // MARK: An Agent's chat

    @MainActor func testAgentInTheMiddle() throws {
        try launchAgentTurns()
        for _ in 0..<2 { app.swipeDown() }
        sleep(3)
        guard let work = middleHeader() else { return XCTFail("no work line on screen after scrolling back") }
        still("agent middle", work)
        still("agent middle", work)
    }

    @MainActor func testAgentAtTheNewestEnd() throws {
        try launchAgent()
        guard let work = lowestHeader() else { return XCTFail("no work line on screen at the newest end") }
        rises("agent newest", work)
        guard let risen = lowestHeader() else { return XCTFail("the opened work line left the screen") }
        followsBack("agent newest", risen)
    }

    @MainActor func testAgentAtTheOldestEnd() throws {
        try launchAgentTurns()
        toOldest()
        sleep(2)
        guard let work = highestHeader() else { return XCTFail("no work line on screen at the oldest end") }
        still("agent oldest", work)
        still("agent oldest", work)
    }
}

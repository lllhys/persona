import XCTest

/// The sidebar's places and tasks (docs/design/sidebar-navigation.md, #6599): a place keeps its stack while the user goes
/// elsewhere from the sidebar, a task opens over the drawer and comes back to it, and the drawer's closing never runs
/// together with a push, a slide or a sheet. Demo Mode, the shell on an iPhone.
final class ShellPlacesUITests: XCTestCase {
    private var app: XCUIApplication!
    private var finger: ShellFinger!

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor private func launch() {
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-probes", "YES", "-newAssistant", "YES", "-tab", "assistant", "-dev.openChat", ""]
        // `PERSONA_SHOTS_TEXT=AX5`: the largest accessibility text size, for the screenshots.
        if ProcessInfo.processInfo.environment["PERSONA_SHOTS_TEXT"] == "AX5" {
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"]
        }
        app.launchPinned()
        finger = ShellFinger(app)
        see(button("assistant-sidebar-button"), "☰", timeout: 15)
        sleep(1)
    }

    @MainActor private func button(_ id: String) -> XCUIElement { app.buttons[id].firstMatch }
    @MainActor private var sidebar: XCUIElement { app.descendants(matching: .any)["assistant-sidebar"] }
    @MainActor private var chatComposer: XCUIElement { app.descendants(matching: .any)["shell-composer-field"].firstMatch }

    @MainActor @discardableResult private func see(_ e: XCUIElement, _ what: String, timeout: TimeInterval = 10) -> XCUIElement {
        let found = e.waitForExistence(timeout: timeout)
        // The iPad's tabs have no shell, so no probe to read.
        let trace = app.descendants(matching: .any)["probe.shell"].exists ? app.shellTrace : ""
        if !found {
            shot("missing \(what)")
            print("PLACES-TREE \(what)\n\(app.debugDescription)\n\(trace)")
        }
        XCTAssertTrue(found, "no \(what) — \(trace)")
        return e
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        // `PERSONA_SHOTS=<dir>` (TEST_RUNNER_PERSONA_SHOTS): the PR's screenshots.
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let env = ProcessInfo.processInfo.environment
            let look = [env["PERSONA_SHOTS_APPEARANCE"], env["PERSONA_SHOTS_TEXT"]].compactMap { $0 }.joined(separator: "-")
            let url = URL(fileURLWithPath: dir).appendingPathComponent("places-\(look.isEmpty ? "" : look + "-")\(name).png")
            try? app.screenshot().pngRepresentation.write(to: url)
        }
    }

    /// Opens the drawer from the current page's only leading button, its place root's ☰.
    @MainActor private func openDrawer(_ id: String) {
        sleep(1) // a push or a close settled
        see(button(id), "☰ (\(id))").tap()
        see(sidebar, "the drawer from \(id)", timeout: 5)
        XCTAssertTrue(waitFor { self.app.shellShown?.drawerOpen == true }, "the drawer did not open — \(app.shellTrace)")
        sleep(1)
    }

    @MainActor private func pick(_ row: String) {
        see(button(row), "\(row) in the drawer").tap()
        XCTAssertTrue(waitFor { self.app.shellShown?.drawerOpen == false }, "the drawer stayed open after \(row) — \(app.shellTrace)")
    }

    @MainActor private func waitFor(_ seconds: TimeInterval = 5, _ met: () -> Bool) -> Bool {
        let end = Date().addingTimeInterval(seconds)
        while Date() < end {
            if met() { return true }
            usleep(150_000)
        }
        return met()
    }

    /// The serial of the last trace event containing `text` (the trace's lines read "<serial> @<time> <event>").
    @MainActor private func serial(of text: String, in trace: [String]) -> Int? {
        trace.last { $0.contains(text) }.flatMap { Int($0.split(separator: " ").first ?? "") }
    }

    @MainActor private var machines: XCUIElement { app.navigationBars["机器"] }
    @MainActor private var knowledge: XCUIElement { app.navigationBars["知识库"] }

    /// Machines → a machine → Back: a place root has only ☰, a sub-page only Back. The place's path preservation
    /// stays covered by ShellPlacesTests and the iPad flow below; this phone flow checks the one-button navigation.
    @MainActor func testAPlaceKeepsItsStack() {
        launch()
        openDrawer("assistant-sidebar-button")
        pick("sidebar-machines")
        see(machines, "机器")
        assertPlaceRoot(machines, "Machines")
        shot("0-machines-root")
        let card = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'MacBook'")).firstMatch
        for _ in 0..<6 where !(card.exists && card.isHittable) {
            app.swipeUp(velocity: .slow)
        }
        see(card, "MacBook's card").tap()
        let detail = app.navigationBars["MacBook"]
        XCTAssertTrue(detail.waitForExistence(timeout: 5), "the machine's page did not push")
        XCTAssertTrue(machines.waitForNonExistence(timeout: 5), "the machine list is still in front")
        assertSubPage(detail, "Machine")
        shot("1-machine-page")

        detail.buttons["BackButton"].firstMatch.tap()
        XCTAssertTrue(detail.waitForNonExistence(timeout: 5), "Back did not settle on Machines")
        assertPlaceRoot(machines, "Machines after Back")
        shot("2-machines-root-after-back")

        // Re-enter from the root: only Back on the child again, then settle on the root before opening its drawer.
        see(card, "MacBook after Back").tap()
        XCTAssertTrue(detail.waitForExistence(timeout: 5), "the machine did not reopen from its root")
        assertSubPage(detail, "Machine reopened from root")
        detail.buttons["BackButton"].firstMatch.tap()
        XCTAssertTrue(detail.waitForNonExistence(timeout: 5), "the second Back did not settle on Machines")
        assertPlaceRoot(machines, "Machines after reopening and Back")

        // Back to the conversation is a drawer choice, not a second button beside ☰.
        openDrawer("shell-place-sidebar")
        pick("sidebar-main")
        see(chatComposer, "the conversation chosen from Machines' drawer")
    }

    @MainActor private func assertPlaceRoot(_ bar: XCUIElement, _ name: String) {
        XCTAssertTrue(button("shell-place-sidebar").waitForExistence(timeout: 5), "\(name): no ☰")
        XCTAssertFalse(bar.buttons["BackButton"].exists, "\(name): Back beside ☰")
        assertSides(bar, leading: 1, trailing: 0, name)
    }

    @MainActor private func assertSubPage(_ bar: XCUIElement, _ name: String) {
        XCTAssertTrue(bar.buttons["BackButton"].waitForExistence(timeout: 5), "\(name): no Back")
        XCTAssertFalse(button("shell-place-sidebar").exists, "\(name): ☰ beside Back")
        assertSides(bar, leading: 1, trailing: 0, name)
    }

    /// Count direct bar controls by their frames, not nested accessibility elements inside one menu button.
    @MainActor private func assertSides(_ bar: XCUIElement, leading: Int, trailing: Int, _ name: String) {
        let center = bar.frame.midX
        let left = bar.children(matching: .button).allElementsBoundByIndex.filter { $0.frame.midX < center }
        let right = bar.children(matching: .button).allElementsBoundByIndex.filter { $0.frame.midX > center }
        XCTAssertEqual(left.count, leading, "\(name): leading controls: \(bar.debugDescription)")
        XCTAssertLessThanOrEqual(right.count, max(trailing, 1), "\(name): more than one trailing control: \(bar.debugDescription)")
    }

    /// The iPad's sidebar column: the same places keep their stacks beside it (the column never closes).
    @MainActor func testAPlaceKeepsItsStackBesideTheIPadColumn() throws {
        guard UIDevice.current.userInterfaceIdiom == .pad else { throw XCTSkip("the iPad's sidebar column") }
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-newAssistant", "YES", "-tab", "assistant", "-dev.openChat", ""]
        app.launchPinned()
        see(button("sidebar-machines"), "机器 in the column", timeout: 15).tap()
        see(machines, "机器")
        let card = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'MacBook'")).firstMatch
        sleep(1)
        see(card, "MacBook's card").tap()
        XCTAssertTrue(machines.waitForNonExistence(timeout: 5), "the machine's page did not push")
        let title = "MacBook"
        see(app.navigationBars[title], "the machine's page")
        shot("ipad-1-machine-page")
        button("sidebar-知识库").tap()
        see(knowledge, "知识库")
        sleep(1)
        button("sidebar-machines").tap()
        see(app.navigationBars[title], "the machine's page again, kept while on 知识库")
        XCTAssertFalse(machines.exists, "Machines came back at its root")
        shot("ipad-2-machine-page-kept")
    }

    /// From Agents the page switches under the open drawer (no slide, no push while the drawer moves), then the drawer
    /// closes: the place is in the trace with the drawer still out, before its close.
    @MainActor func testAPlaceSwitchesBeforeTheDrawerCloses() {
        launch()
        finger.switchTo(agents: true, "Agents before the drawer")
        openDrawer("work-sidebar-button")
        pick("sidebar-machines")
        see(machines, "机器 from Agents")
        XCTAssertTrue(waitFor { self.app.shellShown?.strip == "assistant" }, "the strip is not on 助手 — \(app.shellTrace)")
        let trace = app.shellShown?.trace ?? []
        let place = serial(of: "place machines root drawer=true", in: trace)
        let close = serial(of: "drawer close", in: trace)
        XCTAssertNotNil(place, "the place did not switch under the open drawer — \(trace)")
        XCTAssertNotNil(close, "no close — \(trace)")
        if let place, let close { XCTAssertLessThan(place, close, "the drawer closed before the page switched — \(trace)") }
        // The strip snapped under the drawer: no animated slide after the tap.
        let tap = serial(of: "select 0", in: trace) ?? 0
        XCTAssertFalse(trace.contains { (Int($0.split(separator: " ").first ?? "") ?? 0) > tap && $0.contains("moving=true") },
                       "the strip slid while the drawer closed — \(trace)")
        shot("4-machines-from-agents")
    }

    /// Settings opens over the open drawer, and Done comes back to the drawer as it was.
    @MainActor func testSettingsReturnsToTheDrawer() {
        launch()
        openDrawer("assistant-sidebar-button")
        see(app.buttons.matching(NSPredicate(format: "label == '设置'")).firstMatch, "设置 at the drawer's foot").tap()
        let close = see(button("settings-close"), "Settings' close button")
        XCTAssertEqual(app.shellShown?.drawerOpen, true, "the drawer closed under Settings — \(app.shellTrace)")
        shot("5-settings-over-drawer")
        sleep(1)
        close.tap()
        XCTAssertTrue(close.waitForNonExistence(timeout: 5), "Settings did not close")
        see(button("sidebar-machines"), "the drawer's rows after Settings")
        XCTAssertEqual(app.shellShown?.drawerOpen, true, "the drawer is not open after Settings — \(app.shellTrace)")
        shot("6-drawer-after-settings")
    }

    /// A conversation picked in Archived opens once the sheet has gone: the drawer starts closing after it.
    @MainActor func testArchivedPickClosesAfterTheSheet() {
        launch()
        openDrawer("assistant-sidebar-button")
        let row = app.buttons.matching(identifier: "sidebar-conversation").matching(NSPredicate(format: "label BEGINSWITH %@", "帮我约周五")).firstMatch
        see(row, "帮我约周五 in the drawer").swipeLeft()
        see(app.buttons["归档"].firstMatch, "归档").tap()
        XCTAssertTrue(row.waitForNonExistence(timeout: 5), "not archived")
        see(button("sidebar-archived"), "已归档").tap()
        let archived = see(app.buttons.matching(identifier: "archived-conversation").firstMatch, "the archived conversation")
        sleep(1)
        archived.tap()
        XCTAssertTrue(waitFor { self.app.shellShown?.drawerOpen == false }, "the drawer stayed open — \(app.shellTrace)")
        let trace = app.shellShown?.trace ?? []
        let dismissed = serial(of: "task dismissed, open", in: trace)
        let close = serial(of: "drawer close", in: trace)
        XCTAssertNotNil(dismissed, "the pick did not wait for the sheet — \(trace)")
        if let dismissed, let close { XCTAssertLessThan(dismissed, close, "the drawer closed with the sheet still going — \(trace)") }
        see(chatComposer, "the picked conversation")
    }
}

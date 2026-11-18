import XCTest

/// The shell's Assistant | Agents switch after a page from the drawer (docs/apple/ios.md「New shell」): open Knowledge (or Inbox, Scheduled Tasks,
/// Machines), go back to the conversation (its row in the place's drawer, or the edge swipe: a place's root has ☰ and no Back,
/// docs/design/sidebar-navigation.md), switch to Agents: the Agents list must be what is drawn, not just the switch saying so (TestFlight 70:
/// the switch said Agents over the empty chat); switch back: Assistant again. Swiped back from Knowledge with its search open, the
/// root kept a system navigation bar that pushed the top bar ~106 pt down: a tap on the switch where it belongs hit
/// the empty bar (`RootBarHidden`).
final class ShellSwitchTests: XCTestCase {
    private var app: XCUIApplication!
    private var finger: ShellFinger!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard UIDevice.current.userInterfaceIdiom == .phone else { throw XCTSkip("the iPhone shell's switch: the iPad has its sidebar column and tabs") }
    }

    @MainActor @discardableResult private func see(_ e: XCUIElement, _ what: String, timeout: TimeInterval = 10) -> XCUIElement {
        let found = e.waitForExistence(timeout: timeout)
        if !found { attachScreen("missing \(what)") }
        XCTAssertTrue(found, "no \(what)")
        return e
    }

    @MainActor private func attachScreen(_ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
    }

    /// The shell on Assistant's empty chat: against scripts/mock/mock_hub.py when PERSONA_MOCK_HUB is set (connected, like a phone
    /// with its hub), else in Demo Mode.
    @MainActor private func launchShell() {
        app = XCUIApplication()
        if let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] {
            let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
            app.launchArguments = ["-pairURL", pair, "-demo.on", "NO"]
        } else {
            app.launchArguments = ["-demo.on", "YES"]
        }
        app.launchArguments += ["-probes", "YES", "-newAssistant", "YES", "-tab", "assistant"]
        app.launchPinned()
        finger = ShellFinger(app)
    }

    @MainActor private func button(_ id: String) -> XCUIElement { app.buttons[id].firstMatch }

    /// Waits until `e` is on screen and can be tapped (a page under the other one exists but is hidden).
    @MainActor private func reachable(_ e: XCUIElement, _ what: String, timeout: TimeInterval = 5) {
        let ok = NSPredicate(format: "exists == true AND hittable == true")
        let met = XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: ok, object: e)], timeout: timeout) == .completed
        if !met {
            attachScreen("not reachable: \(what)")
            print("SWITCHTREE-BEGIN \(what)\n\(app.debugDescription)\nSWITCHTREE-END")
        }
        XCTAssertTrue(met, "\(what) is not on screen")
    }

    /// Waits (the switch slides the pages) until Agents is drawn, or is not.
    @MainActor private func expectAgents(_ shown: Bool, _ what: String) {
        let now = app.waitAgentsDrawn(shown)
        if !now { attachScreen(what) }
        XCTAssertTrue(now, shown ? "Agents is not drawn: \(what)" : "Agents still drawn: \(what)")
    }

    /// Assistant's empty chat: its composer (New Chat is disabled there, and a disabled button is not hittable).
    @MainActor private var chatComposer: XCUIElement { app.descendants(matching: .any)["shell-composer-field"].firstMatch }

    /// The top bar is at the top of the screen (a system navigation bar left over the root pushed it ~106 pt down).
    @MainActor private func barAtTop(_ what: String) {
        let menu = button("assistant-sidebar-button")
        var met = menu.exists && menu.frame.minY < 100
        for _ in 0..<10 where !met {
            usleep(300_000)
            met = menu.exists && menu.frame.minY < 100
        }
        if !met {
            attachScreen("bar pushed down: \(what)")
            print("SWITCHTREE-BEGIN \(what)\n\(app.debugDescription)\nSWITCHTREE-END")
        }
        XCTAssertTrue(met, "助手's top bar is not at the top (\(menu.frame)) \(what)")
    }

    /// Agents, then Assistant again, tapped where a finger taps the switch, each one really in front in time (the strip,
    /// the switch and the tab agree).
    @MainActor private func switchBothWays(_ how: String) {
        finger.switchTo(agents: true, "switched to Agents after \(how)")
        reachable(button("agents-new"), "the Agents page (新建会话) after \(how)")
        finger.switchTo(agents: false, "switched back to 助手 after \(how)")
        reachable(chatComposer, "助手 again after \(how)")
    }

    /// Back from a place to the conversation: the place's ☰ and the conversation's row in the drawer (a place's root has
    /// no Back, #7799), or the edge swipe as a finger does it (held, slow; a quick flick is not taken for the edge
    /// gesture on iOS 27 simulators).
    @MainActor private func back(from page: XCUIElement, swipe: Bool) {
        for _ in 0..<3 where page.exists {
            sleep(1) // once the push has settled (a tap during its animation can be lost)
            if swipe {
                let w = app.windows.firstMatch
                w.coordinate(withNormalizedOffset: CGVector(dx: 0.005, dy: 0.5))
                    .press(forDuration: 0.1, thenDragTo: w.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)),
                           withVelocity: .slow, thenHoldForDuration: 0.1)
            } else {
                see(page.buttons["shell-place-sidebar"].firstMatch, "☰ on the place").tap()
                see(button("sidebar-main"), "the conversation in the place's drawer").tap()
            }
            _ = page.waitForNonExistence(timeout: 3)
        }
    }

    /// From Assistant, then from Agents: opens `entry` from the drawer, goes back (the button, then the swipe), switches to
    /// Agents and back to Assistant, checking each time that the page under the switch is the one it says.
    @MainActor private func roundTrip(_ entry: String, title: String) {
        launchShell()
        see(button("assistant-sidebar-button"), "☰", timeout: 15)
        for (from, swipe) in [("助手", false), ("Agents", true), ("助手", true), ("Agents", false)] {
            let how = "\(title) from \(from), back by \(swipe ? "swipe" : "the drawer")"
            if from == "Agents" {
                finger.switchTo(agents: true, "Agents before \(how)")
                reachable(button("agents-new"), "Agents before \(how)")
            }
            see(button(from == "Agents" ? "work-sidebar-button" : "assistant-sidebar-button"), "☰ on \(from)").tapCenter()
            see(button(entry), "\(entry) in the drawer").tap()
            let page = app.navigationBars[title]
            see(page, "\(title) pushed on 助手 (\(how))")
            back(from: page, swipe: swipe)
            XCTAssertFalse(page.exists, "still on \(how)")
            reachable(chatComposer, "助手's composer after \(how)")
            barAtTop("after \(how)")
            expectAgents(false, "助手 after \(how)")
            switchBothWays(how)
        }
        app.terminate()
    }

    @MainActor func testSwitchAfterKnowledge() { roundTrip("sidebar-知识库", title: "知识库") }
    @MainActor func testSwitchAfterInbox() { roundTrip("sidebar-inbox", title: "收件箱") }
    @MainActor func testSwitchAfterRoutines() { roundTrip("sidebar-定时任务", title: "定时任务") }
    @MainActor func testSwitchAfterMachines() { roundTrip("sidebar-machines", title: "机器") }

    /// Knowledge searched, and swiped back with the search still open: UIKit left the system navigation bar over Assistant's root
    /// (the top bar ~106 pt down, a tap on the switch where it belongs hitting the empty bar).
    @MainActor func testSwitchAfterSwipingBackFromASearch() {
        launchShell()
        let page = app.navigationBars["知识库"]
        see(button("assistant-sidebar-button"), "☰", timeout: 15).tapCenter()
        see(button("sidebar-知识库"), "知识库 in the drawer").tap()
        see(page, "知识库")
        sleep(1)
        let field = see(app.searchFields.firstMatch, "知识库's search")
        field.tapCenter()
        field.typeText("a")
        sleep(1)
        back(from: page, swipe: true) // with the search open there is no back button (Cancel is in its place)
        let how = "swiping back from 知识库's search"
        reachable(chatComposer, "助手's composer after \(how)")
        barAtTop("after \(how)")
        attachScreen("助手 after \(how)")
        switchBothWays(how)
    }
}

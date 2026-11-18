import XCTest

/// The Sidebar layout's column coming and going beside the page (#4206), on the iPad held on its side. With one width it
/// only slides, so its rows are not laid out again on every frame; before #4206 the line's handle (#2624) resized the
/// column under the finger. Frames come from the app's `FrameStats` (`PerfTestCase`): `PERF_SIM=<an iPad>
/// scripts/app/perf-test.sh sidebar` (an optimized build, a mock with many agents). XCTest's taps and waits are inside the
/// measured block here, so its accessibility snapshots count in the hitches; compare runs of this test with each other.
final class PerfSidebarTests: PerfTestCase {
    override func setUpWithError() throws {
        try super.setUpWithError()
        XCUIDevice.shared.orientation = .landscapeLeft
    }

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
        super.tearDown()
    }

    private static let args = ["-tab", "assistant", "-app.tab", "assistant", "-dev.openChat", "", "-openSplitFresh", "YES",
                               "-app.navigation", "sidebar"]

    /// The column folded and brought back with its buttons, three times: the head's fold where it has one (#4206), else
    /// the page's sidebar button both ways; the page's brings it back.
    @MainActor func testSidebarHideAndShow() throws {
        let app = launch(Self.args, ready: XCUIApplication().descendants(matching: .any)["sidebar-main"].firstMatch)
        guard app.windows.firstMatch.frame.width > 900 else { throw XCTSkip("the iPad on its side only") }
        let fold = app.descendants(matching: .any)["sidebar-fold-column"].firstMatch
        let page = app.descendants(matching: .any)["assistant-sidebar-button"].firstMatch
        let main = app.descendants(matching: .any)["sidebar-main"].firstMatch
        measureFrames(metrics()) {
            for _ in 0..<3 {
                (fold.exists ? fold : page).tap()
                XCTAssertTrue(main.until("exists == false OR isHittable == false"), "the column did not go")
                page.tap()
                XCTAssertTrue(main.until("exists == true AND isHittable == true"), "the column did not come back")
            }
        }
    }

    /// The same, on the app's cue (`FrameProbe.toggleSidebar`): the app flips the column with its buttons' animation and
    /// the test only waits, so the measured frames are the app's alone; `-perfCounts YES` adds the views' bodies run
    /// meanwhile, and each frame's interval and main-thread time are in `frames.json` (`deltas_ms`, `busies_ms`).
    @MainActor func testSidebarHideAndShowByCue() throws {
        let app = launch(Self.args + ["-perfCounts", "YES"], ready: XCUIApplication().descendants(matching: .any)["sidebar-main"].firstMatch)
        guard app.windows.firstMatch.frame.width > 900 else { throw XCTSkip("the iPad on its side only") }
        measureFrames(metrics()) {
            for _ in 0..<6 { // hide, show, three times: the column out again at the end
                darwinPost("com.persona.perf.sidebar.toggle")
                usleep(1_200_000)
            }
        }
    }

    /// A finger's drag from the line beside the column out of the way, then from the screen's edge back, three times.
    /// Since #4206 pictures of the screen follow the finger and the split changes once; before, the same drag resized the
    /// column under the finger down to its least width and closed it past that (#2624's handle), and the drag back from
    /// the page's edge opened it.
    @MainActor func testSidebarDrag() throws {
        let app = launch(Self.args, ready: XCUIApplication().descendants(matching: .any)["sidebar-main"].firstMatch)
        guard app.windows.firstMatch.frame.width > 900 else { throw XCTSkip("the iPad on its side only") }
        let main = app.descendants(matching: .any)["sidebar-main"].firstMatch
        let window = app.windows.firstMatch.frame
        // The line between the column and the page: the column's own edge (the handle's place before #4206).
        let width = app.descendants(matching: .any)["unified-sidebar"].firstMatch.frame.maxX
        func drag(from x: CGFloat, by dx: CGFloat) {
            let start = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: window.minX + x, dy: window.midY))
            start.press(forDuration: 0.05, thenDragTo: start.withOffset(CGVector(dx: dx, dy: 0)), withVelocity: .slow, thenHoldForDuration: 0.05)
        }
        measureFrames(metrics()) {
            for _ in 0..<3 {
                drag(from: width, by: -(width * 0.8))
                XCTAssertTrue(main.until("exists == false OR isHittable == false"), "the column did not slide away")
                drag(from: 2, by: width * 0.8)
                XCTAssertTrue(main.until("exists == true AND isHittable == true"), "the column did not slide back")
            }
        }
    }
}

private extension XCUIElement {
    func until(_ format: String, timeout: TimeInterval = 5) -> Bool {
        XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: format), object: self)], timeout: timeout) == .completed
    }
}

import XCTest

/// Run Agent Here offering Codex (#5504): on a computer whose Codex launchers say `run_here` the sheet's launcher menu
/// lists Codex and its footer says nothing about it; on one whose don't (an older node) Codex stays out and the footer
/// says where it starts. Against the mock's terminal panes, the MacBook saying run_here, the server not:
///   PORT=<port> MOCK_TERMINAL_PANES=1 MOCK_RUN_HERE_CODEX=macbook python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_RUNHERE_HUB=http://127.0.0.1:<port> xcodebuild test … -only-testing:PersonaUITests/RunHereCodexTests
/// `TEST_RUNNER_PERSONA_SHOTS=<dir>` saves each state in light and dark; `TEST_RUNNER_PERSONA_SHOTS_BEFORE=1` takes the
/// shots only (a build without the feature: nothing is asserted).
final class RunHereCodexTests: XCTestCase {
    private var app: XCUIApplication!
    private var look = ""
    private var before: Bool { ProcessInfo.processInfo.environment["PERSONA_SHOTS_BEFORE"] != nil }
    private let codexFooter = "Codex 在自己的标签页里启动"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["PERSONA_RUNHERE_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TERMINAL_PANES=1 MOCK_RUN_HERE_CODEX=macbook (PERSONA_RUNHERE_HUB)")
        }
    }

    private var hub: String { ProcessInfo.processInfo.environment["PERSONA_RUNHERE_HUB"]! }

    /// Run Agent Here for `machine`'s first herdr space, its launchers loaded.
    @MainActor private func open(_ machine: String) {
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-showRunAgentHere", machine]
        app.launchPinned()
        let picker = launcherPicker
        XCTAssertTrue(picker.waitForExistence(timeout: 30), "no launcher picker on \(machine):\n\(app.debugDescription.prefix(4000))")
        sleep(2) // the launchers' health and the footer settle
    }

    /// The sheet's launcher row: a menu whose button reads the launcher picked.
    private var launcherPicker: XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "启动器")).firstMatch
    }

    private func option(_ name: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label == %@", name)).firstMatch
    }

    private var footerSaysCodex: Bool {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", codexFooter)).firstMatch.exists
    }

    @MainActor private func openMenu() {
        for _ in 0..<3 where !option("sonnetcc").exists {
            launcherPicker.tap()
            _ = option("sonnetcc").waitForExistence(timeout: 5)
        }
        XCTAssertTrue(option("sonnetcc").exists, "the launcher menu did not open:\n\(app.debugDescription.prefix(4000))")
        sleep(1)
    }

    @MainActor func testShots() throws {
        let was = XCUIDevice.shared.appearance
        defer { XCUIDevice.shared.appearance = was }
        for mode in [XCUIDevice.Appearance.light, .dark] {
            XCUIDevice.shared.appearance = mode
            look = mode == .dark ? "dark" : "light"

            open("macbook")
            shot("offered")
            if !before { XCTAssertFalse(footerSaysCodex, "the footer sends Codex elsewhere where it is offered here") }
            openMenu()
            shot("offered-menu")
            if !before { XCTAssertTrue(option("codex").exists, "no Codex where the computer says run_here") }
            app.terminate()

            open("server")
            shot("older")
            if !before { XCTAssertTrue(footerSaysCodex, "the footer doesn't say where Codex starts on an older computer") }
            openMenu()
            shot("older-menu")
            if !before { XCTAssertFalse(option("codex").exists, "Codex offered where the computer doesn't say run_here") }
            app.terminate()
        }
    }

    @MainActor private func shot(_ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = "run-here-codex \(name) \(look)"
        a.lifetime = .keepAlways
        add(a)
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "run-here-codex-\(name)-\(look).png"))
    }
}

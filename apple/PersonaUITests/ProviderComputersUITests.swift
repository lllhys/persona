import XCTest

/// A provider's Computers page (#2333) in Demo Mode (DemoHub, no hub needed), opened with `-openProviderComputers <state>` on a
/// made-up provider: Selected Computers with its checklist, the toggles and Per-Computer Keys (remove, undo), Hub Only, and the
/// legacy note. TEST_RUNNER_PERSONA_SHOTS=<dir> keeps a screenshot of each, named with TEST_RUNNER_PERSONA_APPEARANCE.
final class ProviderComputersUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws { continueAfterFailure = false }

    @MainActor private func launch(_ state: String) {
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-tab", "settings", "-dev.openChat", "",
                               "-openModelLaunchers", "YES", "-openProviderComputers", state]
        app.launchPinned()
    }

    @MainActor private func shot(_ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        let look = ProcessInfo.processInfo.environment["PERSONA_APPEARANCE"] ?? "light"
        try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "computers-\(name)-\(look).png"))
    }

    @MainActor private func el(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    @MainActor func testSelectedComputersAndTheirKeys() {
        launch("keys")
        let selected = el("computers.share.selected")
        XCTAssertTrue(selected.waitForExistence(timeout: 15), "no Computers page:\n\(app.debugDescription)")
        XCTAssertTrue(selected.isSelected)
        XCTAssertTrue(el("computers.machine.server").exists && el("computers.push").exists && el("computers.ownService").exists)
        shot("1-selected")

        app.swipeUp()
        let remove = el("computers.key.server.remove")
        XCTAssertTrue(remove.waitForExistence(timeout: 5), "no stored key on the first computer:\n\(app.debugDescription)")
        XCTAssertTrue(el("computers.key.macbook").exists, "no key field for the second computer")
        shot("2-keys")
        remove.tap()
        XCTAssertTrue(el("computers.key.server.undo").waitForExistence(timeout: 5))
        el("computers.key.server.undo").tap()
        XCTAssertTrue(el("computers.key.server.remove").waitForExistence(timeout: 5))
        PasswordPrompt.assertNone(app, "a computer's own key")

        // Hub Only: nothing travels, so no toggles and no new keys; a stored key stays listed so it can still be removed.
        app.swipeDown()
        el("computers.share.none").tap()
        XCTAssertTrue(el("computers.push").waitForNonExistence(timeout: 5))
        XCTAssertFalse(el("computers.key.macbook").exists, "Hub Only still offers a key for a computer without one")
        shot("3-hub-only")
    }

    @MainActor func testALegacyProviderSaysSo() {
        launch("legacy")
        XCTAssertTrue(el("computers.legacy").waitForExistence(timeout: 15), "no legacy note:\n\(app.debugDescription)")
        XCTAssertTrue(el("computers.share.all").isSelected)
        shot("4-legacy")
    }

    @MainActor func testADirectAPIHasNoSignInToggle() {
        launch("api")
        XCTAssertTrue(el("computers.share.all").waitForExistence(timeout: 15), "no Computers page:\n\(app.debugDescription)")
        XCTAssertTrue(el("computers.push").exists)
        XCTAssertFalse(el("computers.ownService").exists)
        shot("5-api")
    }
}

import XCTest

/// Settings › About › Developing Persona (#9460, docs/operations/developing-persona.md): the switch, the rows it adds,
/// the question before turning it off with something recorded, and Export Diagnostics. Needs a mock hub in
/// `PERSONA_MOCK_HUB`; `PERSONA_SHOTS=<dir>` keeps `dev-mode-<1-off|2-on|3-confirm|4-export|5-send|6-chat>-<light|dark>.png`.
final class DevModeUITests: XCTestCase {
    override func setUp() { continueAfterFailure = false }

    @MainActor private func launch(_ extra: [String] = []) throws -> XCUIApplication {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("Needs a mock hub in PERSONA_MOCK_HUB")
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-dev.openChat", "", "-devMode.on", "NO",
                               "-AppleLanguages", "(zh-Hans)", "-AppleLocale", "zh_CN"] + extra
        app.launchPinned()
        return app
    }

    @MainActor private func element(_ id: String, _ app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
    }

    /// Lets a sheet or the keyboard finish moving before a picture.
    @MainActor private func settle(_ app: XCUIApplication) {
        _ = app.wait(for: .runningForeground, timeout: 1)
        Thread.sleep(forTimeInterval: 3)
    }

    @MainActor private func shot(_ name: String, _ look: String) {
        let png = XCUIScreen.main.screenshot().pngRepresentation
        let attachment = XCTAttachment(data: png, uniformTypeIdentifier: "public.png")
        attachment.name = "dev-mode-\(name)-\(look)"
        attachment.lifetime = .keepAlways
        add(attachment)
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent("dev-mode-\(name)-\(look).png"))
    }

    /// One appearance a run (`PERSONA_SHOTS_APPEARANCE`, light by default; the run script sets the simulator's too).
    @MainActor func testSwitchExportAndTurnOff() throws {
        do {
            let look = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] == "dark" ? "dark" : "light"
            XCUIDevice.shared.appearance = look == "dark" ? .dark : .light
            let app = try launch(["-openAbout", "YES"])
            let toggle = element("dev-mode-switch", app)
            XCTAssertTrue(toggle.waitForExistence(timeout: 30), "no Developing Persona switch:\n\(app.debugDescription)")
            XCTAssertFalse(element("dev-mode-export", app).exists, "export shows only while on")
            shot("1-off", look)

            toggle.switches.firstMatch.tap()
            XCTAssertTrue(element("dev-mode-export", app).waitForExistence(timeout: 10), "no export row after turning on")
            XCTAssertTrue(element("dev-mode-recorded", app).exists)
            // Something is recorded at once (the line saying recording started), so turning off asks first.
            let recorded = element("dev-mode-recorded", app)
            let sized = NSPredicate(format: "label CONTAINS %@", "·")
            expectation(for: sized, evaluatedWith: recorded)
            waitForExpectations(timeout: 10)
            shot("2-on", look)

            toggle.switches.firstMatch.tap()
            let alert = app.alerts.firstMatch
            XCTAssertTrue(alert.waitForExistence(timeout: 10), "no question before deleting what was recorded")
            XCTAssertTrue(alert.buttons["先导出…"].exists)
            XCTAssertTrue(alert.buttons["关闭并删除"].exists)
            shot("3-confirm", look)
            alert.buttons["先导出…"].tap()

            XCTAssertTrue(element("dev-export-summary", app).waitForExistence(timeout: 10), "Export First… did not open the export page")
            XCTAssertTrue(element("dev-export-logs", app).exists)
            XCTAssertTrue(element("dev-export-share", app).exists)
            shot("4-export", look)
            // Unticking a category and ticking it again.
            element("dev-export-errors", app).tap()
            XCTAssertFalse(element("dev-export-errors", app).isSelected)
            element("dev-export-errors", app).tap()
            XCTAssertTrue(element("dev-export-errors", app).isSelected)

            app.navigationBars.buttons.firstMatch.tap()
            XCTAssertTrue(toggle.waitForExistence(timeout: 10))
            toggle.switches.firstMatch.tap()
            XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 10))
            app.alerts.firstMatch.buttons["关闭并删除"].tap()
            XCTAssertTrue(element("dev-mode-export", app).waitForNonExistence(timeout: 10), "still on after Turn Off and Delete")
            app.terminate()
        }
    }

    /// Send to Agent: the export page's first button builds the package and opens Send To; Persona takes it to the main
    /// chat, the zip as a file card over the box (uploaded to the mock) and the note in the box, not sent.
    @MainActor func testSendToAgent() throws {
        let look = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] == "dark" ? "dark" : "light"
        XCUIDevice.shared.appearance = look == "dark" ? .dark : .light
        let app = try launch(["-openAbout", "YES"])
        let toggle = element("dev-mode-switch", app)
        XCTAssertTrue(toggle.waitForExistence(timeout: 30), "no Developing Persona switch:\n\(app.debugDescription)")
        toggle.switches.firstMatch.tap()
        XCTAssertTrue(element("dev-mode-export", app).waitForExistence(timeout: 10))
        element("dev-mode-export", app).tap()

        let send = element("dev-export-send", app)
        XCTAssertTrue(send.waitForExistence(timeout: 10), "no Send to Agent on the export page")
        XCTAssertTrue(element("dev-export-share", app).exists)
        shot("4-export", look)
        send.tap()
        XCTAssertTrue(element("dev-send.sheet", app).waitForExistence(timeout: 20), "Send To did not open")
        XCTAssertTrue(element("dev-send.file", app).exists)
        XCTAssertTrue(element("dev-send.persona", app).exists)
        settle(app)
        shot("5-send", look)
        // Again a little later: the sheet has stopped moving, the two pictures are the same.
        Thread.sleep(forTimeInterval: 3)
        shot("5-send-later", look)
        element("dev-send.persona", app).tap()

        let card = element("file-attachments", app)
        XCTAssertTrue(card.waitForExistence(timeout: 20), "the package is not over the main chat's box:\n\(app.debugDescription)")
        XCTAssertTrue(element("file-attachment-ready", app).waitForExistence(timeout: 20), "the package did not upload")
        let note = NSPredicate(format: "value CONTAINS %@", "这是我设备上的 Persona 诊断数据")
        XCTAssertTrue(app.textViews.matching(note).firstMatch.waitForExistence(timeout: 10)
                      || app.textFields.matching(note).firstMatch.exists, "no note in the box")
        // The keyboard's first-use tip on a fresh simulator covers the box: put it away for the picture.
        let tip = app.buttons["Continue"]
        if tip.waitForExistence(timeout: 2) { tip.tap() }
        settle(app)
        shot("6-chat", look)
        app.terminate()
    }
}

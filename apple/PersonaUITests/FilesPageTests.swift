import XCTest

/// The file page (#5827) against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB) with MOCK_FILES=1 MOCK_FILE_LINKS=1
/// MOCK_FILE_OPS=1: the path bar from the home with the session's folder tagged, Back / Forward / Up, links with their
/// target and a broken one in red, Info for a link and a script, finding by name, a folder outside the home read-only,
/// and a row's long press. Each step is attached as a screenshot in light and dark (also written to PERSONA_SHOTS).
final class FilesPageTests: XCTestCase {
    private var hub: String!
    private let widget = "macbook/a1f3c2d4-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB) with MOCK_FILES=1 MOCK_FILE_LINKS=1 MOCK_FILE_OPS=1")
        }
        self.hub = hub
    }

    @MainActor private func launch(_ scheme: String, _ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", widget, "-openAgentsFresh", "YES", "-openChatsFresh", "YES",
                               "-ui.scheme", scheme, "-openPane", "files"] + extra
        app.launchPinned()
        dismissSystemAlerts(wait: 3)
        return app
    }

    /// SpringBoard's alerts (the icon that follows Light & Dark, a notification question).
    @MainActor private func dismissSystemAlerts(wait: TimeInterval = 1) {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        var deadline = Date.now.addingTimeInterval(wait)
        while Date.now < deadline {
            let alert = springboard.alerts.firstMatch
            if alert.waitForExistence(timeout: 0.5) {
                for label in ["允许", "Allow", "好", "OK"] where alert.buttons[label].exists { alert.buttons[label].tap(); break }
                deadline = Date.now.addingTimeInterval(1.5)
            }
        }
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let png = app.screenshot()
        let a = XCTAttachment(screenshot: png)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("files-\(name).png"))
        }
    }

    @MainActor private func waitForRow(_ app: XCUIApplication, _ name: String, _ why: String) -> XCUIElement {
        let row = app.descendants(matching: .any)["files.row." + name].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 30), "\(why): no row \(name):\n\(app.debugDescription)")
        return row
    }

    /// Some element whose label or value has text (a row or a labeled row reads as one element).
    @MainActor private func text(_ app: XCUIApplication, _ text: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@ OR value CONTAINS %@", text, text)).firstMatch
    }

    @MainActor func testFilesPageLight() throws { try page("light") }
    @MainActor func testFilesPageDark() throws { try page("dark") }

    @MainActor private func page(_ scheme: String) throws {
        // The session's folder: links, the path bar, the session's tag.
        var app = launch(scheme)
        _ = waitForRow(app, "shared", "the session's folder")
        XCTAssertTrue(text(app, "链接断了 · 指向的 ../legacy/settings.json 不存在").exists, "the broken link is not said:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["files.home"].exists, "no home in the path bar")
        Thread.sleep(forTimeInterval: 1)
        shot(app, "list-\(scheme)")

        // Into a folder and back.
        waitForRow(app, "Sources", "into Sources").tap()
        _ = waitForRow(app, "App", "inside Sources")
        app.buttons["files.back"].tap()
        _ = waitForRow(app, "shared", "back")
        XCTAssertTrue(app.buttons["files.forward"].isEnabled, "Forward is off after Back")

        // A long press on a script.
        waitForRow(app, "deploy.sh", "the script").press(forDuration: 1.0)
        XCTAssertTrue(app.buttons["简介"].waitForExistence(timeout: 5), "no Info in the row's menu:\n\(app.debugDescription)")
        Thread.sleep(forTimeInterval: 0.5)
        shot(app, "menu-\(scheme)")
        app.buttons["简介"].tap()
        XCTAssertTrue(text(app, "rwxr-xr-x · 755").waitForExistence(timeout: 10), "no permission code in Info:\n\(app.debugDescription)")
        Thread.sleep(forTimeInterval: 0.5)
        shot(app, "info-\(scheme)")
        app.terminate()

        // A link's Info: what it says, where it leads.
        app = launch(scheme, ["-showFilesInfo", "shared"])
        XCTAssertTrue(text(app, "../shared").waitForExistence(timeout: 30), "the link's Info does not say what it says:\n\(app.debugDescription)")
        XCTAssertTrue(text(app, "~/code/shared").waitForExistence(timeout: 10), "no real location")
        Thread.sleep(forTimeInterval: 0.5)
        shot(app, "info-link-\(scheme)")
        app.buttons["files.info.real"].tap()
        _ = waitForRow(app, "colors.json", "the link's real location")
        app.terminate()

        // Finding by name.
        app = launch(scheme, ["-showFilesFind", "swift"])
        let found = app.descendants(matching: .any)["files.found.Sources/App/main.swift"].firstMatch
        XCTAssertTrue(found.waitForExistence(timeout: 30), "main.swift not found:\n\(app.debugDescription)")
        Thread.sleep(forTimeInterval: 0.5)
        shot(app, "find-\(scheme)")
        app.terminate()

        // Above the home: read only, no upload.
        app = launch(scheme, ["-showFilesPath", "/Users"])
        XCTAssertTrue(app.descendants(matching: .any)["files.readonly"].waitForExistence(timeout: 30), "no Read Only outside the home:\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["files.upload"].exists, "Upload offered outside the home")
        Thread.sleep(forTimeInterval: 0.5)
        shot(app, "readonly-\(scheme)")
        app.terminate()
    }
}

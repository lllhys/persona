import XCTest

/// The file page's actions (#5828–#5832) against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB) with MOCK_FILES=1
/// MOCK_FILE_LINKS=1 MOCK_FILE_OPS=1 MOCK_TERMINAL_PANES=1: ＋ (New Folder, New Text File, uploads), the editor (find and
/// replace, a save refused because the file changed on the computer, Overwrite Anyway asked twice), the selection bar
/// (Copy To's picker, Compress), Permissions (the switches, the confirmation), Open Terminal Here. Each step is attached
/// as a screenshot in light and dark (also written to PERSONA_SHOTS).
final class FilesActionsTests: XCTestCase {
    private var hub: String!
    private let widget = "macbook/a1f3c2d4-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB) with MOCK_FILES=1 MOCK_FILE_LINKS=1 MOCK_FILE_OPS=1 MOCK_TERMINAL_PANES=1")
        }
        self.hub = hub
    }

    @MainActor private func launch(_ scheme: String, _ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", widget, "-openAgentsFresh", "YES", "-openChatsFresh", "YES",
                               "-ui.scheme", scheme] + extra
        app.launchPinned()
        dismissSystemAlerts(wait: 3)
        return app
    }

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
        Thread.sleep(forTimeInterval: 0.6)
        let png = app.screenshot()
        let a = XCTAttachment(screenshot: png)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("actions-\(name).png"))
        }
    }

    @MainActor private func element(_ app: XCUIApplication, _ id: String, _ why: String, timeout: TimeInterval = 20) -> XCUIElement {
        let e = app.descendants(matching: .any)[id].firstMatch
        XCTAssertTrue(e.waitForExistence(timeout: timeout), "\(why): no \(id):\n\(app.debugDescription)")
        return e
    }

    @MainActor private func button(_ app: XCUIApplication, _ label: String, _ why: String, timeout: TimeInterval = 8) -> XCUIElement {
        let b = app.buttons[label].firstMatch
        XCTAssertTrue(b.waitForExistence(timeout: timeout), "\(why): no button \(label):\n\(app.debugDescription)")
        return b
    }

    /// The mock changes a file behind an open editor.
    private func touch(_ rel: String) {
        let url = URL(string: hub + "/mock/touch?path=" + rel.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed)!)!
        _ = try? Data(contentsOf: url)
    }

    @MainActor func testActionsLight() throws { try actions("light") }
    @MainActor func testActionsDark() throws { try actions("dark") }

    @MainActor private func actions(_ scheme: String) throws {
        // ＋: new and upload; a new text file opens.
        var app = launch(scheme, ["-openPane", "files"])
        element(app, "files.row.README.md", "the session's folder", timeout: 30)
        element(app, "files.upload", "＋").tap()
        button(app, "新建文本文件", "＋ menu")
        shot(app, "new-menu-\(scheme)")
        app.buttons["新建文本文件"].firstMatch.tap()
        // An alert's text field is the system's: found through the alert, not by an identifier set in SwiftUI.
        let field = app.alerts.firstMatch.textFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 8), "the name field:\n\(app.debugDescription)")
        field.tap()
        let typed = (field.value as? String)?.count ?? 0
        // One name per run: light and dark share the mock, which answers "exists" for a name taken.
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: typed) + (scheme == "dark" ? "todo.txt" : "notes.txt"))
        shot(app, "new-text-\(scheme)")
        // A tap on Create while the alert is still settling can be lost (seen once in dark): tap again while it stays up.
        let opened = app.descendants(matching: .any)["file-close"]
        for _ in 0..<3 where app.alerts.firstMatch.exists {
            app.alerts.buttons["创建"].firstMatch.tap()
            if opened.waitForExistence(timeout: 5) { break }
        }
        XCTAssertTrue(opened.waitForExistence(timeout: 15), "the new file did not open:\n\(app.debugDescription)")
        app.terminate()

        // The editor: Edit from ⋯, find and replace, a conflict, Overwrite Anyway asked twice.
        app = launch(scheme, ["-openFilePath", "Sources/App/main.swift"])
        XCTAssertTrue(app.descendants(matching: .any)["file-code"].waitForExistence(timeout: 30), "no code view")
        app.buttons["更多"].firstMatch.tap()
        button(app, "编辑", "⋯ menu").tap()
        let editor = element(app, "file-editor", "the editor")
        editor.tap()
        editor.typeText("// 小改动\n")
        shot(app, "editor-\(scheme)")
        if app.buttons["editor.find"].waitForExistence(timeout: 3) {
            app.buttons["editor.find"].tap()
            shot(app, "editor-find-\(scheme)")
            app.typeText(XCUIKeyboardKey.escape.rawValue)
        }
        touch("code/weather/Sources/App/main.swift")
        element(app, "editor.save", "Save").tap()
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 10), "no conflict alert:\n\(app.debugDescription)")
        shot(app, "editor-conflict-\(scheme)")
        app.alerts.buttons["仍然覆盖"].firstMatch.tap()
        XCTAssertTrue(app.alerts.buttons["覆盖"].waitForExistence(timeout: 5), "Overwrite Anyway was not asked again")
        shot(app, "editor-overwrite-\(scheme)")
        app.alerts.buttons["覆盖"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["已保存"].waitForExistence(timeout: 10), "no Saved:\n\(app.debugDescription)")
        app.terminate()

        // Select two: the bar; Copy To's picker; Compress.
        app = launch(scheme, ["-openPane", "files"])
        element(app, "files.row.README.md", "the session's folder", timeout: 30)
        element(app, "files.select", "Select").tap()
        element(app, "files.row.README.md", "a row").tap()
        element(app, "files.row.deploy.sh", "a row").tap()
        shot(app, "select-\(scheme)")
        element(app, "files.select.copy", "Copy To").tap()
        element(app, "files.move.here", "the picker")
        shot(app, "copy-picker-\(scheme)")
        app.buttons["sheet-close"].firstMatch.tap()
        element(app, "files.select.compress", "Compress").tap()
        XCTAssertTrue(app.descendants(matching: .any)["files.row.归档.zip"].waitForExistence(timeout: 15), "no zip in the list:\n\(app.debugDescription)")
        shot(app, "compressed-\(scheme)")

        // Permissions from a row's long press: the switches, then the confirmation.
        element(app, "files.row.deploy.sh", "the script").press(forDuration: 1.0)
        button(app, "权限…", "the row's menu").tap()
        let groupWrite = element(app, "files.perm.1.1", "the group's write switch")
        shot(app, "perm-\(scheme)")
        groupWrite.tap()
        element(app, "files.perm.save", "Save").tap()
        XCTAssertTrue(app.alerts.buttons["修改"].waitForExistence(timeout: 5), "no confirmation:\n\(app.debugDescription)")
        shot(app, "perm-confirm-\(scheme)")
        app.alerts.buttons["修改"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["权限已修改"].waitForExistence(timeout: 10), "no Permissions changed")

        // Open Terminal Here.
        element(app, "files.more", "⋯").tap()
        shot(app, "more-\(scheme)")
        button(app, "在这里打开终端", "⋯ menu").tap()
        Thread.sleep(forTimeInterval: 3)
        shot(app, "terminal-\(scheme)")
        XCTAssertEqual(app.state, .runningForeground)
        app.terminate()
    }
}

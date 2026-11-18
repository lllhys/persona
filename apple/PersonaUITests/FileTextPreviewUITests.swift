import XCTest

final class FileTextPreviewUITests: XCTestCase {
    override func setUp() { continueAfterFailure = false }

    @MainActor private func launch(_ path: String) throws -> XCUIApplication {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("Run the file_text_hub.py fixture with PERSONA_MOCK_HUB")
        }
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock",
                               "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000006", "-openFilePath", path,
                               "-openAgentsFresh", "YES", "-openChatsFresh", "YES", "-AppleLanguages", "(zh-Hans)", "-ui.scheme", "system"]
        if let size = ProcessInfo.processInfo.environment["PERSONA_UI_TEXT_SIZE"] {
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", size]
        }
        app.launchPinned()
        LiveActivityShots.allowNotifications()
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        LiveActivityShots.allowNotifications()
        XCTAssertFalse(XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.firstMatch.exists,
                       "System prompts must not cover screenshot evidence")
        let look = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] ?? "light"
        let name = look + "-" + name
        // The host fixture uses simctl for evidence at the exact asserted state; XCTest keeps an attachment too.
        if let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"],
           let url = URL(string: hub + "/mock/preview-shot?name=" + name) {
            let saved = expectation(description: "Host saved simulator screenshot")
            URLSession.shared.dataTask(with: url) { _, response, error in
                XCTAssertNil(error)
                XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
                saved.fulfill()
            }.resume()
            wait(for: [saved], timeout: 40)
        }
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    @MainActor func testEmptyFileAndReloadGrowingOutput() throws {
        let app = try launch("/tmp/preview/empty.output")
        XCTAssertTrue(app.staticTexts["这个文件是空的"].waitForExistence(timeout: 30), app.debugDescription)
        XCTAssertFalse(app.staticTexts["无法预览"].exists)
        shot(app, "empty-output")
        app.buttons["file-menu"].tap()
        let reload = app.buttons["file-reload"]
        XCTAssertTrue(reload.waitForExistence(timeout: 5), app.debugDescription)
        reload.tap()
        XCTAssertTrue(app.staticTexts["Command output is now available."].waitForExistence(timeout: 20), app.debugDescription)
        XCTAssertFalse(app.staticTexts["这个文件是空的"].exists)
        shot(app, "reloaded-output")
    }

    @MainActor func testTextExtensionsAndUTF8Fallback() throws {
        for name in ["task.output", "task.log", "task"] {
            let app = try launch("/tmp/preview/" + name)
            XCTAssertTrue(app.descendants(matching: .any)["file-code"].waitForExistence(timeout: 30), app.debugDescription)
            XCTAssertTrue(app.staticTexts["All checks passed."].waitForExistence(timeout: 10), app.debugDescription)
            XCTAssertFalse(app.staticTexts["无法预览"].exists)
            shot(app, name)
            app.terminate()
        }
    }
}

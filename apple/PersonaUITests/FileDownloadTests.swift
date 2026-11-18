import XCTest

/// Download in the file viewer (#5826) against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB) with MOCK_FILES=1: a tap on
/// Download saves a picture to Photos (after the system's permission alert) and says so with an Open button; a long
/// press offers Save to Photos, Save to Files and Share; a code file goes to the system's Save to Files. Each step is
/// attached as a screenshot, in light and dark (also written to PERSONA_SHOTS when set).
final class FileDownloadTests: XCTestCase {
    private var hub: String!
    private let widget = "macbook/a1f3c2d4-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB) with MOCK_FILES=1")
        }
        self.hub = hub
    }

    @MainActor private func launch(_ path: String, scheme: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.resetAuthorizationStatus(for: .photos) // the alert comes up, like on a fresh install
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", widget, "-openAgentsFresh", "YES", "-openChatsFresh", "YES",
                               "-ui.scheme", scheme, "-openFilePath", path]
        app.launchPinned()
        return app
    }

    /// SpringBoard's alerts: the icon that follows Light & Dark, a notification question, the photo library's.
    @MainActor private func allowSystemAlerts(wait: TimeInterval = 1) {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        var deadline = Date.now.addingTimeInterval(wait)
        while Date.now < deadline {
            let alert = springboard.alerts.firstMatch
            if alert.waitForExistence(timeout: 0.5) {
                let allow = alert.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ OR label BEGINSWITH %@ OR label == %@ OR label == %@",
                                                               "允许", "Allow", "好", "OK")).firstMatch
                if allow.exists { allow.tap() }
                deadline = Date.now.addingTimeInterval(1.5) // another may follow
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
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("download-\(name).png"))
        }
    }

    @MainActor func testDownloadLight() throws { try download(scheme: "light") }
    @MainActor func testDownloadDark() throws { try download(scheme: "dark") }

    @MainActor private func download(scheme: String) throws {
        // A picture: the tap saves it to Photos.
        var app = launch("docs/icon.png", scheme: scheme)
        allowSystemAlerts(wait: 3)
        let download = app.buttons["file-download"]
        XCTAssertTrue(download.waitForExistence(timeout: 30), "no Download in the viewer:\n\(app.debugDescription)")
        _ = XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: "isEnabled == true"), object: download)], timeout: 10)
        Thread.sleep(forTimeInterval: 1) // the picture drawn
        shot(app, "picture-\(scheme)")

        download.press(forDuration: 1.0)
        let toPhotos = app.buttons["存到照片"]
        XCTAssertTrue(toPhotos.waitForExistence(timeout: 5), "no Save to Photos on a long press:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["存到「文件」"].exists, "no Save to Files on a long press")
        XCTAssertTrue(app.buttons["分享"].exists, "no Share on a long press")
        shot(app, "menu-\(scheme)")
        toPhotos.tap()
        allowSystemAlerts(wait: 4)
        let saved = app.staticTexts["已保存到相册"].firstMatch
        XCTAssertTrue(saved.waitForExistence(timeout: 15), "no 已保存到相册 (state \(app.state.rawValue)):\n\(app.debugDescription)")
        XCTAssertEqual(app.state, .runningForeground, "the app is gone after saving")
        XCTAssertTrue(app.buttons["打开"].exists, "the toast has no Open")
        Thread.sleep(forTimeInterval: 0.5)
        shot(app, "saved-\(scheme)")
        app.terminate()

        // Code: the tap opens the system's Save to Files.
        app = launch("Sources/App/main.swift", scheme: scheme)
        allowSystemAlerts(wait: 3)
        let code = app.buttons["file-download"]
        XCTAssertTrue(code.waitForExistence(timeout: 30), "no Download for a code file:\n\(app.debugDescription)")
        XCTAssertTrue(app.descendants(matching: .any)["file-code"].waitForExistence(timeout: 15), "the code view did not open")
        code.tap()
        Thread.sleep(forTimeInterval: 4) // the system's picker comes up from its own process
        shot(app, "files-\(scheme)")
        XCTAssertEqual(app.state, .runningForeground, "the app is gone after Download")
        app.terminate()
    }
}

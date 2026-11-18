import XCTest

final class VideoFileUITests: XCTestCase {
    @MainActor func testVideoOpensAndReopens() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_VIDEO_HUB"] ?? ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("需要带 MOCK_VIDEO_FILE 的独立 mock hub")
        }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let appearance = ProcessInfo.processInfo.environment["PERSONA_VIDEO_APPEARANCE"] ?? "system"
        defer { app.terminate() }
        for pass in 0..<2 {
            if pass == 0 {
                // Use the shipping Shell route for playback acceptance too.
                app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000001",
                                       "-openAgentsFresh", "YES", "-openChatsFresh", "YES"]
                app.launchPinned()
            }
            let chip = app.buttons["file-chip-/Users/me/code/weather/media/preview.mp4"].firstMatch
            XCTAssertTrue(chip.waitForExistence(timeout: 30), "生成的视频没有文件入口：\(app.debugDescription)")
            chip.tap()
            let player = app.descendants(matching: .any)["file-video-player"].firstMatch
            XCTAssertTrue(player.waitForExistence(timeout: 30), "视频没有打开：\(app.debugDescription)")
            XCTAssertFalse(app.staticTexts["无法预览"].exists)
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.name = "video-preview-\(UITestStyle.name)-\(appearance)-\(pass)"
            shot.lifetime = .keepAlways
            add(shot)
            app.buttons.matching(NSPredicate(format: "identifier IN %@ OR label == %@", ["file-close", "sheet-close"], "完成")).firstMatch.tap()
            XCTAssertTrue(player.waitForNonExistence(timeout: 5), "关闭后播放器仍在页面上")
        }
    }
}

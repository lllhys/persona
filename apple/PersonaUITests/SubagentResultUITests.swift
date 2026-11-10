import XCTest

final class SubagentResultUITests: XCTestCase {
    private var app: XCUIApplication!
    private var hub = ""
    private let chat = "macbook/a1f3c2d4-0000-4000-8000-0000000c1e02"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_SUBAGENT_RESULTS_HUB"] else {
            throw XCTSkip("needs the synthetic subagent fixture")
        }
        self.hub = hub
        app = XCUIApplication()
    }

    @MainActor private func launch() {
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", chat,
                               "-openAllWork", "YES", "-openChatsFresh", "YES"]
        app.launchPinned()
    }

    @MainActor private func text(_ marker: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", marker)).firstMatch
    }

    @MainActor private func shot(_ name: String) throws {
        let screenshot = app.screenshot()
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let path = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let directory = URL(fileURLWithPath: path, isDirectory: true)
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            try screenshot.pngRepresentation.write(to: directory.appendingPathComponent(name + ".png"))
        }
    }

    @MainActor func testCompletedResultsFoldWhileTheRunningAgentKeepsItsProgress() throws {
        launch()
        let finished = app.buttons.matching(identifier: "chat.subagent-result")
            .matching(NSPredicate(format: "label CONTAINS %@", "结算流程")).firstMatch
        XCTAssertTrue(finished.waitForExistence(timeout: 25))
        let running = app.descendants(matching: .any).matching(identifier: "chat.subagent-card").firstMatch
        XCTAssertTrue(running.waitForExistence(timeout: 10))
        XCTAssertTrue(text("跑筛选用例").exists)
        XCTAssertFalse(text("结算流程 42 个用例全部通过").exists)
        XCTAssertFalse(text("来自 general-purpose").exists)
        try shot("subagent-results-folded")

        finished.tap()
        XCTAssertTrue(text("结算流程 42 个用例全部通过").waitForExistence(timeout: 10))
        try shot("subagent-result-open")
        app.swipeDown()
        finished.tap()
        expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: text("结算流程 42 个用例全部通过"))
        waitForExpectations(timeout: 5)
        XCTAssertTrue(running.exists)
    }
}

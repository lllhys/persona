import XCTest

/// The file viewer (docs/apple/ios.md「File viewer」) against scripts/mock/mock_hub.py started with MOCK_FILES=1 (PERSONA_MOCK_HUB):
/// only paths that are there on the conversation's machine become chips; code opens with line numbers, JSON as an
/// outline, Markdown rendered.
final class FileViewerUITests: XCTestCase {
    private var hub: String!
    private let widget = "macbook/a1f3c2d4-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB) with MOCK_FILES=1")
        }
        self.hub = hub
    }

    @MainActor private func launch(_ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", widget, "-openAgentsFresh", "YES", "-openChatsFresh", "YES"] + extra
        app.launchPinned()
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/persona-files-\(name).png"))
    }

    /// The link in the text that opens `path` in the file viewer. In a chat on the iPhone every code span is a link too,
    /// one a tap copies (#2984, `CodeCopyMarkdown`): `app.links[path]` finds either. A link in a Text carries its URL
    /// as its identifier, so the scheme tells them apart: `persona-path:` opens the file (`PathLinkURL`),
    /// `persona-copy:` copies the code (`CodeCopyURL`).
    @MainActor private func fileLink(_ app: XCUIApplication, _ path: String) -> XCUIElement {
        app.links.matching(NSPredicate(format: "label == %@ AND identifier BEGINSWITH %@", path, "persona-path:")).firstMatch
    }

    @MainActor private func copyLink(_ app: XCUIApplication, _ path: String) -> XCUIElement {
        app.links.matching(NSPredicate(format: "label == %@ AND identifier BEGINSWITH %@", path, "persona-copy:")).firstMatch
    }

    /// The last message names first one path on another machine and one deleted, then six files that are there, each
    /// in backticks: the six are links to their files in the text (#1706, no chips for them), the other two plain
    /// inline code (a tap copies it, #2984).
    @MainActor func testOnlyPathsThatAreThereAreLinks() throws {
        let app = launch()
        let link = fileLink(app, "Sources/App.swift")
        XCTAssertTrue(link.waitForExistence(timeout: 20), "no link for a file that is there:\n\(app.debugDescription)")
        for p in ["server/handlers.go", "data/forecast.json", "/Users/me/code/weather/README.md", "config/app.yaml", "/Users/me/code/weather/logs/api.log"] {
            XCTAssertTrue(fileLink(app, p).exists, "no link for \(p)")
            XCTAssertFalse(app.buttons["file-chip-" + p].exists, "a linked path is a chip too: \(p)")
        }
        for (p, what) in [("~/.persona/personas/doudou", "another machine's path"), ("/Users/me/code/weather/old/Removed.swift", "a deleted file")] {
            XCTAssertFalse(fileLink(app, p).exists, "\(what) is linked")
            XCTAssertTrue(copyLink(app, p).exists, "\(what) is not inline code a tap copies:\n\(app.debugDescription)")
        }
        shot(app, "links")

        fileLink(app, "server/handlers.go").tap()
        XCTAssertTrue(app.descendants(matching: .any)["file-code"].waitForExistence(timeout: 15), "the code view did not open")
        XCTAssertTrue(app.staticTexts["package server"].waitForExistence(timeout: 5), "the first lines are not there")
        shot(app, "go")
    }

    /// MOCK_PATH_LINKS=1: a question and a reply naming paths in their words, with lines (#1706). A path with a line
    /// opens the file there: line 126's text is on screen (only the lines near what is shown are made).
    @MainActor func testAPathWithALineOpensAtIt() throws {
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_PATH_LINKS"] == "1" else {
            throw XCTSkip("needs the mock hub with MOCK_PATH_LINKS=1 (and TEST_RUNNER_PERSONA_MOCK_PATH_LINKS=1)")
        }
        let app = launch()
        let reply = app.links["server/handlers.go:126"].firstMatch
        XCTAssertTrue(reply.waitForExistence(timeout: 20), "no link for a path with a line:\n\(app.debugDescription)")
        XCTAssertTrue(app.links["/Users/me/code/weather/README.md:12"].exists, "a path in the reply's words is not linked")
        XCTAssertFalse(app.links["/tmp/nope/notes.md"].exists, "a path not there is linked")
        shot(app, "path-links")
        reply.tap()
        XCTAssertTrue(app.descendants(matching: .any)["file-code"].waitForExistence(timeout: 15), "the code view did not open")
        let line = app.staticTexts["// handleForecast7 answers GET /v1/forecast/7 with the next hours for a city."]
        XCTAssertTrue(line.waitForExistence(timeout: 10), "not at line 126:\n\(app.debugDescription)")
        XCTAssertFalse(app.staticTexts["package server"].exists, "still at the top")
        shot(app, "path-line")
    }

    @MainActor func testJSONOpensAsAnOutline() throws {
        let app = launch(["-openFilePath", "data/forecast.json"])
        // The outline (after the whole file is read and parsed): the top level open, the forecasts folded.
        let forecasts = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", #""forecasts": [ … ]"#)).firstMatch
        XCTAssertTrue(forecasts.waitForExistence(timeout: 30), "no outline:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", #""version": 3,"#)).firstMatch.exists)
        shot(app, "json")
        forecasts.tap()
        let open = app.staticTexts.matching(NSPredicate(format: "label ENDSWITH %@", #""forecasts": ["#)).firstMatch
        XCTAssertTrue(open.waitForExistence(timeout: 5), "tapping did not open the array")
        shot(app, "json-open")
    }

    /// A picture opened as a file shows its own full-screen viewer on top of the sheet at a tap (#1331): the chat page's
    /// viewer cannot present while the sheet is up.
    @MainActor func testPictureOpensItsViewerOverTheSheet() throws {
        let app = launch(["-openFilePath", "data/chart.png"])
        let tappable = app.descendants(matching: .any)["file-image"].firstMatch
        let found = tappable.waitForExistence(timeout: 30)
        shot(app, "picture")
        XCTAssertTrue(found, "no picture:\n\(app.debugDescription)")
        XCTAssertGreaterThan(tappable.frame.width, 300, "the picture is a thumbnail, not the sheet's width")
        tappable.tap()
        XCTAssertTrue(app.buttons["viewer-close"].waitForExistence(timeout: 5), "the tap opened no viewer while the sheet is up")
        shot(app, "picture-viewer")
    }

    @MainActor func testMarkdownIsRendered() throws {
        let app = launch(["-openFilePath", "README.md"])
        XCTAssertTrue(app.descendants(matching: .any)["file-markdown"].waitForExistence(timeout: 20), "no rendered markdown")
        XCTAssertTrue(app.staticTexts["天气 App 开发手册"].waitForExistence(timeout: 10))
        shot(app, "markdown")
    }
}

/// Opening times and frames while scrolling the big made-up files (`-showFilePerf YES` writes them to the app's
/// Caches/file-perf.log; the numbers in docs/apple/ios.md come from here). Opened from an idle conversation (a working one
/// redraws behind the sheet). Only with PERSONA_FILE_PERF=1.
final class FileViewerPerfTests: XCTestCase {
    @MainActor func testOpenAndScrollBigFiles() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"],
              ProcessInfo.processInfo.environment["PERSONA_FILE_PERF"] == "1" else {
            throw XCTSkip("set PERSONA_FILE_PERF=1 and PERSONA_MOCK_HUB (MOCK_FILES=1)")
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        // PERSONA_FILE_PERF_ONLY: one file (by a part of its name); PERSONA_FILE_PERF_ARGS: more launch arguments.
        let only = ProcessInfo.processInfo.environment["PERSONA_FILE_PERF_ONLY"].flatMap { $0.isEmpty ? nil : $0 }
        let extra = (ProcessInfo.processInfo.environment["PERSONA_FILE_PERF_ARGS"] ?? "").split(separator: " ").map(String.init)
        for file in ["server/handlers.go", "data/forecast.json", "README.md", "logs/api.log"] where only == nil || file.contains(only!) {
            let app = XCUIApplication()
            app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000006",
                                   "-openFilePath", "/Users/me/code/weather/" + file, "-showFilePerf", "YES", "-showFileAutoScroll", "YES"] + extra
            app.launchPinned()
            let view = app.descendants(matching: .any).matching(NSPredicate(format: "identifier IN %@", ["file-code", "file-markdown"])).firstMatch
            XCTAssertTrue(view.waitForExistence(timeout: 30), "\(file) did not open")
            // The app scrolls itself (`-showFileAutoScroll`): no XCUITest gestures while it measures.
            sleep(12)
            app.terminate()
        }
    }
}

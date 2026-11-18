import XCTest

/// UI version 5's code review (#827, docs/extensions/extensions.md "UI version 5: code review") on the schema's shared
/// snapshot (schema/extension/examples/showcase/code-review-v5.json), through the mock hub's showcase extension
/// (`MOCK_EXTENSIONS=1`, `TEST_RUNNER_PERSONA_EXTENSIONS_HUB`): the toolbar over the diffs (viewed so far, the open
/// threads, display options), a viewed file folded at first and saying why, a file checked viewed folding and counted,
/// the open threads' button unfolding the next one's file, the lines over a hunk asked of the extension (`context`;
/// the mock makes them up), a suggestion in a thread drawn as a change, a thread to resolve, a comment on the lines
/// between two line numbers with a suggestion started from them, a rename's similarity. In both styles (each launch passes
/// `-ui.style`), light and dark; with `PERSONA_SHOTS`, screenshots. `PERSONA_REVIEW_BEFORE=1`: only the screenshots, for
/// an app from before (the same steps, nothing checked).
final class ExtensionCodeReviewUITests: XCTestCase {
    private let env = ProcessInfo.processInfo.environment
    private var before: Bool { env["PERSONA_REVIEW_BEFORE"] == "1" }

    @MainActor func testTheReviewToolbarViewedMarksAndFolds() throws {
        guard let hub = env["PERSONA_EXTENSIONS_HUB"] else { throw XCTSkip("Needs MOCK_EXTENSIONS=1 (PERSONA_EXTENSIONS_HUB)") }
        continueAfterFailure = false
        for style in ["glass", "beautiful"] { for scheme in ["light", "dark"] {
            let look = scheme + "-" + style
            let app = XCUIApplication()
            let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
            // Whitespace shown at first, whatever a run that stopped part way left.
            app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-ui.style", style, "-ui.scheme", scheme,
                                   "-openExtension", "showcase", "-openExtensionPage", "snapshots",
                                   "-extension.code.ignoreWhitespace", "NO"]
            app.launchPersona()
            dismissSystemAlerts(wait: 4)
            let row = app.buttons["plugin-ui-action-snapshot-code-review-v5"]
            XCTAssertTrue(row.waitForExistence(timeout: 20), app.debugDescription)
            for _ in 0..<8 where !row.isHittable { app.swipeUp() }
            row.tap()
            XCTAssertTrue(app.descendants(matching: .any)["extension-ui-ready"].waitForExistence(timeout: 15), app.debugDescription)
            // A phone has the tree first, the diffs one step in.
            let all = app.buttons["code-open-main"]
            if all.waitForExistence(timeout: 5) { all.tap() }
            XCTAssertTrue(text(app, "src/upload/retry.go").waitForExistence(timeout: 10), app.debugDescription)
            shot(app, "diffs-" + look)
            if before {
                app.swipeUp()
                shot(app, "diffs-more-" + look)
                app.terminate()
                continue
            }

            // The toolbar: retry.go is viewed (the extension's mark), two threads are open (one is a draft).
            let viewed = app.descendants(matching: .any)["code-review-viewed"]
            XCTAssertTrue(viewed.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(viewed.label.contains("1 / 5"), viewed.label)
            let open = app.buttons["code-review-unresolved"]
            XCTAssertTrue(open.exists && open.label.contains("2"), open.debugDescription)
            // Viewed, so folded at first, saying why.
            let retryFolded = app.buttons["code-diff-folded-diff:src/upload/retry.go"]
            XCTAssertTrue(retryFolded.exists, app.debugDescription)

            // client.go checked viewed: counted, and folded.
            let box = app.descendants(matching: .any)["code-diff-viewed-src/upload/client.go"]
            XCTAssertTrue(box.waitForExistence(timeout: 5), app.debugDescription)
            box.tap()
            XCTAssertTrue(app.buttons["code-diff-folded-diff:src/upload/client.go"].waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(viewed.label.contains("2 / 5"), viewed.label)
            shot(app, "viewed-" + look)

            // The open threads' button: the first is on retry.go, which unfolds to show it.
            open.tap()
            XCTAssertTrue(retryFolded.waitForNonExistence(timeout: 5), app.debugDescription)
            // retry.go's draft thread suggests a change: drawn as one.
            XCTAssertTrue(text(app, "建议的修改").waitForExistence(timeout: 5), app.debugDescription)
            shot(app, "thread-" + look)

            // The display options: whitespace.
            app.buttons["code-review-options"].tap()
            let whitespace = app.buttons["忽略空白"].exists ? app.buttons["忽略空白"] : app.switches["忽略空白"]
            XCTAssertTrue(whitespace.waitForExistence(timeout: 5), app.debugDescription)
            shot(app, "options-" + look)
            // On and off again: the steps below read client.go's diff with whitespace shown (ignoring it redraws its
            // re-indented lines as unchanged; ExtensionCodeReviewTests checks that).
            whitespace.tap()
            app.buttons["code-review-options"].tap()
            XCTAssertTrue(whitespace.waitForExistence(timeout: 5), app.debugDescription)
            whitespace.tap()

            // client.go's 19 lines over its first hunk, asked of the extension (`context`) and shown.
            let gap = app.buttons["code-gap-diff:src/upload/client.go-0-all"]
            for _ in 0..<4 where !gap.exists { app.swipeDown() }
            if !gap.exists { app.buttons["code-diff-folded-diff:src/upload/client.go"].tap() } // folded when checked viewed
            XCTAssertTrue(gap.waitForExistence(timeout: 5), app.debugDescription)
            gap.tap()
            let shown = text(app, "src/upload/client.go: line 19")
            XCTAssertTrue(shown.waitForExistence(timeout: 5), app.debugDescription)
            reveal(app, shown)
            shot(app, "context-" + look)
            // Its thread can be resolved.
            XCTAssertTrue(app.buttons["code-thread-resolve-t3"].exists, app.debugDescription)

            // Two line numbers: a comment on the lines between, a suggestion started from them.
            tapNumber(app, text(app, "新增第 22 行"))
            tapNumber(app, text(app, "新增第 25 行"))
            XCTAssertTrue(text(app, "评论第 22–25 行").waitForExistence(timeout: 5), app.debugDescription)
            app.buttons["code-inline-suggest"].tap()
            let field = app.descendants(matching: .any)["code-inline-comment"]
            XCTAssertTrue((field.value as? String)?.contains("suggestion") == true, field.debugDescription)
            shot(app, "comment-range-" + look)
            app.buttons["code-inline-cancel"].tap()

            // A rename says how alike it is.
            let limits = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "相似 96%")).firstMatch
            for _ in 0..<6 where !limits.exists { app.swipeUp() }
            XCTAssertTrue(limits.exists, app.debugDescription)
            shot(app, "renamed-" + look)
            // Back as it was for the next run: client.go not viewed (its box is back up the page).
            for _ in 0..<12 where !box.isHittable { app.swipeDown() }
            box.tap()
            app.terminate()
        } }
    }

    /// A diff line's number (the gutter's first column), scrolled to first: a tap at a point does not scroll.
    @MainActor private func tapNumber(_ app: XCUIApplication, _ line: XCUIElement) {
        XCTAssertTrue(line.waitForExistence(timeout: 5))
        reveal(app, line)
        line.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0.5)).withOffset(CGVector(dx: 14, dy: 0)).tap()
    }

    /// Drags the page a little at a time, slowly and held at the end so it does not fling, until `element` is in the
    /// middle of the screen: clear of the bar at the bottom and of the header and its back row at the top (a fling once
    /// carried a line under the back row, and the tap on its number went back to the file tree).
    @MainActor private func reveal(_ app: XCUIApplication, _ element: XCUIElement) {
        let height = app.frame.height
        let lower = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.65))
        let upper = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.45))
        for _ in 0..<12 {
            let frame = element.frame
            if frame.maxY > height * 0.66 {
                lower.press(forDuration: 0.05, thenDragTo: upper, withVelocity: .slow, thenHoldForDuration: 0.2)
            } else if frame.minY < height * 0.3 {
                upper.press(forDuration: 0.05, thenDragTo: lower, withVelocity: .slow, thenHoldForDuration: 0.2)
            } else {
                return
            }
        }
    }

    @MainActor private func text(_ app: XCUIApplication, _ t: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", t)).firstMatch
    }

    /// The notification question and the icon change note are SpringBoard's.
    @MainActor private func dismissSystemAlerts(wait: TimeInterval = 1) {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        var deadline = Date.now.addingTimeInterval(wait)
        while Date.now < deadline {
            let alert = springboard.alerts.firstMatch
            guard alert.waitForExistence(timeout: 0.5) else { continue }
            guard let label = ["允许", "Allow", "好", "OK"].first(where: { alert.buttons[$0].exists }) else {
                XCTFail("a system alert nobody answers:\n\(springboard.debugDescription)")
                return
            }
            alert.buttons[label].tap()
            deadline = Date.now.addingTimeInterval(1.5)
        }
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let png = app.screenshot()
        let a = XCTAttachment(screenshot: png)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = env["PERSONA_SHOTS"] {
            try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir)
                .appendingPathComponent("code-review-\(before ? "before" : "after")-\(name).png"))
        }
    }
}

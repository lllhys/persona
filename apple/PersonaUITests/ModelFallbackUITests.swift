import XCTest

/// After an upstream refusal (#4489), against scripts/mock/mock_hub.py with MOCK_MODEL_FALLBACK=1 MOCK_MAIN_ACTIONS=1
/// (`TEST_RUNNER_PERSONA_MODEL_FALLBACK_HUB=http://127.0.0.1:<port>`): a terminal agent shows the refusal's line and a
/// card with only the /model step, and its API error offers no Continue; the Persona main conversation's card offers
/// Switch Back (「回到配置的模型」) and New Main Chat, and going back leaves only the pending note. There the line and the card
/// each say one plain sentence with the model's short name, the record under a closed Details (#5202). Light and dark,
/// and the main conversation at the largest text size in zh-Hans, English and Japanese, where the card's buttons stack;
/// `TEST_RUNNER_PERSONA_SHOTS=<dir>` keeps the screenshots (`fallback-<step>-<scheme>.png`).
/// `TEST_RUNNER_PERSONA_PHASE=before` takes the same screenshots on an older build, checking nothing it doesn't do.
final class ModelFallbackUITests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var before: Bool { env["PERSONA_PHASE"] == "before" }
    private let agent = "macbook/a1f3c2d4-0000-4000-8000-000000000006"
    private let main = "server/b2e4d6f8-0000-4000-8000-000000000001"

    @MainActor private func launch(_ hub: String, _ args: [String]) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair] + args
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 3) { springboard.buttons[label].tap() }
        return app
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        guard let dir = env["PERSONA_SHOTS"] else { return }
        // The app icon follows the color scheme, and the system says so in an alert over the page: closed first.
        let board = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        if board.alerts.firstMatch.waitForExistence(timeout: 2) {
            for label in ["OK", "好"] where board.alerts.buttons[label].exists { // i18n-ignore: the system's own button
                board.alerts.buttons[label].tap(); sleep(1); break
            }
        }
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "fallback-\(name).png"))
    }

    @MainActor private func labeled(_ app: XCUIApplication, _ text: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    @MainActor func testRefusalLineFallbackCardAndGoingBack() throws {
        guard let hub = env["PERSONA_MODEL_FALLBACK_HUB"] else {
            throw XCTSkip("needs mock_hub.py with MOCK_MODEL_FALLBACK=1 MOCK_MAIN_ACTIONS=1 (PERSONA_MODEL_FALLBACK_HUB)")
        }
        continueAfterFailure = false
        for scheme in ["light", "dark", "large", "large-en", "large-ja"] {
            _ = try? Data(contentsOf: URL(string: hub + "/mock/model-fallback/reset")!)

            let large = scheme.hasPrefix("large")
            let language: [String] = switch scheme {
            case "large-en": ["-AppleLanguages", "(en)", "-AppleLocale", "en_US"]
            case "large-ja": ["-AppleLanguages", "(ja)", "-AppleLocale", "ja_JP"]
            default: []
            }
            let look = large
                ? ["-ui.scheme", "light", "-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"] + language
                : ["-ui.scheme", scheme]
            if !large {
                // A terminal agent: the refusal's line after the exchange, the card with only the /model step, no Continue.
                let app = launch(hub, ["-dev.openChat", "", "-openSession", agent] + look)
                let card = labeled(app, "/model")
                if !before {
                    XCTAssertTrue(app.descendants(matching: .any)["refusal-notice"].firstMatch.waitForExistence(timeout: 20), app.debugDescription)
                    XCTAssertTrue(card.waitForExistence(timeout: 10), "the fallback card")
                    XCTAssertTrue(card.label.contains("Sonnet 5.5"), card.label)
                    XCTAssertFalse(app.buttons["回到配置的模型"].exists, "a terminal session can't go back from here")
                    XCTAssertFalse(app.buttons["继续"].exists, "Continue would go to the same model")
                } else {
                    _ = labeled(app, "API Error").waitForExistence(timeout: 20)
                }
                shot(app, "agent-\(scheme)")
            }

            // The Persona main conversation: one plain sentence each, both buttons; going back leaves the pending note
            // and no button.
            let app = launch(hub, ["-tab", "assistant", "-openAssistant", main] + look)
            let back = app.buttons["回到配置的模型"].firstMatch
            if scheme == "large-en" || scheme == "large-ja" {
                // The card's words in English and Japanese: one under another, each whole on one line in the
                // screenshot (the longer Back to Configured Model ended in "…" there, and so did the Japanese).
                let (backTitle, newTitle) = scheme == "large-en"
                    ? ("Switch Back", "New Main Chat") : ("元に戻す", "新しいチャット")
                let card = app.descendants(matching: .any)["model-fallback-card"].firstMatch
                let backHere = card.buttons[backTitle].firstMatch
                XCTAssertTrue(backHere.waitForExistence(timeout: 20), app.debugDescription)
                if !before {
                    XCTAssertGreaterThanOrEqual(card.buttons[newTitle].firstMatch.frame.minY, backHere.frame.maxY - 1,
                                                "the buttons stack at the largest size")
                }
                shot(app, "main-\(scheme)")
                continue
            }
            if before {
                _ = labeled(app, "API Error").waitForExistence(timeout: 20)
                shot(app, "main-\(scheme)")
                continue
            }
            XCTAssertTrue(back.waitForExistence(timeout: 20), app.debugDescription)
            XCTAssertTrue(app.buttons["新建主对话"].firstMatch.exists, "New Main Chat on the main conversation")
            XCTAssertTrue(labeled(app, "这个对话现在用 Sonnet 5.5 回复").exists, "the card's plain sentence")
            // At the largest size the line may be above the screen in the lazy list: the card only, its buttons one
            // under another (side by side each was cut to a letter), and the shot.
            if large {
                let card = app.descendants(matching: .any)["model-fallback-card"].firstMatch
                XCTAssertTrue(card.buttons["回到配置的模型"].firstMatch.exists, "the card's buttons")
                XCTAssertGreaterThanOrEqual(card.buttons["新建主对话"].firstMatch.frame.minY, card.buttons["回到配置的模型"].firstMatch.frame.maxY - 1,
                                            "the buttons stack at the largest size")
                shot(app, "main-\(scheme)")
                continue
            }
            let line = labeled(app, "上游拒绝了这次请求，已改用 Sonnet 5.5")
            XCTAssertTrue(line.waitForExistence(timeout: 10), "the refusal's plain sentence")
            XCTAssertFalse(labeled(app, "拒绝原文").exists, "the record stays under a closed Details")
            shot(app, "main-\(scheme)")
            // The identifier may land on the disclosure's own button.
            let details = app.descendants(matching: .any)["refusal-details"].firstMatch
            XCTAssertTrue(details.exists, "the refusal's Details")
            let toggle = details.elementType == .button ? details : details.buttons.firstMatch
            toggle.tap()
            XCTAssertTrue(labeled(app, "拒绝原文").waitForExistence(timeout: 5), "Details opens to the record")
            shot(app, "main-details-\(scheme)")
            toggle.tap()
            // The card's own Details: the models and the error since.
            let cardDetails = app.descendants(matching: .any)["model-fallback-card"].firstMatch.buttons["详情"].firstMatch
            XCTAssertTrue(cardDetails.exists, "the card's Details")
            cardDetails.tap()
            XCTAssertTrue(labeled(app, "回退模型：Sonnet 5.5").waitForExistence(timeout: 5), "the card's Details opens to the record")
            shot(app, "main-card-details-\(scheme)")
            cardDetails.tap()
            back.tap()
            XCTAssertTrue(labeled(app, "下次启动将使用配置的模型").waitForExistence(timeout: 15), app.debugDescription)
            XCTAssertFalse(app.buttons["回到配置的模型"].exists)
            XCTAssertTrue(labeled(app, "配置的模型：Opus 5.5").exists)
            shot(app, "main-pending-\(scheme)")
        }
    }
}

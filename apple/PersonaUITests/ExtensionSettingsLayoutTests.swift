import XCTest

/// Runs the shared host settings shell against three public synthetic extensions.
/// Unsupported actions remain visible; the app never sends a removal request.
final class ExtensionSettingsLayoutTests: XCTestCase {
    @MainActor func testThreeExtensionsFollowTheHostLayout() throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["PERSONA_EXTENSIONS_HUB"] else {
            throw XCTSkip("Requires the dedicated extension mock hub")
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let look = env["PERSONA_EXTENSIONS_APPEARANCE"] ?? "light"
        let app = XCUIApplication()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for id in ["telegram", "example-gateway", "voice"] {
            app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openExtension", id]
            app.launchPinned()
            for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 2) {
                springboard.buttons[label].tap()
            }
            let enabled = app.switches["plugin-enabled"]
            XCTAssertTrue(enabled.waitForExistence(timeout: 20), "Missing first Enabled control: \(app.debugDescription)")
            capture(app, "\(id)-top-\(look)", env: env)
            var seen = Set<String>()
            var ordered: [String] = []
            let headings = ["extension-section-enabled", "extension-section-configuration", "extension-section-information", "extension-section-updates", "extension-section-danger"]
            for _ in 0..<16 {
                let visible = headings.compactMap { heading -> (String, CGFloat)? in
                    let element = app.descendants(matching: .any).matching(identifier: heading).firstMatch
                    guard element.exists, element.isHittable else { return nil }
                    return (heading, element.frame.minY)
                }.sorted { $0.1 < $1.1 }
                for (heading, _) in visible where seen.insert(heading).inserted { ordered.append(heading) }
                let removal = app.buttons["extension-uninstall"]
                if removal.exists && removal.isHittable {
                    XCTAssertFalse(removal.isEnabled, "A safe removal preview is unavailable")
                    break
                }
                app.swipeUp()
            }
            XCTAssertEqual(ordered, headings, "Wrong or missing host section order: \(app.debugDescription)")
            XCTAssertTrue(app.buttons["extension-check-updates"].exists)
            XCTAssertTrue(app.switches["plugin-auto-upgrade"].exists)
            if id != "example-gateway" {
                XCTAssertFalse(app.buttons["extension-check-updates"].isEnabled)
                XCTAssertFalse(app.switches["plugin-auto-upgrade"].isEnabled)
            }
            capture(app, "\(id)-bottom-\(look)", env: env)
            app.terminate()
        }
    }

    @MainActor private func capture(_ app: XCUIApplication, _ name: String, env: [String: String]) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let directory = env["PERSONA_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: directory).appendingPathComponent("ios-\(name).png"))
        }
    }
}

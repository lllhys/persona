import XCTest

/// Pictures in a reply fit the text (#9474): a lone one is one height and its shape's width, runs are squares (the
/// sixth counting the rest), one that did not load is a small tile, and a tap opens the viewer on all of the reply's.
///   PORT=18961 MOCK_AGENTS=1 MOCK_REPLY_IMAGES=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18961 xcodebuild test … -only-testing:PersonaUITests/ReplyPicturesUITests
/// Light, then dark.
final class ReplyPicturesUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_AGENTS=1 MOCK_REPLY_IMAGES=1 (PERSONA_MOCK_HUB)")
        }
    }

    @MainActor private func shot(_ name: String, _ scheme: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "reply-pictures \(name) \(UITestStyle.name) \(scheme)"
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor private func picture(_ label: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: "reply-picture").matching(NSPredicate(format: "label == %@", label)).firstMatch
    }

    /// Within a point of `want` (frames come back in points, rounded by the screen's scale).
    private func near(_ got: CGFloat, _ want: CGFloat) -> Bool { abs(got - want) <= 1 }

    @MainActor func testPicturesFitTheReplyAndOpenTogether() {
        for scheme in ["light", "dark"] { check(scheme) }
    }

    @MainActor private func check(_ scheme: String) {
        let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"]!
        app?.terminate()
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000008", "-ui.scheme", scheme]
        app.launchPinned()
        acceptIconAlert() // the icon follows the scheme

        // The reply's end: the run of eight (six squares, the sixth counting two more) and the one that failed.
        let failed = app.descendants(matching: .any)["reply-picture-failed"]
        XCTAssertTrue(failed.waitForExistence(timeout: 30), "the picture the hub could not fetch did not become a tile")
        XCTAssertTrue(near(failed.frame.width, 80) && near(failed.frame.height, 80), "failed tile \(failed.frame)")
        let more = app.descendants(matching: .any)["reply-picture-more"]
        XCTAssertTrue(more.waitForExistence(timeout: 10), "no +N on the sixth square")
        XCTAssertTrue(near(more.frame.width, more.frame.height), "the counted square is not square: \(more.frame)")
        sleep(2)
        shot("end", scheme)

        // Up the reply: the lone pictures are one height, as wide as their shape.
        let kitchen = picture("厨房")
        for _ in 0 ..< 6 where !kitchen.isHittable { app.swipeDown() }
        XCTAssertTrue(kitchen.waitForExistence(timeout: 10), "no kitchen picture")
        XCTAssertTrue(near(kitchen.frame.height, 240) && near(kitchen.frame.width, 180), "portrait \(kitchen.frame), want 180 × 240")
        let room = picture("客厅")
        XCTAssertTrue(room.exists && near(room.frame.height, 240) && near(room.frame.width, 320), "landscape \(room.frame), want 320 × 240")
        let building = picture("楼"), street = picture("街道")
        XCTAssertTrue(building.exists && street.exists, "the pair is not drawn")
        XCTAssertTrue(near(building.frame.width, building.frame.height) && near(building.frame.width, street.frame.width)
            && near(building.frame.minY, street.frame.minY), "the pair is not two squares side by side: \(building.frame) \(street.frame)")
        sleep(1)
        shot("lone", scheme)

        // A tap opens the viewer on every picture of the reply (14), at the one tapped.
        room.tap()
        let title = app.descendants(matching: .any)["viewer-title"]
        XCTAssertTrue(title.waitForExistence(timeout: 10), "the viewer did not open")
        XCTAssertTrue(title.label.contains("4 / 14"), "viewer at \(title.label), want 4 / 14")
        sleep(2)
        shot("viewer", scheme)
    }
}

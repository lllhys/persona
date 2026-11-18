import XCTest

/// The auxiliary first-item launch follows the Inbox's filters, independently of the explicit-item send and
/// translation tests. Run against scripts/mock/mock_hub.py's default Inbox with MOCK_MANAGERS=0.
final class InboxFirstItemUITests: XCTestCase {
    @MainActor func testOpenInboxFirstOpensFirstOpenItemMatchingFilters() async throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_MANAGERS=0 (PERSONA_MOCK_HUB)")
        }

        // Check the fixture before launching: several open mails match, and their first is not the whole Inbox's first.
        let request = URLRequest(url: try XCTUnwrap(URL(string: hub + "/api/v1/inbox?sources=all")), timeoutInterval: 10)
        let (data, response) = try await URLSession.shared.data(for: request)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
        let body = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        let items = try XCTUnwrap(body["items"] as? [[String: Any]])
        let open = items.filter { $0["done_at"] == nil || $0["done_at"] is NSNull }
        let matching = open.filter { $0["kind"] as? String == "mail" && $0["account"] as? String == "acc1" }
        XCTAssertGreaterThan(matching.count, 1, "the fixture must offer a choice of open mails")
        let first = try XCTUnwrap(matching.first)
        XCTAssertEqual(first["id"] as? String, "mail:acc1:t4:504", "the default mock Inbox fixture changed")
        XCTAssertNotEqual(open.first?["id"] as? String, first["id"] as? String, "the filters must change the first item")
        let title = try XCTUnwrap(first["title"] as? String)

        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-inbox.kind", "mail", "-inbox.source", "acc1",
                               "-openInboxFirst", "YES", "-translate.enabled", "NO",
                               "-ui.scheme", ProcessInfo.processInfo.environment["PERSONA_UI_SCHEME"] ?? "light"]
        app.launchPinned()

        // Both controls belong to the opened detail; the same title on an Inbox row is insufficient.
        XCTAssertTrue(app.buttons["inbox.detail.source"].waitForExistence(timeout: 20), "openInboxFirst did not open an item detail")
        XCTAssertTrue(app.navigationBars[title].waitForExistence(timeout: 5), "openInboxFirst opened the wrong item")
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = "inbox-first-item"
        shot.lifetime = .keepAlways
        add(shot)
    }
}

import XCTest

/// Files sent to an agent (docs/protocol/protocol.md「Sending files」, #8361), against scripts/mock/mock_hub.py
/// (TEST_RUNNER_PERSONA_UPLOAD_HUB=http://127.0.0.1:<port>; MOCK_UPLOAD_KBPS slows it down enough to see the bar).
final class FileUploadTests: XCTestCase {
    private let widget = "a1f3c2d4-0000-4000-8000-000000000001"

    private func launch(_ args: [String]) throws -> XCUIApplication {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_UPLOAD_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_UPLOAD_HUB)")
        }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/" + widget] + args
        app.launchPinned()
        return app
    }

    /// How many times the mock was asked for `path` (GET, any query).
    private func hits(_ path: String) -> Int {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_UPLOAD_HUB"],
              let data = try? Data(contentsOf: URL(string: hub + "/mock/hits")!),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return 0 }
        return (obj["hits"] as? [String: Int])?["GET " + path] ?? 0
    }

    /// Opening a chat at launch builds it once (#2061): a push before the list appeared previously
    /// and the chat was built three times, its items read three times and `-openUploadDemo` uploading twice.
    @MainActor func testOpeningAChatAtLaunchReadsItOnce() throws {
        let items = "/api/v1/sessions/macbook/\(widget)/items"
        let before = hits(items)
        let app = try launch([])
        let plus = app.buttons["shell-composer-add"].firstMatch
        XCTAssertTrue(plus.waitForExistence(timeout: 15), "the chat did not open:\n\(app.debugDescription)")
        sleep(3) // a second and third build came within half a second of the first
        XCTAssertEqual(hits(items) - before, 1, "the chat's items read once")
    }

    /// The + menu offers Files next to Photos and Take Photo.
    @MainActor func testMenuHasFiles() throws {
        let app = try launch([])
        let plus = app.buttons["shell-composer-add"].firstMatch
        XCTAssertTrue(plus.waitForExistence(timeout: 15), "no + button:\n\(app.debugDescription)")
        plus.tap()
        XCTAssertTrue(app.buttons["文件"].waitForExistence(timeout: 5), "no 文件 item:\n\(app.debugDescription)")
    }

    /// A file picked goes up as a card above the box (#8361): typing meanwhile is never interrupted, the words stay
    /// exactly as typed, and the message carries the file as a `<persona-file kind="file">` tag after them, kept in the
    /// computer's uploads.
    @MainActor func testUploadBecomesACard() throws {
        let app = try launch(["-openUploadDemo", "季度报告.pdf"])
        let cancel = app.buttons["取消上传 季度报告.pdf"]
        XCTAssertTrue(cancel.waitForExistence(timeout: 15), "no card on its way:\n\(app.debugDescription)")
        let field = app.textFields.firstMatch.exists ? app.textFields.firstMatch : app.textViews.firstMatch
        field.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.9)).tap() // the end of a draft left by an earlier run
        let left = (field.value as? String ?? "").count
        if left > 0 { field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: left + 2)) }
        field.typeText("look at")
        let ready = app.descendants(matching: .any)["file-attachment-ready"].firstMatch
        XCTAssertTrue(ready.waitForExistence(timeout: 90), "the file did not get there:\n\(app.debugDescription)")
        XCTAssertEqual(field.value as? String, "look at", "the words changed while the file uploaded")
        let before = try sent().count
        let send = app.buttons.matching(NSPredicate(format: "identifier == %@ AND enabled == true", "shell-composer-send")).firstMatch
        XCTAssertTrue(send.waitForExistence(timeout: 10), "no send button:\n\(app.debugDescription)")
        send.tap()
        let went = NSPredicate { _, _ in ((try? self.sent().count) ?? 0) > before }
        expectation(for: went, evaluatedWith: nil)
        waitForExpectations(timeout: 15)
        let text = try XCTUnwrap(try sent().last)
        // The tag's name follows the hub's naming (docs/protocol/rolling-upgrade-compatibility.md): either is the file.
        XCTAssertNotNil(text.range(of: #"^look at\n\n<[a-z]+-file kind="file" path="/Users/me/\.persona/uploads/"#, options: .regularExpression), text)
        XCTAssertTrue(text.contains("name=\"季度报告.pdf\""), text)
        XCTAssertFalse(ready.exists, "the card stayed above the box after sending")
    }

    /// What the mock received as messages, oldest first.
    private func sent() throws -> [String] {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_UPLOAD_HUB"] else { return [] }
        let data = try Data(contentsOf: URL(string: hub + "/mock/sent")!)
        struct Output: Decodable { struct Message: Decodable { var text: String? }; var sent: [Message] }
        return try JSONDecoder().decode(Output.self, from: data).sent.compactMap(\.text)
    }
}

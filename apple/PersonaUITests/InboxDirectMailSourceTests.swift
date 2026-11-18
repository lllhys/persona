import XCTest

/// A mail item drawn by an extension opens its native source with the extension's one explicit action. Opening the
/// extension detail alone builds no mail reader and reports nothing seen. Back restores the same extension page and action
/// without another source action or seen report.
///
/// Run against Fixtures/inbox_direct_source_mail_hub.py (PERSONA_MOCK_HUB), whose lifecycle and free-port contract match
/// the suite's other local wrappers. TEST_RUNNER_PERSONA_SHOTS=<dir> keeps the screen after that one action;
/// TEST_RUNNER_PERSONA_UI_SCHEME=dark captures the dark rendering.
final class InboxDirectMailSourceTests: XCTestCase {
    private static let item = "mail:acc1:t1:501"
    private static let account = "acc1"
    private static let uid = 501
    private static let extensionMarker = "Extension-rendered mail detail"
    /// The stock mail's own words, past its preview: only the native reader draws them.
    private static let mailBody = "议程见附件，有问题随时说"

    private var app: XCUIApplication!
    private var hub = ""
    private var regularWidth = false
    private var layout: String { regularWidth ? "regular" : "phone" }

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs apple/PersonaUITests/Fixtures/inbox_direct_source_mail_hub.py (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.name = "inbox-direct-mail-source-failure-\(layout)"
            shot.lifetime = .keepAlways
            add(shot)
        }
        app?.terminate()
        super.tearDown()
    }

    @MainActor func testExtensionOpenLoadsNativeMailAndReportsSeenWithOneAction() throws {
        try resetFixture()
        let baseline = try mailSeenReports().count
        openExtensionDetail()

        let extensionPage = element("inbox-plugin-item")
        XCTAssertTrue(extensionPage.waitForExistence(timeout: 20),
                      "the mail did not open as its extension-rendered detail:\n\(app.debugDescription)")
        let marker = app.staticTexts[Self.extensionMarker].firstMatch
        XCTAssertTrue(marker.waitForExistence(timeout: 10), "the wrapper's extension View is absent:\n\(app.debugDescription)")
        let open = element("plugin-ui-action-open")
        requireHittable(open, "the extension View's Open native mail action")

        // The extension detail has not constructed the native reader.
        XCTAssertFalse(mailBodyElement().exists, "the native mail reader was built before the extension's explicit action")
        sleep(2) // A constructed reader fetches immediately, then reports seen; give any erroneous report time to arrive.
        XCTAssertEqual(try mailSeenReports().count, baseline,
                       "opening the extension-rendered detail reported the mail seen before its explicit action")
        XCTAssertFalse(mailBodyElement().exists, "the native mail body appeared before the extension's explicit action")

        // This is the test's only source-opening action.
        open.tap()
        XCTAssertTrue(mailBodyElement().waitForExistence(timeout: 20),
                      "the extension's one open action did not load the native mail body:\n\(app.debugDescription)")
        XCTAssertGreaterThan(try waitForSeenReports(after: baseline).count, baseline,
                             "the direct-opened native mail reader did not report UID \(Self.uid) seen")
        let back = element("inbox-plugin-item-back")
        requireHittable(back, "the native source's Back-to-extension control")
        shot("after-one-action")

        let seenCountAfterOpen = try mailSeenReports().count
        back.tap()
        XCTAssertTrue(extensionPage.waitForExistence(timeout: 10),
                      "Back did not restore the extension-rendered mail detail:\n\(app.debugDescription)")
        XCTAssertTrue(marker.waitForExistence(timeout: 10),
                      "Back did not restore the extension View:\n\(app.debugDescription)")
        requireHittable(open, "the extension View's Open native mail action after Back")
        sleep(2) // returning to the extension must not reconstruct the reader or report it seen again
        XCTAssertEqual(try mailSeenReports().count, seenCountAfterOpen,
                       "Back to the extension page added another seen report for UID \(Self.uid)")
    }

    // MARK: - Driving the app

    @MainActor private func openExtensionDetail() {
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let environment = ProcessInfo.processInfo.environment
        app.launchArguments = ["-pairURL", pair, "-ui.scheme", environment["PERSONA_UI_SCHEME"] ?? "light",
                               "-tab", "mail", "-dev.openChat", "", "-openSplitFresh", "YES",
                               "-inbox.kind", "", "-inbox.source", "", "-inbox.grouping", "",
                               "-openInboxItem", Self.item]
        app.launchPinned()
        acceptIconAlert()
        XCTAssertTrue(app.windows.firstMatch.waitForExistence(timeout: 10), "no app window")
        regularWidth = app.windows.firstMatch.frame.width > 600
    }

    @MainActor private func mailBodyElement() -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", Self.mailBody)).firstMatch
    }

    @MainActor private func element(_ id: String) -> XCUIElement {
        app.descendants(matching: .any)[id].firstMatch
    }

    @MainActor private func requireHittable(_ element: XCUIElement, _ what: String) {
        for _ in 0..<6 where !(element.exists && element.isHittable) {
            app.swipeUp(velocity: .slow)
            _ = element.waitForExistence(timeout: 1)
        }
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == true AND hittable == true"),
                                                    object: element)
        XCTAssertEqual(XCTWaiter().wait(for: [expectation], timeout: 10), .completed,
                       "\(what) is not on screen:\n\(app.debugDescription)")
    }

    @MainActor private func shot(_ name: String) {
        let png = app.screenshot().pngRepresentation
        let attachment = XCTAttachment(data: png, uniformTypeIdentifier: "public.png")
        attachment.name = "inbox-direct-mail-source-\(name)-\(layout)"
        attachment.lifetime = .keepAlways
        add(attachment)
        let environment = ProcessInfo.processInfo.environment
        if let directory = environment["PERSONA_SHOTS"] {
            let appearance = environment["PERSONA_SHOTS_APPEARANCE"] ?? environment["PERSONA_UI_SCHEME"] ?? "light"
            let file = "inbox-direct-mail-source-\(name)-\(layout)-\(UITestStyle.name)-\(appearance).png"
            try? png.write(to: URL(fileURLWithPath: directory).appending(path: file))
        }
    }

    // MARK: - What the stock mock was told

    private func resetFixture() throws {
        try post("/api/v1/inbox/done", body: ["ids": [Self.item], "done": false])
        try post("/api/v1/mail/messages/\(Self.account)/\(Self.uid)/flags", body: ["mailbox": "INBOX", "seen": false])
    }

    private func post(_ path: String, body: [String: Any]) throws {
        let url = try XCTUnwrap(URL(string: hub + path))
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        let answered = expectation(description: "POST " + path)
        URLSession.shared.dataTask(with: request) { _, response, error in
            XCTAssertNil(error)
            XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
            answered.fulfill()
        }.resume()
        wait(for: [answered], timeout: 10)
    }

    /// Only this message's seen writes; the reset's unseen write and other accounts or UIDs are left out.
    private func mailSeenReports() throws -> [[String: Any]] {
        let data = try Data(contentsOf: try XCTUnwrap(URL(string: hub + "/mock/mail")))
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        let flags = try XCTUnwrap(object["flags"] as? [[String: Any]])
        return flags.filter {
            $0["account_id"] as? String == Self.account
                && ($0["uid"] as? NSNumber)?.intValue == Self.uid
                && $0["mailbox"] as? String == "INBOX"
                && $0["seen"] as? Bool == true
        }
    }

    private func waitForSeenReports(after baseline: Int, timeout: TimeInterval = 10) throws -> [[String: Any]] {
        let deadline = Date.now.addingTimeInterval(timeout)
        var current = try mailSeenReports()
        while current.count <= baseline, Date.now < deadline {
            usleep(250_000)
            current = try mailSeenReports()
        }
        return current
    }
}

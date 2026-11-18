import XCTest

/// Share › Persona from the system's own apps (#2282): Safari (the page's title and link, read by `SharePage.js`),
/// Photos (a seeded picture) and Files (a seeded PDF in On My iPhone), each through its real share sheet, sent to the
/// main conversation of scripts/mock/mock_hub.py (TEST_RUNNER_PERSONA_MOCK_HUB, which Persona pairs with first: the
/// extension reads that pairing from the keychain). Run with scripts/app/share-from-apps.sh, which seeds Photos and
/// Files and needs a stock simulator (the slim one has no Safari to drive). TEST_RUNNER_PERSONA_SHOTS=<dir> keeps each
/// screen, named with TEST_RUNNER_PERSONA_SHOTS_APPEARANCE.
final class ShareFromAppsTests: XCTestCase {
    private var hub: String!
    /// What scripts/app/share-from-apps.sh put in Files' On My iPhone.
    static let seededFile = "Share test.pdf"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)") }
        self.hub = hub
        _ = try? Data(contentsOf: URL(string: hub + "/mock/reset")!)
    }

    /// Safari: the page's title and its link reach the hub together, once each.
    @MainActor func testSafariSendsThePagesTitleAndLink() throws {
        pair()
        let page = hub + "/mock/share-page"
        let safari = XCUIApplication(bundleIdentifier: "com.apple.mobilesafari")
        safari.launch()
        safari.open(URL(string: page)!)
        skipFirstRun(safari)
        XCTAssertTrue(safari.staticTexts["A Page Worth Reading"].waitForExistence(timeout: 30), "Safari did not show the page")
        tapShare(in: safari)
        let sheet = pickPersona(in: safari)
        let shared = sheet.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "A Page Worth Reading")).firstMatch
        XCTAssertTrue(shared.waitForExistence(timeout: 10), "the sheet does not show the page's title")
        shot("share-safari")
        send(in: sheet)
        let sent = try XCTUnwrap(waitForSent { ($0["text"] as? String ?? "").contains(page) }, "the hub got nothing with the page's link")
        let text = sent["text"] as? String ?? ""
        XCTAssertTrue(text.contains("A Page Worth Reading\n" + page), "title and link are not together: \(text)")
        XCTAssertEqual(text.components(separatedBy: page).count, 2, "the link is in the message twice: \(text)")
        XCTAssertEqual(text.components(separatedBy: "A Page Worth Reading").count, 2, "the title is in the message twice: \(text)")
    }

    /// Photos: the seeded picture goes as a picture.
    @MainActor func testPhotosSendsThePicture() throws {
        pair()
        let photos = XCUIApplication(bundleIdentifier: "com.apple.mobileslideshow")
        photos.launch()
        skipFirstRun(photos)
        // Photos reopens where it was left: a run before may have left one picture open, which is the one to share then.
        let share = photos.buttons.matching(NSPredicate(format: "identifier == 'ShareButton' OR label IN {'Share', '共享', '共有'}")).firstMatch
        if !(share.waitForExistence(timeout: 5) && share.isHittable) {
            let picture = photos.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'PXGGridLayout-Info' OR label BEGINSWITH 'Photo'")).firstMatch
            XCTAssertTrue(picture.waitForExistence(timeout: 30), "no picture in Photos (scripts/app/share-from-apps.sh seeds one)")
            picture.tap()
        }
        tapShare(in: photos)
        let sheet = pickPersona(in: photos)
        shot("share-photos")
        send(in: sheet)
        let sent = try XCTUnwrap(waitForSent { !(($0["images"] as? [Any]) ?? []).isEmpty }, "the hub got no picture")
        XCTAssertEqual((sent["images"] as? [Any])?.count, 1, "not one picture: \(sent)")
    }

    /// Files: the seeded PDF is uploaded and its path follows in the message; the sheet shows its size and the limit.
    @MainActor func testFilesSendsTheFile() throws {
        pair()
        let files = XCUIApplication(bundleIdentifier: "com.apple.DocumentsApp")
        files.launch()
        skipFirstRun(files)
        let file = openOnMyIPhone(files)
        file.press(forDuration: 1.2)
        let share = files.buttons.matching(NSPredicate(format: "label IN {'Share', '共享', '共有'}")).firstMatch
        XCTAssertTrue(share.waitForExistence(timeout: 10), "no Share in the file's menu")
        share.tap()
        let sheet = pickPersona(in: files)
        XCTAssertTrue(sheet.descendants(matching: .any)["share-size-total"].waitForExistence(timeout: 10), "no total size under the files")
        shot("share-files")
        send(in: sheet)
        let name = (Self.seededFile as NSString).deletingPathExtension
        XCTAssertNotNil(waitForSent { ($0["text"] as? String ?? "").contains(name) }, "the file's path is not in the message")
    }

    // MARK: - Steps

    /// Persona paired with the mock hub (the extension reads that pairing), then left in the background.
    @MainActor private func pair() {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let scheme = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] ?? "light"
        app.launchArguments = ["-pairURL", pair, "-ui.scheme", scheme]
        app.launchPinned()
        LiveActivityShots.allowNotifications()
        XCTAssertTrue(app.wait(for: .runningForeground, timeout: 20))
        // Paired (saved to the keychain) before the app first asks the hub for its sessions.
        let deadline = Date.now.addingTimeInterval(20)
        var asked = false
        while !asked, Date.now < deadline {
            if let data = try? Data(contentsOf: URL(string: hub + "/mock/hits")!),
               let hits = (try? JSONSerialization.jsonObject(with: data) as? [String: Any])?["hits"] as? [String: Any] {
                asked = hits.keys.contains { $0.hasPrefix("GET /api/v1/sessions") }
            }
            if !asked { RunLoop.current.run(until: .now.addingTimeInterval(0.5)) }
        }
        XCTAssertTrue(asked, "Persona never asked the mock hub for its sessions: not paired")
    }

    /// A system app's first-run screens (What's New, Welcome), when they are there.
    @MainActor private func skipFirstRun(_ app: XCUIApplication) {
        for _ in 0..<3 {
            let next = app.buttons.matching(NSPredicate(format: "label IN {'Continue', 'Not Now', 'Done', 'OK', '继续', '以后', '完成', '好', '続ける', '今はしない', '完了'}")).firstMatch
            guard next.waitForExistence(timeout: 3) else { return }
            next.tap()
        }
    }

    /// The app's Share button: on the bar, or in the page's More menu (Safari's compact bar).
    @MainActor private func tapShare(in app: XCUIApplication) {
        let share = app.buttons.matching(NSPredicate(format: "identifier == 'ShareButton' OR label IN {'Share', '共享', '共有'}")).firstMatch
        if share.waitForExistence(timeout: 5), share.isHittable {
            share.tap()
            return
        }
        let more = app.buttons.matching(NSPredicate(format: "label IN {'More', 'Page Menu', '更多', 'その他'} OR identifier IN {'MoreMenuButton', 'PageFormatMenuButton'}")).firstMatch
        XCTAssertTrue(more.waitForExistence(timeout: 10), "no Share or More button:\n\(app.debugDescription)")
        more.tap()
        XCTAssertTrue(share.waitForExistence(timeout: 10), "no Share in the menu")
        share.tap()
    }

    /// Persona in the system share sheet, then the extension's sheet (drawn in the sharing app).
    @MainActor private func pickPersona(in app: XCUIApplication) -> XCUIApplication {
        let persona = app.descendants(matching: .any).matching(NSPredicate(format: "label == 'Persona' AND elementType IN {%d, %d}",
                                                                           XCUIElement.ElementType.cell.rawValue, XCUIElement.ElementType.button.rawValue)).firstMatch
        XCTAssertTrue(persona.waitForExistence(timeout: 20), "Persona is not in the share sheet:\n\(app.debugDescription)")
        persona.tap()
        let main = app.descendants(matching: .any)["share-target-main"]
        XCTAssertTrue(main.waitForExistence(timeout: 30), "the extension's sheet did not list the main conversation")
        return app
    }

    @MainActor private func send(in sheet: XCUIApplication) {
        let send = sheet.navigationBars.buttons.matching(NSPredicate(format: "label IN {'发送', 'Send', '送信'}")).firstMatch
        XCTAssertTrue(send.waitForExistence(timeout: 5) && send.isEnabled, "Send is off")
        send.tap()
    }

    /// Browse › On My iPhone, and the seeded file there.
    @MainActor private func openOnMyIPhone(_ files: XCUIApplication) -> XCUIElement {
        let file = files.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", (Self.seededFile as NSString).deletingPathExtension)).firstMatch
        // Files opens where it was last: On My iPhone after the first run.
        if file.waitForExistence(timeout: 10) { return file }
        let browse = files.buttons.matching(NSPredicate(format: "label IN {'Browse', '浏览', 'ブラウズ'}")).firstMatch
        if browse.waitForExistence(timeout: 5) { browse.tap() }
        let place = files.descendants(matching: .any).matching(NSPredicate(format: "label IN {'On My iPhone', '我的iPhone', '我的 iPhone', 'このiPhone内'}")).firstMatch
        XCTAssertTrue(place.waitForExistence(timeout: 10), "no On My iPhone in Files:\n\(files.debugDescription)")
        place.tap()
        XCTAssertTrue(file.waitForExistence(timeout: 10), "\(Self.seededFile) is not in On My iPhone (scripts/app/share-from-apps.sh seeds it)")
        return file
    }

    /// The first send since the test began that `matches`, waiting up to `timeout` (GET /mock/sent keeps each body).
    private func waitForSent(timeout: TimeInterval = 30, _ matches: ([String: Any]) -> Bool) -> [String: Any]? {
        let deadline = Date.now.addingTimeInterval(timeout)
        while Date.now < deadline {
            if let data = try? Data(contentsOf: URL(string: hub + "/mock/sent")!),
               let sent = (try? JSONSerialization.jsonObject(with: data) as? [String: Any])?["sent"] as? [[String: Any]],
               let hit = sent.first(where: matches) {
                return hit
            }
            Thread.sleep(forTimeInterval: 0.5)
        }
        return nil
    }

    @MainActor private func shot(_ name: String) {
        let s = XCUIScreen.main.screenshot()
        let mode = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"] ?? "light"
        let a = XCTAttachment(screenshot: s)
        a.name = "\(name)-\(mode)"
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name)-\(mode).png"))
        }
    }
}

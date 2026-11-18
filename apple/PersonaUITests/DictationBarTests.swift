import XCTest

/// The dictation bar (#5843): the shell composer's mic tapped turns it into [✕] wave and ■ ↑. ✕ drops what was heard,
/// ■ puts it into the draft without sending, ↑ puts it in and sends; ■ is where the mic was, so a second tap there stops
/// (#9619). Held, the mic dictates while held and letting go keeps the words; a slide left drops them (#9619). Words come
/// from `-showDictationSample` (a simulator has no microphone). Against scripts/mock/mock_hub.py
/// (TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:<port>).
final class DictationBarTests: XCTestCase {
    private let main = "server/b2e4d6f8-0000-4000-8000-000000000001"
    private let words = "dictated words here"
    private var app: XCUIApplication!
    private var hub = ""

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        hub = h
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.name = "failure"
            shot.lifetime = .keepAlways
            add(shot)
        }
        super.tearDown()
    }

    @MainActor private func openMain(_ more: [String] = []) {
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant", "-openAssistant", main,
                               "-showDictationSample", words] + more
        app.launchPinned()
        XCTAssertTrue(mic.waitForExistence(timeout: 30), "no mic")
        clearDraft()
        offBarUnnamed("before the mic")
    }

    private var mic: XCUIElement { app.buttons["shell-composer-mic"].firstMatch }
    private var field: XCUIElement { app.descendants(matching: .any)["shell-composer-field"].firstMatch }
    private func bar(_ part: String) -> XCUIElement { app.buttons["dictation-bar-" + part].firstMatch }

    /// While the bar is off its parts are listed without a label or identifier: hidden, they still are in XCUITest's
    /// tree, and lookups of the composer's send by its label 「发送」 found the off bar's ↑ (#7684). A bar label counts
    /// only over the composer and outside its own controls (`shell-composer-…` and what they hold): the mic says
    /// 「停止听写」 while it is held, and other screens have a 「取消」 of their own.
    @MainActor private func offBarUnnamed(_ when: String) {
        let names: Set = ["取消", "停止听写", "发送", "正在听…"]
        XCTAssertTrue(wait {
            guard let root = try? self.app.snapshot() else { return false }
            var others: [XCUIElementSnapshot] = [], composer = CGRect.null
            Self.split(root, others: &others, composer: &composer)
            return !others.contains { $0.identifier.hasPrefix("dictation-bar-") || (names.contains($0.label) && composer.intersects($0.frame)) }
        }, "the off bar has names (\(when)):\n\(app.debugDescription)")
    }

    /// The composer's own controls make up its frame; every other element goes to `others`.
    private static func split(_ e: XCUIElementSnapshot, others: inout [XCUIElementSnapshot], composer: inout CGRect) {
        if e.identifier.hasPrefix("shell-composer-") {
            composer = composer.union(e.frame)
            return
        }
        others.append(e)
        for child in e.children { split(child, others: &others, composer: &composer) }
    }

    /// What the field holds (its value is the placeholder while it is empty).
    @MainActor private var draft: String {
        let value = field.value as? String ?? ""
        return value == field.placeholderValue ? "" : value
    }

    /// Empties the draft wherever the caret is, and fails the test if it is still there: earlier runs keep their draft
    /// across launches. The tap that focuses the field leaves the caret where the field's selection was before the text
    /// came in (a restored draft, the words ■ put in), at its start, where deletes remove nothing (#8799); a second tap,
    /// at the focused field's end, puts the caret after the last character first.
    @MainActor private func clearDraft(line: UInt = #line) {
        guard !draft.isEmpty else { return }
        let hide = app.buttons["shell-composer-dismiss-keyboard"].firstMatch // there while the field has the focus
        for _ in 0..<3 where !draft.isEmpty {
            if !hide.exists {
                field.tap()
                _ = hide.waitForExistence(timeout: 5)
            }
            field.coordinate(withNormalizedOffset: CGVector(dx: 0.97, dy: 0.9)).tap()
            field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: draft.count + 2))
        }
        XCTAssertTrue(wait { self.draft.isEmpty }, "the draft was not cleared: \(draft)", line: line)
        hide.tap()
        sleep(2) // drafts are saved a moment after the last change
    }

    /// Taps the mic and waits until all the sample words were heard (one every 350 ms). The bar's parts are always
    /// built (hidden from accessibility while it is off, which XCUITest's tree still lists): open is the mic listening
    /// and ✕ hittable.
    @MainActor private func dictate() {
        mic.tap()
        XCTAssertTrue(wait { self.mic.label == self.listening && self.bar("cancel").isHittable }, "the bar did not open")
        XCTAssertTrue(bar("stop").isHittable && bar("send").isHittable, "the bar's ■ or ↑ cannot be tapped")
        sleep(2)
    }

    /// The mic's spoken name while it listens (zh-Hans: the tests run in it), and when it does not.
    private let listening = "停止听写"

    /// The bar closed: dictation is over.
    @MainActor private func closed() -> Bool { wait { self.mic.label != self.listening } }

    @MainActor private func wait(_ timeout: TimeInterval = 5, _ ok: @escaping () -> Bool) -> Bool {
        let end = Date.now.addingTimeInterval(timeout)
        repeat {
            if ok() { return true }
            usleep(200_000)
        } while Date.now < end
        return false
    }

    private func sent() throws -> [String] {
        let data = try Data(contentsOf: URL(string: hub + "/mock/sent")!)
        let json = try JSONSerialization.jsonObject(with: data) as? [String: Any]
        return ((json?["sent"] as? [Any]) ?? []).map { "\($0)" }
    }

    @MainActor func testStopPutsTheWordsInTheDraftWithoutSending() throws {
        openMain()
        let before = try sent().count
        dictate()
        bar("stop").tap()
        XCTAssertTrue(closed(), "the bar did not close")
        offBarUnnamed("after ■")
        XCTAssertEqual(draft, words)
        XCTAssertEqual(try sent().count, before, "■ sent the message")
        clearDraft() // fails if the words stay: saved, they would be the next run's draft
    }

    @MainActor func testCancelDropsWhatWasHeard() throws {
        openMain()
        dictate()
        bar("cancel").tap()
        XCTAssertTrue(closed(), "the bar did not close")
        offBarUnnamed("after ✕")
        XCTAssertEqual(draft, "", "✕ kept the words")
        XCTAssertTrue(mic.isHittable, "the composer did not come back")
    }

    @MainActor func testSendSendsWhatWasHeard() throws {
        openMain()
        let before = try sent().count
        dictate()
        bar("send").tap()
        XCTAssertTrue(closed(), "the bar did not close")
        offBarUnnamed("after ↑")
        let deadline = Date.now.addingTimeInterval(5)
        while try sent().count == before, Date.now < deadline { usleep(200_000) }
        let all = try sent()
        XCTAssertEqual(all.count, before + 1, "↑ did not send")
        XCTAssertTrue(all.last?.contains(words) == true, "↑ sent something else: \(all.last ?? "")")
        XCTAssertEqual(draft, "", "the draft kept the sent words")
    }

    /// Where the mic is on the screen, as the finger finds it again.
    @MainActor private func micSpot() -> XCUICoordinate {
        let frame = mic.frame
        return app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: frame.midX, dy: frame.midY))
    }

    /// The bar is not up: the mic took the touch as a hold, not a tap.
    @MainActor private func barShut() -> Bool { wait(2) { !self.bar("cancel").isHittable } }

    @MainActor func testASecondTapWhereTheMicWasStopsWithoutSending() throws {
        openMain()
        let before = try sent().count
        let spot = micSpot()
        dictate()
        spot.tap()
        XCTAssertTrue(closed(), "a second tap where the mic was did not end dictation")
        offBarUnnamed("after the second tap")
        XCTAssertEqual(draft, words, "the second tap did not put the words in the draft")
        XCTAssertEqual(try sent().count, before, "the second tap sent the message")
        clearDraft()
    }

    /// A slow tap, longer than Telegram's 0.19 s and shorter than `Theme.Hold.delay`, is still a tap.
    @MainActor func testASlowTapIsStillATap() throws {
        openMain()
        mic.press(forDuration: 0.2)
        XCTAssertTrue(wait { self.mic.label == self.listening && self.bar("cancel").isHittable }, "a 0.2 s press did not open the bar")
        bar("cancel").tap()
        XCTAssertTrue(closed(), "the bar did not close")
        XCTAssertEqual(draft, "", "✕ kept the words")
    }

    @MainActor func testHoldingDictatesAndLettingGoKeepsTheWords() throws {
        openMain()
        let before = try sent().count
        mic.press(forDuration: 3)
        XCTAssertTrue(closed(), "dictation went on after the finger lifted")
        XCTAssertTrue(barShut(), "letting go of the hold counted as a tap and opened the bar")
        XCTAssertEqual(draft, words, "letting go did not keep what was heard")
        XCTAssertEqual(try sent().count, before, "a hold sent the message")
        clearDraft()
    }

    @MainActor func testHoldingAndSlidingLeftDropsTheWords() throws {
        openMain()
        let before = try sent().count
        let spot = micSpot()
        spot.press(forDuration: 2.5, thenDragTo: spot.withOffset(CGVector(dx: -220, dy: 0)))
        XCTAssertTrue(closed(), "dictation went on after the slide")
        XCTAssertTrue(barShut(), "the slide's end counted as a tap and opened the bar")
        XCTAssertEqual(draft, "", "the slide left kept the words")
        XCTAssertEqual(try sent().count, before, "a hold sent the message")
    }

    /// TEST_RUNNER_PERSONA_SHOTS=<dir> (TEST_RUNNER_PERSONA_SHOTS_APPEARANCE names them light / dark): the mic idle, tapped
    /// (the bar, ■ where the mic was), held, and held with the finger slid toward cancel (`-showDictationHold`).
    @MainActor func testShots() throws {
        guard ProcessInfo.processInfo.environment["PERSONA_SHOTS"] != nil else { throw XCTSkip("needs PERSONA_SHOTS") }
        openMain()
        shot("mic-idle")
        dictate()
        shot("mic-tapped")
        bar("cancel").tap()
        XCTAssertTrue(closed(), "the bar did not close")
        for (name, left) in [("mic-held", "0"), ("mic-sliding", "110")] {
            app.terminate()
            openMain(["-showDictationHold", left])
            sleep(3)
            shot(name)
        }
    }

    private func shot(_ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let look = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"].map { "-" + $0 } ?? ""
            let device = UIDevice.current.userInterfaceIdiom == .pad ? "-ipad" : ""
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + device + look + ".png"))
        }
    }
}

import XCTest

/// The new-conversation starter's launch, mic-to-bar entry and typing. The same file is copied onto the base worktree
/// for before/after measurements, so only the app under test changes. Mock and optimized build: `perf-test.sh composer`.
/// Frame windows use one warm-up plus `PERSONA_PERF_ITERATIONS` measured launches; launch uses XCTest's own warm-up.
final class PerfStarterTests: PerfTestCase {
    private static let words = "starter dictation performance sample"
    private static let typed = "why does this starter stutter while typing, and how long does a key take to show up\nsecond line\nthird"

    @MainActor private var field: XCUIElement { any("shell-composer-field") }

    @MainActor private func prepareStarter(pairing: Bool) {
        app = XCUIApplication()
        app.launchArguments = ["-dev.openChat", "", "-app.tab", "assistant", "-tab", "assistant", "-newAssistant", "YES",
                               "-showDictationSample", Self.words]
        if pairing { app.launchArguments += ["-pairURL", pair] }
    }

    @MainActor private func openStarter() {
        launch(["-dev.openChat", "", "-app.tab", "assistant", "-tab", "assistant", "-newAssistant", "YES",
                "-showDictationSample", Self.words], timeout: 30, settle: 2)
        see(any("assistant-welcome"), "the starter", timeout: 30)
        see(field, "the starter composer")
    }

    @MainActor private func point(_ element: XCUIElement) -> XCUICoordinate {
        let frame = element.frame
        return app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: frame.midX, dy: frame.midY))
    }

    /// A single labelled frame window. Waiting for its arrival before the next launch keeps labels and runs aligned.
    @MainActor private func sample(_ label: String, index: Int, block: () -> Void) -> [String: Any] {
        let name = "PerfStarterTests/" + label
        _ = mock("frames_label?name=" + name)
        darwinPost("com.persona.perf.frames.start")
        block()
        usleep(300_000)
        darwinPost("com.persona.perf.frames.stop")
        let deadline = Date.now.addingTimeInterval(15)
        var runs: [[String: Any]] = []
        while Date.now < deadline {
            runs = (mock("frames?name=" + name)["runs"] as? [[String: Any]]) ?? []
            if runs.count > index { return runs[index] }
            usleep(100_000)
        }
        XCTFail("no \(label) frame sample for run \(index)")
        return [:]
    }

    @MainActor private func save(_ label: String, runs: [[String: Any]]) {
        XCTAssertEqual(runs.count, iterations + 1, "one warm-up and every measured run must arrive")
        for run in runs {
            XCTAssertGreaterThan((run["frames"] as? NSNumber)?.doubleValue ?? 0, 0)
            XCTAssertGreaterThan((run["seconds"] as? NSNumber)?.doubleValue ?? 0, 0)
            XCTAssertNil(run["error"])
        }
        let payload: [String: Any] = ["label": label, "warm_up_count": 1, "measured_iterations": iterations, "runs": runs]
        guard let data = try? JSONSerialization.data(withJSONObject: payload, options: [.prettyPrinted, .sortedKeys]) else { return }
        let attachment = XCTAttachment(data: data, uniformTypeIdentifier: "public.json")
        attachment.name = "starter-\(label)-frames.json"
        attachment.lifetime = .keepAlways
        add(attachment)
        if let directory = ProcessInfo.processInfo.environment["PERSONA_PERF_OUT"] {
            try? data.write(to: URL(fileURLWithPath: directory).appending(path: "starter-\(label)-frames.json"))
        }
    }

    /// A fresh process to the new-conversation starter's first displayed frame, with saved pairing from an unmeasured seed.
    @MainActor func testStarterLaunch() throws {
        prepareStarter(pairing: true)
        app.terminate()
        app.launchPinned()
        allowNotifications()
        acceptIconAlert()
        see(any("assistant-welcome"), "the seeded starter", timeout: 30)
        app.terminate()

        prepareStarter(pairing: false)
        var firstLaunch = true
        measure(metrics: [XCTApplicationLaunchMetric(waitUntilResponsive: false)], options: options) {
            app.launchPinned()
            if firstLaunch {
                allowNotifications()
                acceptIconAlert()
                firstLaunch = false
            }
            see(any("assistant-welcome"), "the measured starter", timeout: 30)
            see(field, "the measured starter composer")
        }
    }

    /// Tap the starter's mic and record only the bar's entry. The simulator's sample stands in for microphone audio.
    @MainActor func testStarterMicEntry() throws {
        var runs: [[String: Any]] = []
        for index in 0...iterations {
            openStarter()
            let mic = point(any("shell-composer-mic"))
            runs.append(sample("mic", index: index) {
                mic.tap()
                usleep(1_200_000)
            })
            XCTAssertTrue(any("dictation-bar-cancel").waitForExistence(timeout: 5), "the dictation bar did not enter")
            app.terminate()
        }
        save("mic", runs: runs)
    }

    /// Type plain letters into the focused starter; the entered value is checked and clearing is outside the window.
    @MainActor func testStarterTyping() throws {
        var runs: [[String: Any]] = []
        for index in 0...iterations {
            openStarter()
            field.tap()
            sleep(1)
            runs.append(sample("typing", index: index) {
                field.typeText(Self.typed)
            })
            // The simulator may use its hardware keyboard, so a software-keyboard element is not the entry check.
            XCTAssertEqual(field.value as? String, Self.typed, "the measured text did not reach the starter field")
            field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: Self.typed.count + 2))
            app.terminate()
        }
        save("typing", runs: runs)
    }
}

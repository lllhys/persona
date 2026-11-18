import XCTest

/// Your own long messages, folded (#5676), scrolled: an Agent chat of forty exchanges, each opened by a long pasted
/// report, glided by the app itself (`measureVerifiedGlide`: 6000 pt into the history and back at 2000 pt/s). One
/// optimized build, three ways, alternating so the machine's load falls on all of them:
/// - `Folded`: as it ships;
/// - `Whole`: every bubble whole, as before #5676 (`-showWholeBubbles YES`);
/// - `Twin`: the same exchanges with each report already cut to about the lines a folded one shows, drawn whole (no fold
///   at all): as many rows to the screen as `Folded`, so `Folded` against `Twin` is what folding itself costs, and
///   `Whole` against `Twin` what showing three times the exchanges to the screen does.
/// Mock: MOCK_LONG_INPUT=perf at TEST_RUNNER_PERSONA_PERF_HUB (`PerfTestCase`); without it the tests skip.
final class PerfLongInputTests: PerfTestCase {
    static let chat = "macbook/a1f3c2d4-0000-4000-8000-000005676a00"
    static let twin = "macbook/a1f3c2d4-0000-4000-8000-000005676a01"

    @MainActor private func open(_ chat: String, whole: Bool) {
        launch(["-app.tab", "chats", "-tab", "dev", "-dev.openChat", "", "-openSession", chat, "-holdChromeOpen", "YES",
                "-loadAll", "YES"] + (whole ? ["-showWholeBubbles", "YES"] : []))
        see(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "可以合")).firstMatch,
            "the chat", timeout: 30)
        sleep(2)
    }

    @MainActor func testGlide1Folded() throws {
        open(Self.chat, whole: false)
        measureVerifiedGlide()
    }

    @MainActor func testGlide2Whole() throws {
        open(Self.chat, whole: true)
        measureVerifiedGlide()
    }

    @MainActor func testGlide3Twin() throws {
        open(Self.twin, whole: true)
        measureVerifiedGlide()
    }

    @MainActor func testGlide4Twin() throws {
        open(Self.twin, whole: true)
        measureVerifiedGlide()
    }

    @MainActor func testGlide5Whole() throws {
        open(Self.chat, whole: true)
        measureVerifiedGlide()
    }

    @MainActor func testGlide6Folded() throws {
        open(Self.chat, whole: false)
        measureVerifiedGlide()
    }
}

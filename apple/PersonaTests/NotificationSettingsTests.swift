import Foundation
import Testing
@testable import Persona

/// Settings › Notifications (`NotificationSettingsModel`) without the page: a read that fails stops the spinner and
/// says why (#1860). Tests run in zh-Hans.
@MainActor struct NotificationSettingsTests {
    @Test func readsThePrefsAndAppliesThem() async {
        var applied: [NotifyPrefs] = []
        let m = NotificationSettingsModel(deps: .init(load: { NotifyPrefs(done: .always) }, applied: { applied.append($0) }))
        #expect(m.spinning && m.prefs == nil)
        await m.load()
        #expect(!m.spinning && m.prefs == NotifyPrefs(done: .always) && m.error == nil && !m.loading)
        #expect(applied == [NotifyPrefs(done: .always)])
    }

    @Test func aReadThatFailsStopsTheSpinnerAndSaysWhy() async {
        let m = NotificationSettingsModel(deps: .init(load: { throw APIError(status: 503, message: "助手没有连上") }))
        await m.load()
        #expect(!m.spinning && m.prefs == nil)
        #expect(m.error == "没读到：助手没有连上")
    }

    /// A timeout or no network is quiet elsewhere (`Error.shown`), but here it is all the page has to say.
    @Test func aTimeoutOrNoNetworkStillSaysSomething() async {
        for e: Error in [URLError(.timedOut), URLError(.cannotConnectToHost), URLError(.notConnectedToInternet)] {
            let m = NotificationSettingsModel(deps: .init(load: { throw e }))
            await m.load()
            #expect(!m.spinning && m.error == "没读到：连不上 hub")
        }
    }

    /// Leaving the page cancels the read: nothing to say, and the next visit reads again.
    @Test func aCancelledReadSaysNothing() async {
        let m = NotificationSettingsModel(deps: .init(load: { throw CancellationError() }))
        await m.load()
        #expect(m.error == nil && m.prefs == nil)
    }

    @Test func retryAfterAFailureReadsThePrefs() async {
        var fail = true
        var applied: [NotifyPrefs] = []
        let m = NotificationSettingsModel(deps: .init(load: {
            if fail { throw URLError(.timedOut) }
            return NotifyPrefs(liveActivity: false)
        }, applied: { applied.append($0) }))
        await m.load()
        #expect(m.error != nil && applied.isEmpty)
        fail = false
        await m.reloadIfNeeded()
        #expect(m.prefs == NotifyPrefs(liveActivity: false) && m.error == nil && !m.spinning)
        #expect(applied == [NotifyPrefs(liveActivity: false)])
    }

    /// Once read, coming back to the app or the hub reconnecting does not read again (and undo an unsaved change).
    @Test func reloadIfNeededLeavesReadPrefsAlone() async {
        var loads = 0
        let m = NotificationSettingsModel(deps: .init(load: { loads += 1; return NotifyPrefs() }))
        await m.load()
        m.prefs?.input = false
        await m.reloadIfNeeded()
        #expect(loads == 1 && m.prefs?.input == false)
    }

    /// A failed re-read (the page shown again) keeps what was read: no error over working controls.
    @Test func aFailedReReadKeepsThePrefs() async {
        var fail = false
        let m = NotificationSettingsModel(deps: .init(load: {
            if fail { throw APIError(status: 503, message: "x") }
            return NotifyPrefs()
        }))
        await m.load()
        fail = true
        await m.load()
        #expect(m.prefs == NotifyPrefs() && m.error == nil && !m.spinning)
    }

    // MARK: Saves in order (#1913)

    /// A hub whose answers the test hands out one at a time, recording each PUT's body.
    @MainActor private final class SlowHub {
        var bodies: [NotifyPrefs] = []
        private var waiting: [CheckedContinuation<Result<NotifyPrefs, Error>, Never>] = []

        func save(_ p: NotifyPrefs) async throws -> NotifyPrefs {
            bodies.append(p)
            return try await withCheckedContinuation { waiting.append($0) }.get()
        }

        /// The PUT waiting longest gets its answer (its own body, or the error).
        func answer(_ error: Error? = nil) async throws {
            for _ in 0..<100 where waiting.isEmpty { await Task.yield() }
            try #require(!waiting.isEmpty)
            let body = bodies[bodies.count - waiting.count]
            waiting.removeFirst().resume(returning: error.map { .failure($0) } ?? .success(body))
            for _ in 0..<20 { await Task.yield() }
        }

        var inFlight: Int { waiting.count }
    }

    private func model(_ hub: SlowHub, applied: @escaping (NotifyPrefs) -> Void = { _ in },
                       failed: @escaping (Error) -> Void = { _ in }) async -> NotificationSettingsModel {
        let m = NotificationSettingsModel(deps: .init(load: { NotifyPrefs() }, save: { try await hub.save($0) },
                                                      applied: applied, failed: failed))
        await m.load()
        return m
    }

    /// Two quick changes: the page keeps both, one PUT at a time, the second over the first's answer; the hub ends on both.
    @Test(.timeLimit(.minutes(1))) func twoChangesSaveInTheOrderMade() async throws {
        let hub = SlowHub()
        let m = await model(hub)
        let saving = m.change { $0.input = false }
        m.change { $0.done = .always }
        #expect(m.prefs?.input == false && m.prefs?.done == .always)
        for _ in 0..<20 { await Task.yield() }
        try #require(hub.inFlight == 1, "one PUT at a time") // more would wait for answers the test never gives
        try await hub.answer()
        #expect(m.prefs?.input == false && m.prefs?.done == .always, "the first answer doesn't undo the second change")
        try await hub.answer()
        await saving?.value
        #expect(hub.bodies.map(\.input) == [false, false] && hub.bodies.map(\.done) == [.long, .always])
        #expect(m.prefs == NotifyPrefs(input: false, done: .always))
    }

    /// The first save fails while the second waits: only the first change goes, and no PUT carries it.
    @Test(.timeLimit(.minutes(1))) func aFailedChangeRevertsAloneAndIsNeverSaved() async throws {
        let hub = SlowHub()
        var errors = 0
        let m = await model(hub, failed: { _ in errors += 1 })
        let saving = m.change { $0.input = false }
        m.change { $0.done = .always }
        try await hub.answer(URLError(.timedOut))
        #expect(errors == 1)
        #expect(m.prefs?.input == true && m.prefs?.done == .always)
        try await hub.answer()
        await saving?.value
        #expect(hub.bodies.last == NotifyPrefs(done: .always), "the second PUT doesn't carry the failed change")
        #expect(m.prefs == NotifyPrefs(done: .always))
    }

    /// The last change fails after an earlier one went through: back to the hub's last answer, not to before both.
    @Test(.timeLimit(.minutes(1))) func aFailedLastChangeGoesBackToTheHubsLastAnswer() async throws {
        let hub = SlowHub()
        var applied: [NotifyPrefs] = []
        let m = await model(hub, applied: { applied.append($0) })
        let saving = m.change { $0.input = false }
        m.change { $0.liveActivity = false }
        try await hub.answer()
        try await hub.answer(APIError(status: 503, message: "x"))
        await saving?.value
        #expect(m.prefs == NotifyPrefs(input: false))
        #expect(applied.last == NotifyPrefs(input: false), "the device goes by what the hub kept")
    }

    @Test func nothingIsSavedBeforeTheFirstRead() {
        let hub = SlowHub()
        let m = NotificationSettingsModel(deps: .init(load: { NotifyPrefs() }, save: { try await hub.save($0) }))
        #expect(m.change { $0.input = false } == nil)
        #expect(m.prefs == nil && hub.bodies.isEmpty)
    }

    /// Quiet hours go their own way: shown over the rest while on their way, and their answer is the hub's last one.
    @Test(.timeLimit(.minutes(1))) func quietHoursShowAndSettle() async throws {
        let hub = SlowHub()
        let m = await model(hub)
        let quiet = QuietHours(enabled: true, start: "22:00", end: "07:00", tz: "UTC")
        m.showQuiet(quiet)
        let saving = m.change { $0.input = false }
        #expect(m.prefs?.quiet == quiet && m.prefs?.input == false)
        try await hub.answer()
        await saving?.value
        #expect(m.prefs?.quiet == quiet, "a notification answer doesn't hide the quiet hours on their way")
        m.quietFailed()
        #expect(m.prefs?.quiet == nil && m.prefs?.input == false)
        var kept = NotifyPrefs(input: false)
        kept.quiet = quiet
        m.quietSaved(kept)
        #expect(m.prefs == kept)
    }
}

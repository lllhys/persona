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
}

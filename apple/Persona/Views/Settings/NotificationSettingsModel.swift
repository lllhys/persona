import Foundation
import Observation

/// Settings › Notifications (`NotificationSettingsView`): this device's push choices, kept on the hub per device
/// (notify.go). A read that fails stops the page's spinner and says why, with Retry (#1860): before, the page spun for
/// good with every row disabled, the system-settings buttons included, and nothing read the prefs again.
@Observable final class NotificationSettingsModel {
    struct Deps {
        var load: () async throws -> NotifyPrefs
        /// The choice the app goes by now (the Live Activity follows it).
        var applied: (NotifyPrefs) -> Void = { _ in }
    }

    /// Nil until the hub answered. The page's optimistic saves write it too.
    var prefs: NotifyPrefs?
    /// Why the prefs could not be read (nil while nothing failed, once read, or for a cancelled read).
    private(set) var error: String?
    private(set) var loading = false
    @ObservationIgnored private let deps: Deps

    init(deps: Deps) { self.deps = deps }

    /// Waiting for the hub's first answer: the page shows a spinner.
    var spinning: Bool { prefs == nil && error == nil }

    func load() async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        do {
            let p = try await deps.load()
            prefs = p
            error = nil
            deps.applied(p)
        } catch {
            if prefs == nil, let why = Self.loadFailure(error) { self.error = why }
        }
    }

    /// Reads again only while nothing was read (back in the app, the hub link up again).
    func reloadIfNeeded() async {
        if prefs == nil { await load() }
    }

    /// What the page says when the read failed; nil for a cancelled read (the page went away). A timeout or no network
    /// has no `shown` text but is said too: the page has nothing else to show in its place (as `TriageSettingsModel`).
    static func loadFailure(_ error: Error) -> String? {
        if error.isCancellation { return nil }
        let why = error.shown ?? String(localized: "Can't reach the hub")
        return String(localized: "Couldn't load: \(why)")
    }
}

import Foundation
import Observation

/// Settings › Notifications (`NotificationSettingsView`): this device's push choices, kept on the hub per device
/// (notify.go). A read that fails stops the page's spinner and says why, with Retry (#1860): before, the page spun for
/// good with every row disabled, the system-settings buttons included, and nothing read the prefs again.
///
/// Saves go one at a time, in the order they were made (#1913): each control's change is an edit queued over the hub's
/// last answer, and each PUT carries that answer plus one edit. Before, every change sent its own PUT with the whole
/// object at once and the page took whichever answer came last, so it could show (and hand the device) an older choice,
/// roll back to the values from before two changes, or have the hub keep a change the page said had failed.
@Observable final class NotificationSettingsModel {
    struct Deps {
        var load: () async throws -> NotifyPrefs
        /// Saves the whole choice (`PUT /api/v1/devices/{id}/prefs`); the hub answers what it keeps.
        var save: (NotifyPrefs) async throws -> NotifyPrefs = { $0 }
        /// The choice the app goes by now (the Live Activity follows it): the one shown, and the hub's once it answered.
        var applied: (NotifyPrefs) -> Void = { _ in }
        /// A change the hub kept.
        var saved: () -> Void = {}
        /// A change that didn't go through: it is gone from the page, the error is for the person.
        var failed: (Error) -> Void = { _ in }
    }

    /// One control's change.
    typealias Edit = (inout NotifyPrefs) -> Void

    /// Nil until the hub answered. What the page shows: the hub's last answer with the changes still waiting on top.
    var prefs: NotifyPrefs?
    /// The hub's last answer.
    @ObservationIgnored private var confirmed: NotifyPrefs?
    /// Changes not answered yet, in the order they were made; the first is the one being saved.
    @ObservationIgnored private var edits: [Edit] = []
    @ObservationIgnored private var saving: Task<Void, Never>?
    /// Quiet hours on their way (they have an endpoint and a sequence of their own, the page's `saveQuiet`).
    @ObservationIgnored private var quietShown: QuietHours?
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
            confirmed = try await deps.load()
            error = nil
            show()
        } catch {
            if prefs == nil, let why = Self.loadFailure(error) { self.error = why }
        }
    }

    /// Reads again only while nothing was read (back in the app, the hub link up again).
    func reloadIfNeeded() async {
        if prefs == nil { await load() }
    }

    /// A control changed: shown at once, saved after the changes before it. Nothing before the first read (the controls
    /// are off then). Returns the saving under way, for tests.
    @discardableResult
    func change(_ edit: @escaping Edit) -> Task<Void, Never>? {
        guard confirmed != nil else { return nil }
        edits.append(edit)
        show()
        if let saving { return saving }
        let task = Task { await saveInOrder() }
        saving = task
        return task
    }

    /// One PUT at a time: the hub's last answer plus the first edit waiting. A failure drops that edit alone, so no
    /// later PUT carries it.
    private func saveInOrder() async {
        while let edit = edits.first, var next = confirmed {
            edit(&next)
            do {
                confirmed = try await deps.save(next)
                deps.saved()
            } catch {
                deps.failed(error)
            }
            edits.removeFirst()
            show()
        }
        saving = nil
    }

    /// Quiet hours chosen, on their way: shown over the rest until `quietSaved` / `quietFailed`.
    func showQuiet(_ quiet: QuietHours) {
        quietShown = quiet
        show()
    }

    /// The hub kept the quiet hours, and answered with all of this device's prefs.
    func quietSaved(_ saved: NotifyPrefs) {
        quietShown = nil
        confirmed = saved
        show()
    }

    func quietFailed() {
        quietShown = nil
        show()
    }

    /// The page and the device go by the hub's last answer with what is still waiting on top.
    private func show() {
        guard var shown = confirmed else { return }
        for edit in edits { edit(&shown) }
        if let quietShown { shown.quiet = quietShown }
        prefs = shown
        deps.applied(shown)
    }

    /// What the page says when the read failed; nil for a cancelled read (the page went away). A timeout or no network
    /// has no `shown` text but is said too: the page has nothing else to show in its place (as `TriageSettingsModel`).
    static func loadFailure(_ error: Error) -> String? {
        if error.isCancellation { return nil }
        let why = error.shown ?? String(localized: "Can't reach the hub")
        return String(localized: "Couldn't load: \(why)")
    }
}

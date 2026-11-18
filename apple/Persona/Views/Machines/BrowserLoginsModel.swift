import SwiftUI
import PersonaWire

/// A computer's Browser Sign-ins (#1304, #5821): its sites, reading them, importing and clearing, shared by the section on
/// the computer's page and the list page it opens, so a clear on one shows on the other. What the pages draw (the list
/// for the search and order, the summary, which sites have expired) is worked out here once per change, never in `body`
/// (docs/apple/performance.md P4). Tests run it against a `FakeHub` (BrowserLoginsModelTests).
@Observable final class BrowserLoginsModel {
    struct Deps {
        var api: () -> APIClient?
        var toast: (Toast) -> Void

        static func live(group: String) -> Deps {
            let model = AppModel.shared
            return Deps(api: { model.api(group) }, toast: { model.toast = $0 })
        }
    }

    typealias Order = BrowserSiteList.Order

    let machine: String
    /// nil while reading (or offline).
    private(set) var sites: [BrowserCookieSite]?
    private(set) var error: String?
    /// An import or a clear is on its way.
    private(set) var busy = false
    private(set) var summary = BrowserSiteList.Summary(count: 0, first: nil, expired: 0)
    /// The list page's search (settled: the page passes it on a moment after the last key) and order, and what they show.
    private(set) var query = ""
    private(set) var order: Order = .name
    private(set) var shown: [BrowserCookieSite] = []
    private(set) var expired: Set<String> = []
    @ObservationIgnored private let deps: Deps
    /// The latest read: an older one that answers after it doesn't land.
    @ObservationIgnored private var reads = 0
    /// The connection state of the last read.
    @ObservationIgnored private var loadedOnline: Bool?

    init(machine: String, deps: Deps) {
        self.machine = machine
        self.deps = deps
    }

    /// More than a few: the page folds them behind one row that opens the whole list.
    var folded: Bool { (sites?.count ?? 0) > BrowserSiteList.inPlace }

    /// The section came on screen: reads the sites unless it already has them for this connection state (the section
    /// is rebuilt with its page's content; an open list must not go blank each time).
    func appear(online: Bool) async {
        if online == loadedOnline, sites != nil { return }
        await load(online: online)
    }

    func load(online: Bool) async {
        loadedOnline = online
        reads += 1
        let read = reads
        set(nil)
        error = nil
        guard online, let api = deps.api() else { return }
        do {
            let answer = try await api.browserCookies(machine: machine)
            guard read == reads else { return }
            set(answer.sites)
        } catch {
            guard read == reads else { return }
            self.error = error.shown
        }
    }

    func search(_ text: String) {
        guard text != query else { return }
        query = text
        derive()
    }

    func sort(_ order: Order) {
        guard order != self.order else { return }
        self.order = order
        derive()
    }

    /// Sends the ticked cookies; true once they are in (the picked file is dropped then).
    func importCookies(_ cookies: [BrowserCookie]) async -> Bool {
        guard !cookies.isEmpty, let api = deps.api() else { return false }
        busy = true
        defer { busy = false }
        do {
            let a = try await api.importBrowserCookies(machine: machine, cookies)
            set(a.sites)
            let skipped = a.skipped ?? 0
            let imported = a.imported ?? 0
            deps.toast(Toast(skipped > 0 ? String(localized: "Imported \(imported); skipped \(skipped) expired or malformed") : String(localized: "Imported \(imported) cookies"), style: .success))
            return true
        } catch {
            if let m = error.shown { deps.toast(Toast(String(localized: "Couldn't import: \(m)"), style: .error)) }
            return false
        }
    }

    /// Clears these sites in one call; true once done.
    @discardableResult func clear(_ domains: [String]) async -> Bool {
        guard !domains.isEmpty, let api = deps.api() else { return false }
        busy = true
        defer { busy = false }
        do {
            let a = try await api.clearBrowserCookies(machine: machine, sites: domains)
            set(a.sites)
            deps.toast(Toast(String(localized: "Cleared \(a.removed ?? 0) cookies"), style: .success))
            return true
        } catch {
            if let m = error.shown { deps.toast(Toast(String(localized: "Couldn't clear: \(m)"), style: .error)) }
            return false
        }
    }

    private func set(_ list: [BrowserCookieSite]?) {
        sites = list
        let now = Date.now
        summary = BrowserSiteList.summary(list ?? [], now: now)
        expired = Set((list ?? []).filter { $0.expired(at: now) }.map(\.domain))
        derive()
    }

    private func derive() {
        shown = BrowserSiteList.shown(sites ?? [], query: query, order: order)
    }
}

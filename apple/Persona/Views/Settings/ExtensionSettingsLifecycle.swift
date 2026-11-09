import Foundation
import Observation

/// Capabilities follow provenance, never the extension's current running state.
/// Raw origins may contain local paths or credentials and must not be shown.
nonisolated struct ExtensionSettingsPolicy {
    let canEnable: Bool
    let canCheck: Bool
    let canAutoUpgrade: Bool
    let canUninstall: Bool
    let updateReason: String
    let uninstallReason: String
    let source: String
    let provenance: String
    let runtime: String
    let market: String?

    static var uninstallPreviewGap: String {
        String(localized: "This manager does not provide a safe uninstall preview. Remove the extension at its source.")
    }
    static var rollbackPreviewGap: String {
        String(localized: "This manager does not provide a rollback preview.")
    }

    init(detail: HubExtensionDetail) {
        let plugin = detail.plugin
        // kind is Manifest.Kind(), a contribution category; installed runtime metadata is not exposed.
        runtime = String(localized: "Not provided")
        let builtin = plugin.source == "builtin"
        let configured = detail.readOnly || ["hub.json", "manager.json"].contains(plugin.source ?? "")
        let attached = plugin.attached != nil
        let installed = plugin.source == "installed" && !attached && !configured
        let origin = detail.origin ?? ""
        let parts = origin.hasPrefix("market:") ? String(origin.dropFirst(7)).split(separator: "/", maxSplits: 1, omittingEmptySubsequences: false) : []
        market = installed && parts.count == 2 && !parts[0].isEmpty && !parts[1].isEmpty ? String(parts[0]) : nil
        canEnable = !configured && plugin.source != "dev" && (plugin.enabled != nil || builtin)
        canCheck = market != nil
        canAutoUpgrade = canCheck && plugin.autoUpgrade != nil
        // DELETE is destructive even when dependency checks fail. There is no safe removal preview API.
        canUninstall = false
        if attached {
            source = String(localized: "Attached")
            updateReason = String(localized: "This extension is not managed by an extension market. Update it at its source.")
            uninstallReason = String(localized: "This extension is managed outside the extension market. Remove it at its source.")
        } else if builtin {
            source = String(localized: "Built-in")
            updateReason = String(localized: "Built-in extensions are updated with Persona.")
            uninstallReason = String(localized: "Built-in extensions cannot be uninstalled. Turn the extension off instead.")
        } else if configured {
            source = String(localized: "Configured (manager.json)")
            updateReason = String(localized: "This extension is not managed by an extension market. Update it at its source.")
            uninstallReason = String(localized: "This extension is managed outside the extension market. Remove it at its source.")
        } else {
            switch plugin.source {
            case "dev": source = String(localized: "Development")
            case "installed": source = String(localized: "Installed [Extension Market Views a8b6c3]")
            default: source = String(localized: "Not provided")
            }
            updateReason = canCheck && !canAutoUpgrade ? String(localized: "This manager does not provide this action.") :
                (canCheck ? "" : String(localized: "This extension is not managed by an extension market. Update it at its source."))
            uninstallReason = canCheck ? Self.uninstallPreviewGap : String(localized: "This extension is managed outside the extension market. Remove it at its source.")
        }
        // Manager-owned market identifiers are safe provenance; never expose local paths or URLs.
        provenance = attached ? String(localized: "Attached") :
            (origin.isEmpty ? String(localized: "Not provided") :
                (market != nil ? origin : String(localized: "Local installation")))
    }
}

/// Shared by standard settings views. It never touches the configuration page's draft.
@Observable final class ExtensionSettingsLifecycle {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    let id: String
    let manager: String?
    private(set) var busy = false
    private(set) var error: String?
    private(set) var globalAuto: Bool?
    private(set) var candidate: ExtensionUpgrade?
    private(set) var checked = false
    private(set) var preview: ExtensionPreview?
    private(set) var detail: HubExtensionDetail?
    @ObservationIgnored private let deps: Deps

    var policy: ExtensionSettingsPolicy? { detail.map(ExtensionSettingsPolicy.init(detail:)) }
    var previousVersion: String? { detail?.previousVersion }
    var rollbackReason: String { ExtensionSettingsPolicy.rollbackPreviewGap }

    init(id: String, manager: String? = nil, deps: Deps = .live) {
        self.id = id
        self.manager = manager
        self.deps = deps
    }

    private func api() throws -> APIClient {
        guard let api = deps.api()?.manager(manager) else {
            throw APIError(status: 0, message: String(localized: "Not connected to the hub yet"))
        }
        return api
    }

    func loadMetadata(detail: HubExtensionDetail) async {
        guard !busy, detail.plugin.id == id else { return }
        busy = true
        defer { busy = false }
        self.detail = detail
        preview = nil
        candidate = detail.plugin.upgrade
        checked = false
        globalAuto = nil
        error = nil
        guard ExtensionSettingsPolicy(detail: detail).canCheck else { return }
        do {
            let upgrades = try await api().pluginUpgrades()
            globalAuto = upgrades.auto
            candidate = upgrades.upgrades.first { $0.id == id }
        } catch { record(error) }
    }

    @discardableResult func checkForUpdates() async -> Bool {
        guard !busy, policy?.canCheck == true else { return false }
        busy = true
        checked = false
        defer { busy = false }
        do {
            _ = try await checkedCandidate(refresh: true)
            error = nil
            return true
        } catch { record(error); return false }
    }

    /// Read canonical installed metadata and the market before saying no update exists.
    private func checkedCandidate(refresh: Bool) async throws -> ExtensionUpgrade? {
        let api = try api()
        let detail = try await api.plugin(id)
        self.detail = detail
        let policy = ExtensionSettingsPolicy(detail: detail)
        guard let market = policy.market else {
            throw APIError(status: 0, message: policy.updateReason)
        }
        let catalog = try await api.pluginCatalog(market: market, refresh: refresh)
        guard let entryMarket = catalog.markets.first(where: { $0.id == market }), entryMarket.error?.isEmpty != false else {
            throw APIError(status: 0, message: String(localized: "The extension market is unavailable. Check its source and try again."))
        }
        guard let entry = catalog.plugins.first(where: { $0.market == market && $0.pluginID == id }) else {
            throw APIError(status: 0, message: String(localized: "This extension is no longer listed in its market."))
        }
        if let problem = entry.problem, !problem.isEmpty { throw APIError(status: 0, message: problem) }
        let upgrades = try await api.pluginUpgrades()
        globalAuto = upgrades.auto
        let next = upgrades.upgrades.first { $0.id == id && $0.market == market }
        candidate = entry.upgradable == true ? (next ?? ExtensionUpgrade(id: id, name: entry.name, market: market,
            from: detail.plugin.version ?? entry.installed ?? "", to: entry.version, state: "off")) : nil
        checked = true
        return candidate
    }

    func prepareUpgrade() async {
        guard !busy, policy?.canCheck == true else { return }
        busy = true
        defer { busy = false }
        preview = nil
        do {
            guard let candidate = try await checkedCandidate(refresh: false) else { error = nil; return }
            let next = try await api().previewCatalogExtension(market: candidate.market, plugin: id)
            guard next.manifest.id == id, next.upgrade == true, next.market == candidate.market,
                  next.from == detail?.plugin.version else {
                throw APIError(status: 0, message: String(localized: "This manager does not provide this action."))
            }
            preview = next
            error = nil
        } catch { record(error) }
    }

    /// Called only by the user's final confirmation; preparing never agrees to added permissions.
    @discardableResult func confirmUpgrade() async -> Bool {
        guard !busy, let preview, policy?.canCheck == true else { return false }
        busy = true
        defer { busy = false }
        if let problem = preview.confirmationProblem(at: .now) { error = problem; return false }
        guard preview.manifest.id == id, preview.market == policy?.market, preview.from == detail?.plugin.version else {
            error = String(localized: "This manager does not provide this action.")
            return false
        }
        do {
            detail = try await api().upgradeExtension(id, preview: preview)
            self.preview = nil
            candidate = nil
            error = nil
            return true
        } catch { record(error); return false }
    }

    func dismissPreview() {
        guard !busy else { return }
        preview = nil
    }

    private func record(_ error: Error) {
        if let apiError = error as? APIError, [404, 405, 501].contains(apiError.status) {
            self.error = String(localized: "This manager does not provide this action.")
        } else { self.error = error.localizedDescription }
    }
}

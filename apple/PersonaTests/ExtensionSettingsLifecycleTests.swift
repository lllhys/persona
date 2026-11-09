import Foundation
import Testing
@testable import Persona

@MainActor
struct ExtensionSettingsLifecycleTests {
    private static let installed = #"{"id":"p","name":"P","kind":"ui","status":"disabled","source":"installed","enabled":false,"version":"1","origin":"market:m1/p","auto_upgrade":true,"auto_upgrade_paused":"disabled","config":{"value":"saved"},"previous_version":"0","permissions":[]}"#
    private static let upgrades = #"{"auto":true,"upgrades":[{"id":"p","market":"m1","from":"1","to":"2","state":"needs_permission"}]}"#
    private static let catalog = #"{"markets":[{"id":"m1","name":"Market","spec":"https://market.example.com","has_token":false}],"plugins":[{"id":"p","market":"m1","name":"P","version":"2","installed":"1","upgradable":true}]}"#
    private static let preview = #"{"preview_id":"pv","manifest":{"id":"p","version":"2"},"permissions":[{"key":"network","title":"Network"}],"permissions_digest":"digest","upgrade":true,"from":"1","market":"m1","more_permissions":true}"#

    private func decode(_ json: String = Self.installed) throws -> HubExtensionDetail {
        try JSON.decoder.decode(HubExtensionDetail.self, from: Data(json.utf8))
    }

    private func configure(_ hub: FakeHub) {
        hub.on("GET", "/api/v1/plugins/p/-/", json: Self.installed)
        hub.on("GET", "/api/v1/plugins/-/upgrades", json: Self.upgrades)
        hub.on("GET", "/api/v1/plugins/-/catalog", json: Self.catalog)
        hub.on("POST", "/api/v1/plugins/-/catalog/preview", json: Self.preview)
        hub.on("POST", "/api/v1/plugins/p/-/upgrade", json: Self.installed)
    }

    @Test func policySeparatesProvenanceFromCapabilities() throws {
        var detail = try decode()
        let market = ExtensionSettingsPolicy(detail: detail)
        #expect(market.market == "m1" && market.canCheck && market.canAutoUpgrade && market.canEnable)
        #expect(!market.canUninstall && market.uninstallReason == ExtensionSettingsPolicy.uninstallPreviewGap)
        #expect(market.runtime == String(localized: "Not provided"))
        #expect(market.runtime != detail.plugin.kind, "a contribution category is not execution-runtime metadata")
        #expect(ExtensionText.autoUpgradeShown(detail.plugin))
        var renamed = detail
        renamed.plugin.id = "renamed-p"
        let renamedPolicy = ExtensionSettingsPolicy(detail: renamed)
        #expect(renamedPolicy.market == "m1" && renamedPolicy.canCheck)
        #expect(renamedPolicy.provenance == detail.origin, "installation provenance may retain an earlier package ID")
        detail.plugin.source = "builtin"
        let builtin = ExtensionSettingsPolicy(detail: detail)
        #expect(builtin.canEnable && !builtin.canCheck && !builtin.canAutoUpgrade && !builtin.canUninstall)
        #expect(builtin.updateReason == String(localized: "Built-in extensions are updated with Persona."))
        detail.plugin.source = "hub.json"
        #expect(!ExtensionSettingsPolicy(detail: detail).canEnable)
        detail.plugin.source = "dev"
        detail.origin = "/private/example/extension"
        let development = ExtensionSettingsPolicy(detail: detail)
        #expect(development.source == String(localized: "Development"))
        #expect(!development.canCheck && !development.canAutoUpgrade && !development.canUninstall)
        detail.plugin.source = "installed"
        for origin in ["/private/example/extension", "https://user:password@example.com/archive", "remote:private-endpoint", "market:", "market:/p"] {
            detail.origin = origin
            let policy = ExtensionSettingsPolicy(detail: detail)
            #expect(policy.market == nil && !policy.canCheck)
            #expect(policy.provenance == String(localized: "Local installation"))
            #expect(!policy.provenance.contains(origin))
        }
        detail.origin = nil
        #expect(ExtensionSettingsPolicy(detail: detail).provenance == String(localized: "Not provided"))
        detail.origin = "market:m1/p"
        detail.plugin.autoUpgrade = nil
        #expect(!ExtensionSettingsPolicy(detail: detail).canAutoUpgrade)
    }

    @Test func attachedAndSandboxDecodeTheirExistingContract() throws {
        let extra = #", "attached":{"online":false,"since":"2026-11-01T00:00:00Z","error":"offline","version":"2"},"sandbox":{"enabled":true,"default":true,"chosen":false,"active":false,"why":"unsupported","detail":"Unavailable","trusted":true,"egress":{"allowed":3}}"#
        let detail = try decode(String(Self.installed.dropLast()) + extra + "}")
        #expect(detail.plugin.attached?.online == false && detail.plugin.attached?.version == "2")
        #expect(detail.plugin.attached?.since != nil)
        #expect(detail.plugin.sandbox?.enabled == true && detail.plugin.sandbox?.active == false)
        #expect(detail.plugin.sandbox?.egress["allowed"] == .number(3))
        let attached = ExtensionSettingsPolicy(detail: detail)
        #expect(attached.source == String(localized: "Attached"))
        #expect(!attached.canCheck && !attached.canAutoUpgrade && !attached.canUninstall)
        #expect(attached.provenance == String(localized: "Attached"))
    }

    @Test func previewRequiresFinalConfirmationAndBindsEveryRequestToManager() async throws {
        let hub = FakeHub()
        configure(hub)
        let lifecycle = ExtensionSettingsLifecycle(id: "p", manager: "assistant", deps: .init(api: { hub.client }))
        await lifecycle.loadMetadata(detail: try decode())
        await lifecycle.checkForUpdates()
        #expect(lifecycle.globalAuto == true && lifecycle.candidate?.to == "2")
        await lifecycle.prepareUpgrade()
        #expect(lifecycle.preview?.morePermissions == true)
        #expect(hub.sent("POST", "/api/v1/plugins/p/-/upgrade").isEmpty)
        lifecycle.dismissPreview()
        #expect(!((await lifecycle.confirmUpgrade())))
        #expect(hub.sent("POST", "/api/v1/plugins/p/-/upgrade").isEmpty)
        await lifecycle.prepareUpgrade()
        #expect(await lifecycle.confirmUpgrade())
        let request = try #require(hub.sent("POST", "/api/v1/plugins/p/-/upgrade").first)
        let body = try JSON.decoder.decode(JSONValue.self, from: request.body ?? Data())
        #expect(body["preview_id"] == .string("pv") && body["permissions_digest"] == .string("digest"))
        #expect(hub.requests.allSatisfy { $0.headers[APIClient.managerHeader] == "assistant" })
        #expect(hub.requests.allSatisfy { $0.method != "DELETE" && !$0.path.hasSuffix("rollback") })
        #expect(lifecycle.previousVersion == "0" && lifecycle.rollbackReason == ExtensionSettingsPolicy.rollbackPreviewGap)
    }

    @Test func unavailableAndMissingMarketsNeverLookUpToDateOrClearPageDraft() async throws {
        let hub = FakeHub()
        configure(hub)
        let page = ExtensionModel(id: "p", deps: .init(api: { hub.client }))
        await page.load()
        page.draft["value"] = .string("editing")
        page.numberText["count"] = "not-a-number"
        let lifecycle = ExtensionSettingsLifecycle(id: "p", deps: .init(api: { hub.client }))
        await lifecycle.loadMetadata(detail: try #require(page.detail))
        hub.on("GET", "/api/v1/plugins/-/catalog", json: #"{"markets":[{"id":"m1","name":"Market","has_token":false,"error":"unreachable"}],"plugins":[]}"#)
        await lifecycle.checkForUpdates()
        #expect(lifecycle.error == String(localized: "The extension market is unavailable. Check its source and try again."))
        #expect(lifecycle.candidate != nil)
        hub.on("GET", "/api/v1/plugins/-/catalog", json: #"{"markets":[{"id":"m1","name":"Market","has_token":false}],"plugins":[]}"#)
        await lifecycle.checkForUpdates()
        #expect(lifecycle.error == String(localized: "This extension is no longer listed in its market."))
        #expect(lifecycle.candidate != nil)
        #expect(page.draft["value"] == .string("editing") && page.numberText["count"] == "not-a-number")
    }

    @Test func busyPreventsReentrantRequests() async throws {
        let hub = FakeHub()
        configure(hub)
        let lifecycle = ExtensionSettingsLifecycle(id: "p", deps: .init(api: { hub.client }))
        await lifecycle.loadMetadata(detail: try decode())
        await lifecycle.prepareUpgrade()
        let prepared = lifecycle.preview
        hub.on("POST", "/api/v1/plugins/p/-/upgrade") { _ in
            #expect(lifecycle.busy)
            lifecycle.dismissPreview()
            #expect(lifecycle.preview == prepared, "a busy confirmation cannot discard its preview")
            return Data(Self.installed.utf8)
        }
        #expect(await lifecycle.confirmUpgrade())
        #expect(!lifecycle.busy)
        #expect(!((await lifecycle.confirmUpgrade())))
        #expect(hub.sent("POST", "/api/v1/plugins/p/-/upgrade").count == 1)
    }

    @Test func autoUpgradeHasItsOwnGuardAndKeepsTheDraft() async throws {
        let hub = FakeHub()
        configure(hub)
        let page = ExtensionModel(id: "p", manager: "assistant", deps: .init(api: { hub.client }))
        await page.load()
        page.draft["value"] = .string("editing")
        hub.on("PUT", "/api/v1/plugins/p/-/auto-upgrade") { req in
            #expect(page.changingAutoUpgrade)
            #expect(req.headers[APIClient.managerHeader] == "assistant")
            return Data(Self.installed.utf8)
        }
        await page.setAutoUpgrade(false)
        #expect(!page.changingAutoUpgrade && page.draft["value"] == .string("editing"))
        #expect(hub.sent("PUT", "/api/v1/plugins/p/-/auto-upgrade").count == 1)
    }
}

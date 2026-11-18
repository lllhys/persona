import Foundation
import Testing
@testable import Persona

/// The badges on the ways to Settings (its tab, the drawer's button) count the extensions' entries read naming no
/// assistant, the default assistant's, only while the default is current: Settings shows the current assistant's under
/// Extensions, so with another one current they would point at nothing there. Made-up values.
@MainActor
struct SettingsBadgeTests {
    private static let slots = #"""
    {"ui":[{"plugin":"example-gateway","id":"usage","title":"Usage","placement":"nav_page","icon":"chart","badge":{"text":"3","tone":"danger"}}]}
    """#

    @Test func theBadgeCountsTheDefaultsEntriesWhileItIsCurrent() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/plugins/-/ui", json: Self.slots)
        let entries = ExtensionSlots(deps: .init(api: { hub.client }), features: { ["plugin_ui_slots"] })
        await entries.reload()
        #expect(entries.total.count == 3)

        let model = AppModel(stateFolder: FileManager.default.temporaryDirectory.appending(path: "settings-badge-" + UUID().uuidString))
        model.linkHooksForTests = (transport: try #require(hub.client.transport), socket: { SyncSocket() })
        model.connectOnly(.demo)
        model.managerRosters[""] = ManagerRoster(managers: [
            HubManager(id: "assistant", name: "Home", online: true, isDefault: true, builtin: true),
            HubManager(id: "work", name: "Work", online: true),
        ], defaultID: "assistant")
        #expect(entries.settingsTotal(model).count == 3, "the default current: as before")
        model.selectAssistantManager("work")
        let shown = entries.settingsTotal(model)
        #expect(shown.count == 0 && !shown.dot, "another assistant current: nothing Settings shows")
        model.selectAssistantManager("assistant")
        #expect(entries.settingsTotal(model).count == 3)
    }
}

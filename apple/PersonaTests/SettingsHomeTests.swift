import Foundation
import Testing
@testable import Persona

/// Settings names the current assistant whenever the hub answers a roster, even when that roster contains only one
/// assistant. A legacy hub without a roster keeps the neutral heading. Made-up values.
@MainActor
struct SettingsHomeTests {
    /// The default (`assistant`) and `work`.
    private static let roster = ManagerRoster(managers: [
        HubManager(id: "assistant", name: "助手", online: true, isDefault: true, builtin: true),
        HubManager(id: "work", name: "工作", online: true),
    ], defaultID: "assistant")
    private static let one = ManagerRoster(managers: [
        HubManager(id: "assistant", name: "助手", online: true, isDefault: true, builtin: true),
    ], defaultID: "assistant")

    /// A model on one made-up hub: no socket, its requests go to `hub`.
    private func model(_ hub: FakeHub = FakeHub(), roster: ManagerRoster? = roster) throws -> AppModel {
        let model = AppModel(stateFolder: FileManager.default.temporaryDirectory.appending(path: "settings-home-" + UUID().uuidString))
        model.linkHooksForTests = (transport: try #require(hub.client.transport), socket: { SyncSocket() })
        model.connectOnly(.demo)
        model.managerRosters[""] = roster
        return model
    }

    @Test func multiAssistantSettingsNameTheCurrentAssistant() throws {
        let model = try model()
        var content = SettingsAssistantHeading.content(for: model)
        #expect(content.manager?.id == "assistant")
        #expect(content.title == "助手的设置")
        #expect(content.showsSwitch)
        #expect(content.footnote == "只显示「助手」自己的内容。要改别的助手，先切换过去。")

        model.selectAssistantManager("work")
        content = SettingsAssistantHeading.content(for: model)
        #expect(content.manager?.id == "work")
        #expect(content.title == "工作的设置")
        #expect(content.showsSwitch)
        #expect(content.footnote == "只显示「工作」自己的内容。要改别的助手，先切换过去。")
    }

    @Test func singleAssistantSettingsStillOfferTheSwitcher() throws {
        let content = SettingsAssistantHeading.content(for: try model(roster: Self.one))
        #expect(content.manager?.id == "assistant")
        #expect(content.title == "助手的设置")
        #expect(content.showsSwitch)
        #expect(content.footnote == "只显示「助手」自己的内容。要改别的助手，先切换过去。")
    }

    @Test func legacyHubSettingsKeepANeutralHeadingWithoutSwitcherOrFootnote() throws {
        let content = SettingsAssistantHeading.content(for: try model(roster: nil))
        #expect(content.manager == nil)
        #expect(content.title == "当前助手的设置")
        #expect(!content.showsSwitch)
        #expect(content.footnote == nil)
    }

    @Test func sharedSettingsGroupsHaveOneScopeIndependentFooter() {
        #expect(SettingsAssistantHeading.commonGroupsFooter == "这两组和当前是哪个助手无关。")
    }
}

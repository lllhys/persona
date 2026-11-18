import Foundation
import Testing
@testable import Persona

/// The sidebar shows the current assistant whenever the hub has a roster and an installed current assistant, including
/// a roster of one. Filtering, request naming and waiting counts still start only with several installed assistants.
@MainActor
struct AssistantIdentityTests {
    private func model(_ roster: ManagerRoster?) throws -> AppModel {
        let hub = FakeHub()
        let model = AppModel(stateFolder: FileManager.default.temporaryDirectory.appending(path: "assistant-identity-" + UUID().uuidString))
        model.linkHooksForTests = (transport: try #require(hub.client.transport), socket: { SyncSocket() })
        model.connectOnly(.demo)
        model.managerRosters[""] = roster
        return model
    }

    private func assistant(online: Bool = true) -> HubManager {
        HubManager(id: "assistant", name: "助手", online: online, isDefault: true, builtin: true)
    }

    @Test(arguments: [true, false])
    func oneInstalledAssistantShowsTheSwitcherWhetherOnlineOrOffline(online: Bool) throws {
        let model = try model(ManagerRoster(managers: [assistant(online: online)], defaultID: "assistant"))
        #expect(model.showsAssistantSwitcher)
        let current = try #require(model.currentAssistant)
        #expect(current.id == "assistant" && current.name == "助手" && current.online == online)

        // The switcher's wider gate does not turn one assistant into a scoped or named one. Establish the hub-level
        // group explicitly so these nil/empty results exercise the isMulti gates rather than a missing hub.
        #expect(model.hub?.group == "")
        #expect(model.managerRoster?.isMulti == false)
        #expect(model.assistantManagerID == nil)
        #expect(model.assistantManager == nil)
        #expect(model.assistantScope == nil)
        #expect(model.assistantBadges.isEmpty)
        #expect(!model.othersWaiting)
    }

    @Test func severalInstalledAssistantsShowTheSwitcher() throws {
        let work = HubManager(id: "m-1", name: "工作", online: true)
        let model = try model(ManagerRoster(managers: [assistant(), work], defaultID: "assistant"))
        #expect(model.showsAssistantSwitcher)
        #expect(model.currentAssistant?.id == "assistant")
    }

    @Test func aLegacyHubWithoutARosterDoesNotShowTheSwitcher() throws {
        let model = try model(nil)
        #expect(!model.showsAssistantSwitcher)
        #expect(model.currentAssistant == nil)
    }

    @Test func anEmptyRosterDoesNotShowTheSwitcher() throws {
        let model = try model(ManagerRoster(managers: [], defaultID: "assistant"))
        #expect(!model.showsAssistantSwitcher)
        #expect(model.currentAssistant == nil)
    }

    @Test func aRosterWithOnlyAnInstallingAssistantDoesNotShowTheSwitcher() throws {
        let installing = HubManager(id: "m-installing", name: "安装中", online: false,
                                    installing: Date(timeIntervalSince1970: 1_800_000_000))
        let model = try model(ManagerRoster(managers: [installing], defaultID: installing.id))
        #expect(!model.showsAssistantSwitcher)
        #expect(model.currentAssistant == nil)
    }

    @Test func oneInstalledAndOneInstallingAssistantShowTheSwitcher() throws {
        let installing = HubManager(id: "m-installing", name: "安装中", online: false,
                                    installing: Date(timeIntervalSince1970: 1_800_000_000))
        let model = try model(ManagerRoster(managers: [assistant(), installing], defaultID: "assistant"))
        #expect(model.showsAssistantSwitcher)
        #expect(model.currentAssistant?.id == "assistant")
        #expect(model.managerRoster?.isMulti == false, "an installing assistant is not a second installed assistant")
    }
}

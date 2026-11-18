import Foundation
import Testing
@testable import Persona

/// Model settings for what runs on the default assistant whichever is current (Translation's model, a voice call's model,
/// the inbox's triage banner) are the default assistant's (docs/assistant/managers.md "The current assistant"): their
/// sheets read and save naming no manager, also while another assistant is current; Settings' own Models stay the
/// current assistant's. With another assistant current, Translation and the default's editor say whose settings they are.
/// Made-up values.
@MainActor
struct ModelSettingsOwnerTests {
    private static let header = APIClient.managerHeader
    private static let detail = #"{"purpose":{"id":"translate","category_id":"translate","name":"Translation","group":"Models","kind":"launcher","uses":[],"override":{},"effective":{"model":"model-a"},"revision":0,"fields":["provider","model"]},"providers":[]}"#

    private func named(_ requests: [LocalRequest]) -> [String?] { requests.map { $0.headers[Self.header] } }

    @Test func theOwnerPicksTheClient() {
        let app = FakeHub().client.showing("work")
        #expect(ModelSettingsOwner.current.client(app).managerID == "work")
        let home = ModelSettingsOwner.defaultAssistant.client(app)
        #expect(home.managerID == nil && home.shownManager == nil, "the default's names none")
        #expect(ModelSettingsOwner.current.client(FakeHub().client.showing(nil)).managerID == nil)
    }

    /// The default's sheet reads and saves the default's model with another assistant current; the current owner names it.
    @Test func theDefaultsSheetReadsAndSavesTheDefaultsModel() async {
        for (owner, want) in [(ModelSettingsOwner.defaultAssistant, nil), (.current, "work")] as [(ModelSettingsOwner, String?)] {
            let hub = FakeHub()
            hub.on("GET", "/api/v1/model-config/purposes/translate", json: Self.detail)
            hub.on("PATCH", "/api/v1/model-config/purposes/translate", json: Self.detail)
            let page = ModelConfigurationModel(deps: .init(api: { hub.client.showing("work") }))
            page.owner = owner
            await page.load(purposeID: "translate")
            page.draft.model = "model-b"
            #expect(await page.save())
            #expect(hub.requests.count == 2)
            #expect(named(hub.requests) == [want, want], "\(owner): \(named(hub.requests))")
        }
    }

    @Test func translationSaysWhoseModelItIs() throws {
        let hub = FakeHub()
        let model = AppModel(stateFolder: FileManager.default.temporaryDirectory.appending(path: "model-owner-" + UUID().uuidString))
        model.linkHooksForTests = (transport: try #require(hub.client.transport), socket: { SyncSocket() })
        model.connectOnly(.demo)
        model.managerRosters[""] = ManagerRoster(managers: [
            HubManager(id: "assistant", name: "助手", online: true, isDefault: true, builtin: true),
            HubManager(id: "work", name: "工作", online: true),
        ], defaultID: "assistant")
        #expect(TranslationModelNote.defaultName(model) == nil, "the default current: nothing to say")
        model.selectAssistantManager("work")
        #expect(TranslationModelNote.defaultName(model) == "助手")
        #expect(TranslationModelNote.text("助手") == "翻译用的是默认助手「助手」的模型，不是当前助手的。")
        model.managerRosters[""] = ManagerRoster(managers: [HubManager(id: "assistant", name: "助手", online: true, isDefault: true)])
        #expect(TranslationModelNote.defaultName(model) == nil, "one assistant: nothing to tell apart")
    }

    @Test func defaultAssistantEditorSaysWhoseSettingsTheyAre() throws {
        let hub = FakeHub()
        let model = AppModel(stateFolder: FileManager.default.temporaryDirectory.appending(path: "model-owner-" + UUID().uuidString))
        model.linkHooksForTests = (transport: try #require(hub.client.transport), socket: { SyncSocket() })
        model.connectOnly(.demo)
        model.managerRosters[""] = ManagerRoster(managers: [
            HubManager(id: "assistant", name: "助手", online: true, isDefault: true, builtin: true),
            HubManager(id: "work", name: "工作", online: true),
        ], defaultID: "assistant")
        #expect(ModelPurposeEditorView.defaultAssistantNote(owner: .defaultAssistant, model: model) == nil,
                "the default current: nothing to say")
        model.selectAssistantManager("work")
        #expect(ModelPurposeEditorView.defaultAssistantNote(owner: .defaultAssistant, model: model) == "这是默认助手「助手」的设置。")
        #expect(ModelPurposeEditorView.defaultAssistantNote(owner: .current, model: model) == nil,
                "the current assistant's own editor: nothing to say")
    }
}

import Foundation
import Testing
@testable import Persona

/// A page's Subscriptions entry is its own assistant's (docs/extensions/subscriptions.md "Pages"): each assistant's manager
/// keeps its own subscriptions and the hub sends the summary to the one a request names, so an extension's or a
/// connector's page reads it naming the page's manager (the current assistant's in Settings; none for the default's) and
/// opens that assistant's subscription pages. One that can't be read leaves the row as it was. Made-up values.
@MainActor
struct SettingsSubscriptionsOwnerTests {
    private static let summary = #"{"connectors":[{"connector":"github","extension":"example","subscribable":true,"on":2}]}"#
    private static let roster = ManagerRoster(managers: [
        HubManager(id: "assistant", name: "助手", online: true, isDefault: true, builtin: true),
        HubManager(id: "work", name: "工作", online: true),
    ], defaultID: "assistant")

    /// A model on one made-up hub: no socket, its requests go to `hub`.
    private func model(_ hub: FakeHub = FakeHub()) throws -> AppModel {
        let model = AppModel(stateFolder: FileManager.default.temporaryDirectory.appending(path: "settings-subscriptions-owner-" + UUID().uuidString))
        model.linkHooksForTests = (transport: try #require(hub.client.transport), socket: { SyncSocket() })
        model.connectOnly(.demo)
        model.managerRosters[""] = Self.roster
        return model
    }

    @Test func aPagesSummaryNamesItsAssistant() async {
        for manager in ["work", nil] as [String?] {
            let hub = FakeHub()
            hub.on("GET", "/api/v1/subscriptions/summary", json: Self.summary)
            // The app's client with "work" current names it on the assistant's own routes only; the page names its own.
            let lines = await SubscriptionCounts.summary(hub.client.showing("work"), manager: manager)
            #expect(SubscriptionCounts.of(lines ?? []) { $0.connector == "github" } == SubscriptionCounts(on: 2, failing: 0))
            #expect(hub.requests.map { $0.headers[APIClient.managerHeader] } == [manager], "\(manager ?? "the default")")
        }
        let down = FakeHub()
        #expect(await SubscriptionCounts.summary(down.client, manager: "work") == nil, "can't be read: the row keeps what it had")
    }

    /// The entry opens the page's assistant's subscription pages; offline replacement follows the same owner normalization.
    @Test func theEntryOpensItsAssistantsPages() throws {
        let model = try model()
        let named = SettingsPage.subscriptions(state: "connector:github", manager: "work")
        let unnamed = SettingsPage.subscriptions(state: "connector:github")
        #expect(named != unnamed)
        #expect(unnamed == SettingsPage.subscriptions(state: "connector:github", manager: nil))
        #expect(unnamed.belongsToCurrentAssistant(in: model))
        #expect(!named.belongsToCurrentAssistant(in: model))

        model.selectAssistantManager("work")
        #expect(named.belongsToCurrentAssistant(in: model))
        #expect(unnamed.belongsToCurrentAssistant(in: model), "an unstamped route follows the current assistant")
    }
}

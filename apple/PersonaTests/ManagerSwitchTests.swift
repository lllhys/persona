import Foundation
import Testing
@testable import Persona

/// The Persona page's manager switcher (docs/assistant/managers.md "Clients"): the assistant's routes name the manager the
/// page shows, except one conversation's and the health space's, and an item's client names only its own manager, which
/// stamps the conversations relayed from it; the page starts on the default, goes back to it when its manager leaves the
/// roster, reads itself again when the default moves or its manager is back, lists that manager's conversations,
/// holds what it read of each manager in this run, opens a conversation from elsewhere on its own manager's page, and
/// sends nothing to an offline one. Made-up values.
@MainActor
struct ManagerSwitchTests {
    private static let header = "X-Persona-Manager"

    /// The default (`assistant`), `m-1` online and `m-2` offline.
    private static let roster = #"""
    {"default":"assistant","managers":[
      {"id":"assistant","name":"助手","online":true,"default":true,"builtin":true},
      {"id":"m-1","name":"工作","online":true},
      {"id":"m-2","name":"家里","online":false,"last_seen":"2026-11-08T05:00:00Z"}]}
    """#
    private static let withoutWork = #"""
    {"default":"assistant","managers":[
      {"id":"assistant","name":"助手","online":true,"default":true,"builtin":true},
      {"id":"m-2","name":"家里","online":false}]}
    """#
    private static let oneManager = #"{"default":"assistant","managers":[{"id":"assistant","name":"助手","online":true,"default":true}]}"#

    private static func roster(_ json: String = roster) throws -> ManagerRoster {
        ManagerRoster(try JSON.decoder.decode(APIClient.ManagerList.self, from: Data(json.utf8)))
    }

    private func session(_ id: String, manager: String?, archived: Bool = false, space: String? = nil) throws -> Session {
        let stamp = manager.map { #","manager":"\#($0)""# } ?? ""
        let where_ = space.map { #","space":"\#($0)""# } ?? ""
        return try JSON.decoder.decode(Session.self, from: Data(
            #"{"machine_id":"box","session_id":"\#(id)","status":"idle","assistant":true,"archived":\#(archived)\#(stamp)\#(where_)}"#.utf8))
    }

    /// A model on one made-up hub: no socket, its requests go to `hub`.
    private func model(_ hub: FakeHub, roster: ManagerRoster?) throws -> AppModel {
        let transport = try #require(hub.client.transport)
        let model = AppModel(stateFolder: FileManager.default.temporaryDirectory.appending(path: "manager-switch-" + UUID().uuidString))
        model.linkHooksForTests = (transport: transport, socket: { SyncSocket() })
        model.connectOnly(.demo)
        model.managerRosters[""] = roster
        return model
    }

    // MARK: requests

    @Test func onlyTheAssistantsOwnRoutesGoByThePage() {
        #expect(APIClient.assistantRoute("/api/v1/assistant", query: [:]))
        #expect(APIClient.assistantRoute("/api/v1/assistant/main/new", query: [:]))
        #expect(APIClient.assistantRoute("/api/v1/assistant/conversations", query: [:]), "starting one is the page's")
        #expect(!APIClient.assistantRoute("/api/v1/assistant/conversations/c1/feed", query: [:]), "one conversation's: its own manager's")
        #expect(!APIClient.assistantRoute("/api/v1/assistant/memory", query: ["space": "health"]), "the health space: the default's")
        #expect(!APIClient.assistantRoute("/api/v1/assistant?space=health", query: [:]))
        #expect(APIClient.assistantRoute("/api/v1/assistant/memory", query: ["space": ""]), "an empty space is none")
        #expect(!APIClient.assistantRoute("/api/v1/assistants", query: [:]) && !APIClient.assistantRoute("/api/v1/inbox", query: [:]))
    }

    @Test func theRequestsNameThePagesManagerOrTheItemsOwn() async throws {
        let hub = FakeHub()
        let page = hub.client.showing("m-1")
        _ = try? await page.assistant()
        _ = try? await page.scheduledTasks()
        _ = try? await page.startConversation(prompt: "hi")
        _ = try? await page.startConversation(prompt: "hi", space: Session.healthSpace)
        _ = try? await page.healthConversations()
        _ = try? await page.archiveConversation("c1", archived: true)
        _ = try? await page.managers()
        _ = try? await page.manager(nil).assistant()
        _ = try? await page.manager("m-2").archiveConversation("c2", archived: true)
        _ = try? await hub.client.assistant()
        let named = hub.requests.map { $0.headers[Self.header] }
        #expect(named == ["m-1", "m-1", "m-1", nil, nil, nil, nil, nil, "m-2", nil],
                "the page's routes name it; a health start, the health list, a conversation's route, another API, the default item's client and no pick name none; an item's client names its own")
        #expect(hub.requests[3].path == "/api/v1/assistant/conversations")
    }

    @Test func anotherManagersRelayedConversationsAreStampedWithIt() async throws {
        let saved = UserDefaults.standard.object(forKey: "assistant.current")
        defer { UserDefaults.standard.set(saved, forKey: "assistant.current") }
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant", json: #"{"machine_id":"w","launcher":"codex","cwd":"/w","conversations":[{"machine_id":"w","session_id":"w1","status":"idle","assistant":true},{"machine_id":"w","session_id":"w2","status":"idle","assistant":true,"manager":"m-2"}]}"#)
        hub.on("POST", "/api/v1/assistant/conversations", json: #"{"session":{"machine_id":"w","session_id":"w3","status":"idle","assistant":true}}"#)
        let page = hub.client.showing("m-1")
        #expect(try await page.assistant().conversations.map(\.manager) == ["m-1", "m-2"], "a stamp it came with stays")
        #expect(try await page.startConversation(prompt: "hi").manager == "m-1")
        #expect(try await page.startConversation(prompt: "hi", space: Session.healthSpace).manager == nil, "the health space: the default's")
        #expect(try await page.manager("m-2").startConversation(prompt: "hi").manager == "m-2")
        #expect(try await hub.client.assistant().conversations.map(\.manager) == [nil, "m-2"], "the default's page names none and stamps none")

        let model = try model(hub, roster: try Self.roster())
        model.selectAssistantManager("m-1")
        let api = try #require(model.api)
        let s = try await api.startConversation(prompt: "hi")
        model.began(s, text: "hi")
        #expect(model.assistantConversations.map(\.sessionID) == ["w3"], "listed on its manager's page")
        model.openAssistantConversation(s.key)
        #expect(model.assistantManagerID == "m-1", "and opened there")
    }

    @Test func aTimeZoneSavedForTheManagerShownBeforeDoesntLand() async throws {
        let saved = UserDefaults.standard.object(forKey: "assistant.current")
        defer { UserDefaults.standard.set(saved, forKey: "assistant.current") }
        let hub = FakeHub()
        let tz = TimeZone.current.identifier
        hub.on("GET", "/api/v1/assistant") { req in
            Data((req.headers[Self.header] == "m-1" ? #"{"machine_id":"w","launcher":"codex","cwd":"/w","conversations":[],"timezone":"\#(tz)"}"#
                                                     : #"{"machine_id":"a","launcher":"claude","cwd":"/a","conversations":[],"timezone":"Nowhere/Else"}"#).utf8)
        }
        let model = try model(hub, roster: try Self.roster())
        hub.on("PUT", "/api/v1/assistant") { _ in
            model.selectAssistantManager("m-1") // the page moves on while the default's zone is saved
            return Data(#"{"machine_id":"a","launcher":"late","cwd":"/a","conversations":[],"timezone":"\#(tz)"}"#.utf8)
        }
        await model.refreshAssistant()
        #expect(model.assistantManagerID == "m-1" && model.assistantConfig?.launcher != "late")
        #expect(model.assistantByGroup[model.hub?.group ?? ""]?.launcher == "claude", "the default's held settings are what its page read")
    }

    @Test func anotherDefaultOrTheShownManagerBackOnlineReadsThePageAgain() async throws {
        let saved = UserDefaults.standard.object(forKey: "assistant.current")
        defer { UserDefaults.standard.set(saved, forKey: "assistant.current") }
        let hub = FakeHub()
        let tz = TimeZone.current.identifier
        hub.on("GET", "/api/v1/assistant", json: #"{"machine_id":"a","launcher":"claude","cwd":"/a","conversations":[],"timezone":"\#(tz)"}"#)
        let model = try model(hub, roster: try Self.roster())
        func reads(_ m: String?) -> Int { hub.sent("GET", "/api/v1/assistant").filter { $0.headers[Self.header] == m }.count }
        model.selectAssistantManager("m-2")
        await waitUntil { reads("m-2") == 1 }
        hub.on("GET", "/api/v1/managers", json: #"""
        {"default":"assistant","managers":[{"id":"assistant","name":"助手","online":true,"default":true,"builtin":true},
          {"id":"m-1","name":"工作","online":true},{"id":"m-2","name":"家里","online":true}]}
        """#)
        await model.refreshManagerRoster("")
        #expect(model.assistantManagerOffline == nil)
        await waitUntil { reads("m-2") == 2 }
        #expect(reads("m-2") == 2, "back online: its page is read again")

        model.selectAssistantManager(nil)
        await waitUntil { model.assistantConfig?.launcher == "claude" }
        hub.on("GET", "/api/v1/managers", json: #"""
        {"default":"m-1","managers":[{"id":"assistant","name":"助手","online":true,"builtin":true},
          {"id":"m-1","name":"工作","online":true,"default":true},{"id":"m-2","name":"家里","online":true}]}
        """#)
        let before = reads(nil)
        await model.refreshManagerRoster("")
        #expect(model.assistantConfig == nil, "what was held as the default's is the one before's")
        await waitUntil { reads(nil) == before + 1 }
        #expect(reads(nil) == before + 1, "the new default's page is read")
    }

    @Test func theDefaultMovingUnderAnotherManagersPageReadsTheNewDefaultsSettings() async throws {
        let saved = UserDefaults.standard.object(forKey: "assistant.current")
        defer { UserDefaults.standard.set(saved, forKey: "assistant.current") }
        let hub = FakeHub()
        let tz = TimeZone.current.identifier
        hub.on("GET", "/api/v1/assistant") { req in
            Data((req.headers[Self.header] == "m-1" ? #"{"machine_id":"w","launcher":"codex","cwd":"/w","conversations":[],"timezone":"\#(tz)"}"#
                                                     : #"{"machine_id":"h","launcher":"claude","cwd":"/h","conversations":[],"timezone":"\#(tz)"}"#).utf8)
        }
        let model = try model(hub, roster: try Self.roster())
        model.selectAssistantManager("m-1")
        await waitUntil { model.assistantConfig?.launcher == "codex" }
        hub.on("GET", "/api/v1/managers", json: #"""
        {"default":"m-2","managers":[{"id":"assistant","name":"助手","online":true,"builtin":true},
          {"id":"m-1","name":"工作","online":true},{"id":"m-2","name":"家里","online":true,"default":true}]}
        """#)
        await model.refreshManagerRoster("")
        #expect(model.assistantManagerID == "m-1")
        let group = model.hub?.group ?? ""
        await waitUntil { model.assistantByGroup[group] != nil }
        #expect(model.assistantByGroup[group]?.launcher == "claude", "Health and the channels have the new default's settings")
    }

    @Test func aSettingsReadForAPageLeftDoesntSayTheyCouldntLoad() async throws {
        let saved = UserDefaults.standard.object(forKey: "assistant.current")
        defer { UserDefaults.standard.set(saved, forKey: "assistant.current") }
        let hub = FakeHub()
        let tz = TimeZone.current.identifier
        hub.on("GET", "/api/v1/assistant") { req in
            Data((req.headers[Self.header] == "m-1" ? #"{"machine_id":"w","launcher":"codex","cwd":"/w","conversations":[],"timezone":"\#(tz)"}"#
                                                     : #"{"machine_id":"a","launcher":"","cwd":"/a","conversations":[],"timezone":"Nowhere/Else"}"#).utf8)
        }
        let model = try model(hub, roster: try Self.roster())
        hub.on("PUT", "/api/v1/assistant") { _ in
            model.selectAssistantManager("m-1") // the page moves on while the default's zone is saved, which fails
            throw APIError(status: 500, message: "down")
        }
        await model.refreshAssistant()
        #expect(model.assistantManagerID == "m-1" && !model.assistantConfigLoadFailed)
    }

    @Test func noMainConversationStartsBeforeItsManagersSettingsAreRead() async throws {
        let saved = UserDefaults.standard.object(forKey: "assistant.current")
        defer { UserDefaults.standard.set(saved, forKey: "assistant.current") }
        let hub = FakeHub()
        let tz = TimeZone.current.identifier
        hub.on("GET", "/api/v1/assistant") { _ in throw APIError(status: 503, message: "away") }
        hub.on("POST", "/api/v1/assistant/conversations",
               json: #"{"session":{"session_id":"s1","status":"working","state":"working","assistant":true,"pending_interactions":0}}"#)
        let model = try model(hub, roster: try Self.roster())
        func main() throws -> Any? {
            let body = try #require(hub.sent("POST", "/api/v1/assistant/conversations").last?.body)
            return try #require(JSONSerialization.jsonObject(with: body) as? [String: Any])["main"]
        }
        model.selectAssistantManager("m-1")
        await model.askPersona("hello")
        #expect(try main() == nil, "whether m-1 has a main conversation is unknown: a plain one, which replaces none")
        hub.on("GET", "/api/v1/assistant", json: #"{"machine_id":"w","launcher":"codex","cwd":"/w","conversations":[],"timezone":"\#(tz)"}"#)
        await model.refreshAssistant()
        await model.askPersona("hello again")
        #expect(try main() as? Bool == true, "read, with none: the first becomes it")
    }

    private func waitUntil(_ what: () -> Bool) async {
        let deadline = Date.now + 5
        while !what(), Date.now < deadline { try? await Task.sleep(for: .milliseconds(20)) }
    }

    // MARK: the page's manager

    @Test func thePageStartsOnTheDefaultAndShowsThePick() throws {
        let hub = FakeHub()
        let model = try model(hub, roster: try Self.roster())
        #expect(model.assistantManagerID == nil && model.assistantManager?.id == "assistant" && model.api?.shownManager == nil)
        model.selectAssistantManager("m-1")
        #expect(model.assistantManagerID == "m-1" && model.assistantManager?.name == "工作")
        #expect(model.api?.shownManager == "m-1" && model.api("")?.shownManager == "m-1")
        let before = try #require(model.api)
        model.selectAssistantManager("gone")
        #expect(model.assistantManagerID == nil, "one the roster doesn't have: the default")
        #expect(!model.showsPage(of: before), "what was read for the manager before doesn't land")
        model.selectAssistantManager("m-2")
        #expect(model.assistantManagerOffline?.id == "m-2")
        model.selectAssistantManager("assistant")
        #expect(model.assistantManagerID == nil && model.assistantPicks.isEmpty)

        model.managerRosters[""] = try Self.roster(Self.oneManager)
        model.selectAssistantManager("m-1")
        #expect(model.assistantManagerID == nil && model.assistantManager == nil && model.assistantPicks.isEmpty, "one manager: no switcher")
    }

    @Test func eachManagersSettingsComeBackWithIt() async throws {
        let saved = UserDefaults.standard.object(forKey: "assistant.current")
        defer { UserDefaults.standard.set(saved, forKey: "assistant.current") }
        let hub = FakeHub()
        let tz = TimeZone.current.identifier
        hub.on("GET", "/api/v1/assistant") { req in
            let other = req.headers[Self.header] == "m-1"
            return Data((other ? #"{"machine_id":"w","launcher":"codex","cwd":"/w","conversations":[],"timezone":"\#(tz)"}"#
                               : #"{"machine_id":"a","launcher":"claude","cwd":"/a","conversations":[],"timezone":"\#(tz)","main":"main-a"}"#).utf8)
        }
        let model = try model(hub, roster: try Self.roster())
        await model.refreshAssistant()
        #expect(model.assistantConfig?.main == "main-a")
        model.selectAssistantManager("m-1")
        #expect(model.assistantConfig == nil, "nothing read of it yet in this run")
        await model.refreshAssistant()
        #expect(model.assistantConfig?.launcher == "codex" && model.assistantConfig?.main == nil)
        #expect(model.healthMachineID == "a", "the health space stays on the default's machine")
        model.selectAssistantManager(nil)
        #expect(model.assistantConfig?.main == "main-a", "the default's, held")
        model.selectAssistantManager("m-1")
        #expect(model.assistantConfig?.launcher == "codex", "what it read in this run")
    }

    @Test func aManagerLeavingTheRosterTakesThePageBackToTheDefault() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/managers", json: Self.withoutWork)
        let model = try model(hub, roster: try Self.roster())
        model.selectAssistantManager("m-1")
        await model.refreshManagerRoster("")
        #expect(model.assistantManagerID == nil && model.assistantManager?.id == "assistant" && model.api?.shownManager == nil)
        #expect(model.assistantPicks.isEmpty, "it doesn't come back with the manager")
    }

    @Test func theListsAreTheShownManagers() throws {
        let roster = try Self.roster()
        let sessions = [try session("a1", manager: "assistant"), try session("w1", manager: "m-1"), try session("n1", manager: nil),
                        try session("x1", manager: "gone"), try session("w2", manager: "m-1", archived: true),
                        try session("h1", manager: "assistant", space: Session.healthSpace), try session("h2", manager: "m-1", space: Session.healthSpace)]
        let every = AssistantLists(sessions, group: "")
        #expect(Set(every.open.map(\.sessionID)) == ["a1", "w1", "n1", "x1"] && every.health.count == 2, "one manager: every conversation")
        let work = AssistantLists(sessions, group: "", shown: .init(roster: roster, manager: "m-1"))
        #expect(work.open.map(\.sessionID) == ["w1"] && work.archived.map(\.sessionID) == ["w2"])
        #expect(work.health.map(\.sessionID) == ["h1"], "the health space stays the default's")
        let mine = AssistantLists(sessions, group: "", shown: .init(roster: roster, manager: "assistant"))
        #expect(Set(mine.open.map(\.sessionID)) == ["a1", "n1", "x1"], "no stamp, or one the roster doesn't have: the default's")
    }

    @Test func aConversationOpenedElsewhereShowsItsManagersPage() throws {
        let saved = UserDefaults.standard.object(forKey: "assistant.current")
        defer { UserDefaults.standard.set(saved, forKey: "assistant.current") }
        let hub = FakeHub()
        let model = try model(hub, roster: try Self.roster())
        model.applySessions([MachineRef(group: "", id: "box"): [try session("w1", manager: "m-1"), try session("a1", manager: "assistant"),
                                                                 try session("h1", manager: "assistant", space: Session.healthSpace)]])
        model.openAssistantConversation(SessionKey(machineID: "box", sessionID: "w1"))
        #expect(model.assistantManagerID == "m-1" && model.assistantCurrent?.sessionID == "w1")
        #expect(model.assistantConversations.map(\.sessionID) == ["w1"])
        #expect(model.defaultAssistantConversations.map(\.sessionID) == ["a1"], "a channel's targets stay the default's")
        model.openAssistantConversation(SessionKey(machineID: "box", sessionID: "not-listed"), manager: "assistant")
        #expect(model.assistantManagerID == nil, "one not listed yet: the manager it came with")
        model.openAssistantConversation(SessionKey(machineID: "box", sessionID: "w1"))
        model.openAssistantConversation(SessionKey(machineID: "box", sessionID: "h1"))
        #expect(model.assistantManagerID == "m-1", "the health space has no switcher: the page stays")
    }

    @Test func nothingGoesToAnOfflineManager() async throws {
        let hub = FakeHub()
        let model = try model(hub, roster: try Self.roster())
        #expect(model.offlineManager(of: try session("h", manager: "m-2"))?.name == "家里")
        #expect(model.offlineManager(of: try session("w", manager: "m-1")) == nil && model.offlineManager(of: nil) == nil)
        model.selectAssistantManager("m-2")
        await model.askPersona("hello")
        #expect(model.toast?.style == .error && model.toast?.message.contains("家里") == true)
        #expect(hub.sent("POST", "/api/v1/assistant/conversations").isEmpty)
        model.managerRosters[""] = try Self.roster(Self.oneManager)
        #expect(model.offlineManager(of: try session("h", manager: "m-2")) == nil, "one manager: nothing of it shows")
    }
}

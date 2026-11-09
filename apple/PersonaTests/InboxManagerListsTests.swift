import Foundation
import Testing
@testable import Persona

/// The Inbox's Rules and Muted lists with several managers (docs/assistant/managers.md "Grouping"): each manager's own
/// list is read with it named and shown under its name, the default first; one that can't be read says so in its place;
/// Remove and Unmute go to the manager whose it is (still after another became the default), a rule or mute with the same
/// id elsewhere stays, and one that fails to go comes back alone. A reload read before a removal landed doesn't bring it
/// back, and only the newest load lands.
@MainActor
struct InboxManagerListsTests {
    private static let roster = ManagerRoster(managers: [
        HubManager(id: "assistant", name: "助手", online: true, isDefault: true),
        HubManager(id: "m-2", name: "工作", online: true),
        HubManager(id: "m-3", name: "家里", online: false),
    ], defaultID: "assistant")

    /// A hub whose managers each answer `path` with their own `key` list, each read naming its manager (the default too);
    /// m-3 is offline.
    private static func hub(_ path: String, _ key: String, mine: String, work: String) -> FakeHub {
        let hub = FakeHub()
        hub.on("GET", path) { req in
            switch req.headers["X-Persona-Manager"] {
            case "assistant": return Data(#"{"\#(key)":[\#(mine)]}"#.utf8)
            case "m-2": return Data(#"{"\#(key)":[\#(work)]}"#.utf8)
            default: throw APIError(status: 503, message: "assistant offline")
            }
        }
        return hub
    }

    /// The hub's client, with the first request `held` picks kept back until `gate` opens.
    private static func holdingClient(_ hub: FakeHub, _ gate: Gate, _ held: @escaping @Sendable (LocalRequest) -> Bool) -> APIClient {
        var client = hub.client
        let answer = client.transport!
        client.transport = { req in
            if held(req) { await gate.pass() }
            return try await answer(req)
        }
        return client
    }

    /// Keeps the first request through it back until `open()`; later ones pass.
    private actor Gate {
        private var passed = 0
        private var held: CheckedContinuation<Void, Never>?
        private var watching: [CheckedContinuation<Void, Never>] = []

        func pass() async {
            passed += 1
            guard passed == 1 else { return }
            await withCheckedContinuation { c in
                held = c
                watching.forEach { $0.resume() }
                watching = []
            }
        }

        /// Returns once the first request is held.
        func holding() async {
            if held != nil { return }
            await withCheckedContinuation { watching.append($0) }
        }

        func open() {
            held?.resume()
            held = nil
        }
    }

    @Test func eachManagersRulesAreReadUnderItsNameAndRemovedOnIt() async throws {
        let hub = Self.hub("/api/v1/inbox/policies", "policies",
                           mine: #"{"id":"p1","source":"mail","tag":"sender","value":"a@example.com","name":"甲","tier":"action","created_at":"2026-11-04T01:00:00Z"}"#,
                           work: #"{"id":"p1","source":"mail","tag":"sender","value":"b@example.com","name":"乙","tier":"noise","created_at":"2026-11-05T01:00:00Z"}"#)
        var named: [String?] = []
        hub.on("DELETE", "/api/v1/inbox/policies/p1") { req in
            named.append(req.headers["X-Persona-Manager"])
            return Data(#"{"ok":true}"#.utf8)
        }
        var toasts: [Toast] = []
        let m = InboxRulesModel(deps: .init(api: { hub.client }, toast: { toasts.append($0) }, roster: { Self.roster }))
        await m.load()
        #expect(m.shares?.map(\.manager.id) == ["assistant", "m-2", "m-3"])
        #expect(m.shares?.map { $0.items.map(\.name) } == [["甲"], ["乙"], []])
        #expect(m.shares?.map { $0.items.first?.manager } == ["assistant", "m-2", nil])
        #expect(m.shares?.map(\.failed) == [nil, nil, "离线中：它回来后这里会显示它的规则。"])
        #expect(m.state.value?.map(\.name) == ["乙", "甲"])

        await m.remove(try #require(m.shares?[1].items.first))
        #expect(named == ["m-2"])
        #expect(m.shownShares?.map(\.manager.id) == ["assistant", "m-3"]) // m-2 has none left
        #expect(m.state.value?.map(\.name) == ["甲"]) // the default's rule with the same id stays
        #expect(toasts.map(\.message) == ["已移除“乙”的规则"])
    }

    @Test func aRuleThatFailsToGoComesBackUnderItsManager() async throws {
        let hub = Self.hub("/api/v1/inbox/policies", "policies", mine: "",
                           work: #"{"id":"p9","source":"mail","tag":"sender","value":"b@example.com","name":"乙","tier":"noise"}"#)
        hub.on("DELETE", "/api/v1/inbox/policies/p9") { _ in throw APIError(status: 400, message: "坏了") }
        let m = InboxRulesModel(deps: .init(api: { hub.client }, toast: { _ in }, roster: { Self.roster }))
        await m.load()
        await m.remove(try #require(m.shares?[1].items.first))
        #expect(m.shownShares?.map(\.manager.id) == ["m-2", "m-3"])
        #expect(m.shares?[1].items.map(\.id) == ["p9"])
    }

    @Test func aRuleThatFailsToGoLeavesWhatChangedMeanwhile() async throws {
        let hub = Self.hub("/api/v1/inbox/policies", "policies",
                           mine: #"{"id":"p1","source":"mail","tag":"sender","value":"a@example.com","name":"甲","tier":"action"}"#,
                           work: #"{"id":"p9","source":"mail","tag":"sender","value":"b@example.com","name":"乙","tier":"noise"}"#)
        hub.on("DELETE", "/api/v1/inbox/policies/p1") { _ in Data(#"{"ok":true}"#.utf8) }
        hub.on("DELETE", "/api/v1/inbox/policies/p9") { _ in throw APIError(status: 400, message: "坏了") }
        let m = InboxRulesModel(deps: .init(api: { hub.client }, toast: { _ in }, roster: { Self.roster }))
        await m.load()
        let mine = try #require(m.shares?[0].items.first), work = try #require(m.shares?[1].items.first)
        async let failing: Void = m.remove(work)
        async let going: Void = m.remove(mine)
        _ = await (failing, going)
        #expect(m.state.value?.map(\.id) == ["p9"], "the default's rule, gone meanwhile, doesn't come back")
        #expect(m.shownShares?.map(\.manager.id) == ["m-2", "m-3"])
    }

    @Test func aRemovedRuleStaysGoneWhenAReloadReadBeforeItLands() async throws {
        let hub = Self.hub("/api/v1/inbox/policies", "policies", mine: "",
                           work: #"{"id":"p9","source":"mail","tag":"sender","value":"b@example.com","name":"乙","tier":"noise"}"#)
        hub.on("DELETE", "/api/v1/inbox/policies/p9") { _ in Data(#"{"ok":true}"#.utf8) }
        let gate = Gate()
        let client = Self.holdingClient(hub, gate) { $0.method == "DELETE" }
        let m = InboxRulesModel(deps: .init(api: { client }, toast: { _ in }, roster: { Self.roster }))
        await m.load()
        let work = try #require(m.shares?[1].items.first)
        let removing = Task { await m.remove(work) }
        await gate.holding()
        await m.load() // read while the removal was on its way: the hub still had it
        #expect(m.shares?[1].items.map(\.id) == ["p9"])
        await gate.open()
        await removing.value
        #expect(m.state.value?.isEmpty == true)
        #expect(m.shownShares?.map(\.manager.id) == ["m-3"])
    }

    @Test func aLoadOvertakenByANewerOneDoesntLand() async throws {
        let hub = Self.hub("/api/v1/inbox/policies", "policies",
                           mine: #"{"id":"p1","source":"mail","tag":"sender","value":"a@example.com","name":"甲","tier":"action"}"#,
                           work: #"{"id":"p9","source":"mail","tag":"sender","value":"b@example.com","name":"乙","tier":"noise"}"#)
        let gate = Gate()
        let client = Self.holdingClient(hub, gate) { $0.headers["X-Persona-Manager"] == "m-2" }
        var roster = Self.roster
        let m = InboxRulesModel(deps: .init(api: { client }, toast: { _ in }, roster: { roster }))
        let first = Task { await m.load() } // m-2's rules hang
        await gate.holding()
        roster = ManagerRoster(managers: [Self.roster.managers[0], Self.roster.managers[2]], defaultID: "assistant") // m-2 was removed meanwhile
        await m.load()
        #expect(m.shares?.map(\.manager.id) == ["assistant", "m-3"])
        await gate.open()
        await first.value
        #expect(m.shares?.map(\.manager.id) == ["assistant", "m-3"], "the older load, with m-2, doesn't land")
        #expect(m.state.value?.map(\.name) == ["甲"])
    }

    @Test func theDefaultsRulesGoToItAfterAnotherBecameTheDefault() async throws {
        let hub = Self.hub("/api/v1/inbox/policies", "policies",
                           mine: #"{"id":"p1","source":"mail","tag":"sender","value":"a@example.com","name":"甲","tier":"action"}"#, work: "")
        var named: [String?] = []
        hub.on("DELETE", "/api/v1/inbox/policies/p1") { req in
            named.append(req.headers["X-Persona-Manager"])
            return Data(#"{"ok":true}"#.utf8)
        }
        var roster = Self.roster
        let m = InboxRulesModel(deps: .init(api: { hub.client }, toast: { _ in }, roster: { roster }))
        await m.load()
        let before = m.rosterKey
        roster = ManagerRoster(managers: Self.roster.managers, defaultID: "m-2") // made the default on another device
        #expect(m.rosterKey != before, "the list is read again")
        await m.remove(try #require(m.shares?[0].items.first))
        #expect(named == ["assistant"], "the manager it came from, named now that it isn't the default")
    }

    @Test func removeAndUnmuteNameTheManagerTheyWereReadFromAfterItBecameTheDefault() async throws {
        let hub = Self.hub("/api/v1/inbox/policies", "policies", mine: "",
                           work: #"{"id":"p2","source":"mail","tag":"sender","value":"b@example.com","name":"乙","tier":"noise"}"#)
        hub.on("GET", "/api/v1/inbox/mutes") { req in
            req.headers["X-Persona-Manager"] == "m-2"
                ? Data(#"{"mutes":[{"id":"u2","source":"github","tag":"repo","value":"example-org/two","name":"example-org/two"}]}"#.utf8)
                : Data(#"{"mutes":[]}"#.utf8)
        }
        var named: [String?] = []
        for path in ["/api/v1/inbox/policies/p2", "/api/v1/inbox/mutes/u2"] {
            hub.on("DELETE", path) { req in
                named.append(req.headers["X-Persona-Manager"])
                return Data(#"{"ok":true}"#.utf8)
            }
        }
        var roster = Self.roster
        let rules = InboxRulesModel(deps: .init(api: { hub.client }, toast: { _ in }, roster: { roster }))
        let mutes = InboxMutesModel(deps: .init(api: { hub.client }, roster: { roster }, show: { _ in }))
        await rules.load()
        await mutes.load()
        roster = ManagerRoster(managers: Self.roster.managers, defaultID: "m-2") // made the default on another device
        await rules.remove(try #require(rules.shares?[1].items.first))
        await mutes.unmute(try #require(mutes.shares?[1].items.first))
        #expect(named == ["m-2", "m-2"], "the manager each came from, named although it is the default now")
    }

    @Test func theDefaultsListsAreReadWithItsNameWhileTheHubsDefaultHasMovedOn() async throws {
        // m-2 was made the default on another device and this roster hasn't heard yet: a read naming none gets m-2's.
        let hub = FakeHub()
        hub.on("GET", "/api/v1/inbox/policies") { req in
            let id = req.headers["X-Persona-Manager"] ?? "m-2"
            return Data(#"{"policies":[{"id":"p-\#(id)","source":"mail","tag":"sender","value":"a@example.com","name":"\#(id)"}]}"#.utf8)
        }
        hub.on("GET", "/api/v1/inbox/mutes") { req in
            let id = req.headers["X-Persona-Manager"] ?? "m-2"
            return Data(#"{"mutes":[{"id":"u-\#(id)","source":"github","tag":"repo","value":"example-org/one","name":"\#(id)"}]}"#.utf8)
        }
        let rules = InboxRulesModel(deps: .init(api: { hub.client }, toast: { _ in }, roster: { Self.roster }))
        let mutes = InboxMutesModel(deps: .init(api: { hub.client }, roster: { Self.roster }, show: { _ in }))
        await rules.load()
        await mutes.load()
        #expect(rules.shares?.map { $0.items.map(\.id) } == [["p-assistant"], ["p-m-2"], ["p-m-3"]])
        #expect(mutes.shares?.map { $0.items.map(\.id) } == [["u-assistant"], ["u-m-2"], ["u-m-3"]])
    }

    @Test func oneManagerReadsAndChangesOneListAsBefore() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/inbox/policies", json: #"{"policies":[{"id":"p1","source":"mail","tag":"sender","value":"a@example.com","name":"甲","tier":"action"}]}"#)
        hub.on("GET", "/api/v1/inbox/mutes", json: #"{"mutes":[{"id":"u1","source":"github","tag":"repo","value":"example-org/one","name":"example-org/one"}]}"#)
        hub.on("DELETE", "/api/v1/inbox/policies/p1", json: #"{"ok":true}"#)
        hub.on("DELETE", "/api/v1/inbox/mutes/u1", json: #"{"ok":true}"#)
        let one = ManagerRoster(managers: [HubManager(id: "assistant", name: "助手", online: true, isDefault: true)], defaultID: "assistant")
        let rules = InboxRulesModel(deps: .init(api: { hub.client }, toast: { _ in }, roster: { one }))
        let mutes = InboxMutesModel(deps: .init(api: { hub.client }, roster: { one }, show: { _ in }))
        await rules.load()
        await mutes.load()
        #expect(rules.shares == nil && mutes.shares == nil)
        await rules.remove(try #require(rules.state.value?.first))
        await mutes.unmute(try #require(mutes.state.value?.first))
        let named = [("GET", "/api/v1/inbox/policies"), ("GET", "/api/v1/inbox/mutes"), ("DELETE", "/api/v1/inbox/policies/p1"),
                     ("DELETE", "/api/v1/inbox/mutes/u1")].flatMap { hub.sent($0.0, $0.1).map { $0.headers["X-Persona-Manager"] } }
        #expect(named == [nil, nil, nil, nil])
    }

    @Test func eachManagersMutesAreReadUnderItsNameAndTakenBackOnIt() async throws {
        let hub = Self.hub("/api/v1/inbox/mutes", "mutes",
                           mine: #"{"id":"m1","source":"github","tag":"repo","value":"example-org/one","name":"example-org/one","created_at":"2026-11-04T01:00:00Z"}"#,
                           work: #"{"id":"m1","source":"github","tag":"repo","value":"example-org/two","name":"example-org/two","created_at":"2026-11-05T01:00:00Z"}"#)
        var named: [String?] = []
        hub.on("DELETE", "/api/v1/inbox/mutes/m1") { req in
            named.append(req.headers["X-Persona-Manager"])
            if req.headers["X-Persona-Manager"] == "assistant" { throw APIError(status: 400, message: "坏了") }
            return Data(#"{"ok":true}"#.utf8)
        }
        var shown: [String] = []
        let m = InboxMutesModel(deps: .init(api: { hub.client }, roster: { Self.roster }, show: { shown.append("\($0)") }))
        await m.load()
        #expect(m.shares?.map { $0.items.map(\.name) } == [["example-org/one"], ["example-org/two"], []])
        #expect(m.shares?.map(\.failed) == [nil, nil, "离线中：它回来后这里会显示它的静音。"])
        #expect(m.state.value?.map(\.name) == ["example-org/two", "example-org/one"])

        await m.unmute(try #require(m.shares?[1].items.first))
        #expect(m.shownShares?.map(\.manager.id) == ["assistant", "m-3"])
        await m.unmute(try #require(m.shares?[0].items.first))
        #expect(named == ["m-2", "assistant"])
        #expect(m.shares?[0].items.map(\.name) == ["example-org/one"]) // the default's failed: back
        #expect(m.shares?[1].items.isEmpty == true && shown.count == 1)
    }

    @Test func anUnmuteStaysGoneWhenAReloadReadBeforeItLands() async throws {
        let hub = Self.hub("/api/v1/inbox/mutes", "mutes", mine: "",
                           work: #"{"id":"m9","source":"github","tag":"repo","value":"example-org/two","name":"example-org/two"}"#)
        hub.on("DELETE", "/api/v1/inbox/mutes/m9") { _ in Data(#"{"ok":true}"#.utf8) }
        let gate = Gate()
        let client = Self.holdingClient(hub, gate) { $0.method == "DELETE" }
        let m = InboxMutesModel(deps: .init(api: { client }, roster: { Self.roster }, show: { _ in }))
        await m.load()
        let work = try #require(m.shares?[1].items.first)
        let unmuting = Task { await m.unmute(work) }
        await gate.holding()
        await m.load() // read while the unmute was on its way: the hub still had it
        #expect(m.shares?[1].items.map(\.id) == ["m9"])
        await gate.open()
        await unmuting.value
        #expect(m.state.value?.isEmpty == true)
        #expect(m.shownShares?.map(\.manager.id) == ["m-3"])
    }

    @Test func aFailedLoadOvertakenByANewerOneSaysNothing() async throws {
        let hub = FakeHub()
        var answers = 0
        hub.on("GET", "/api/v1/inbox/mutes") { _ in
            answers += 1
            if answers == 2 { throw APIError(status: 503, message: "超时") } // the first load's, answered last
            return Data(#"{"mutes":[{"id":"m1","source":"github","tag":"repo","value":"example-org/one","name":"example-org/one"}]}"#.utf8)
        }
        let gate = Gate()
        let client = Self.holdingClient(hub, gate) { $0.method == "GET" }
        var shown: [String] = []
        let m = InboxMutesModel(deps: .init(api: { client }, roster: { nil }, show: { shown.append("\($0)") }))
        let first = Task { await m.load() }
        await gate.holding()
        await m.load()
        await gate.open()
        await first.value
        #expect(m.state.value?.map(\.name) == ["example-org/one"])
        #expect(shown.isEmpty)
    }
}

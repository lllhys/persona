import Foundation
import Testing
@testable import Persona
import PersonaWire

/// Settings › Managers (docs/assistant/managers.md): the roster decodes, the status line and where it runs, why Delete is off,
/// a new manager's install code (with and without the commands) and its countdown, the changes and the hub's refusals in
/// plain words, a hub without the roster, and the `managers` push. Made-up values.
@MainActor
struct ManagersModelTests {
    nonisolated private static let start = Date(timeIntervalSince1970: 1_800_000_000)

    nonisolated private static func at(_ seconds: TimeInterval) -> String {
        ISO8601DateFormatter().string(from: start.addingTimeInterval(seconds))
    }

    nonisolated private static let roster = #"""
    {"default":"assistant","managers":[
      {"id":"assistant","name":"助手","online":true,"default":true,"builtin":true,"host":"server","os":"linux","deploy":"child","version":"1.0.0","since":"\#(at(-3600))"},
      {"id":"m-1","name":"工作","online":true,"host":"MacBook","os":"darwin","arch":"arm64","deploy":"service"},
      {"id":"m-2","name":"家里","online":false,"host":"NAS","os":"linux","deploy":"docker","last_seen":"\#(at(-3 * 3600))"},
      {"id":"m-3","name":"测试","online":false,"installing":"\#(at(7 * 60 + 10))"}]}
    """#

    nonisolated private static func code(_ value: String, commands: Bool) -> String {
        let c = commands ? #","commands":{"service":"curl -fsSL https://hub.example.com/i/\#(value) | sh","docker":"docker run example \#(value)"}"# : ""
        return #"{"code":"\#(value)","expires_at":"\#(at(600))"\#(c)}"#
    }

    /// A clock the test moves by hand.
    @MainActor final class Clock { var now = Date(timeIntervalSince1970: 1_800_000_000) }

    private func model(_ hub: FakeHub, _ clock: Clock = Clock()) -> ManagersModel {
        ManagersModel(deps: .init(api: { hub.client }, now: { clock.now }))
    }

    private func refuse(_ status: Int, code: String? = nil) -> FakeHub.Route {
        { _ in throw APIError(status: status, message: "raw", fields: code.map { ["code": $0] } ?? [:]) }
    }

    private func body(_ req: LocalRequest?) throws -> [String: Any] {
        let data = try #require(req?.body)
        return try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    @Test func readsTheRoster() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/managers", json: Self.roster)
        let page = model(hub)
        await page.load()
        let list = try #require(page.managers)
        #expect(list.map(\.id) == ["assistant", "m-1", "m-2", "m-3"])
        let builtin = try #require(page.manager("assistant"))
        #expect(builtin.isDefault && builtin.builtin && builtin.online && builtin.version == "1.0.0" && builtin.since != nil)
        #expect(page.manager("m-2")?.lastSeen == Self.start.addingTimeInterval(-3 * 3600))
        #expect(page.manager("m-3")?.installing != nil)
        #expect(!page.unsupported && page.error == nil)
    }

    @Test func saysWhereEachRunsAndHowItIs() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/managers", json: Self.roster)
        let page = model(hub)
        await page.load()
        let now = Self.start
        #expect(ManagersText.whereItRuns(try #require(page.manager("assistant"))) == "server · Linux · hub 自己的")
        #expect(ManagersText.whereItRuns(try #require(page.manager("m-1"))) == "MacBook · macOS · 服务")
        #expect(ManagersText.whereItRuns(try #require(page.manager("m-2"))) == "NAS · Linux · 容器")
        #expect(ManagersText.whereItRuns(try #require(page.manager("m-3"))) == "")
        #expect(ManagersText.deploy(HubManager(id: "x", name: "x", online: false, deploy: "external")) == "单独运行")
        #expect(ManagersText.deploy(HubManager(id: "x", name: "x", online: false, os: "freebsd", deploy: "nomad")) == "nomad")
        #expect(ManagersText.os("freebsd") == "freebsd")

        let online = ManagersText.status(try #require(page.manager("m-1")), now: now)
        #expect(online.text == "在线" && online.tone == .online)
        let offline = ManagersText.status(try #require(page.manager("m-2")), now: now)
        #expect(offline.text.hasPrefix("离线 · 最后同步于 ") && offline.tone == .offline)
        #expect(ManagersText.status(HubManager(id: "x", name: "x", online: false), now: now).text == "离线")
        let installing = ManagersText.status(try #require(page.manager("m-3")), now: now)
        #expect(installing.text == "安装中 · 码 7 分钟后过期" && installing.tone == .installing)
        let ran = ManagersText.status(try #require(page.manager("m-3")), now: now.addingTimeInterval(3600))
        #expect(ran.text == "安装中 · 码已过期")
        #expect(ManagersText.minutesLeft(now.addingTimeInterval(600.04), now: now) == 10)
        #expect(ManagersText.minutesLeft(now.addingTimeInterval(10), now: now) == 1)
    }

    @Test func deleteIsOffForTheHubsOwnAndTheDefault() {
        #expect(ManagersText.deleteBlocked(HubManager(id: "a", name: "a", online: true, isDefault: true, builtin: true)) == "hub 自己运行的助手不能删除")
        #expect(ManagersText.deleteBlocked(HubManager(id: "b", name: "b", online: true, isDefault: true)) == "默认助手不能删除，先把别的设为默认")
        #expect(ManagersText.deleteBlocked(HubManager(id: "c", name: "c", online: false)) == nil)
    }

    @Test func aHubWithoutTheRosterSaysSo() async {
        let page = model(FakeHub()) // no route: 404 without a code
        await page.load()
        #expect(page.unsupported && page.managers == nil)
    }

    @Test func aNewManagerShowsItsCodeAndCommands() async throws {
        let hub = FakeHub(), clock = Clock()
        hub.on("GET", "/api/v1/managers", json: Self.roster)
        hub.on("POST", "/api/v1/managers") { _ in
            Data((#"{"manager":{"id":"m-9","name":"书房","online":false,"installing":"\#(Self.at(600))"},"#
                + String(Self.code("mi_new", commands: true).dropFirst())).utf8)
        }
        let page = model(hub, clock)
        #expect(await page.create(name: "  书房 "))
        #expect(try body(hub.sent("POST", "/api/v1/managers").first)["name"] as? String == "书房")
        let s = try #require(page.shown)
        #expect(s.id == "m-9" && s.name == "书房" && s.code.code == "mi_new")
        #expect(s.code.commands?.service == "curl -fsSL https://hub.example.com/i/mi_new | sh")
        #expect(s.code.commands?.docker == "docker run example mi_new")
        #expect(page.secondsLeft == 600 && !page.expired)
        #expect(hub.sent("GET", "/api/v1/managers").count == 1) // the list again, not waiting for the push

        clock.now = Self.start.addingTimeInterval(601)
        page.tick()
        #expect(page.expired)
        hub.on("POST", "/api/v1/managers/m-9/code", json: Self.code("mi_again", commands: false))
        clock.now = Self.start
        await page.renewShown()
        #expect(page.shown?.code.code == "mi_again" && page.shown?.code.commands == nil && !page.expired)
    }

    @Test func aNameMustBeOneToFortyCharacters() async {
        let hub = FakeHub()
        let page = model(hub)
        #expect(await page.create(name: "   ") == false)
        #expect(page.failure == "名字要 1 到 40 个字")
        #expect(hub.sent("POST", "/api/v1/managers").isEmpty)
        #expect(ManagersText.validName(String(repeating: "字", count: 40)))
        #expect(!ManagersText.validName(String(repeating: "字", count: 41)))
        // Counted in runes as the hub does: 20 thumbs-up with a skin tone are 20 characters but 40 scalars.
        #expect(ManagersText.validName(String(repeating: "👍🏽", count: 20)))
        #expect(!ManagersText.validName(String(repeating: "👍🏽", count: 21)))
    }

    /// The hub forgets a manager that was never installed once its code expired (404 no_manager on …/code): renewing
    /// makes it again under the same name instead of failing.
    @Test func renewingAnExpiredNewManagerMakesItAgain() async throws {
        let hub = FakeHub(), clock = Clock()
        hub.on("GET", "/api/v1/managers", json: Self.roster)
        hub.on("POST", "/api/v1/managers") { req in
            let name = (try? JSONSerialization.jsonObject(with: req.body ?? Data()) as? [String: String])?["name"] ?? ""
            // The first one made is m-9; the one made again after its code expired is m-10.
            let id = hub.sent("POST", "/api/v1/managers").count > 1 ? "m-10" : "m-9"
            return Data((#"{"manager":{"id":"\#(id)","name":"\#(name)","online":false},"#
                + String(Self.code("mi_" + id, commands: false).dropFirst())).utf8)
        }
        hub.on("POST", "/api/v1/managers/m-9/code", refuse(404, code: "no_manager"))
        let page = model(hub, clock)
        #expect(await page.create(name: "书房"))
        #expect(page.shown?.id == "m-9" && page.shown?.neverInstalled == true)
        clock.now = Self.start.addingTimeInterval(601)
        page.tick()
        #expect(page.expired)
        clock.now = Self.start
        await page.renewShown()
        #expect(page.failure == nil)
        #expect(page.shown?.id == "m-10" && page.shown?.name == "书房" && page.shown?.code.code == "mi_m-10" && !page.expired)
        #expect(hub.sent("POST", "/api/v1/managers/m-9/code").count == 1)
        let bodies = try hub.sent("POST", "/api/v1/managers").map { try body($0)["name"] as? String }
        #expect(bodies == ["书房", "书房"])
    }

    /// An installed manager's 404 is not papered over: it is gone, and the page says so.
    @Test func anInstalledManagerGoneIsNotMadeAgain() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/managers", json: Self.roster)
        hub.on("POST", "/api/v1/managers/m-1/code", refuse(404, code: "no_manager"))
        let page = model(hub)
        await page.load()
        #expect(await page.newCode(try #require(page.manager("m-1"))) == false)
        #expect(page.failure == "这个助手已经不在了")
        #expect(hub.sent("POST", "/api/v1/managers").isEmpty)
    }

    @Test func renamesMakesDefaultAndInstallsElsewhere() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/managers", json: Self.roster)
        hub.on("PATCH", "/api/v1/managers/m-2", json: #"{"manager":{"id":"m-2","name":"家","online":false}}"#)
        hub.on("POST", "/api/v1/managers/m-1/code", json: Self.code("mi_move", commands: false))
        let page = model(hub)
        await page.load()
        let home = try #require(page.manager("m-2"))
        #expect(await page.rename(home, to: " 家 "))
        #expect(await page.makeDefault(home))
        let sent = hub.sent("PATCH", "/api/v1/managers/m-2")
        #expect(sent.count == 2)
        #expect(try body(sent.first)["name"] as? String == "家")
        #expect(try body(sent.last)["default"] as? Bool == true)
        #expect(await page.newCode(try #require(page.manager("m-1"))))
        #expect(page.shown?.id == "m-1" && page.shown?.name == "工作" && page.shown?.code.code == "mi_move")
    }

    /// The built-in assistant's switcher choice (#9055): both fields, and their absence (an older hub, or false).
    @Test func readsTheSwitcherChoice() throws {
        let list = try JSON.decoder.decode(APIClient.ManagerList.self, from: Data(#"""
        {"default":"m-1","managers":[{"id":"assistant","name":"助手","online":true,"builtin":true,"hide":true,"hidden":true},
          {"id":"m-1","name":"工作","online":true,"default":true}]}
        """#.utf8)).managers
        #expect(list[0].hide && list[0].hidden)
        #expect(!list[1].hide && !list[1].hidden, "left out: false")
        let inUse = try JSON.decoder.decode(HubManager.self, from: Data(#"{"id":"assistant","builtin":true,"hide":true}"#.utf8))
        #expect(inUse.hide && !inUse.hidden)
    }

    /// Show in Switcher on the built-in assistant's page: off sends `{"hide": true}`, on `{"hide": false}`, each shown
    /// at once; a refusal puts it back and says why; an older hub that keeps nothing says the hub needs updating.
    @Test func showsOrHidesTheBuiltInAssistantInTheSwitcher() async throws {
        let hub = FakeHub()
        var hide = false, oldHub = false
        hub.on("GET", "/api/v1/managers") { _ in
            let choice = hide && !oldHub ? #","hide":true,"hidden":true"# : ""
            return Data(#"""
            {"default":"m-1","managers":[{"id":"assistant","name":"助手","online":true,"builtin":true\#(choice)},
              {"id":"m-1","name":"工作","online":true,"default":true}]}
            """#.utf8)
        }
        hub.on("PATCH", "/api/v1/managers/assistant") { req in
            hide = (try? JSONSerialization.jsonObject(with: req.body ?? Data()) as? [String: Bool])?["hide"] ?? hide
            return Data(#"{"manager":{"id":"assistant","name":"助手","online":true}}"#.utf8)
        }
        let page = model(hub)
        await page.load()
        let builtin = try #require(page.manager("assistant"))
        #expect(!ManagersText.switcherLocked(builtin) && !builtin.hide)

        #expect(await page.showInSwitcher(builtin, false))
        #expect(page.manager("assistant")?.hide == true && page.manager("assistant")?.hidden == true && page.failure == nil)
        #expect(await page.showInSwitcher(try #require(page.manager("assistant")), true))
        #expect(page.manager("assistant")?.hide == false && page.manager("assistant")?.hidden == false)
        let sent = try hub.sent("PATCH", "/api/v1/managers/assistant").map { try body($0)["hide"] as? Bool }
        #expect(sent == [true, false])

        hub.on("PATCH", "/api/v1/managers/assistant", refuse(409, code: "builtin_in_use"))
        #expect(await page.showInSwitcher(try #require(page.manager("assistant")), false) == false)
        #expect(page.manager("assistant")?.hide == false, "put back")
        #expect(page.failure == "它正在使用（是默认助手或有电脑），现在不能隐藏")

        oldHub = true
        hub.on("PATCH", "/api/v1/managers/assistant", json: #"{"manager":{"id":"assistant","name":"助手","online":true}}"#)
        #expect(await page.showInSwitcher(try #require(page.manager("assistant")), false) == false)
        #expect(page.manager("assistant")?.hide == false && page.failure == "这台 hub 的版本还不能隐藏它，升级 hub 后再试")
    }

    /// Why the switch is off-limits or what it waits for, under it.
    @Test func theSwitchSaysWhyAndWhen() {
        func builtin(isDefault: Bool = false, hide: Bool = false, hidden: Bool = false) -> HubManager {
            HubManager(id: "assistant", name: "助手", online: true, isDefault: isDefault, builtin: true, hide: hide, hidden: hidden)
        }
        #expect(ManagersText.switcherLocked(builtin(isDefault: true)), "the default can't be hidden")
        #expect(ManagersText.switcherNote(builtin(isDefault: true)) == "默认助手总会列出。要隐藏这个助手，先把别的助手设为默认。")
        #expect(!ManagersText.switcherLocked(builtin(isDefault: true, hide: true)), "but shown again it always can be")
        #expect(ManagersText.switcherNote(builtin(isDefault: true, hide: true)) == "它是默认助手，所以暂时仍会列出；别的助手成为默认后会再次隐藏。")
        #expect(ManagersText.switcherNote(builtin(hide: true)) == "它有电脑，所以暂时仍会列出；没有电脑后会再次隐藏。")
        #expect(ManagersText.switcherNote(builtin(hide: true, hidden: true)).hasPrefix("关掉后"))
        #expect(ManagersText.switcherNote(builtin()).hasPrefix("关掉后") && !ManagersText.switcherLocked(builtin()))
    }

    @Test func refusalsInPlainWords() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/managers", json: Self.roster)
        hub.on("DELETE", "/api/v1/managers/m-1", refuse(409, code: "default_manager"))
        let page = model(hub)
        await page.load()
        #expect(await page.delete(try #require(page.manager("m-1"))) == false)
        #expect(page.failure == "这是默认助手，先把别的设为默认再删除")
        let say = { (status: Int, code: String?) in ManagersText.failure(APIError(status: status, message: "raw", fields: code.map { ["code": $0] } ?? [:])) }
        #expect(say(409, "builtin_manager") == "这是 hub 自己运行的助手，不能删除或换地方装")
        #expect(say(404, "no_manager") == "这个助手已经不在了")
        #expect(say(409, "installing") == "它还没装好，装好后才能改名或设为默认")
        #expect(say(400, nil) == "名字要 1 到 40 个字")
        #expect(say(403, nil) == "这台设备没有修改权限")
        #expect(say(500, nil) == "raw")
        #expect(ManagersText.unsupported(APIError(status: 404, message: "x")))
        #expect(!ManagersText.unsupported(APIError(status: 404, message: "x", fields: ["code": "no_manager"])))
    }

    @Test func deletesAndReadsTheListAgain() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/managers", json: Self.roster)
        hub.on("DELETE", "/api/v1/managers/m-3", json: #"{"ok":true}"#)
        let page = model(hub)
        await page.load()
        #expect(await page.delete(try #require(page.manager("m-3"))))
        #expect(hub.sent("DELETE", "/api/v1/managers/m-3").count == 1)
        #expect(hub.sent("GET", "/api/v1/managers").count == 2)
    }

    /// The hub's push when the roster changed, and the socket asking for it.
    @Test func thePushAndTheFeature() throws {
        guard case .managers = try #require(ServerEvent.decode(Data(#"{"type":"managers"}"#.utf8))) else {
            Issue.record("not managers"); return
        }
        let features = LiveConnection.queryItems(EventCursor()).first { $0.name == "features" }?.value ?? ""
        #expect(features.split(separator: ",").contains("managers"))
    }
}

import Foundation
import Testing
@testable import Persona

@MainActor
struct AssistantStarterModelsTests {
    private let box = MachineRef(id: "box")
    private let launchersPath = "/api/v1/machines/box/launchers"
    private let modelsPath = "/api/v1/machines/box/launchers/team/models"

    private func catalog(_ model: String = "chosen", levels: [String] = ["low", "high"]) -> String {
        let levels = levels.map { "\"\($0)\"" }.joined(separator: ",")
        return "{\"agent\":\"claude\",\"models\":[{\"id\":\"\(model)\",\"default\":true,\"efforts\":[\(levels)]}]}"
    }

    private func launcherJSON(_ model: String = "chosen") -> String {
        "{\"launchers\":[{\"name\":\"team\",\"model\":\"\(model)\"}],\"default\":{\"provider\":\"launcher.team\",\"model\":\"\(model)\"}}"
    }

    private func starter(_ hub: FakeHub, cache: AssistantStarterCache) -> AssistantStarterModels {
        AssistantStarterModels(deps: .init(launchers: { machine, manager, _ in
            try await hub.client.manager(manager).launcherList(machine: machine.id, purpose: "assistant")
        }, api: { _ in hub.client.showing("unrelated") }, cache: cache))
    }

    @Test func assistantCatalogRequestsNameTheShownManagerAndAgentRequestsDoNot() async throws {
        let hub = FakeHub()
        hub.on("GET", launchersPath, json: launcherJSON())
        hub.on("GET", modelsPath, json: catalog())
        let client = hub.client.showing("work")
        let _ = try await client.launcherList(machine: "box", purpose: "assistant")
        let _: LauncherModels = try await client.launcherModels(machine: "box", launcher: "team", purpose: "assistant")
        let _: [AgentModelOption] = try await client.launcherModels(machine: "box", launcher: "team", purpose: "assistant")
        let _ = try await client.launcherList(machine: "box")
        let _: LauncherModels = try await client.launcherModels(machine: "box", launcher: "team")
        #expect(hub.requests.map { $0.headers[APIClient.managerHeader] } == ["work", "work", "work", nil, nil])
    }

    @Test func assistantLauncherCacheSeparatesManagersAndReusesEachManagersAnswer() async throws {
        let hub = FakeHub()
        hub.on("GET", launchersPath) { req in
            Data(launcherJSON(req.headers[APIClient.managerHeader] ?? "default").utf8)
        }
        let app = AppModel()
        for manager in ["work", "home", "work"] {
            app.apiForTests = hub.client.showing(manager)
            #expect(try await app.assistantLaunchers(box).default?.model == manager)
            #expect(app.cachedAssistantLaunchers(box)?.default?.model == manager)
        }
        #expect(hub.sent("GET", launchersPath).count == 2)
    }

    @Test(arguments: [false, true]) func anOldReplyCannotRefillTheNewCache(invalidateSettings: Bool) async throws {
        let hub = FakeHub()
        let gate = StarterReadGate()
        defer { gate.finish() }
        var calls = 0
        hub.onAsync("GET", launchersPath) { _ in
            calls += 1
            if calls == 1 {
                await gate.wait()
                return Data(launcherJSON("old").utf8)
            }
            return Data(launcherJSON("new").utf8)
        }
        let app = AppModel()
        app.apiForTests = hub.client.showing("work")
        let first = Task { try await app.assistantLaunchers(box) }
        try await waitForRefreshTest { gate.started }
        if invalidateSettings { app.launcherDefaultsChanged() }
        #expect(try await app.assistantLaunchers(box, refresh: true).default?.model == "new")
        gate.finish()
        _ = try await first.value
        #expect(try await app.assistantLaunchers(box).default?.model == "new")
        #expect(calls == 2)
    }

    @Test func sendingAndReopeningUseKnownCapabilitiesWithoutARequest() async {
        let hub = FakeHub()
        hub.on("GET", launchersPath, json: launcherJSON())
        hub.on("GET", modelsPath, json: catalog())
        let cache = AssistantStarterCache()
        let source = AssistantStarterModels.source(box, launcher: "team", fallback: nil, manager: "work", revision: 3)
        let model = starter(hub, cache: cache)
        #expect(await model.load(box, launcher: "team", fallback: nil, manager: "work", revision: 3))
        let count = hub.requests.count
        #expect(cache.retained("high", source: source) == "high")
        #expect(cache.retained("ultra", source: source) == "")
        let reopened = starter(hub, cache: cache)
        #expect(await reopened.load(box, launcher: "team", fallback: nil, manager: "work", revision: 3))
        #expect(reopened.options(for: source).map(\.id) == ["low", "high"])
        #expect(hub.requests.count == count)
    }

    @Test func aFailedRefreshKeepsTheVerifiedChoiceAndUnknownSourcesKeepTheSavedChoice() async {
        let hub = FakeHub()
        hub.on("GET", launchersPath, json: launcherJSON())
        hub.on("GET", modelsPath, json: catalog())
        let cache = AssistantStarterCache()
        let model = starter(hub, cache: cache)
        let source = AssistantStarterModels.source(box, launcher: "team", fallback: nil)
        #expect(await model.load(box, launcher: "team", fallback: nil))
        hub.on("GET", modelsPath) { _ in throw APIError(status: 502, message: "Temporarily unavailable") }
        #expect(!(await model.load(box, launcher: "team", fallback: nil, refresh: true)))
        #expect(model.retained("high", source: source) == "high")
        let changed = AssistantStarterModels.source(box, launcher: "team", fallback: nil, revision: 1)
        #expect(!(await model.load(box, launcher: "team", fallback: nil, revision: 1)))
        #expect(model.retained("high", source: changed) == "high")
        #expect(model.options(for: changed).isEmpty)
    }

    @Test func aMissingFirstModelCatalogKeepsLaunchersSelectableAndAllowsRetry() async {
        let hub = FakeHub()
        hub.on("GET", launchersPath, json: #"{"launchers":[{"name":"team","model":"chosen"},{"name":"other"}]}"#)
        hub.on("GET", modelsPath) { _ in throw APIError(status: 404, message: "Model catalog unavailable") }
        let model = starter(hub, cache: AssistantStarterCache())
        let source = AssistantStarterModels.source(box, launcher: "team", fallback: nil)
        #expect(!(await model.load(box, launcher: "team", fallback: nil)))
        #expect(model.launchers.map(\.name) == ["team", "other"])
        #expect(model.retained("high", source: source) == "high")
        hub.on("GET", modelsPath, json: catalog())
        #expect(await model.load(box, launcher: "team", fallback: nil))
        #expect(model.options(for: source).map(\.id) == ["low", "high"])
        #expect(hub.sent("GET", modelsPath).count == 2)
    }

    @Test func onlyExplicitlyKnownEmptyCapabilitiesClearEffort() async {
        let hub = FakeHub()
        hub.on("GET", launchersPath, json: launcherJSON())
        hub.on("GET", modelsPath, json: #"{"agent":"runtime","models":[{"id":"other","efforts":[] }]}"#)
        let cache = AssistantStarterCache()
        let model = starter(hub, cache: cache)
        let source = AssistantStarterModels.source(box, launcher: "team", fallback: nil)
        #expect(await model.load(box, launcher: "team", fallback: nil))
        #expect(model.retained("high", source: source) == "high")
        hub.on("GET", modelsPath, json: catalog(levels: []))
        #expect(await model.load(box, launcher: "team", fallback: nil, refresh: true))
        #expect(model.retained("high", source: source) == "")
    }

    @Test func managerAndRevisionArePartOfCapabilityIdentityAndHealthUsesTheDefault() async {
        let hub = FakeHub()
        hub.on("GET", launchersPath) { req in
            Data(launcherJSON(req.headers[APIClient.managerHeader] ?? "default").utf8)
        }
        hub.on("GET", modelsPath) { req in
            let manager = req.headers[APIClient.managerHeader] ?? "default"
            return Data(catalog(manager, levels: manager == "work" ? ["high"] : ["low"]).utf8)
        }
        let cache = AssistantStarterCache()
        let model = starter(hub, cache: cache)
        let work = AssistantStarterModels.source(box, launcher: "team", fallback: nil, manager: "work")
        let home = AssistantStarterModels.source(box, launcher: "team", fallback: nil, manager: "home")
        #expect(await model.load(box, launcher: "team", fallback: nil, manager: "work"))
        #expect(await model.load(box, launcher: "team", fallback: nil, manager: "home"))
        #expect(cache.retained("high", source: work) == "high")
        #expect(cache.retained("high", source: home) == "")
        #expect(cache.retained("high", source: AssistantStarterModels.source(box, launcher: "team", fallback: nil, manager: "home", revision: 1)) == "high")
        #expect(await model.load(box, launcher: "team", fallback: nil))
        #expect(hub.sent("GET", launchersPath).last?.headers[APIClient.managerHeader] == nil)
        #expect(hub.sent("GET", modelsPath).last?.headers[APIClient.managerHeader] == nil)
    }

    @Test func aLateReplyFromThePreviousManagerCannotReplaceCurrentChoices() async throws {
        let hub = FakeHub()
        hub.on("GET", launchersPath, json: launcherJSON())
        let gate = StarterReadGate()
        defer { gate.finish() }
        hub.onAsync("GET", modelsPath) { req in
            if req.headers[APIClient.managerHeader] == "work" {
                await gate.wait()
                return Data(catalog(levels: ["high"]).utf8)
            }
            return Data(catalog(levels: ["low"]).utf8)
        }
        let cache = AssistantStarterCache()
        let model = starter(hub, cache: cache)
        let first = Task { await model.load(box, launcher: "team", fallback: nil, manager: "work") }
        try await waitForRefreshTest { gate.started }
        #expect(await model.load(box, launcher: "team", fallback: nil, manager: "home"))
        gate.finish()
        #expect(!(await first.value))
        let source = AssistantStarterModels.source(box, launcher: "team", fallback: nil, manager: "home")
        #expect(model.options(for: source).map(\.id) == ["low"])
    }
}

@MainActor private final class StarterReadGate {
    private var continuation: CheckedContinuation<Void, Never>?
    private(set) var started = false
    func wait() async {
        started = true
        await withCheckedContinuation { continuation = $0 }
    }
    func finish() {
        continuation?.resume()
        continuation = nil
    }
}

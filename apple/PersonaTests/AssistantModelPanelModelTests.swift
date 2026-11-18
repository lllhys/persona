import Foundation
import PersonaWire
import Testing
@testable import Persona

@MainActor
struct AssistantModelPanelModelTests {
    private final class Recorder {
        var launcherRefreshes: [Bool] = []
        var modelRefreshes: [String: [Bool]] = [:]
        var kept: ModelPick?
        var dropped = false
        var launcherError: APIError?
        var modelErrors: [String: APIError] = [:]
    }

    private let key = SessionKey(machineID: "box", sessionID: "main")
    private let machine = MachineRef(id: "box")
    private let response = APIClient.LaunchersResponse(
        launchers: [
            Launcher(name: "claude", model: "claude-opus-5-5"),
            Launcher(name: "team-api", model: "claude-sonnet-5-5")
        ],
        default: nil,
        unavailable: nil
    )

    private func deps(_ recorder: Recorder) -> AssistantModelPanelModel.Deps {
        .init(
            launchers: { _, refresh in
                recorder.launcherRefreshes.append(refresh)
                if let error = recorder.launcherError { throw error }
                return response
            },
            models: { _, launcher, refresh in
                recorder.modelRefreshes[launcher, default: []].append(refresh)
                if let error = recorder.modelErrors[launcher] { throw error }
                return LauncherModels(agent: launcher == "claude" ? "claude" : "codex",
                                      models: [], locked: nil, machine: nil)
            },
            keep: { _, pick in
                recorder.kept = pick
                return true
            },
            drop: { _ in
                recorder.dropped = true
                return true
            }
        )
    }

    @Test func pullToRefreshRefreshesTheLauncherAndEveryModelCatalog() async throws {
        let recorder = Recorder()
        let panel = AssistantModelPanelModel(key: key, machine: machine, deps: deps(recorder))

        await panel.load(refresh: true)

        #expect(recorder.launcherRefreshes == [true])
        #expect(recorder.modelRefreshes["claude"] == [true])
        #expect(recorder.modelRefreshes["team-api"] == [true])
        #expect(panel.loading == false)
        #expect(panel.loadError == nil)
    }

    @Test func a404BecomesAnEmptyCatalogInsteadOfLoadingForever() async throws {
        let recorder = Recorder()
        recorder.modelErrors["team-api"] = APIError(status: 404, message: "no models")
        let panel = AssistantModelPanelModel(key: key, machine: machine, deps: deps(recorder))

        await panel.load()

        #expect(panel.list("team-api")?.models == [])
        #expect(panel.error("team-api") == nil)
        #expect(panel.list("claude")?.models == [])
    }

    @Test func aTemporaryRefreshErrorKeepsTheLastGoodCatalogAndShowsRetry() async throws {
        let recorder = Recorder()
        let panel = AssistantModelPanelModel(key: key, machine: machine, deps: deps(recorder))
        await panel.load()
        #expect(panel.list("claude") != nil)

        recorder.modelErrors["claude"] = APIError(status: 502, message: "hub down")
        await panel.load(refresh: true)

        #expect(panel.list("claude") != nil)
        #expect(panel.error("claude") == "hub down")
        #expect(recorder.modelRefreshes["claude"] == [false, true])
    }

    @Test func aLauncherRefreshErrorKeepsTheResponseAndShowsRetry() async throws {
        let recorder = Recorder()
        let panel = AssistantModelPanelModel(key: key, machine: machine, deps: deps(recorder))
        await panel.load()
        #expect(panel.response != nil)

        recorder.launcherError = APIError(status: 502, message: "launcher hub down")
        await panel.load(refresh: true)

        #expect(panel.response != nil)
        #expect(panel.loadError == "launcher hub down")
    }

    @Test func aProviderMissingFromBothListsNeedsRecovery() async throws {
        let recorder = Recorder()
        let panel = AssistantModelPanelModel(key: key, machine: machine, deps: deps(recorder))

        await panel.load()

        #expect(panel.isMissing("old-gw"))
        #expect(!panel.isMissing("claude"))
        #expect(!panel.isMissing("team-api"))
    }

    @Test func chooseAndFollowDefaultCallTheConversationModelEndpoints() async throws {
        let recorder = Recorder()
        let panel = AssistantModelPanelModel(key: key, machine: machine, deps: deps(recorder))
        let session = try session()

        let changed = await panel.choose(launcher: "claude", model: "haiku", session: session)
        let reset = await panel.followDefault()

        #expect(changed)
        #expect(recorder.kept == ModelPick(launcher: "claude", model: "haiku", effort: "high"))
        #expect(reset)
        #expect(recorder.dropped)
    }

    private func session() throws -> Session {
        let json = #"{"machine_id":"box","session_id":"main","launcher":"claude","model":"opus","effort":"high"}"#
        return try JSON.decoder.decode(Session.self, from: Data(json.utf8))
    }
}

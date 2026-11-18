import PersonaWire
import SwiftUI

/// New conversations inherit their effective model. A missing catalog is unknown, not proof that a saved effort is invalid.
nonisolated enum AssistantStarterEffort {
    static func knownOptions(launcher: String, response: APIClient.LaunchersResponse, list: LauncherModels?) -> [SessionModes.Option]? {
        guard let list else { return nil }
        let configured = response.default?.launcher == launcher ? response.default?.model : nil
        let own = response.launchers.first { $0.name == launcher }?.model
        let named = configured.flatMap { $0.isEmpty ? nil : $0 }
            ?? own.flatMap { ModelID.isValid($0) ? $0 : nil }
        let model: AgentModel?
        if let named {
            model = list.models.first { $0.id == named } ?? list.models.first { $0.resolved == named }
        } else {
            model = list.models.first(where: \.isDefault)
        }
        guard let levels = model?.efforts else { return nil }
        var seen = Set<String>()
        return levels.filter { $0 != "auto" && seen.insert($0).inserted }.map { id in
            SessionModes.effort(id) ?? SessionModes.Option(id: id, title: id, detail: "", symbol: Theme.Icon.effort)
        }
    }

    static func options(launcher: String, response: APIClient.LaunchersResponse, list: LauncherModels?) -> [SessionModes.Option] {
        knownOptions(launcher: launcher, response: response, list: list) ?? []
    }

    static func retained(_ effort: String, options: [SessionModes.Option]) -> String {
        options.contains { $0.id == effort } ? effort : ""
    }
}

/// Lives with the paired AppModel. Sending only consults this bounded cache; it never starts a catalog request.
@MainActor final class AssistantStarterCache {
    struct Entry {
        var launchers: [Launcher]
        var efforts: [SessionModes.Option]?
    }
    private var entries: [String: Entry] = [:]

    func entry(for source: String) -> Entry? { entries[source] }

    func keep(_ entry: Entry, for source: String) {
        if entries[source] == nil && entries.count >= 64 { entries.removeAll() }
        entries[source] = entry
    }

    func retained(_ effort: String, source: String) -> String {
        guard let options = entries[source]?.efforts else { return effort }
        return AssistantStarterEffort.retained(effort, options: options)
    }

    func clear() { entries.removeAll() }
}

/// Prefetches the current starter's choices. Manager and settings revision are part of every request and cache identity.
@Observable final class AssistantStarterModels {
    struct Deps {
        var launchers: (MachineRef, String?, Bool) async throws -> APIClient.LaunchersResponse
        var api: (String) -> APIClient?
        var cache: AssistantStarterCache? = nil
        static var live: Deps {
            .init(launchers: { machine, manager, refresh in
                guard let api = AppModel.shared.api(machine.group)?.manager(manager) else {
                    throw APIError(status: 0, message: String(localized: "Not connected to the hub yet"))
                }
                return try await AppModel.shared.assistantLaunchers(machine, refresh: refresh, using: api)
            }, api: { AppModel.shared.api($0) })
        }
    }
    private(set) var launchers: [Launcher] = []
    private(set) var efforts: [SessionModes.Option] = []
    private(set) var key = ""
    @ObservationIgnored private let deps: Deps
    @ObservationIgnored private var requestID = 0
    private var cache: AssistantStarterCache { deps.cache ?? AppModel.shared.assistantStarterCache }

    init(deps: Deps = .live) { self.deps = deps }

    static func source(_ machine: MachineRef, launcher: String?, fallback: String?, manager: String? = nil, revision: Int = 0) -> String {
        "\(machine.path)|\(manager ?? "")|\(launcher ?? "")|\(fallback ?? "")|\(revision)"
    }

    func options(for source: String) -> [SessionModes.Option] { source == key ? efforts : [] }
    func retained(_ effort: String, source: String) -> String { cache.retained(effort, source: source) }

    @discardableResult
    func load(_ machine: MachineRef, launcher: String?, fallback: String?, manager: String? = nil, revision: Int = 0, refresh: Bool = false) async -> Bool {
        let source = Self.source(machine, launcher: launcher, fallback: fallback, manager: manager, revision: revision)
        requestID += 1
        let request = requestID
        key = source
        let previous = cache.entry(for: source)
        launchers = previous?.launchers ?? []
        efforts = previous?.efforts ?? []
        if previous != nil && !refresh { return true }
        do {
            let response = try await deps.launchers(machine, manager, refresh)
            guard requestID == request, !Task.isCancelled else { return false }
            let listed = response.launchers.filter(\.runsAssistant)
            // Available launchers remain selectable even if this launcher's model catalog is temporarily unavailable.
            launchers = listed
            guard let name = launcher ?? response.default?.launcher ?? fallback,
                  let api = deps.api(machine.group)?.manager(manager) else { return false }
            let list = try await api.launcherModels(machine: machine.id, launcher: name, refresh: refresh, purpose: "assistant")
            guard requestID == request, !Task.isCancelled else { return false }
            let known = AssistantStarterEffort.knownOptions(launcher: name, response: response, list: list)
            efforts = known ?? previous?.efforts ?? []
            if known != nil || previous?.efforts != nil {
                cache.keep(.init(launchers: listed, efforts: known ?? previous?.efforts), for: source)
            }
            return true
        } catch {
            // The already verified choice stays intact during a transient failure; the server still validates a send.
            return false
        }
    }
}

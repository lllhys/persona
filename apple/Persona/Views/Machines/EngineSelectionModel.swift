import Foundation
import Observation

/// One engine per machine, or none (the empty host set, #3302). Editing stays local until the hub accepts the PUT.
@Observable final class EngineSelectionModel {
    struct Deps {
        var api: () -> APIClient?
        var updated: (Machine) -> Void
    }

    var selected: MachineEngine
    private(set) var saving = false
    private(set) var error: String?
    private var edited = false
    @ObservationIgnored private let deps: Deps

    init(machine: Machine, pairing: Bool, deps: Deps) {
        selected = Self.initialChoice(machine, pairing: pairing)
        self.deps = deps
    }

    static func pairingMachine(_ machines: [Machine]) -> Machine? {
        machines.first { $0.kind == "local" } ?? machines.first
    }

    /// The machine's current engine: herdr when it was never chosen (2026-11-01 user: an engine must be picked, herdr
    /// by default; no guessing from what happens to be detected). An empty host set is no terminal engine, though
    /// `engine` says herdr for it.
    static func initialChoice(_ machine: Machine, pairing: Bool) -> MachineEngine {
        machine.engineHosts?.isEmpty == true ? .noEngine : machine.engine
    }

    /// Older hubs and offline machines may not report detection; their known engines can still be configured. No
    /// terminal engine comes last, and only from a hub that reports host sets: an older one would refuse it.
    static func options(_ machine: Machine) -> [DetectedEngine] {
        var options = machine.engines.isEmpty
            ? MachineEngine.allCases.map { DetectedEngine(id: $0.rawValue, name: $0.name, available: false) }
            : machine.engines
        if offersNoEngine(machine) {
            options.append(.init(id: MachineEngine.noEngine.rawValue, name: MachineEngine.noEngine.name, available: true))
        }
        return options
    }

    static func offersNoEngine(_ machine: Machine) -> Bool { machine.engineHosts != nil }

    static func canSelect(_ engine: MachineEngine, on machine: Machine) -> Bool {
        if engine == .noEngine { return offersNoEngine(machine) }
        return MachineEngine.allCases.contains(engine) || machine.engines.contains { $0.id == engine.rawValue && $0.available }
    }

    func choose(_ engine: MachineEngine) {
        selected = engine
        edited = true
        error = nil
    }

    /// Live detection and current-engine changes refresh the picker until the user starts editing it.
    func synchronize(_ machine: Machine, pairing: Bool) {
        guard !edited && !saving else { return }
        selected = Self.initialChoice(machine, pairing: pairing)
    }

    func save(machineID: String) async -> Bool {
        guard !saving else { return false }
        saving = true
        error = nil
        defer { saving = false }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "Not connected to the hub yet")) }
            let saved = try await api.setEngine(machine: machineID, engine: selected)
            deps.updated(saved)
            selected = Self.initialChoice(saved, pairing: false)
            edited = false
            return true
        } catch {
            // Timeouts and no network get a calm line, not the raw URL error: the pairing step needs a reason to stay.
            self.error = error.shown.map { String(localized: "Couldn't save: \($0) [Image Viewer fcc6eb]") }
                ?? String(localized: "Couldn't save: can't reach the hub, nothing was saved")
            return false
        }
    }
}

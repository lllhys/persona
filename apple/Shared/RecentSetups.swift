import Foundation
import Observation
import PersonaWire

/// One setup that started an Agent from New Agent (#2134), kept on this device only; the rules every client follows are
/// in spec/fixtures/recents/README.md.
/// The shape is generated from schema/wire/recents.schema.json (`RecentSetup` in PersonaWire).
nonisolated extension RecentSetup {
    /// The same setup: every field but when it started.
    func isSame(as other: Self) -> Bool {
        var o = other
        o.at = at
        return o == self
    }
}

/// New Agent's recent setups (spec/fixtures/recents): record a start, the cards to show, a machine's model.
nonisolated enum RecentSetups {
    static let maxEntries = 20
    static let maxCards = 3

    /// A machine the hub lists, by `MachineRef.path`.
    struct Machine: Codable, Hashable, Sendable {
        var id: String
        var online: Bool
    }

    /// A card: the entry at `index`, and whether its machine is online (an offline one is shown and can't be picked).
    struct Card: Codable, Hashable, Sendable {
        var index: Int
        var online: Bool
    }

    /// A start: the same setup again (all but `at`) moves first with its new time; at most `maxEntries` kept.
    static func record(_ setup: RecentSetup, into entries: [RecentSetup]) -> [RecentSetup] {
        Array(([setup] + entries.filter { !$0.isSame(as: setup) }).prefix(maxEntries))
    }

    /// The entries whose machine the hub still lists, in order, at most `maxCards`.
    static func cards(_ entries: [RecentSetup], machines: [Machine]) -> [Card] {
        let online = Dictionary(machines.map { ($0.id, $0.online) }, uniquingKeysWith: { a, _ in a })
        return Array(entries.indices.compactMap { i in online[entries[i].machine].map { Card(index: i, online: $0) } }.prefix(maxCards))
    }

    /// The model New Agent restores for a machine and its launcher: the one its latest entry with that launcher started;
    /// "" (the default) when there is none.
    static func model(_ entries: [RecentSetup], machine: String, launcher: String) -> String {
        entries.first { $0.machine == machine && $0.launcher == launcher }?.model ?? ""
    }

    /// `at` for a start now.
    static func at(_ date: Date) -> String {
        date.formatted(.iso8601)
    }
}

/// The recent setups on this device, one `StateCache` file. One for the app: a sheet opened right after a start shows
/// it, though the file is written a moment later.
@MainActor @Observable final class RecentSetupsStore {
    static let stateName = "recent-setups"
    static let shared = RecentSetupsStore()

    private(set) var entries: [RecentSetup]
    @ObservationIgnored private let store: @MainActor ([RecentSetup]) -> Void

    init(load: () -> [RecentSetup]? = { StateCache.load(RecentSetupsStore.stateName) },
         store: @escaping @MainActor ([RecentSetup]) -> Void = { StateCache.save(RecentSetupsStore.stateName, $0) }) {
        entries = load() ?? []
        self.store = store
    }

    func record(_ setup: RecentSetup) {
        entries = RecentSetups.record(setup, into: entries)
        store(entries)
    }

    /// Remove from Recent: that setup only.
    func remove(_ setup: RecentSetup) {
        entries.removeAll { $0.isSame(as: setup) }
        store(entries)
    }

    /// Clear Recent Setups.
    func clear() {
        entries = []
        store(entries)
    }

    func cards(_ machines: [RecentSetups.Machine]) -> [(setup: RecentSetup, online: Bool)] {
        RecentSetups.cards(entries, machines: machines).map { (entries[$0.index], $0.online) }
    }

    func model(machine: String, launcher: String) -> String {
        RecentSetups.model(entries, machine: machine, launcher: launcher)
    }
}

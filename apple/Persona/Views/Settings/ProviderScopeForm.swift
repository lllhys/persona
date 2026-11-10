import Foundation

/// A Persona-managed provider's device scope (docs/integrations/launchers.md "Device scope and on-demand delivery"): which
/// computers it runs on, when its key travels, whether it stands back where the harness has a service of its own, and keys
/// for single computers. The form `ProviderComputersPage` edits, its check, the fields a POST / PUT carries, and the
/// summaries the rows show. Mirrors the web's `lib/provider-scope.ts`.
nonisolated struct ProviderScopeForm: Equatable, Sendable {
    nonisolated enum Share: String, CaseIterable, Identifiable, Sendable {
        /// Every confirmed personal computer; work computers only when added (`work`).
        case all
        /// Only the computers in `machines`.
        case selected
        /// No computer: the key stays with the hub (direct API calls still use it).
        case none
        var id: String { rawValue }

        var title: String {
            switch self {
            case .all: String(localized: "All Computers")
            case .selected: String(localized: "Selected Computers")
            case .none: String(localized: "Hub Only")
            }
        }
    }

    nonisolated enum Delivery: String, Sendable {
        /// A computer fetches the key the first time it needs it.
        case onDemand = "on_demand"
        /// Every computer in scope fetches it as soon as it connects (Keep a Copy on Each Computer).
        case push
    }

    nonisolated enum OwnService: String, Sendable {
        /// Not used on a computer whose harness already has a service of its own.
        case skip
        /// Used there too.
        case use
    }

    /// The hub's feature for the scope fields; without it nothing here is shown or sent.
    static let feature = "launcher_scope"

    var share: Share = .all
    /// With `selected`: the computers it runs on.
    var machines: [String] = []
    /// With `all`: the work computers added to it.
    var work: [String] = []
    var delivery: Delivery = .onDemand
    /// nil: not chosen, so a new provider follows the hub's default (`ownService(assign:kind:)`).
    var ownService: OwnService? = nil
    /// Keys typed for single computers; never prefilled.
    var keys: [String: String] = [:]
    /// Computers whose stored key goes when saved.
    var remove: [String] = []

    /// What the hub gives a new provider.
    init() {}

    /// The scope a stored provider opens with; nil when its hub does not know scopes.
    init?(view: LauncherView?) {
        guard let v = view, let raw = v.share, let share = Share(rawValue: raw) else { return nil }
        self.share = share
        let ids = v.scopeMachines ?? []
        machines = share == .selected ? ids : []
        work = share == .all ? ids : []
        delivery = Delivery(rawValue: v.delivery ?? "") ?? .onDemand
        ownService = OwnService(rawValue: v.ownService ?? "") ?? .skip
    }

    /// What `own_service` comes to: the choice, else the hub's default for a new provider (use when it is assigned to
    /// uses right away or is a direct API connection, skip otherwise).
    func ownService(assign: Bool, kind: String) -> OwnService {
        ownService ?? (assign || kind == "api" ? .use : .skip)
    }

    /// The ids `scope_machines` carries for the share chosen.
    var scopeMachines: [String] {
        switch share {
        case .selected: machines
        case .all: work
        case .none: []
        }
    }

    // MARK: Computers

    /// A work computer, when the hub's machine list says so; hubs that only mark pending ones make none.
    static func isWork(_ m: Machine) -> Bool { m.trust == "work" }

    /// The computers Selected Computers offers: confirmed ones, plus whatever is already chosen (kept to untick).
    static func choosable(_ all: [Machine], chosen: [String]) -> [String] {
        var out = all.filter { !$0.isPending || chosen.contains($0.id) }.map(\.id)
        for id in chosen where !out.contains(id) { out.append(id) }
        return out
    }

    /// The work computers All Computers can add: the known ones, plus those already added.
    static func addableWork(_ all: [Machine], added: [String]) -> [String] {
        var out = all.filter { isWork($0) && !$0.isPending }.map(\.id)
        for id in added where !out.contains(id) { out.append(id) }
        return out
    }

    /// The computers that may get the provider, whose own key can be set: none for Hub Only.
    func keyMachines(_ all: [Machine]) -> [String] {
        switch share {
        case .none: return []
        case .selected: return machines
        case .all:
            var out = all.filter { !$0.isPending && !Self.isWork($0) }.map(\.id)
            for id in work where !out.contains(id) { out.append(id) }
            return out
        }
    }

    /// The computers Per-Computer Keys lists: those that may get it, then any that still has a key of its own.
    func keyRows(_ all: [Machine], stored: [String]) -> [String] {
        var out = keyMachines(all)
        for id in stored where !out.contains(id) { out.append(id) }
        return out
    }

    /// How many keys are typed (not blank).
    var typedKeys: Int { keys.values.filter { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }.count }

    /// What keeps the scope from being saved (the hub's own check, said earlier); nil when it can be.
    var problem: String? {
        share == .selected && machines.isEmpty ? String(localized: "Choose at least one computer.") : nil
    }

    /// Ticks or unticks a computer of Selected Computers (`work`: of the work computers added to All Computers).
    mutating func toggle(_ id: String, work isWork: Bool = false) {
        if isWork {
            if work.contains(id) { work.removeAll { $0 == id } } else { work.append(id) }
        } else {
            if machines.contains(id) { machines.removeAll { $0 == id } } else { machines.append(id) }
        }
    }

    // MARK: What a save sends

    nonisolated struct Fields: Equatable, Sendable {
        var share: String?
        var scopeMachines: [String]?
        var delivery: String?
        var ownService: String?
        var machineKeys: [String: String]?
    }

    /// The scope's fields of POST / PUT /api/v1/launchers. A PUT (`old` has a share) sends a field only when it changed
    /// (left out keeps what is stored, so a legacy provider stays as it was until one is changed); a POST only what
    /// differs from the hub's defaults. `scope_machines` goes with the share whenever either changed. `machine_keys`
    /// carries the keys typed for computers that may get the provider (or already have one of their own), "" for each
    /// one removed.
    func fields(old: LauncherView?, machines all: [Machine], assign: Bool = false, kind: String) -> Fields {
        var f = Fields()
        let ids = scopeMachines
        let own = ownService(assign: assign, kind: kind)
        if let old, let oldShare = old.share {
            let shareChanged = share.rawValue != oldShare
            let oldIDs = old.scopeMachines ?? []
            if shareChanged { f.share = share.rawValue }
            if shareChanged || ids.count != oldIDs.count || Set(ids) != Set(oldIDs) { f.scopeMachines = ids }
            if delivery.rawValue != (old.delivery ?? Delivery.onDemand.rawValue) { f.delivery = delivery.rawValue }
            if own.rawValue != (old.ownService ?? OwnService.skip.rawValue) { f.ownService = own.rawValue }
        } else {
            if share != .all { f.share = share.rawValue }
            if !ids.isEmpty { f.scopeMachines = ids }
            if delivery != .onDemand { f.delivery = delivery.rawValue }
            if ownService != nil, own != (assign || kind == "api" ? .use : .skip) { f.ownService = own.rawValue }
        }
        var out: [String: String] = [:]
        let allowed = keyMachines(all) + (old?.keyMachines ?? [])
        for (id, key) in keys {
            let k = key.trimmingCharacters(in: .whitespacesAndNewlines)
            if !k.isEmpty, allowed.contains(id) { out[id] = k }
        }
        for id in remove where out[id] == nil { out[id] = "" }
        if !out.isEmpty { f.machineKeys = out }
        return f
    }

    /// Puts the scope's fields into a save.
    func apply(to input: inout LauncherInput, old: LauncherView?, machines all: [Machine], assign: Bool = false, kind: String) {
        let f = fields(old: old, machines: all, assign: assign, kind: kind)
        input.share = f.share
        input.scopeMachines = f.scopeMachines
        input.delivery = f.delivery
        input.ownService = f.ownService
        input.machineKeys = f.machineKeys
    }

    // MARK: Words

    /// Whether the node can tell the harness has a service of its own (Claude Code, Codex): only then is the own-service
    /// rule offered and said.
    static func ownServiceDetected(kind: String?, agent: String?) -> Bool {
        kind != "api" && ["", "claude", "codex"].contains(agent ?? "")
    }

    /// Where it runs, in a few words: 「Only agentbox」, 「All computers + 1 work computer」, 「Hub only」.
    static func whereText(share: String?, machines ids: [String], legacy: Bool, name: (String) -> String) -> String {
        if share == Share.none.rawValue { return String(localized: "Hub only") }
        if legacy { return String(localized: "All computers, work ones included") }
        if share == Share.selected.rawValue {
            guard !ids.isEmpty else { return String(localized: "No computers chosen") }
            return String(localized: "Only \(ListFormatter.localizedString(byJoining: ids.map(name))) [Provider Scope Form]")
        }
        return ids.isEmpty ? String(localized: "All computers") : String(localized: "All computers + \(ids.count) work computers")
    }

    /// One line for the detail page: where, how the key travels, and (for Claude Code and Codex) whether it also runs
    /// where the harness is signed in. 「Only agentbox · On demand · Skips signed-in computers」.
    static func summary(_ v: LauncherView, name: (String) -> String) -> String {
        let place = whereText(share: v.share, machines: v.scopeMachines ?? [], legacy: v.legacyScope == true, name: name)
        guard v.share != Share.none.rawValue else { return place }
        var parts = [place, v.delivery == Delivery.push.rawValue ? String(localized: "Copy on each") : String(localized: "On demand")]
        if ownServiceDetected(kind: v.kind, agent: v.agent) {
            parts.append(v.ownService == OwnService.skip.rawValue ? String(localized: "Skips signed-in computers") : String(localized: "Also where signed in"))
        }
        return parts.joined(separator: " · ")
    }

    /// The Computers row's value in the editors: where it runs.
    func rowSummary(legacy: Bool, name: (String) -> String) -> String {
        Self.whereText(share: share.rawValue, machines: scopeMachines, legacy: legacy, name: name)
    }

    // MARK: Harness groups

    /// The harness a managed provider runs in: a direct API connection its own group, a launcher its agent.
    static func harness(of l: LauncherView) -> String {
        if l.kind == "api" { return "api" }
        let a = l.agent ?? ""
        return a.isEmpty ? "claude" : a
    }

    private static let harnessOrder = ["claude", "codex", "agy", "omp", "api"]

    /// Providers grouped by harness: the known ones in a fixed order, any other after them; the list's order kept within each.
    static func harnessGroups(_ list: [LauncherView]) -> [HarnessGroup] {
        var out: [HarnessGroup] = []
        for l in list {
            let h = harness(of: l)
            if let i = out.firstIndex(where: { $0.harness == h }) { out[i].items.append(l) } else { out.append(HarnessGroup(harness: h, items: [l])) }
        }
        let rank = { (h: String) in harnessOrder.firstIndex(of: h) ?? harnessOrder.count }
        return out.enumerated().sorted { (rank($0.element.harness), $0.offset) < (rank($1.element.harness), $1.offset) }.map(\.element)
    }

    nonisolated struct HarnessGroup: Identifiable, Sendable {
        var harness: String
        var items: [LauncherView]
        var id: String { harness }
        /// The products' own names, Direct API translated.
        var title: String {
            switch harness {
            case "claude": "Claude Code"
            case "codex": "Codex"
            case "agy": "Antigravity"
            case "omp": "Oh My Pi"
            case "api": String(localized: "Direct API")
            default: harness
            }
        }
    }
}

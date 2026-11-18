import Foundation

// An extension being installed, upgraded or removed on the hub, step by step
// (docs/protocol/protocol.md「Extension install, upgrade and uninstall progress」, #3277, #3307): the hub runs it in the background and pushes the whole operation in an
// `extension_operation` event every time it moves on. Only with a hub that says `extension_operations`; an older hub
// answers the request itself when it is done. Unknown states and steps are kept as they come: a newer hub may add some.

/// One step of an operation (download, verify, unpack, check, consent, tools, install, start, nodes; stop, remove).
public nonisolated struct ExtensionOperationStep: Codable, Hashable, Sendable, Identifiable {
    public enum State: String, Codable, Sendable {
        case pending, running, done, failed, skipped

        /// A state this app does not know reads as still going.
        public init(from decoder: Decoder) throws {
            self = State(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .running
        }
    }

    /// The computers the `nodes` step sends the extension's program to: done of total, and why some failed.
    public struct Machines: Codable, Hashable, Sendable {
        public var done: Int
        public var total: Int
        public var errors: [String: String]?

        public init(done: Int, total: Int, errors: [String: String]? = nil) {
            self.done = done
            self.total = total
            self.errors = errors
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            done = try c.decodeIfPresent(Int.self, forKey: .done) ?? 0
            total = try c.decodeIfPresent(Int.self, forKey: .total) ?? 0
            errors = try? c.decodeIfPresent([String: String].self, forKey: .errors)
        }
    }

    /// One command-line program of the `tools` step (#4507): pending, running, done or failed; `reused` when it is the
    /// hub machine's own copy (nothing downloaded).
    public struct Tool: Codable, Hashable, Sendable, Identifiable {
        public var name: String
        public var state: State
        public var reused: Bool?
        public var id: String { name }

        public init(name: String, state: State, reused: Bool? = nil) {
            self.name = name
            self.state = state
            self.reused = reused
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
            state = try c.decodeIfPresent(State.self, forKey: .state) ?? .pending
            reused = try? c.decodeIfPresent(Bool.self, forKey: .reused)
        }
    }

    public var id: String
    public var state: State
    /// The download's bytes so far and of the whole (the total can be unknown); for `tools`, of the program downloading.
    public var bytes: Int64?
    public var total: Int64?
    /// The package came from the hub's cache: nothing downloaded.
    public var cached: Bool?
    public var machines: Machines?
    /// The `tools` step's programs, in the order they install.
    public var tools: [Tool]?
    public var startedAt: Date?
    public var endedAt: Date?

    /// How far the step is, 0…1; nil: no bar, or one that cannot say how far.
    public var fraction: Double? {
        guard let bytes, let total, total > 0 else { return nil }
        return min(max(Double(bytes) / Double(total), 0), 1)
    }

    public enum CodingKeys: String, CodingKey {
        case id, state, bytes, total, cached, machines, tools
        case startedAt = "started_at", endedAt = "ended_at"
    }

    public init(id: String, state: State, bytes: Int64? = nil, total: Int64? = nil, cached: Bool? = nil, machines: Machines? = nil,
         tools: [Tool]? = nil) {
        self.tools = tools
        self.id = id
        self.state = state
        self.bytes = bytes
        self.total = total
        self.cached = cached
        self.machines = machines
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        state = try c.decodeIfPresent(State.self, forKey: .state) ?? .pending
        bytes = try? c.decodeIfPresent(Int64.self, forKey: .bytes)
        total = try? c.decodeIfPresent(Int64.self, forKey: .total)
        cached = try? c.decodeIfPresent(Bool.self, forKey: .cached)
        machines = try? c.decodeIfPresent(Machines.self, forKey: .machines)
        tools = try? c.decodeIfPresent([Tool].self, forKey: .tools)
        startedAt = try? c.decodeIfPresent(Date.self, forKey: .startedAt)
        endedAt = try? c.decodeIfPresent(Date.self, forKey: .endedAt)
    }
}

/// An install, upgrade or removal and where it stands.
public nonisolated struct ExtensionOperation: Codable, Hashable, Sendable, Identifiable {
    public enum Kind: String, Codable, Sendable {
        case install, upgrade, uninstall

        public init(from decoder: Decoder) throws {
            self = Kind(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .install
        }
    }

    public enum State: String, Codable, Sendable {
        case running
        /// Downloaded and checked: the preview waits for the user's yes (`preview_id`).
        case needsConsent = "needs_consent"
        case succeeded, failed, canceled

        public init(from decoder: Decoder) throws {
            self = State(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .running
        }
    }

    public var id: String
    public var kind: Kind
    /// The extension's id (before the preview, the market's id of it).
    public var extensionID: String
    public var name: String?
    public var market: String?
    public var version: String?
    /// The version an upgrade goes from.
    public var from: String?
    /// Who started it: a device's name, personad extension, the hub (an automatic upgrade).
    public var actor: String?
    public var state: State
    /// The step it is at (or stopped at).
    public var step: String?
    public var steps: [ExtensionOperationStep]
    /// Why it failed, in the language of the request that started it.
    public var error: String?
    public var errorCode: String?
    /// The preview it waits on (needs_consent).
    public var previewID: String?
    /// The hub can start it again (`POST …/-/operations/{id}/retry`).
    public var retryable: Bool
    /// What the user should do once it succeeded: "auth", the extension signs in and is not signed in yet (#4507).
    public var next: String?
    public var startedAt: Date?
    public var updatedAt: Date?
    public var endedAt: Date?
    /// Whose operation it is when another manager's than the default (its push says so, docs/assistant/managers.md
    /// "Clients"); nil: the default's, as everything this app starts is. Set by the app, never decoded or sent.
    public var manager: String?

    /// Going on: running, or waiting for the user's yes.
    public var isActive: Bool { state == .running || state == .needsConsent }
    public var isRunning: Bool { state == .running }
    /// Done, and the extension waits for the user to sign in (the app opens its sign-in).
    public var needsSignIn: Bool { state == .succeeded && next == "auth" }
    public var current: ExtensionOperationStep? {
        steps.first { $0.id == step } ?? steps.first { $0.state == .running || $0.state == .failed }
    }
    public var failedStep: ExtensionOperationStep? {
        state == .failed ? (steps.first { $0.state == .failed } ?? current) : nil
    }
    /// The steps worth showing: the ones this kind skips are left out.
    public var shownSteps: [ExtensionOperationStep] { steps.filter { $0.state != .skipped } }

    public enum CodingKeys: String, CodingKey {
        case id, kind, name, market, version, from, actor, state, step, steps, error, retryable, next
        case extensionID = "extension", plugin
        case errorCode = "error_code", previewID = "preview_id"
        case startedAt = "started_at", updatedAt = "updated_at", endedAt = "ended_at"
    }

    public init(id: String, kind: Kind, extensionID: String, name: String? = nil, version: String? = nil, from: String? = nil,
         market: String? = nil, state: State = .running, step: String? = nil, steps: [ExtensionOperationStep] = [],
         error: String? = nil, errorCode: String? = nil, retryable: Bool = false, startedAt: Date? = nil,
         updatedAt: Date? = nil, endedAt: Date? = nil) {
        self.id = id
        self.kind = kind
        self.extensionID = extensionID
        self.name = name
        self.version = version
        self.from = from
        self.market = market
        self.state = state
        self.step = step
        self.steps = steps
        self.error = error
        self.errorCode = errorCode
        self.retryable = retryable
        self.startedAt = startedAt
        self.updatedAt = updatedAt
        self.endedAt = endedAt
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        kind = try c.decodeIfPresent(Kind.self, forKey: .kind) ?? .install
        // `plugin` from a hub still on the old names.
        extensionID = try c.decodeIfPresent(String.self, forKey: .extensionID) ?? c.decodeIfPresent(String.self, forKey: .plugin) ?? ""
        name = try c.decodeIfPresent(String.self, forKey: .name)
        market = try c.decodeIfPresent(String.self, forKey: .market)
        version = try c.decodeIfPresent(String.self, forKey: .version)
        from = try c.decodeIfPresent(String.self, forKey: .from)
        actor = try c.decodeIfPresent(String.self, forKey: .actor)
        state = try c.decodeIfPresent(State.self, forKey: .state) ?? .running
        step = try c.decodeIfPresent(String.self, forKey: .step)
        steps = (try? c.decodeIfPresent([ExtensionOperationStep].self, forKey: .steps)) ?? []
        error = try c.decodeIfPresent(String.self, forKey: .error)
        errorCode = try c.decodeIfPresent(String.self, forKey: .errorCode)
        previewID = try c.decodeIfPresent(String.self, forKey: .previewID)
        retryable = try c.decodeIfPresent(Bool.self, forKey: .retryable) ?? false
        next = try? c.decodeIfPresent(String.self, forKey: .next)
        startedAt = try? c.decodeIfPresent(Date.self, forKey: .startedAt)
        updatedAt = try? c.decodeIfPresent(Date.self, forKey: .updatedAt)
        endedAt = try? c.decodeIfPresent(Date.self, forKey: .endedAt)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(kind, forKey: .kind)
        try c.encode(extensionID, forKey: .extensionID)
        try c.encodeIfPresent(name, forKey: .name)
        try c.encodeIfPresent(market, forKey: .market)
        try c.encodeIfPresent(version, forKey: .version)
        try c.encodeIfPresent(from, forKey: .from)
        try c.encodeIfPresent(actor, forKey: .actor)
        try c.encode(state, forKey: .state)
        try c.encodeIfPresent(step, forKey: .step)
        try c.encode(steps, forKey: .steps)
        try c.encodeIfPresent(error, forKey: .error)
        try c.encodeIfPresent(errorCode, forKey: .errorCode)
        try c.encodeIfPresent(previewID, forKey: .previewID)
        try c.encode(retryable, forKey: .retryable)
        try c.encodeIfPresent(next, forKey: .next)
        try c.encodeIfPresent(startedAt, forKey: .startedAt)
        try c.encodeIfPresent(updatedAt, forKey: .updatedAt)
        try c.encodeIfPresent(endedAt, forKey: .endedAt)
    }
}

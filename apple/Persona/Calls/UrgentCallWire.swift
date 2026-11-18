import Foundation
import PersonaWire

/// Urgent calls (docs/assistant/urgent-call.md, #8734): what the hub and the manager say about them, as plain data the
/// tests read without a phone call. The assistant rings the owner's iPhone with a VoIP push; the app reports it as a call
/// (`UrgentCallCenter`), and answering opens the call page (`UrgentCallSession`).

/// One ring as the VoIP push carries it, sealed: `{"urgent_call":{id, title, manager, conversation_id, attempt}, iat, hub_id}`.
nonisolated struct UrgentCallRing: Equatable, Sendable {
    var id: String
    var title: String
    /// The manager that rang (its routes answer about the call), "" for the hub's default.
    var manager: String
    var conversationID: String
    var attempt: Int
    /// The hub group whose key opened the push.
    var group: String

    /// How long a ring stays worth reporting: the relay keeps a VoIP push at most a minute, and one that took longer
    /// (a replayed old one) is reported and ended at once.
    static let maxAge: TimeInterval = 90

    /// The ring in an opened push (`PushCrypto.notificationInfo`); nil when it is none, or older than `maxAge`.
    init?(info: [AnyHashable: Any], now: Date = .now) {
        guard let call = info["urgent_call"] as? [String: Any], let id = call["id"] as? String, !id.isEmpty,
              let title = call["title"] as? String else { return nil }
        if let iat = (info["iat"] as? NSNumber)?.doubleValue, abs(now.timeIntervalSince1970 - iat) > Self.maxAge { return nil }
        self.id = id
        self.title = title
        manager = call["manager"] as? String ?? ""
        conversationID = call["conversation_id"] as? String ?? ""
        attempt = (call["attempt"] as? NSNumber)?.intValue ?? 1
        group = info[PushCrypto.groupKey] as? String ?? ""
    }

    init(id: String, title: String, manager: String, conversationID: String, attempt: Int, group: String) {
        self.id = id
        self.title = title
        self.manager = manager
        self.conversationID = conversationID
        self.attempt = attempt
        self.group = group
    }
}

/// A call as `GET /api/v1/assistant/calls/{id}` answers it: the matter the call is about.
nonisolated struct UrgentCallInfo: Codable, Equatable, Sendable, Identifiable {
    struct Rule: Codable, Equatable, Sendable {
        var id: String
        var text: String
    }

    var id: String
    var title: String
    var body: String?
    var why: String?
    var happening: String?
    var rule: Rule?
    var machineID: String?
    var conversationID: String?
    var interactionID: String?
    var state: String
    var attempt: Int?
    var seconds: Int?
    var outcome: String?
    var transcript: String?

    enum CodingKeys: String, CodingKey {
        case id, title, body, why, happening, rule, state, attempt, seconds, outcome, transcript
        case machineID = "machine_id", conversationID = "conversation_id", interactionID = "interaction_id"
    }
}

/// `assistant.urgent_call` as Settings › Urgent calls edits it (`GET` / `PUT /api/v1/assistant/urgent-call`).
nonisolated struct UrgentCallSettings: Codable, Equatable, Sendable {
    struct Rule: Codable, Equatable, Sendable, Identifiable {
        var id: String
        /// The owner's words; empty for a built-in rule that keeps its own wording.
        var text: String?
        var builtin: Bool?
    }

    struct Quiet: Codable, Equatable, Sendable {
        var start: String
        var end: String
    }

    var enabled: Bool
    var rules: [Rule]
    var dailyCap: Int
    var quiet: Quiet?
    var retryMinutes: Int

    enum CodingKeys: String, CodingKey {
        case enabled, rules, quiet
        case dailyCap = "daily_cap", retryMinutes = "retry_minutes"
    }

    static let capRange = 1...10
    static let builtinIDs = ["money_loss", "harm", "agent_disaster"]

    /// Built-in rules taken out, which "Restore" puts back at the top.
    var missingBuiltins: [String] { Self.builtinIDs.filter { id in !rules.contains { $0.id == id } } }

    /// The settings with the built-in rules put back, in their own words.
    func restoringBuiltins() -> UrgentCallSettings {
        var out = self
        let mine = rules.filter { !Self.builtinIDs.contains($0.id) }
        out.rules = Self.builtinIDs.map { Rule(id: $0, text: nil, builtin: true) } + mine
        return out
    }

    /// A new rule's id: the manager keeps it as it is.
    static func newRuleID() -> String { "r_" + String(UUID().uuidString.prefix(6)).lowercased() }
}

/// The settings answer: the settings, how many matters rang today, and whether this device may change them.
nonisolated struct UrgentCallSettingsAnswer: Decodable, Sendable {
    var settings: UrgentCallSettings
    var today: Int?
    var canWrite: Bool?

    enum CodingKeys: String, CodingKey {
        case settings, today
        case canWrite = "can_write"
    }
}

/// What the phone reports about a call (`POST /api/v1/assistant/calls/{id}/state`).
nonisolated enum UrgentCallReport: String, Sendable {
    case answered, declined, missed, ended, seen
}

extension APIClient {
    func urgentCallSettings() async throws -> UrgentCallSettingsAnswer {
        try await getDecodedOffMain("/api/v1/assistant/urgent-call")
    }

    func setUrgentCallSettings(_ settings: UrgentCallSettings) async throws -> UrgentCallSettingsAnswer {
        try JSON.decoder.decode(UrgentCallSettingsAnswer.self, from: try await request("PUT", "/api/v1/assistant/urgent-call", body: settings))
    }

    /// Rings this owner's phones once with a test call; the answer says how many took it.
    func testUrgentCall() async throws -> Int {
        struct Answer: Decodable { var devices: Int? }
        return try JSON.decoder.decode(Answer.self, from: try await request("POST", "/api/v1/assistant/urgent-call/test")).devices ?? 0
    }

    func urgentCall(_ id: String) async throws -> UrgentCallInfo {
        try await getDecodedOffMain("/api/v1/assistant/calls/\(id)")
    }

    /// Reports a call's state; `ended` carries how long it took, a line about what came of it and what was said.
    @discardableResult
    func reportUrgentCall(_ id: String, _ state: UrgentCallReport, seconds: Int? = nil, outcome: String? = nil,
                          transcript: String? = nil) async throws -> UrgentCallInfo {
        struct Body: Encodable {
            var state: String
            var seconds: Int?
            var outcome: String?
            var transcript: String?
        }
        let body = Body(state: state.rawValue, seconds: seconds, outcome: outcome, transcript: transcript)
        return try JSON.decoder.decode(UrgentCallInfo.self, from: try await request("POST", "/api/v1/assistant/calls/\(id)/state", body: body))
    }
}

import Foundation

/// Compacting the main conversation on demand and starting a new one (#1727, docs/assistant/assistant.md「Manual compaction」「新建主对话」):
/// the hub's features, what the chat shows while a compaction runs, and the words the actions use.
nonisolated enum MainConversationActions {
    /// The hub can compact an assistant conversation (POST …/compact).
    static let compactFeature = "assistant_compact"
    /// The hub can archive the main conversation and leave none (POST /api/v1/assistant/main/new).
    static let newMainFeature = "assistant_main_new"
    /// The hub can clear the main conversation's context (POST /api/v1/assistant/main/clear).
    static let clearFeature = "assistant_main_clear"

    /// Clear context is offered on the main conversation when the hub says it can.
    static func offersClear(isMain: Bool, features: Set<String>) -> Bool {
        isMain && features.contains(clearFeature)
    }

    /// A done compaction shows when this page saw it finish, or when it finished this recently.
    static let doneRecent: TimeInterval = 60
    /// A failed one shows when this page saw it fail, or when it failed this recently (until it is closed).
    static let failedRecent: TimeInterval = 10 * 60
    /// How long Context compacted stays.
    static let doneShown: TimeInterval = 4
    /// How long the note says Compacting after asking, before the hub's word on it arrives.
    static let requestGrace: TimeInterval = 15

    /// What the chat shows for a conversation's compaction.
    enum Shown: Hashable, Sendable {
        case running
        case done(key: String)
        case failed(key: String, error: String)
    }

    /// The same compaction's state, whatever its times: what a failure the user closed is remembered by.
    static func key(_ c: CompactionRun) -> String {
        "\(c.startedAt?.timeIntervalSince1970 ?? 0)|\(c.state.rawValue)"
    }

    /// Running while it runs; done / failed only for a change this page saw (`seen`, the state it saw before) or one
    /// that just finished, never an old one on every open. `dismissed`: the key of a failure the user closed.
    static func shown(_ c: CompactionRun?, seen: CompactionRun.State?, now: Date, dismissed: String? = nil) -> Shown? {
        guard let c else { return nil }
        let key = key(c)
        let age = c.finishedAt.map { now.timeIntervalSince($0) } ?? .infinity
        let sawIt = seen == .running
        switch c.state {
        case .running: return .running
        case .done: return sawIt || age <= doneRecent ? .done(key: key) : nil
        case .failed:
            guard dismissed != key, sawIt || age <= failedRecent else { return nil }
            let e = (c.error ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            return .failed(key: key, error: e.isEmpty ? String(localized: "Unknown error") : e)
        }
    }

    /// The title the hub gives the archived main conversation: 「旧主对话 · 至 11月4日」.
    static func archivedTitle(_ now: Date = Date(), calendar: Calendar = .current) -> String {
        var style = Date.FormatStyle.dateTime.month().day()
        style.calendar = calendar
        style.timeZone = calendar.timeZone
        style.locale = .interface
        return String(localized: "Old main chat · until \(now.formatted(style))", comment: "Title of the archived main chat, with the day it was archived")
    }
}

nonisolated extension Compaction {
    /// The divider's words, as the web and Android draw it: 「以上内容已压缩 · 手动 · 压缩前 18.1 万 token」 (the parts known).
    var dividerTitle: String {
        var parts = [String(localized: "Earlier messages compacted")]
        if let source { parts.append(source) }
        if let t = preTokens, t > 0 { parts.append(String(localized: "\(Self.tokens(t)) tokens before")) }
        return parts.joined(separator: " · ")
    }

    /// 「18.1 万」, 8,500.
    static func tokens(_ n: Int) -> String {
        // Chinese counts in 「万」 (as the web and Android); other languages in their own compact form (181K, 「18万」).
        if n >= 10_000, LanguageStore.shared.launched != "zh-Hans" {
            return n.formatted(.number.notation(.compactName).precision(.fractionLength(0...1)).locale(.interface))
        }
        if n >= 10_000 {
            let s = String(format: "%.1f", Double(n) / 10_000)
            return (s.hasSuffix(".0") ? String(s.dropLast(2)) : s) + " 万" // i18n-ignore: Chinese-only unit, other languages return above
        }
        let f = NumberFormatter()
        f.numberStyle = .decimal
        f.locale = Locale(identifier: "en_US")
        return f.string(from: NSNumber(value: n)) ?? String(n)
    }
}

/// The answer to POST /api/v1/assistant/main/new: the archived conversation and whether its summary goes along.
nonisolated struct NewMainResult: Decodable, Sendable {
    var archived: String?
    var carried: Bool?
}

/// The answer to POST /api/v1/assistant/main/clear: the conversation that was archived, empty when there was none.
nonisolated struct ClearMainResult: Decodable, Sendable {
    var ok: Bool?
    var archived: String?
}

/// Where this device is in clearing the main conversation (#5100). Set before the request, so a push that drops the
/// main conversation cannot flash the centered new-chat page. Only the manager it was cleared under keeps the empty page.
nonisolated struct MainClearState: Equatable, Sendable {
    struct Cleared: Equatable, Sendable {
        var group: String
        /// The manager shown when it was cleared; "" is the default manager.
        var manager: String
        var sessionID: String
    }

    var cleared: Cleared?
    var busy = false

    /// False when one is already on its way: the command is used up, and the caller does not post again.
    mutating func begin(group: String, manager: String, sessionID: String) -> Bool {
        if busy { return false }
        busy = true
        cleared = Cleared(group: group, manager: manager, sessionID: sessionID)
        return true
    }

    mutating func succeed() { busy = false }

    /// The hub refused or the call failed: the empty page goes away.
    mutating func fail() {
        busy = false
        cleared = nil
    }

    /// A main conversation that is not the one cleared, for the same manager: the empty page is over.
    /// True when the flag was dropped because a different main appeared.
    mutating func note(group: String, manager: String, mainSessionID: String?) -> Bool {
        guard let cleared, cleared.group == group, cleared.manager == manager else { return false }
        if let mainSessionID, mainSessionID != cleared.sessionID {
            self.cleared = nil
            return true
        }
        return false
    }

    /// The empty conversation page: this manager was cleared here, nothing else is open, and no other main has started.
    /// True while the main id is still the one cleared (the request is on its way) and after the refresh leaves none.
    func showsPage(group: String, manager: String, currentEmpty: Bool, mainSessionID: String?) -> Bool {
        guard currentEmpty, let cleared, cleared.group == group, cleared.manager == manager else { return false }
        if let mainSessionID, mainSessionID != cleared.sessionID { return false }
        return true
    }
}

/// `/clear` in the main conversation: the word alone, nothing attached. Anything more is a message.
nonisolated enum MainClearCommand {
    static let word = "/clear"

    static func isCommand(_ text: String, attachments: Int) -> Bool {
        attachments == 0 && text.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() == word
    }
}

extension APIClient {
    private struct CompactBody: Encodable {
        var machineID: String?
        enum CodingKeys: String, CodingKey { case machineID = "machine_id" }
    }

    private struct NewMainBody: Encodable {
        var carry: String
        var expect: String
    }

    private struct ClearMainBody: Encodable {
        var expect: String
    }

    /// Starts compacting an assistant conversation; its progress comes on the session (`compaction`). A 409 says why
    /// not (`busy`, `compacting`, `gone`, `unsupported`), in words to show the user.
    func compactConversation(_ id: String, machineID: String?) async throws {
        try await post("/api/v1/assistant/conversations/\(id)/compact", body: CompactBody(machineID: machineID?.isEmpty == false ? machineID : nil))
    }

    /// Archives the main conversation `expect` (renamed 「旧主对话 · 至 M月D日」) and leaves none: the next message to the
    /// main conversation starts a new one, with the old one's summary in front of it when `carry`. 409 `main_changed`:
    /// another device replaced it already; `busy`: it is answering or compacting.
    func newMainConversation(carry: Bool, expect: String) async throws -> NewMainResult {
        try await post("/api/v1/assistant/main/new", body: NewMainBody(carry: carry ? "summary" : "none", expect: expect))
    }

    /// Archives the main conversation `expect` and leaves none. Nothing is carried over. A turn is not a refusal.
    /// 409 `main_changed`: another device replaced it already. The caller shows what the hub has.
    func clearMainConversation(expect: String) async throws -> ClearMainResult {
        try await post("/api/v1/assistant/main/clear", body: ClearMainBody(expect: expect))
    }
}

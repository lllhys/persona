import Foundation

/// A task's brief (docs/assistant/unified-entry.md "Briefs"): one record per piece of work, made when it is handed out.
/// The hub sends it on a plan's task before it starts (`tasks[i].brief`). Decode-only: the apps never send it back, and
/// only the parts the plan card draws are read (the web's `lib/brief.ts` reads the rest for the task's page).
public nonisolated struct TaskBrief: Decodable, Hashable, Sendable {
    public init(request: Request?, startedBy: String?, reason: String?, triggerTitle: String?, createdAt: Date?, goal: String?, doneWhen: [String]?, expectMinutes: Int?, outOfBounds: [String]?, reconstructed: Bool?, handover: String?, criteria: [String]?, expectNow: Int?) {
        self.request = request
        self.startedBy = startedBy
        self.reason = reason
        self.triggerTitle = triggerTitle
        self.createdAt = createdAt
        self.goal = goal
        self.doneWhen = doneWhen
        self.expectMinutes = expectMinutes
        self.outOfBounds = outOfBounds
        self.reconstructed = reconstructed
        self.handover = handover
        self.criteria = criteria
        self.expectNow = expectNow
    }

    /// The owner's message that led to the task, as they wrote it (`truncated`: the hub kept its first 2000 characters).
    public struct Request: Decodable, Hashable, Sendable {
        public var text: String
        public var truncated: Bool?
        public var at: Date?

        public init(text: String, truncated: Bool? = nil, at: Date? = nil) {
            self.text = text
            self.truncated = truncated
            self.at = at
        }
    }

    public var request: Request?
    /// owner / assistant / leader / routine / hub.
    public var startedBy: String?
    public var reason: String?
    public var triggerTitle: String?
    public var createdAt: Date?
    public var goal: String?
    public var doneWhen: [String]?
    public var expectMinutes: Int?
    public var outOfBounds: [String]?
    /// Made later for a task from before briefs: `handover` is the prompt its agent got, not the owner's words.
    public var reconstructed: Bool?
    public var handover: String?
    /// `done_when` and `expect_minutes` as the scope changes left them.
    public var criteria: [String]?
    public var expectNow: Int?

    public enum CodingKeys: String, CodingKey {
        case request, reason, goal, criteria, reconstructed, handover
        case startedBy = "started_by", triggerTitle = "trigger_title", createdAt = "created_at", doneWhen = "done_when",
             expectMinutes = "expect_minutes", outOfBounds = "out_of_bounds", expectNow = "expect_minutes_now"
    }

    /// Who asked, as the brief's first line says it (the web's `briefAsked`).
    public enum Asked: Equatable, Sendable {
        case owner(text: String, truncated: Bool, at: Date?)
        case started(by: String, why: String, at: Date?)
        case reconstructed
        case unknown(at: Date?)
    }

    public var asked: Asked {
        if let text = request?.text.trimmingCharacters(in: .whitespacesAndNewlines), !text.isEmpty {
            return .owner(text: text, truncated: request?.truncated == true, at: request?.at)
        }
        if reconstructed == true { return .reconstructed }
        if let by = startedBy, ["assistant", "leader", "routine", "hub"].contains(by) {
            let why = (reason ?? "").isEmpty ? (triggerTitle ?? "") : (reason ?? "")
            return .started(by: by, why: why.trimmingCharacters(in: .whitespacesAndNewlines), at: createdAt)
        }
        return .unknown(at: createdAt)
    }

    /// The criteria in effect now (a hub that sends none: as the brief was made).
    public var criteriaNow: [String] { criteria ?? doneWhen ?? [] }
    /// The expected minutes in effect now; 0 when none was given.
    public var expect: Int { (expectNow ?? 0) > 0 ? expectNow ?? 0 : expectMinutes ?? 0 }
    public var bounds: [String] { outOfBounds ?? [] }
    public var goalText: String { (goal ?? "").trimmingCharacters(in: .whitespacesAndNewlines) }

    /// "About 30 min", "About 2.5 h".
    public static func hoursText(_ minutes: Int) -> String {
        (Double((minutes * 10 + 30) / 60) / 10).formatted(.number.precision(.fractionLength(0...1)))
    }

    /// "2 criteria · about 45 min · 1 out of bounds": what a plan card's folded brief holds, in one line.
    public var summary: String {
        var parts: [String] = []
        let n = criteriaNow.count
        if n == 1 { parts.append(String(localized: "1 criterion")) } else if n > 1 { parts.append(String(localized: "\(n) criteria")) }
        let m = expect
        if m > 0 && m < 120 { parts.append(String(localized: "about \(m) min")) }
        else if m >= 120 { parts.append(String(localized: "about \(TaskBrief.hoursText(m)) h")) }
        if !bounds.isEmpty { parts.append(String(localized: "\(bounds.count) out of bounds")) }
        return parts.joined(separator: " · ")
    }

    /// Whether the plan card has more to open than what its folded form shows (the goal says more than the title, or a
    /// summary, or the owner's words are cut).
    public func hasMore(title: String) -> Bool {
        !summary.isEmpty || (!goalText.isEmpty && goalText != title.trimmingCharacters(in: .whitespacesAndNewlines))
    }
}

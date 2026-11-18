import Foundation

/// Token counts, the same everywhere (#4973): K / M / B with three significant digits and no trailing zeros
/// (1,250 → 1.25K, 2,960,000 → 2.96M, 178,000,000 → 178M); under 1,000 the number itself. Rounding that reaches
/// 1,000 of a unit moves up a unit (999,500 → 1M, not 1000K). The web has the same rules (web/src/lib/tokens.ts).
public nonisolated enum TokenFormat {
    private static let units: [(Double, String)] = [(1e3, "K"), (1e6, "M"), (1e9, "B")]

    public static func count(_ n: Int64) -> String {
        let v = Double(n.magnitude)
        let sign = n < 0 ? "-" : ""
        guard v >= 1000 else { return sign + String(n.magnitude) }
        var i = units.lastIndex { v >= $0.0 } ?? 0
        while true {
            let x = v / units[i].0
            let digits = x >= 100 ? 0 : x >= 10 ? 1 : 2
            let scale = pow(10, Double(digits))
            let rounded = (x * scale).rounded() / scale
            if rounded >= 1000, i + 1 < units.count {
                i += 1
                continue
            }
            return sign + trimmed(String(format: "%.\(digits)f", rounded)) + units[i].1
        }
    }

    public static func count(_ n: Int) -> String { count(Int64(n)) }

    /// The language's own short numbers, at most three digits: 3.2B and 72.9M in English, counted in ten-thousands and
    /// hundred-millions in Chinese and Japanese (the token calendar, docs/server/machines.md "Token usage history").
    /// `count` stays K / M / B everywhere else.
    public static func short(_ n: Int64, locale: Locale = .autoupdatingCurrent) -> String {
        n.formatted(.number.notation(.compactName).precision(.significantDigits(1...3)).locale(locale))
    }

    /// "31K/min": a rate of tokens per minute.
    public static func perMinute(_ n: Int64) -> String { String(localized: "\(count(n))/min", comment: "Tokens per minute, as in 31K/min") }

    private static func trimmed(_ s: String) -> String {
        guard s.contains(".") else { return s }
        var t = s
        while t.hasSuffix("0") { t.removeLast() }
        if t.hasSuffix(".") { t.removeLast() }
        return t
    }
}

/// The four kinds of tokens, in the order a stacked chart puts them from the bottom.
public nonisolated enum UsageKind: String, CaseIterable, Identifiable, Sendable {
    case input, output, cacheWrite, cacheRead

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .input: String(localized: "Input [Token Usage]", comment: "Token usage: input tokens not from the cache")
        case .output: String(localized: "Output [Token Usage]", comment: "Token usage: output tokens")
        case .cacheWrite: String(localized: "Cache Write", comment: "Token usage: tokens written to the prompt cache")
        case .cacheRead: String(localized: "Cache Read", comment: "Token usage: tokens read from the prompt cache")
        }
    }
}

/// The four kinds of tokens and the replies, summed over some time (docs/server/machines.md "Token usage").
public nonisolated struct UsageTokens: Codable, Hashable, Sendable {
    public var input: Int64 = 0
    public var cacheWrite: Int64 = 0
    public var cacheRead: Int64 = 0
    public var output: Int64 = 0
    public var requests: Int = 0

    public enum CodingKeys: String, CodingKey {
        case input = "in", cacheWrite = "cache_write", cacheRead = "cache_read", output = "out", requests = "req"
    }

    public init(input: Int64 = 0, cacheWrite: Int64 = 0, cacheRead: Int64 = 0, output: Int64 = 0, requests: Int = 0) {
        self.input = input
        self.cacheWrite = cacheWrite
        self.cacheRead = cacheRead
        self.output = output
        self.requests = requests
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        input = try c.decodeIfPresent(Int64.self, forKey: .input) ?? 0
        cacheWrite = try c.decodeIfPresent(Int64.self, forKey: .cacheWrite) ?? 0
        cacheRead = try c.decodeIfPresent(Int64.self, forKey: .cacheRead) ?? 0
        output = try c.decodeIfPresent(Int64.self, forKey: .output) ?? 0
        requests = try c.decodeIfPresent(Int.self, forKey: .requests) ?? 0
    }

    /// Every token, cache reads included.
    public var total: Int64 { input + cacheWrite + cacheRead + output }

    /// Cache read / (input + cache write + cache read); nil without any input.
    public var hitRate: Double? {
        let prompt = input + cacheWrite + cacheRead
        return prompt > 0 ? Double(cacheRead) / Double(prompt) : nil
    }

    public func value(_ kind: UsageKind) -> Int64 {
        switch kind {
        case .input: input
        case .output: output
        case .cacheWrite: cacheWrite
        case .cacheRead: cacheRead
        }
    }

    public static func + (a: Self, b: Self) -> Self {
        Self(input: a.input + b.input, cacheWrite: a.cacheWrite + b.cacheWrite, cacheRead: a.cacheRead + b.cacheRead,
             output: a.output + b.output, requests: a.requests + b.requests)
    }
}

/// One step of a usage chart: the step's sums, or a break where the machine was offline.
public nonisolated struct UsagePoint: Decodable, Hashable, Sendable {
    public var t: Int64
    public var tokens: UsageTokens
    public var gap: Bool

    public enum CodingKeys: String, CodingKey { case t, gap }

    public init(t: Int64, tokens: UsageTokens, gap: Bool = false) {
        self.t = t
        self.tokens = tokens
        self.gap = gap
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        t = try c.decode(Int64.self, forKey: .t)
        gap = try c.decodeIfPresent(Bool.self, forKey: .gap) ?? false
        tokens = try UsageTokens(from: decoder)
    }

    public var date: Date { Date(timeIntervalSince1970: TimeInterval(t)) }
}

/// A model's or launcher's sums over the range; a launcher's empty name is the unknown one.
public nonisolated struct UsageShare: Decodable, Hashable, Sendable, Identifiable {
    public var name: String
    public var tokens: UsageTokens

    public enum CodingKeys: String, CodingKey { case name }

    public init(name: String, tokens: UsageTokens) {
        self.name = name
        self.tokens = tokens
    }

    public init(from decoder: Decoder) throws {
        name = try decoder.container(keyedBy: CodingKeys.self).decode(String.self, forKey: .name)
        tokens = try UsageTokens(from: decoder)
    }

    public var id: String { name }
}

/// GET /api/v1/machines/{id}/usage.
public nonisolated struct MachineUsageSeries: Decodable, Hashable, Sendable {
    public init(machineID: String, online: Bool, range: String, step: Int, points: [UsagePoint], total: UsageTokens, models: [UsageShare], launchers: [UsageShare], launchersKnown: Bool, receivedAt: Date? = nil) {
        self.receivedAt = receivedAt
        self.machineID = machineID
        self.online = online
        self.range = range
        self.step = step
        self.points = points
        self.total = total
        self.models = models
        self.launchers = launchers
        self.launchersKnown = launchersKnown
    }

    public var machineID: String
    public var online: Bool
    public var range: String
    public var step: Int
    public var points: [UsagePoint]
    public var total: UsageTokens
    public var models: [UsageShare]
    public var launchers: [UsageShare]
    /// False for an older node: its usage names no launcher, so there is nothing to filter by.
    public var launchersKnown: Bool

    public enum CodingKeys: String, CodingKey {
        case machineID = "machine_id", online, range, step, points, total, models, launchers
        case launchersKnown = "launchers_known"
    }

    /// Minutes in a step: a whole step's sums over this are per minute.
    public var minutesPerStep: Double { max(Double(step) / 60, 1) }

    /// Not on the wire: when the report arrived (APIClient.machineUsage), the end of the step under way (#9205).
    public var receivedAt: Date?

    /// The minutes the sums of the step starting at t cover: the whole step, except the step under way when the report
    /// came (receivedAt), whose sums cover only the part that had passed (at least a minute). Divided by the whole step,
    /// a 10-minute or hourly step read as a fraction of its rate until it ended (#9205). The web has the same rule
    /// (web/src/lib/usage-rate.ts stepMinutes).
    public func minutes(at t: Int64) -> Double {
        let ran = receivedAt.map { $0.timeIntervalSince1970 - Double(t) } ?? Double(step)
        return max(60, min(Double(step), ran)) / 60
    }
}

/// A machine's tokens on its card (GET /api/v1/metrics): today, all tokens per minute now, the last hour by minute.
public nonisolated struct UsageCard: Codable, Hashable, Sendable {
    public init(today: UsageTokens, perMinute: Int64, recent: [Int64]) {
        self.today = today
        self.perMinute = perMinute
        self.recent = recent
    }

    public var today: UsageTokens
    public var perMinute: Int64
    /// The last 60 minutes, oldest first; the last is the minute now.
    public var recent: [Int64]

    public enum CodingKeys: String, CodingKey { case today, perMinute = "per_minute", recent }
}

public nonisolated enum UsageRange: String, CaseIterable, Identifiable, Sendable {
    case hour = "1h", day = "24h", week = "7d"

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .hour: String(localized: "1 Hour")
        case .day: String(localized: "24 Hours")
        case .week: String(localized: "7 Days", comment: "Token usage chart range")
        }
    }

    public var seconds: TimeInterval {
        switch self {
        case .hour: 3600
        case .day: 24 * 3600
        case .week: 7 * 24 * 3600
        }
    }
}

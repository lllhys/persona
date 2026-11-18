import Foundation

// UI version 6's new fields' values (docs/extensions/extensions.md "UI version 6", schema `FieldKind`, #827): they stay
// strings on the wire. date `YYYY-MM-DD`, time `HH:MM` (24 hours), datetime `YYYY-MM-DDTHH:MM` in the page's time zone
// (`context.tz`, this device's), multichoice a JSON array of the chosen values in the choices' order.

nonisolated enum ExtensionFieldValues {
    /// The wire's form of a date, a time or a date and time; nil for another kind.
    static func format(_ kind: String) -> String? {
        switch kind {
        case "date": "yyyy-MM-dd"
        case "time": "HH:mm"
        case "datetime": "yyyy-MM-dd'T'HH:mm"
        default: nil
        }
    }

    private static func formatter(_ pattern: String, zone: TimeZone) -> DateFormatter {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.calendar = Calendar(identifier: .gregorian)
        f.timeZone = zone
        f.dateFormat = pattern
        f.isLenient = false
        return f
    }

    /// A value as a date (a time on `day`, today by default); nil when empty or not written as its kind says.
    static func date(_ s: String?, kind: String, zone: TimeZone = .current, day: Date = .now) -> Date? {
        guard let s, !s.isEmpty, let pattern = format(kind) else { return nil }
        guard let d = formatter(pattern, zone: zone).date(from: s) else { return nil }
        guard kind == "time" else { return d }
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = zone
        let hm = cal.dateComponents([.hour, .minute], from: d)
        return cal.date(bySettingHour: hm.hour ?? 0, minute: hm.minute ?? 0, second: 0, of: day)
    }

    /// A date as its kind's value.
    static func text(_ d: Date, kind: String, zone: TimeZone = .current) -> String {
        guard let pattern = format(kind) else { return "" }
        return formatter(pattern, zone: zone).string(from: d)
    }

    /// The dates a picker allows: from `earliest` to `latest` (either may be missing); a pair the wrong way round
    /// allows any.
    static func range(earliest: String?, latest: String?, kind: String, zone: TimeZone = .current, day: Date = .now) -> (from: Date?, to: Date?) {
        let from = date(earliest, kind: kind, zone: zone, day: day), to = date(latest, kind: kind, zone: zone, day: day)
        if let from, let to, from > to { return (nil, nil) }
        return (from, to)
    }

    /// `d` held to the range.
    static func clamp(_ d: Date, _ r: (from: Date?, to: Date?)) -> Date {
        var out = d
        if let f = r.from, out < f { out = f }
        if let t = r.to, out > t { out = t }
        return out
    }

    /// A multichoice value as the chosen values; anything but a JSON array of strings reads as none chosen.
    static func chosen(_ value: String?) -> [String] {
        guard let data = value?.data(using: .utf8), let a = try? JSONSerialization.jsonObject(with: data) as? [Any] else { return [] }
        return a.compactMap { $0 as? String }
    }

    /// The value a multichoice field sends: the chosen ones in the choices' order, as JSON.
    static func multichoice(_ chosen: Set<String>, order: [String]) -> String {
        let picked = order.filter(chosen.contains)
        guard let data = try? JSONSerialization.data(withJSONObject: picked, options: .withoutEscapingSlashes), let s = String(data: data, encoding: .utf8) else { return "[]" }
        return s
    }
}

import Foundation

/// The assistant's cards in words (#2024, docs/assistant/assistant-views.md): where a reply is read as plain text (the watch,
/// what Siri says), a ```persona-view block is its `fallback`, the words the assistant wrote for exactly this, and an
/// old ```persona-card block its title. A block with neither, one that does not parse, or one still streaming is
/// [Card]: never its JSON. Compiled into the watch (watchOS 10 APIs only).
public nonisolated enum AssistantCardText {
    public static let view = "persona-view"
    public static let card = "persona-card"
    public static var mark: String { String(localized: "[Card]", comment: "Stands in for a card in plain text") }

    /// `text` with every card block replaced by its words; anything else is left as it is.
    public static func replacingCards(_ text: String) -> String {
        guard text.range(of: view, options: .caseInsensitive) != nil || text.range(of: card, options: .caseInsensitive) != nil else { return text }
        var out = ""
        var inside: Fence?
        var body = ""
        for line in lines(text) {
            if let fence = inside {
                if fence.closes(line.text) {
                    out += fence.isCard ? words(fence.language, json: body) + line.ending : line.text + line.ending
                    inside = nil
                    body = ""
                } else if fence.isCard {
                    body += line.text + line.ending
                } else {
                    out += line.text + line.ending
                }
            } else if let fence = Fence(opening: line.text) {
                inside = fence
                if !fence.isCard { out += line.text + line.ending }
            } else {
                out += line.text + line.ending
            }
        }
        if inside?.isCard == true { out += mark }
        return out
    }

    /// Keep the original line endings outside cards. Swift treats CRLF as one newline character, just like LF or CR.
    private static func lines(_ text: String) -> [(text: String, ending: String)] {
        var out: [(text: String, ending: String)] = []
        var start = text.startIndex
        for i in text.indices where text[i].isNewline {
            let next = text.index(after: i)
            out.append((String(text[start..<i]), String(text[i..<next])))
            start = next
        }
        if start < text.endIndex { out.append((String(text[start...]), "")) }
        return out
    }

    /// The words for one block's content.
    public static func words(_ language: String, json: String) -> String {
        guard let data = json.data(using: .utf8),
              let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else { return mark }
        if language == view, let f = nonEmpty(object["fallback"]) { return f }
        if let t = nonEmpty(object["title"]) { return "〔\(t)〕" }
        return mark
    }

    private static func nonEmpty(_ v: Any?) -> String? {
        guard let s = (v as? String)?.trimmingCharacters(in: .whitespacesAndNewlines), !s.isEmpty else { return nil }
        return s
    }

    /// A fenced code block's opening line (CommonMark: up to three spaces, three or more ` or ~, an info string whose
    /// first word is the language).
    private struct Fence {
        var char: Character
        var count: Int
        var language: String
        var isCard: Bool { language == AssistantCardText.view || language == AssistantCardText.card }

        init?(opening line: String) {
            let indent = line.prefix { $0 == " " }
            guard indent.count <= 3 else { return nil }
            let rest = line.dropFirst(indent.count)
            guard let c = rest.first, c == "`" || c == "~" else { return nil }
            let run = rest.prefix { $0 == c }
            guard run.count >= 3 else { return nil }
            let info = rest.dropFirst(run.count).trimmingCharacters(in: .whitespaces)
            if c == "`", info.contains("`") { return nil }
            char = c
            count = run.count
            language = String(info.split(whereSeparator: { $0 == " " || $0 == "\t" }).first ?? "").lowercased()
        }

        /// A closing fence: the same character, at least as long, nothing after it.
        func closes(_ line: String) -> Bool {
            let indent = line.prefix { $0 == " " }
            guard indent.count <= 3 else { return false }
            let rest = line.dropFirst(indent.count).trimmingCharacters(in: .whitespaces)
            return rest.count >= count && rest.allSatisfy { $0 == char }
        }
    }
}

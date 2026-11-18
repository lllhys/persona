import CoreFoundation
import Foundation
import PersonaWire

/// Only a canonical trailing card-source marker is presentation metadata; malformed tags remain visible user text.
nonisolated enum AssistantActionSource {
    struct Parsed: Equatable, Sendable {
        var text: String
        var view: String
        var action: String
        var values: [String: String]
        var html: Bool
    }
    private static let pattern = try! NSRegularExpression(pattern: #"(?:\r\n|\n|\r)<persona-action view="([^"<>\r\n]+)" action="([^"<>\r\n]+)">(\{[^\r\n]*\})</persona-action>[ \t\r\n]*\z"#)

    static func parse(_ text: String) -> Parsed? {
        let whole = NSRange(text.startIndex..., in: text)
        guard let match = pattern.firstMatch(in: text, range: whole), let range = Range(match.range, in: text),
              let viewRange = Range(match.range(at: 1), in: text), let actionRange = Range(match.range(at: 2), in: text),
              let payloadRange = Range(match.range(at: 3), in: text),
              let view = attribute(String(text[viewRange])), let action = attribute(String(text[actionRange])) else { return nil }
        let prefix = String(text[..<range.lowerBound]), payload = String(text[payloadRange])
        guard !AssistantViewBlocks.isInsideFence(prefix), !payload.contains("<"),
              let data = payload.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let values = object["values"] as? [String: String] else { return nil }
        let html: Bool
        if Set(object.keys) == ["values"] { html = false }
        else if Set(object.keys) == ["values", "html"], let value = object["html"] as? NSNumber,
                CFGetTypeID(value) == CFBooleanGetTypeID(), value.boolValue, values.isEmpty { html = true }
        else { return nil }
        return Parsed(text: prefix, view: view, action: action, values: values, html: html)
    }
    static func stripping(_ text: String) -> String { parse(text)?.text ?? text }

    private static func attribute(_ text: String) -> String? {
        let entities = ["&amp;": "&", "&lt;": "<", "&gt;": ">", "&#34;": "\"", "&#39;": "'", "&quot;": "\"", "&apos;": "'"]
        var result = "", start = text.startIndex
        while let amp = text[start...].firstIndex(of: "&") {
            result += text[start..<amp]
            guard let end = text[amp...].firstIndex(of: ";") else { return nil }
            let after = text.index(after: end)
            guard let value = entities[String(text[amp..<after])] else { return nil }
            result += value; start = after
        }
        result += text[start...]
        guard !result.isEmpty, !result.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains) else { return nil }
        return result
    }
}

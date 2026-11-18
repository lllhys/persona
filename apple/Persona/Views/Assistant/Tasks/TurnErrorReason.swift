import PersonaKit
import PersonaWire

/// What a turn's API error says, as a person would: the parse (three regexes) is done once per distinct text.
nonisolated enum TurnErrorReason {
    private static let memo = TextMemo<String?>(limit: 256)

    /// The reason in an "API Error: ..." text; nil when the text is not one.
    static func of(_ text: String?) -> String? {
        guard let text else { return nil }
        return memo(text) { APIErrorInfo.parse($0)?.reason }
    }
}

import Foundation

/// Persona's echo in a meeting it speaks in (#5905, docs/assistant/meeting-minutes.md「Persona's echo」): the recording's
/// microphone hears Persona's answers too, and the transcript cannot tell voices apart. A line is Persona's echo only when
/// it overlaps a stretch Persona spoke in (`RecordingSpan`, widened by `margin`) and its words are what Persona said there
/// (`containment` of its character bigrams), so whatever someone else says meanwhile stays the meeting's. Echo lines stay
/// on the recording, marked; they are left out of what is built from the meeting. The hub's copy of the rule is
/// `server/internal/kb/echo.go`; both run the cases in spec/fixtures/meeting-echo.
nonisolated enum MeetingEcho {
    /// Seconds a stretch is widened by on both sides: the recognizer's times are a little off. A starting value, to
    /// calibrate against labelled recordings.
    static let margin: Double = 0.5
    /// The share of a line's distinct character bigrams that must be among those of what Persona said. A starting value,
    /// to calibrate against labelled recordings.
    static let containment: Double = 0.6

    /// The words of `line` are what `spoken` said: at least `containment` of the line's distinct character bigrams, after
    /// `normalized`, are among the spoken text's. A line of one character is one only when the spoken text holds it; a
    /// line with nothing left is never one.
    static func matches(_ line: String, spoken: String) -> Bool {
        let l = normalized(line)
        guard let first = l.first else { return false }
        let s = normalized(spoken)
        guard l.count >= 2 else { return s.contains(first) }
        let said = bigrams(s)
        let mine = bigrams(l)
        let found = mine.filter(said.contains).count
        return Double(found) / Double(mine.count) >= containment
    }

    /// Text as the rule compares it, as Unicode scalars (the hub's runes): NFKC, each scalar lowercased, letters (L*) and
    /// decimal digits (Nd) only; punctuation, spaces, symbols and marks go.
    static func normalized(_ text: String) -> [Unicode.Scalar] {
        var out: [Unicode.Scalar] = []
        for scalar in text.precomposedStringWithCompatibilityMapping.unicodeScalars {
            for lower in scalar.properties.lowercaseMapping.unicodeScalars where kept(lower) { out.append(lower) }
        }
        return out
    }

    private static func kept(_ scalar: Unicode.Scalar) -> Bool {
        switch scalar.properties.generalCategory {
        case .uppercaseLetter, .lowercaseLetter, .titlecaseLetter, .modifierLetter, .otherLetter, .decimalNumber: true
        default: false
        }
    }

    private static func bigrams(_ scalars: [Unicode.Scalar]) -> Set<UInt64> {
        guard scalars.count >= 2 else { return [] }
        var out = Set<UInt64>()
        for i in 1..<scalars.count { out.insert(UInt64(scalars[i - 1].value) << 32 | UInt64(scalars[i].value)) }
        return out
    }
}

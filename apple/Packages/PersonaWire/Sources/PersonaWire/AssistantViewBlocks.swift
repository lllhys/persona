import Foundation

/// Standalone assistant view fences, matching sdk/go/wire/assistantblocks.go.
/// Extraction keeps invalid, oversized and excess blocks; validation belongs to the caller.
public nonisolated enum AssistantViewBlocks {
    public static let maxBlocks = 3
    public static let maxContentBytes = 64 << 10

    public nonisolated struct Block: Hashable, Sendable {
        public let ordinal: Int
        public let content: String
        public let closed: Bool
        /// The entire original fence, including the closing line's newline, or through EOF if unclosed.
        public let range: Range<String.Index>
    }

    public static func parse(_ text: String) -> [Block] {
        let scalars = text.unicodeScalars
        var blocks: [Block] = []
        var opening: Fence?
        var start = text.startIndex
        var contentStart = start
        var position = start

        while position < text.endIndex {
            var end = text.endIndex
            if let newline = scalars[position...].firstIndex(where: { $0 == "\r" || $0 == "\n" }) {
                end = scalars.index(after: newline)
                if scalars[newline] == "\r", end < text.endIndex, scalars[end] == "\n" {
                    end = scalars.index(after: end)
                }
            }
            let fence = markdownFence(text[position..<end])
            if let current = opening {
                if let fence, fence.character == current.character,
                   fence.length >= current.length, !fence.hasInfo {
                    if current.isView {
                        blocks.append(Block(ordinal: blocks.count, content: String(text[contentStart..<position]),
                                            closed: true, range: start..<end))
                    }
                    opening = nil
                }
            } else if let fence {
                // Track every ordinary fence too: a persona-view inside it is only an example.
                opening = fence
                start = position
                contentStart = end
            }
            position = end
        }
        if opening?.isView == true {
            blocks.append(Block(ordinal: blocks.count, content: String(text[contentStart...]),
                                closed: false, range: start..<text.endIndex))
        }
        return blocks
    }

    /// Used by trailing source-tag recognition: a marker inside an unfinished example is ordinary user text.
    public static func isInsideFence(_ text: String) -> Bool {
        let scalars = text.unicodeScalars
        var opening: Fence?, position = text.startIndex
        while position < text.endIndex {
            var end = text.endIndex
            if let newline = scalars[position...].firstIndex(where: { $0 == "\r" || $0 == "\n" }) {
                end = scalars.index(after: newline)
                if scalars[newline] == "\r", end < text.endIndex, scalars[end] == "\n" { end = scalars.index(after: end) }
            }
            if let fence = markdownFence(text[position..<end]) {
                if let current = opening {
                    if fence.character == current.character, fence.length >= current.length, !fence.hasInfo { opening = nil }
                } else { opening = fence }
            }
            position = end
        }
        return opening != nil
    }

    private nonisolated struct Fence {
        let character: Unicode.Scalar
        let length: Int
        let hasInfo: Bool
        let isView: Bool
    }

    private static func markdownFence(_ line: Substring) -> Fence? {
        let scalars = line.unicodeScalars
        var end = scalars.endIndex
        if end > scalars.startIndex, scalars[scalars.index(before: end)] == "\n" {
            end = scalars.index(before: end)
        }
        if end > scalars.startIndex, scalars[scalars.index(before: end)] == "\r" {
            end = scalars.index(before: end)
        }
        var position = scalars.startIndex
        var spaces = 0
        while position < end, scalars[position] == " " {
            spaces += 1
            if spaces > 3 { return nil }
            position = scalars.index(after: position)
        }
        guard position < end else { return nil }
        let character = scalars[position]
        guard character == "`" || character == "~" else { return nil }
        var length = 0
        while position < end, scalars[position] == character {
            length += 1
            position = scalars.index(after: position)
        }
        guard length >= 3 else { return nil }
        // Go rejects a backtick anywhere in a backtick fence's info, not just in its first word.
        if character == "`", scalars[position..<end].contains("`") { return nil }
        while position < end, isGoWhitespace(scalars[position]) {
            position = scalars.index(after: position)
        }
        let infoStart = position
        while position < end, !isGoWhitespace(scalars[position]) {
            position = scalars.index(after: position)
        }
        return Fence(character: character, length: length, hasInfo: infoStart < end,
                     isView: isViewLanguage(line[infoStart..<position]))
    }

    /// strings.TrimSpace and strings.Fields use Go's Unicode White_Space set, not Foundation's broader sets.
    private static func isGoWhitespace(_ scalar: Unicode.Scalar) -> Bool {
        switch scalar.value {
        case 0x09...0x0D, 0x20, 0x85, 0xA0, 0x1680, 0x2000...0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
            true
        default:
            false
        }
    }

    private static func isViewLanguage(_ language: Substring) -> Bool {
        let expected = "persona-view".unicodeScalars
        let actual = language.unicodeScalars
        guard actual.count == expected.count else { return false }
        for (scalar, wanted) in zip(actual, expected) {
            var value = scalar.value
            if (0x41...0x5A).contains(value) { value += 0x20 }
            // Go's simple Unicode lowercase maps capital dotted I to i, without a combining dot.
            if value == 0x130 { value = 0x69 }
            guard value == wanted.value else { return false }
        }
        return true
    }
}

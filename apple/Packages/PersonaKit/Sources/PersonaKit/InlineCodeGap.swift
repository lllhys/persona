import Foundation

/// A little room where inline code meets a CJK letter (#9477). Inline code has no background under it any more, so
/// code written straight against Chinese words, with no space, ran into them. Before a reply is parsed a thin space
/// (U+2009, about 0.12 em, the web's margin) goes between a code span and a Han, kana, Hangul or bopomofo letter right
/// beside it. Nothing goes beside a space or CJK punctuation, which have room of their own, or beside anything else.
/// Code blocks (fenced and indented), and code spans in a link or an autolink, are left as written, as
/// `CodeCopyMarkdown` leaves them. What is copied stays the source; this is only what is drawn.
public nonisolated enum InlineCodeGap {
    /// What goes in the room: a thin space, so a line may still break there.
    public static let thin: Character = "\u{2009}"

    /// `text` with `gap` on each side of a code span that meets a CJK letter. Select Text's overlay passes a mark of its
    /// own, to find the room again and copy nothing for it (`ReplyText`).
    public static func apply(_ text: String, gap: Character = thin) -> String {
        guard text.contains("`"), text.unicodeScalars.contains(where: { $0.value >= 0x1100 }) else { return text }
        var out: [String] = []
        var fence: MarkdownFence?
        var changed = false
        for line in text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            let t = line.trimmingCharacters(in: .whitespaces)
            if let f = fence {
                if f.isClosed(by: t) { fence = nil }
                out.append(line)
            } else if let f = MarkdownFence.opening(t) {
                fence = f
                out.append(line)
            } else if line.hasPrefix("    ") || line.hasPrefix("\t") { // indented code (as `CodeCopyMarkdown`)
                out.append(line)
            } else {
                let spaced = spans(line, gap: gap)
                changed = changed || spaced != line
                out.append(spaced)
            }
        }
        return changed ? out.joined(separator: "\n") : text
    }

    /// One line outside a code block, with the room added.
    private static func spans(_ line: String, gap: Character) -> String {
        let ns = line as NSString
        let code = PathMarkdown.codeSpans(Array(line.utf16))
        guard !code.isEmpty else { return line }
        let links = PathMarkdown.protectedRanges(in: line)
            .filter { r in !code.contains { NSLocationInRange(r.location, $0.whole) } }
        var out = ""
        var at = 0
        for c in code where !links.contains(where: { NSIntersectionRange($0, c.whole).length > 0 }) {
            let start = c.whole.location, end = NSMaxRange(c.whole)
            let before = start > 0 ? ns.substring(with: ns.rangeOfComposedCharacterSequence(at: start - 1)).first : nil
            let after = end < ns.length ? ns.substring(with: ns.rangeOfComposedCharacterSequence(at: end)).first : nil
            let room = (CJKEmphasis.isCJKLetter(before), CJKEmphasis.isCJKLetter(after))
            guard room.0 || room.1 else { continue }
            out += ns.substring(with: NSRange(location: at, length: start - at))
            if room.0 { out.append(gap) }
            out += ns.substring(with: c.whole)
            if room.1 { out.append(gap) }
            at = end
        }
        return at == 0 ? line : out + ns.substring(from: at)
    }
}

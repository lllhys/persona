import Foundation

/// The lines of a reply as a chat shows them (#2088). CommonMark lets only a list that starts at 1 interrupt a
/// paragraph, so a step list that goes on at "5." right under a line of text joined that paragraph, and the steps read
/// as one run-on block on the phone while the terminal showed them line by line. Before a reply is parsed, a blank line
/// goes in where a line of a paragraph is directly followed by an ordered list item (any number) or a heading. Nothing
/// changes inside a fenced code block, in indented code, or inside a list (between its items, or under one): a blank
/// line there would make the list loose or end it. A heading could interrupt the paragraph anyway; the blank line only
/// makes the two read as the separate blocks they are in every renderer.
public nonisolated enum ReplyBreaks {
    public static func apply(_ text: String) -> String {
        guard text.contains("\n") else { return text }
        var out: [Substring] = []
        var fence: MarkdownFence?
        /// Since a list item, up to a blank line: the lines in between are that item's.
        var inList = false
        /// The previous line is a line of text (a paragraph's, a table row, a quote), not a list item, a heading or code.
        var afterText = false
        var changed = false
        for line in text.split(separator: "\n", omittingEmptySubsequences: false) {
            let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
            let indent = Self.indent(line)
            if let open = fence {
                if open.isClosed(by: trimmed) { fence = nil }
                out.append(line)
                afterText = false
                continue
            }
            if trimmed.isEmpty {
                out.append(line)
                inList = false
                afterText = false
                continue
            }
            if indent < 4, let open = MarkdownFence.opening(trimmed) {
                fence = open
                out.append(line)
                afterText = false
                continue
            }
            let top = indent < 4
            let ordered = top && Self.isOrdered(trimmed)
            let bullet = top && Self.isBullet(trimmed)
            let heading = top && Self.isHeading(trimmed)
            if (ordered || heading) && afterText && !inList {
                out.append("")
                changed = true
            }
            out.append(line)
            if ordered || bullet { inList = true }
            if heading { inList = false }
            afterText = !(ordered || bullet || heading || !top)
        }
        return changed ? out.joined(separator: "\n") : text
    }

    /// Leading spaces, a tab counting as four.
    private static func indent(_ line: Substring) -> Int {
        var n = 0
        for c in line {
            if c == " " { n += 1 } else if c == "\t" { n += 4 } else { break }
        }
        return n
    }

    /// "5. step", "12) step", "3." alone.
    static func isOrdered(_ line: String) -> Bool {
        let digits = line.prefix(while: \.isASCIIDigit)
        guard (1...9).contains(digits.count) else { return false }
        let rest = line.dropFirst(digits.count)
        guard let mark = rest.first, mark == "." || mark == ")" else { return false }
        let after = rest.dropFirst()
        return after.isEmpty || after.first == " " || after.first == "\t"
    }

    /// "- item", "* item", "+ item" (not a rule like "***" or "- - -" with nothing else, which a list never is here:
    /// either way it is not a line of a paragraph).
    static func isBullet(_ line: String) -> Bool {
        guard let c = line.first, c == "-" || c == "*" || c == "+" else { return false }
        let after = line.dropFirst()
        return after.isEmpty || after.first == " " || after.first == "\t"
    }

    /// "# Title" … "###### Title", or the hashes alone.
    static func isHeading(_ line: String) -> Bool {
        let hashes = line.prefix(while: { $0 == "#" }).count
        guard (1...6).contains(hashes) else { return false }
        let after = line.dropFirst(hashes)
        return after.isEmpty || after.first == " " || after.first == "\t"
    }
}

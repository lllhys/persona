import Foundation

/// A tap on inline code copies it (#2984): the chat makes each code span a link of this scheme, and its `openURL`
/// answers one by copying the code.
public nonisolated enum CodeCopyURL {
    public static let scheme = "persona-copy"

    public static func make(_ code: String) -> URL? {
        guard let p = code.addingPercentEncoding(withAllowedCharacters: .alphanumerics) else { return nil }
        return URL(string: scheme + ":" + p)
    }

    public static func parse(_ url: URL) -> String? {
        guard url.scheme == scheme else { return nil }
        return String(url.absoluteString.dropFirst(scheme.count + 1)).removingPercentEncoding
    }
}

/// An answer's Markdown with its code spans made links that copy them (`CodeCopyURL`), as written, backticks and
/// all: the span keeps its look. Code blocks (fenced or indented), formulas and spans already in a link (a path the
/// chat linked, `PathMarkdown.linked`) are left alone.
public nonisolated enum CodeCopyMarkdown {
    public static func linked(_ source: String) -> String {
        guard source.contains("`") else { return source }
        var out: [String] = []
        var fence: MarkdownFence?
        for line in source.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            let t = line.trimmingCharacters(in: .whitespaces)
            if let f = fence {
                if f.isClosed(by: t) { fence = nil }
                out.append(line)
            } else if let f = MarkdownFence.opening(t) {
                fence = f
                out.append(line)
            } else if line.hasPrefix("    ") || line.hasPrefix("\t") { // indented code (as `PathMarkdown`)
                out.append(line)
            } else {
                out.append(spans(line))
            }
        }
        return out.joined(separator: "\n")
    }

    /// One line (outside a code block) with its code spans linked.
    private static func spans(_ line: String) -> String {
        let ns = line as NSString
        let code = PathMarkdown.codeSpans(Array(line.utf16))
        guard !code.isEmpty else { return line }
        let links = PathMarkdown.protectedRanges(in: line)
            .filter { r in !code.contains { NSLocationInRange(r.location, $0.whole) } }
        var out = ""
        var at = 0
        for c in code where !links.contains(where: { NSIntersectionRange($0, c.whole).length > 0 }) {
            var inner = ns.substring(with: c.inner)
            // CommonMark takes one space off each end of a span that has words between them.
            if inner.count > 2, inner.hasPrefix(" "), inner.hasSuffix(" "), !inner.allSatisfy({ $0 == " " }) {
                inner = String(inner.dropFirst().dropLast())
            }
            guard !inner.trimmingCharacters(in: .whitespaces).isEmpty, let url = CodeCopyURL.make(inner) else { continue }
            out += ns.substring(with: NSRange(location: at, length: c.whole.location - at))
            out += "[" + ns.substring(with: c.whole) + "](" + url.absoluteString + ")"
            at = NSMaxRange(c.whole)
        }
        return at == 0 ? line : out + ns.substring(from: at)
    }
}

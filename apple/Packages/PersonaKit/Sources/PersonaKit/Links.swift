import Foundation

/// Web links in text, with one rule for where a link ends, used everywhere links are found (replies' Markdown, link
/// preloading, the terminal; `PathRefs` ends paths at the same punctuation, but takes names in any script):
///
/// - a link starts at `http://` / `https://` (or `www.`) not glued to a word before it;
/// - it ends at whitespace, at any non-ASCII character (CJK text, full-width punctuation 「），。、」, corner brackets…) and at `<>"`'|\{}^`;
/// - trailing `.,;:!?'"*_~` are not part of it, nor a `)` / `]` without its opening one (`/wiki/Foo_(bar)` keeps its pair).
///
/// GFM's autolink (MarkdownUI uses cmark-gfm) only stops at whitespace: in Chinese prose a link took the rest of the
/// paragraph, and NSDataDetector disagreed with both (2026-10-30).
public nonisolated enum Links {
    /// Where the links are, in UTF-16 offsets (NSRange), with their URLs.
    public static func scan(_ text: String) -> [(range: NSRange, url: URL)] {
        let u = Array(text.utf16)
        var out: [(NSRange, URL)] = []
        var i = 0
        while i < u.count {
            guard let (start, bare) = linkStart(u, i) else { i += 1; continue }
            var end = start
            while end < u.count, allowed(u[end]) { end += 1 }
            end = trimmed(u, start, end)
            let s = String(utf16CodeUnits: Array(u[start..<end]), count: end - start)
            let minLength = bare ? 7 : (s.lowercased().hasPrefix("https://") ? 9 : 8) // a host after the scheme
            if end - start >= minLength, let url = URL(string: bare ? "http://" + s : s), url.host() != nil {
                out.append((NSRange(location: start, length: end - start), url))
                i = end
            } else {
                i = start + 1
            }
        }
        return out
    }

    public static func find(in text: String) -> [URL] { scan(text).map(\.url) }

    /// Markdown with its bare links made explicit (`<url>`, `[www.x](http://www.x)`), so cmark-gfm's autolink never
    /// gets to extend them. Code (fenced blocks, indented lines, `spans`) and links already written are left alone.
    public static func markdown(_ md: String) -> String {
        guard md.contains("://") || md.contains("www.") else { return md }
        var out: [String] = []
        var fence: MarkdownFence?
        for line in md.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            let t = line.trimmingCharacters(in: .whitespaces)
            if let f = fence {
                if f.isClosed(by: t) { fence = nil }
                out.append(line)
                continue
            }
            if let f = MarkdownFence.opening(t) {
                fence = f
                out.append(line)
                continue
            }
            if line.hasPrefix("    ") || line.hasPrefix("\t") { // indented code (or a deep list: left as before)
                out.append(line)
                continue
            }
            out.append(wrapLine(line))
        }
        return out.joined(separator: "\n")
    }

    private static func wrapLine(_ line: String) -> String {
        let links = scan(line)
        guard !links.isEmpty else { return line }
        let u = Array(line.utf16)
        let code = codeSpans(u)
        var result: [UInt16] = []
        var pos = 0
        for (r, url) in links {
            let before = r.location >= 2 ? Array(u[(r.location - 2)..<r.location]) : Array(u[0..<r.location])
            let prev = before.last
            let inLink = before == Array("](".utf16) || prev == 0x3C /* < */ || prev == 0x5B /* [ */ || prev == 0x22 || prev == 0x27
            if inLink || code.contains(where: { NSLocationInRange(r.location, $0) }) { continue }
            result += u[pos..<r.location]
            let text = Array(u[r.location..<(r.location + r.length)])
            if text.first == 0x77 || text.first == 0x57 { // www.…
                result += Array("[".utf16) + text + Array("](\(url.absoluteString))".utf16)
            } else {
                result += [0x3C] + text + [0x3E]
            }
            pos = r.location + r.length
        }
        result += u[pos...]
        return String(utf16CodeUnits: result, count: result.count)
    }

    /// Inline code spans of a line: a run of backticks to the next run of the same length.
    private static func codeSpans(_ u: [UInt16]) -> [NSRange] {
        var spans: [NSRange] = []
        var i = 0
        while i < u.count {
            guard u[i] == 0x60 else { i += 1; continue }
            var n = 0
            while i + n < u.count, u[i + n] == 0x60 { n += 1 }
            var j = i + n
            var closed = false
            while j < u.count {
                if u[j] == 0x60 {
                    var m = 0
                    while j + m < u.count, u[j + m] == 0x60 { m += 1 }
                    if m == n { spans.append(NSRange(location: i, length: j + m - i)); i = j + m; closed = true; break }
                    j += m
                } else {
                    j += 1
                }
            }
            if !closed { i += n }
        }
        return spans
    }

    /// A link starting at i: its start and whether it is a bare `www.` one.
    private static func linkStart(_ u: [UInt16], _ i: Int) -> (Int, Bool)? {
        if i > 0, isWordChar(u[i - 1]) { return nil } // "xhttp://" is not a link start
        func has(_ s: String) -> Bool {
            let p = Array(s.utf16)
            guard i + p.count <= u.count else { return false }
            for k in 0..<p.count where lower(u[i + k]) != p[k] { return false }
            return true
        }
        if has("https://") || has("http://") { return (i, false) }
        if has("www.") { return (i, true) }
        return nil
    }

    /// Printable ASCII that may be inside a link.
    private static func allowed(_ c: UInt16) -> Bool {
        guard c > 0x20, c < 0x7F else { return false }
        return !"<>\"`'|\\{}^".utf16.contains(c)
    }

    private static let trailing = Set(".,;:!?*_~".utf16)

    private static func trimmed(_ u: [UInt16], _ start: Int, _ endIn: Int) -> Int {
        var end = endIn
        while end > start {
            let c = u[end - 1]
            if trailing.contains(c) { end -= 1; continue }
            if c == 0x29 || c == 0x5D { // ) ]
                let open: UInt16 = c == 0x29 ? 0x28 : 0x5B
                let opens = u[start..<end].filter { $0 == open }.count, closes = u[start..<end].filter { $0 == c }.count
                if closes > opens { end -= 1; continue }
            }
            break
        }
        return end
    }

    private static func isWordChar(_ c: UInt16) -> Bool {
        (c >= 0x30 && c <= 0x39) || (c >= 0x41 && c <= 0x5A) || (c >= 0x61 && c <= 0x7A) || c == 0x5F
    }

    private static func lower(_ c: UInt16) -> UInt16 { c >= 0x41 && c <= 0x5A ? c + 32 : c }
}

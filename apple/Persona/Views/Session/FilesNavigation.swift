import Foundation

// The file page's navigation and finding (#5827, docs/apple/ios.md "File page"): the page goes anywhere on the computer
// (a session's page starts at its folder and can go above it), back and forward like a browser, up a level and home; the
// path bar names every folder on the way from the home (`~`) or from `/`. Finding matches file and folder names under
// the folder shown, from the node's list of its files (`fs/find`), by the same rule as the web's Files panel.

/// Where the file page is and where it has been. Paths as asked (`~`, absolute); the listing's own path is the shown one.
nonisolated struct FilesNavigation: Equatable, Sendable {
    private(set) var current: String
    private(set) var back: [String] = []
    private(set) var forward: [String] = []
    /// Folders kept for Back (older ones are dropped).
    static let historyLimit = 50

    init(_ start: String) { current = start }

    var canGoBack: Bool { !back.isEmpty }
    var canGoForward: Bool { !forward.isEmpty }

    /// Opens path: the folder before goes on the Back list, Forward is cleared.
    mutating func go(_ path: String) {
        guard !path.isEmpty, path != current else { return }
        back.append(current)
        if back.count > Self.historyLimit { back.removeFirst(back.count - Self.historyLimit) }
        forward = []
        current = path
    }

    mutating func goBack() {
        guard let p = back.popLast() else { return }
        forward.append(current)
        current = p
    }

    mutating func goForward() {
        guard let p = forward.popLast() else { return }
        back.append(current)
        current = p
    }

    /// The folder above shown (a listing's absolute path); nil at `/`.
    static func parent(of shown: String) -> String? {
        guard !shown.isEmpty, shown != "/" else { return nil }
        let p = (shown as NSString).deletingLastPathComponent
        return p.isEmpty ? "/" : p
    }

    /// One folder in the path bar: its name and the path it opens.
    struct Segment: Hashable, Sendable {
        var title: String
        var path: String
        /// The home, drawn as a house.
        var home = false
    }

    /// The path bar for shown: from the home when it is inside it, else from `/`.
    static func segments(_ shown: String, home: String?) -> [Segment] {
        if let home, !home.isEmpty, home != "/", shown == home || shown.hasPrefix(home + "/") {
            var out = [Segment(title: "~", path: home, home: true)]
            var at = home
            for c in shown.dropFirst(home.count).split(separator: "/") {
                at += "/" + c
                out.append(Segment(title: String(c), path: at))
            }
            return out
        }
        var out = [Segment(title: "/", path: "/")]
        var at = ""
        for c in shown.split(separator: "/") {
            at += "/" + c
            out.append(Segment(title: String(c), path: at))
        }
        return out
    }

    /// The path an absolute path is under the home with `~` (for what the page says), else as it is.
    static func tilde(_ path: String, home: String?) -> String {
        guard let home, !home.isEmpty, home != "/" else { return path }
        if path == home { return "~" }
        return path.hasPrefix(home + "/") ? "~" + path.dropFirst(home.count) : path
    }

    /// Whether the computer takes changes in shown (docs/protocol/protocol.md "Changing files": the home, except
    /// `~/.persona` and `~/Library`). Only to grey out what would be refused; the computer decides. Unknown home: yes.
    static func writable(_ shown: String, home: String?) -> Bool {
        guard let home, !home.isEmpty else { return true }
        guard shown == home || shown.hasPrefix(home + "/") else { return false }
        let rel = shown.dropFirst(home.count)
        for blocked in ["/.persona", "/Library"] where rel == blocked || rel.hasPrefix(blocked + "/") { return false }
        return true
    }
}

/// Finding by name under a folder: every typed word in the name (any case), best first: the whole name, then a name
/// that starts with it, then a word in it that does, then anywhere; then folders before files, then the shorter path.
/// The web's Files panel ranks by the same rule (`web/src/lib/files-find-name.ts`).
nonisolated enum FileFind {
    struct Hit: Hashable, Sendable {
        /// Relative to the folder searched, with `/`; a folder ends without one.
        var path: String
        var dir: Bool
        var name: String { (path as NSString).lastPathComponent }
        /// The folder it is in, relative ("" at the top).
        var folder: String { (path as NSString).deletingLastPathComponent }
    }

    static let maxHits = 200

    /// The folders the files are in, as hits of their own, with the files.
    static func entries(_ files: [String]) -> [Hit] {
        var dirs = Set<String>()
        for f in files {
            var d = (f as NSString).deletingLastPathComponent
            while !d.isEmpty, dirs.insert(d).inserted { d = (d as NSString).deletingLastPathComponent }
        }
        return dirs.sorted().map { Hit(path: $0, dir: true) } + files.map { Hit(path: $0, dir: false) }
    }

    static func search(_ entries: [Hit], query: String, limit: Int = maxHits) -> [Hit] {
        let words = query.split(whereSeparator: \.isWhitespace).map { $0.lowercased() }
        guard !words.isEmpty else { return [] }
        var scored: [(Int, Hit)] = []
        for e in entries {
            let name = e.name.lowercased()
            var score = 0
            var fits = true
            for w in words {
                guard let r = name.range(of: w) else { fits = false; break }
                if name == w { score += 4 } else if r.lowerBound == name.startIndex { score += 3 } else if Self.wordStart(name, r.lowerBound) { score += 2 } else { score += 1 }
            }
            if fits { scored.append((score, e)) }
        }
        scored.sort { a, b in
            if a.0 != b.0 { return a.0 > b.0 }
            if a.1.dir != b.1.dir { return a.1.dir }
            if a.1.path.count != b.1.path.count { return a.1.path.count < b.1.path.count }
            return a.1.path < b.1.path
        }
        return scored.prefix(limit).map(\.1)
    }

    private static func wordStart(_ s: String, _ i: String.Index) -> Bool {
        guard i > s.startIndex else { return true }
        let prev = s[s.index(before: i)]
        return !(prev.isLetter || prev.isNumber)
    }
}

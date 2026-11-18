import Foundation

// The pure parts of the code-hosting components (#2961, docs/extensions/extensions.md「UI version 4: code hosting」): a diff's lines numbered from
// its hunks, the two sides paired for the side-by-side look, and a file tree built from full paths. No views here;
// the renderer (ExtensionCodeViews.swift) draws what these work out, and the tests check them on their own.

/// One line of a diff hunk, numbered on each side it is on.
nonisolated struct CodeDiffLine: Hashable, Sendable {
    enum Kind: Hashable, Sendable {
        /// On both sides, unchanged (" ").
        case context
        /// Only on the new side ("+").
        case add
        /// Only on the old side ("-").
        case del
        /// "\ No newline at end of file": on neither side, no number.
        case note
    }

    var kind: Kind
    /// The line without its prefix.
    var text: String
    var old: Int?
    var new: Int?
    /// The words that changed (UTF-16 offsets into `text`), when paired with the line it was changed from or to
    /// (UI version 5's code review, `CodeReview.markWords`); empty otherwise.
    var changed: [Range<Int>] = []

    /// The side a comment on this line is anchored to, and its line there: a removed line on the old side, any
    /// other on the new one (as GitHub and Codebase anchor them).
    var anchor: (side: String, line: Int)? {
        switch kind {
        case .del: old.map { ("old", $0) }
        case .add, .context: new.map { ("new", $0) }
        case .note: nil
        }
    }
}

nonisolated enum CodeDiffModel {
    /// A hunk's lines (each with its prefix) numbered from where the hunk starts on each side. A line without a
    /// known prefix reads as context (a hunk that lost its leading space), an empty one too.
    static func lines(oldStart: Int, newStart: Int, raw: [String]) -> [CodeDiffLine] {
        var old = max(oldStart, 0), new = max(newStart, 0)
        var out: [CodeDiffLine] = []
        out.reserveCapacity(raw.count)
        for r in raw {
            let first = r.first
            let text = first.map { "+- \\".contains($0) } == true ? String(r.dropFirst()) : r
            switch first {
            case "+":
                out.append(CodeDiffLine(kind: .add, text: text, old: nil, new: new))
                new += 1
            case "-":
                out.append(CodeDiffLine(kind: .del, text: text, old: old, new: nil))
                old += 1
            case "\\":
                out.append(CodeDiffLine(kind: .note, text: text.trimmingCharacters(in: .whitespaces), old: nil, new: nil))
            default:
                out.append(CodeDiffLine(kind: .context, text: text, old: old, new: new))
                old += 1
                new += 1
            }
        }
        return out
    }

    /// The side-by-side look's rows: context on both sides; a run of removed lines beside the run of added lines that
    /// follows it, row by row, the shorter run padded with empty cells; notes beside nothing.
    static func pairs(_ lines: [CodeDiffLine]) -> [(left: CodeDiffLine?, right: CodeDiffLine?)] {
        var out: [(left: CodeDiffLine?, right: CodeDiffLine?)] = []
        out.reserveCapacity(lines.count)
        var i = 0
        while i < lines.count {
            let l = lines[i]
            switch l.kind {
            case .context:
                out.append((l, l))
                i += 1
            case .note:
                out.append((l, nil))
                i += 1
            case .add:
                out.append((nil, l))
                i += 1
            case .del:
                var dels: [CodeDiffLine] = []
                while i < lines.count, lines[i].kind == .del { dels.append(lines[i]); i += 1 }
                var adds: [CodeDiffLine] = []
                while i < lines.count, lines[i].kind == .add { adds.append(lines[i]); i += 1 }
                for k in 0..<max(dels.count, adds.count) {
                    out.append((k < dels.count ? dels[k] : nil, k < adds.count ? adds[k] : nil))
                }
            }
        }
        return out
    }

    /// How many lines a hunk adds and removes, when the extension does not say.
    static func counts(_ lines: [CodeDiffLine]) -> (additions: Int, deletions: Int) {
        lines.reduce(into: (0, 0)) { n, l in
            if l.kind == .add { n.0 += 1 } else if l.kind == .del { n.1 += 1 }
        }
    }
}

/// A file tree's entry as the extension gives it: a full path, what it is and what changed.
nonisolated struct CodeTreeEntry: Hashable, Sendable {
    var path: String
    /// "file" | "dir"; nil: a file.
    var kind: String?
    var status: String?
    var additions: Int?
    var deletions: Int?
    var comments: Int?
    var badge: String?
    /// Marked viewed by the extension (UI version 5).
    var viewed: Bool?

    init(path: String, kind: String? = nil, status: String? = nil, additions: Int? = nil, deletions: Int? = nil,
         comments: Int? = nil, badge: String? = nil, viewed: Bool? = nil) {
        self.path = path
        self.viewed = viewed
        self.kind = kind
        self.status = status
        self.additions = additions
        self.deletions = deletions
        self.comments = comments
        self.badge = badge
    }

    var isDir: Bool { kind == "dir" }
}

/// A node of the tree: a file (its entry) or a directory (its children). A directory that only holds one directory is
/// shown as one node, named `a/b/c`, as editors do.
nonisolated struct CodeTreeNode: Identifiable, Hashable, Sendable {
    /// The full path (a directory's without a trailing slash).
    var path: String
    /// What the row shows: the last part, or a run of single-child directories joined with "/".
    var name: String
    var isDir: Bool
    var entry: CodeTreeEntry?
    var children: [CodeTreeNode]
    /// A directory the extension gave without what is in it (`kind: dir`): opening it asks the extension (`expand`).
    var unloaded: Bool
    var id: String { path }

    /// The files under it (itself, for a file): what a folded directory's row counts.
    var fileCount: Int { isDir ? children.reduce(0) { $0 + $1.fileCount } : 1 }
}

nonisolated enum CodeTree {
    /// The tree of the entries' paths: directories first, then files, each by name (case-insensitively, as Finder);
    /// runs of single-child directories joined. Paths are split on "/"; empty parts (a leading or doubled slash)
    /// are dropped, and a path given twice keeps its first entry.
    static func build(_ entries: [CodeTreeEntry]) -> [CodeTreeNode] {
        final class Dir {
            var children: [String: Dir] = [:]
            var files: [String: CodeTreeEntry] = [:]
            var given: CodeTreeEntry?
        }
        let root = Dir()
        for e in entries {
            let parts = e.path.split(separator: "/").map(String.init)
            guard !parts.isEmpty else { continue }
            var d = root
            for p in parts.dropLast() {
                if let c = d.children[p] { d = c } else { let c = Dir(); d.children[p] = c; d = c }
            }
            let last = parts[parts.count - 1]
            if e.isDir {
                let c = d.children[last] ?? Dir()
                d.children[last] = c
                if c.given == nil { c.given = e }
            } else if d.files[last] == nil {
                d.files[last] = e
            }
        }
        func order(_ a: String, _ b: String) -> Bool {
            let r = a.localizedCaseInsensitiveCompare(b)
            return r == .orderedSame ? a < b : r == .orderedAscending
        }
        func nodes(_ d: Dir, prefix: String) -> [CodeTreeNode] {
            var out: [CodeTreeNode] = []
            for name in d.children.keys.sorted(by: order) {
                var dir = d.children[name]!
                var shown = name
                var path = prefix + name
                // Join single-child directories (only when both are loaded: an unloaded one must stay its own row).
                while dir.files.isEmpty, dir.children.count == 1, let only = dir.children.first,
                      !(only.value.children.isEmpty && only.value.files.isEmpty) {
                    shown += "/" + only.key
                    path += "/" + only.key
                    dir = only.value
                }
                let kids = nodes(dir, prefix: path + "/")
                out.append(CodeTreeNode(path: path, name: shown, isDir: true, entry: dir.given, children: kids,
                                        unloaded: kids.isEmpty))
            }
            for name in d.files.keys.sorted(by: order) {
                out.append(CodeTreeNode(path: prefix + name, name: name, isDir: false, entry: d.files[name], children: [],
                                        unloaded: false))
            }
            return out
        }
        return nodes(root, prefix: "")
    }

    /// A row of the tree as a list shows it: the node, how deep, whether it is open.
    struct Row: Identifiable, Hashable, Sendable {
        var node: CodeTreeNode
        var depth: Int
        var open: Bool
        var id: String { node.path }
    }

    /// The rows on screen: every node whose directories above it are all open.
    static func rows(_ nodes: [CodeTreeNode], open: Set<String>, depth: Int = 0) -> [Row] {
        var out: [Row] = []
        for n in nodes {
            let isOpen = n.isDir && open.contains(n.path)
            out.append(Row(node: n, depth: depth, open: isOpen))
            if isOpen { out += rows(n.children, open: open, depth: depth + 1) }
        }
        return out
    }

    /// The directories to open so that `path` shows (its parents), for a tree that starts with the selected file in
    /// view. Only directories that are nodes of the tree (joined runs count as one).
    static func parents(of path: String, in nodes: [CodeTreeNode]) -> Set<String> {
        var out: Set<String> = []
        func walk(_ ns: [CodeTreeNode]) -> Bool {
            for n in ns where n.isDir && (path == n.path || path.hasPrefix(n.path + "/")) {
                out.insert(n.path)
                _ = walk(n.children)
                return true
            }
            return false
        }
        _ = walk(nodes)
        out.remove(path)
        return out
    }

    /// The node at `path` (a directory's children for drilling into it, iPhone), nil when there is none.
    static func node(_ path: String, in nodes: [CodeTreeNode]) -> CodeTreeNode? {
        for n in nodes {
            if n.path == path { return n }
            if n.isDir, path.hasPrefix(n.path + "/"), let f = node(path, in: n.children) { return f }
        }
        return nil
    }
}

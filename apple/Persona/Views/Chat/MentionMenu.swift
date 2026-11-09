import SwiftUI

// The composer's @ list (#2319), the web's (web/src/lib/mentions.ts, web/src/views/chat/MentionMenu.tsx): files this
// chat's tools read or wrote and the session's folder (picking one types `@path `, which Claude Code and Codex read as
// that file; a query with a slash browses that folder on the session's computer, and picking a folder keeps browsing),
// then other agents' sessions (picking one quotes it, sent as a <persona-context> before the message). Drawn in the
// composer's stack where the "/" list sits (`SlashMenu`), by an agent's or Persona's chat (`Composer`) and a new
// conversation (`StarterComposer`). BeautifulUI's prompt bar keeps its own @ menu (`PromptComposer`).

/// The `@word` being typed just before the caret (the web's `mentionAt`): at the start or after whitespace, so an
/// e-mail address is not one. Offsets count UTF-16, as `PathInsert` and `TextSelection.cursor(utf16:)` do.
nonisolated struct MentionToken: Equatable, Sendable {
    /// The `@`.
    var start: Int
    /// Just past the query: the caret.
    var end: Int
    var query: String

    static func at(_ text: String, caret: Int) -> MentionToken? {
        let units = text.utf16
        guard caret >= 0, caret <= units.count,
              let end = units.index(units.startIndex, offsetBy: caret).samePosition(in: text) else { return nil }
        let before = text[..<end]
        guard let at = before.lastIndex(of: "@") else { return nil }
        let query = before[before.index(after: at)...]
        guard !query.contains(where: \.isWhitespace) else { return nil }
        if at > before.startIndex, !before[before.index(before: at)].isWhitespace { return nil }
        return MentionToken(start: at.utf16Offset(in: text), end: caret, query: String(query))
    }

    /// The token a composer's selection is in: an insertion point (a selected range is not typing), or the draft's end
    /// while the field has reported none.
    static func at(_ text: String, selection: TextSelection?) -> MentionToken? {
        guard let range = PathInsert.range(selection, in: text) else { return at(text, caret: text.utf16.count) }
        return range.isEmpty ? at(text, caret: range.lowerBound) : nil
    }

    /// The draft with the token replaced by `insert`, and the caret after it. A picked file ends in a space: one already
    /// after the token is not doubled.
    func replacing(in text: String, with insert: String) -> (text: String, caret: Int) {
        let units = Array(text.utf16)
        let lo = min(start, units.count), hi = min(max(end, lo), units.count)
        let before = String(decoding: units[..<lo], as: UTF16.self)
        var after = String(decoding: units[hi...], as: UTF16.self)
        if insert.hasSuffix(" "), after.hasPrefix(" ") { after.removeFirst() }
        return (before + insert + after, before.utf16.count + insert.utf16.count)
    }

    /// The + menu's Mention (the web's @ button): `@` at the caret, set off from a word before it, so its list opens.
    static func typing(in text: String, caret: Int) -> (text: String, caret: Int) {
        let units = Array(text.utf16)
        let at = max(0, min(caret, units.count))
        let before = String(decoding: units[..<at], as: UTF16.self)
        let after = String(decoding: units[at...], as: UTF16.self)
        let insert = before.last.map { !$0.isWhitespace } == true ? " @" : "@"
        return (before + insert + after, before.utf16.count + insert.utf16.count)
    }
}

/// A row of the @ list.
nonisolated struct MentionRow: Identifiable, Equatable, Sendable {
    enum Kind: Equatable, Sendable {
        /// Typed in the token's place: `@path ` for a file, `@dir/` for a folder (the list then browses it).
        case file(insert: String, folder: Bool)
        /// Quoted with the message (`PromptItems.quote`).
        case session(SessionKey)
    }

    var id: String
    var name: String
    var detail: String
    var kind: Kind

    var isSession: Bool { if case .session = kind { true } else { false } }
}

/// What the @ list offers for a query, as the web's `useMentionRows`.
nonisolated enum MentionRows {
    static let limit = 40

    /// Paths the chat's tools read or wrote, newest first, at most `limit` (absolute, as the tools give them).
    static func usedPaths(_ items: [Item], limit: Int = 12) -> [String] {
        var seen = Set<String>()
        var out: [String] = []
        for it in items.reversed() where it.kind == .toolUse {
            guard let path = it.toolInput?["file_path"]?.string ?? it.toolInput?["notebook_path"]?.string,
                  !path.isEmpty, seen.insert(path).inserted else { continue }
            out.append(path)
            if out.count == limit { break }
        }
        return out
    }

    /// A path as typed after @: relative to the session's folder when inside it.
    static func relPath(_ path: String, cwd: String?) -> String {
        guard let cwd, !cwd.isEmpty else { return path }
        let base = withoutTrailingSlashes(cwd)
        return path.hasPrefix(base + "/") ? String(path.dropFirst(base.count + 1)) : path
    }

    private static func withoutTrailingSlashes(_ path: String) -> String {
        var path = path
        while path.hasSuffix("/") { path.removeLast() }
        return path
    }

    /// Where a query with a slash browses: the folder (under the session's, absolute, or under ~), what stays typed in
    /// front of a picked name, and the rest, which filters. Nil without a slash, or for a relative path with no folder
    /// to start from.
    static func browseTarget(_ query: String, cwd: String?) -> (dir: String, typed: String, filter: String)? {
        guard let slash = query.lastIndex(of: "/") else { return nil }
        let typed = String(query[...slash])
        let filter = String(query[query.index(after: slash)...])
        let part = withoutTrailingSlashes(typed)
        if typed.hasPrefix("/") { return (part.isEmpty ? "/" : part, typed, filter) }
        if typed.hasPrefix("~") { return (part.isEmpty ? "~" : part, typed, filter) }
        guard let cwd, !cwd.isEmpty else { return nil }
        return (withoutTrailingSlashes(cwd) + "/" + part, typed, filter)
    }

    /// The folder the list reads for a query: the one it browses, else the session's.
    static func folder(for query: String, cwd: String?) -> String? {
        if let target = browseTarget(query, cwd: cwd) { return target.dir }
        return cwd.flatMap { $0.isEmpty ? nil : $0 }
    }

    /// A name matches what is typed anywhere in it, ignoring case; hidden ones only when the filter starts with ".".
    static func nameMatches(_ name: String, _ filter: String) -> Bool {
        if name.hasPrefix("."), !filter.hasPrefix(".") { return false }
        return filter.isEmpty || name.lowercased().contains(filter.lowercased())
    }

    /// Names that start with the filter first, then the rest, each keeping its order.
    static func byPrefix<T>(_ rows: [T], name: (T) -> String, filter: String) -> [T] {
        let f = filter.lowercased()
        guard !f.isEmpty else { return rows }
        return rows.filter { name($0).lowercased().hasPrefix(f) } + rows.filter { !name($0).lowercased().hasPrefix(f) }
    }

    /// The second line of a session's row: who answers there and its folder.
    static func sessionDetail(_ s: Session) -> String {
        [s.agentLabel, s.folderName].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// Other agents' sessions matching the query by title or by agent and folder, most recently active first.
    static func sessions(_ all: [Session], except: SessionKey?, query: String, limit: Int = 8) -> [Session] {
        let q = query.lowercased()
        return all.filter { $0.key != except && !$0.assistant }
            .filter { q.isEmpty || $0.displayTitle.lowercased().contains(q) || sessionDetail($0).lowercased().contains(q) }
            .sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
            .prefix(limit)
            .map { $0 }
    }

    /// The rows for a query. `listing` is the folder `folder(for:cwd:)` names, nil until it is read: browsing shows that
    /// folder's entries alone; otherwise the chat's files, the session folder's entries, then the sessions.
    static func rows(query: String, cwd: String?, used: [String], listing: [FileEntry]?, sessions all: [Session],
                     except: SessionKey?) -> [MentionRow] {
        let entries = listing ?? []
        if let target = browseTarget(query, cwd: cwd) {
            let hits = entries.filter { nameMatches($0.name, target.filter) }
            let ordered = byPrefix(hits.filter(\.dir), name: \.name, filter: target.filter)
                + byPrefix(hits.filter { !$0.dir }, name: \.name, filter: target.filter)
            return ordered.prefix(limit).map { e in
                MentionRow(id: "fs:\(target.typed)\(e.name)", name: e.dir ? e.name + "/" : e.name, detail: target.typed,
                           kind: .file(insert: "@\(target.typed)\(e.name)\(e.dir ? "/" : " ")", folder: e.dir))
            }
        }
        let q = query.lowercased()
        var out: [MentionRow] = []
        var listed = Set<String>()
        for path in used {
            let rel = relPath(path, cwd: cwd)
            if !q.isEmpty, !rel.lowercased().contains(q) { continue }
            listed.insert(rel)
            let name = (rel as NSString).lastPathComponent
            let dir = (rel as NSString).deletingLastPathComponent
            out.append(MentionRow(id: "used:\(path)", name: name,
                                  detail: dir.isEmpty || dir == "/" ? String(localized: "Used in this session") : dir,
                                  kind: .file(insert: "@\(rel) ", folder: false)))
        }
        let top = entries.filter { nameMatches($0.name, q) && !listed.contains($0.name) }
        let ordered = byPrefix(top.filter(\.dir), name: \.name, filter: q) + byPrefix(top.filter { !$0.dir }, name: \.name, filter: q)
        for e in ordered.prefix(12) {
            out.append(MentionRow(id: "top:\(e.name)", name: e.dir ? e.name + "/" : e.name,
                                  detail: e.dir ? String(localized: "Folder") : String(localized: "Session folder"),
                                  kind: .file(insert: "@\(e.name)\(e.dir ? "/" : " ")", folder: e.dir)))
        }
        for s in sessions(all, except: except, query: q) {
            out.append(MentionRow(id: "session:\(s.key.path)", name: s.displayTitle, detail: sessionDetail(s), kind: .session(s.key)))
        }
        return Array(out.prefix(limit))
    }
}

/// Where a composer's @ list reads from.
struct MentionSource {
    /// The computer whose folders it lists: the session's, or the assistant's for a new conversation.
    var machine: MachineRef
    /// The session's folder: the first listing and relative paths start there. Nil: only a typed absolute or ~ path is
    /// listed (a new conversation, as on the web).
    var cwd: String?
    /// Absolute paths the chat's tools read or wrote, newest first (read when the list opens).
    var files: () -> [String] = { [] }
    /// This chat, left out of the sessions.
    var except: SessionKey?
}

/// The @ list is open over a composer: the new conversation page makes room for it, Needs You stepping aside until it
/// closes (with the keyboard up, the list, those rows and the composer did not fit, and the composer went under the keyboard).
struct MentionMenuOpen: PreferenceKey {
    static let defaultValue = false
    static func reduce(value: inout Bool, nextValue: () -> Bool) { value = value || nextValue() }
}

/// The folders the @ list read while it was open (`listDir`, the web's `/fs`), each once.
@MainActor @Observable
final class MentionFolders {
    private var listings: [String: [FileEntry]] = [:]

    /// A folder's entries; nil until read (one that could not be read has none).
    func entries(in dir: String) -> [FileEntry]? { listings[dir] }

    /// Reads dir unless it was read, after a short pause: typing through a path asks only for where it stops.
    func load(_ dir: String, cwd: String, machine: MachineRef, api: APIClient?) async {
        guard listings[dir] == nil else { return }
        guard let api else { listings[dir] = []; return }
        try? await Task.sleep(for: .milliseconds(120))
        guard !Task.isCancelled else { return }
        do {
            listings[dir] = try await api.listDir(machine: machine.id, path: dir, cwd: cwd).entries
        } catch {
            if !Task.isCancelled { listings[dir] = [] } // not a folder, or unreadable: no rows from it
        }
    }
}

/// The @ list for the token being typed: reads the folder it needs from the computer, and on a pick types the file or
/// folder in the token's place, or quotes the session and takes the token out.
struct ComposerMentionMenu: View {
    @Environment(AppModel.self) private var model
    var token: MentionToken
    @Binding var text: String
    @Binding var selection: TextSelection?
    var source: MentionSource
    /// A session picked: its quote goes with the message.
    var quote: (ContextRef) -> Void
    @State private var folders = MentionFolders()
    /// The chat's files as the list opened (the web's: the chat may grow meanwhile, the rows stay put).
    @State private var used: [String]?

    private var cwd: String? { source.cwd.flatMap { $0.isEmpty ? nil : $0 } }

    var body: some View {
        let dir = MentionRows.folder(for: token.query, cwd: cwd)
        let listing = dir.flatMap { folders.entries(in: $0) }
        let rows = MentionRows.rows(query: token.query, cwd: cwd, used: used ?? source.files(), listing: listing,
                                    sessions: model.sessionsNow, except: source.except)
        let browsing = MentionRows.browseTarget(token.query, cwd: cwd) != nil
        MentionMenu(rows: rows, empty: dir != nil && listing == nil ? .reading : browsing ? .noMatchInFolder : .noMatch, pick: pick)
            .onAppear { if used == nil { used = source.files() } }
            .task(id: dir) {
                guard let dir else { return }
                await folders.load(dir, cwd: cwd ?? "", machine: source.machine, api: model.api(source.machine.group))
            }
    }

    private func pick(_ row: MentionRow) {
        #if os(iOS) || os(visionOS)
        // A keyboard that keeps its composition out of the draft (#2203): confirmed first, and the list follows the
        // text that leaves. A pick now would write over the words still marked.
        if TypedText.composingFocused {
            TypedText.commitFocused(into: $text)
            return
        }
        #endif
        let insert: String
        switch row.kind {
        case .file(let typed, _):
            insert = typed
        case .session(let key):
            guard let s = model.sessionsNow.first(where: { $0.key == key }) else { return }
            quote(PromptItems.quote(s))
            insert = ""
        }
        let r = token.replacing(in: text, with: insert)
        text = r.text
        selection = .cursor(utf16: r.caret)
    }
}

/// The @ list over the composer: a tap types the row (a file or a folder) or quotes it (a session). Past five and a half
/// rows it scrolls, the half row saying so, as the "/" list (`SlashMenu`).
struct MentionMenu: View {
    /// What the list says while it has no rows.
    enum Empty { case reading, noMatchInFolder, noMatch }

    var rows: [MentionRow]
    var empty: Empty
    var pick: (MentionRow) -> Void
    /// One row's height, as drawn (it follows the text size).
    @State private var rowHeight: CGFloat = 0
    private static let visibleRows: CGFloat = 5.5

    var body: some View {
        ScrollView {
            list
        }
        .scrollBounceBehavior(.basedOnSize)
        .frame(height: rowHeight > 0 ? rowHeight * min(CGFloat(max(rows.count, 1)), Self.visibleRows) : nil)
        .padding(.vertical, Theme.Space.xs)
        .surface(in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Mention a file or session")
        .accessibilityIdentifier("mention-menu")
    }

    private var list: some View {
        VStack(spacing: 0) {
            if rows.isEmpty { emptyRow }
            ForEach(Array(rows.enumerated()), id: \.element.id) { i, row in
                // A line above the sessions, after the files (the web's group line).
                if i > 0, row.isSession, !rows[i - 1].isSession {
                    Divider().padding(.horizontal, Theme.Space.m)
                }
                Button { pick(row) } label: { label(row) }
                    .buttonStyle(.plain)
                    .accessibilityLabel(row.name)
                    .accessibilityHint(row.isSession ? [row.detail, String(localized: "As context")].joined(separator: ", ") : row.detail)
                    .accessibilityIdentifier("mention-menu.\(row.id)")
            }
        }
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { h in rowHeight = h / CGFloat(max(rows.count, 1)) }
    }

    private func label(_ row: MentionRow) -> some View {
        HStack(spacing: Theme.Space.s) {
            ThemedIcon(symbol(row)).font(Theme.Font.secondary).foregroundStyle(.secondary).scaledFrame(width: Theme.Size.iconS)
            Text(row.name).font(Theme.Font.secondaryStrong).foregroundStyle(.primary).truncationMode(.middle)
                .layoutPriority(1)
            Text(row.detail).font(Theme.Font.caption).foregroundStyle(.secondary)
                .truncationMode(row.isSession ? .tail : .head)
            Spacer(minLength: Theme.Space.s)
            if row.isSession {
                Text("As context").font(Theme.Font.caption).foregroundStyle(.tertiary).fixedSize()
            }
        }
        .lineLimit(1)
        .padding(.horizontal, Theme.Space.m)
        .scaledFrame(minHeight: Theme.Size.tapTarget)
        .contentShape(.rect)
    }

    private func symbol(_ row: MentionRow) -> String {
        switch row.kind {
        case .session: Theme.Icon.agentChat
        case .file(_, let folder): folder ? Theme.Icon.folder : Theme.Icon.document
        }
    }

    private var emptyRow: some View {
        HStack(spacing: Theme.Space.s) {
            if empty == .reading { ProgressView().controlSize(.small) }
            switch empty {
            case .reading: Text("Reading folder…")
            case .noMatchInFolder: Text("Nothing matches in this folder")
            case .noMatch: Text("No matching files or sessions")
            }
            Spacer(minLength: 0)
        }
        .font(Theme.Font.secondary)
        .foregroundStyle(.secondary)
        .lineLimit(1)
        .padding(.horizontal, Theme.Space.m)
        .scaledFrame(minHeight: Theme.Size.tapTarget)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("mention-menu.empty")
    }
}

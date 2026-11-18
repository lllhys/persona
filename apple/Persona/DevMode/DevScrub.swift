import Foundation

/// What Developing Persona's package may say (#9460, docs/operations/developing-persona.md "What is never included"):
/// recorded lines carry only code-chosen words and numbers, and free text (an error's message, a log line) passes
/// `text` first. Route patterns are the web error reports' (`web/src/lib/error-reports.ts` `pathPattern`), word for word.
nonisolated enum DevScrub {
    /// The longest free text kept, in UTF-8 bytes.
    static let maxText = 1024

    /// The words of the hub's API paths; any other segment (a machine, session, item id, a name) becomes `:id`. The same
    /// list as the web's `API_WORDS` (DevModeTests checks they match).
    static let apiWords: Set<String> = Set((
        "accounts agents archive assistant attach auth auto-upgrade before bind binding blob blocked botmux bots browser bulk catalog " +
        "channels chats check claude-dirs close code comments computer config connectors contributions conversations convert create " +
        "current data days decisions deploy diff dirs discover dismiss doc doctor documents done draft enabled engine error-reports " +
        "events extensions file files find fix flags fork fresh fs full git health herdr history hub image import inbox info install " +
        "interactions interrupt items kb kb-search lark launchers log login logout looks machines mail managers markets memory messages " +
        "metrics missing model-config modes moments move mutes notify oauth owner pair pair-codes panes people personas pinned pins posts " +
        "prefs prepare presets preview providers purposes queue quick quota raw reactions read reconnect refresh reject rename replies " +
        "reply resolve restore retry rewind route run runs screen secrets seen send sessions settings show skills spaces split start " +
        "status step suggestion tabs task-plans tasks thread thumb today todos token tokens translate trash tree triage ui unarchive " +
        "update upgrade upgrades usage windows workspace-board workspaces worktree").split(separator: " ").map(String.init))

    /// A URL or path as a pattern: no scheme, host, query or fragment, no `/api/v1`, and every segment that isn't one of
    /// the API's words `:id` (`/machines/:id/sessions/:id/items`).
    static func route(_ raw: String) -> String {
        var path = raw.trimmingCharacters(in: .whitespaces)
        if let scheme = path.range(of: "://") {
            let rest = path[scheme.upperBound...]
            path = rest.firstIndex(where: { $0 == "/" || $0 == "?" || $0 == "#" }).map { String(rest[$0...]) } ?? "/"
        }
        if let cut = path.firstIndex(where: { $0 == "?" || $0 == "#" }) { path = String(path[..<cut]) }
        if path.hasPrefix("/api/v1") {
            let rest = path.dropFirst("/api/v1".count)
            if rest.isEmpty || rest.hasPrefix("/") { path = String(rest) }
        }
        let out = path.split(separator: "/", omittingEmptySubsequences: false)
            .map { $0.isEmpty || apiWords.contains(String($0)) ? String($0) : ":id" }
            .joined(separator: "/")
        return out.isEmpty ? "/" : out
    }

    /// A code-chosen word (a WebSocket message's type, a screen's name) as it may be recorded: letters, digits and
    /// `_ - . :` only, at most 48 of them; anything else is `other`.
    static func word(_ raw: String) -> String {
        let allowed = raw.unicodeScalars.allSatisfy { ($0.isASCII && ($0.properties.isAlphabetic || ("0"..."9").contains($0))) || "_-.:".unicodeScalars.contains($0) }
        return !raw.isEmpty && raw.count <= 48 && allowed ? raw : "other"
    }

    private static let rules: [(NSRegularExpression, String)] = [
        // A URL keeps its path only: hosts, queries and fragments (tokens, pairing codes) go.
        (#"(?i)\b[a-z][a-z0-9+.-]*://[^\s/?#'"<>()]*([^\s?#'"<>()]*)[^\s'"<>()]*"#, "$1"),
        // What is left of a query after a path.
        (#"(/[^\s'"<>()?#]*)[?#][^\s'"<>()]*"#, "$1"),
        (#"\b(Bearer|Basic|Token)\s+[A-Za-z0-9._~+/=-]+"#, "$1 <redacted>"),
        (#"(?i)\b(access_token|refresh_token|id_token|token|api_key|apikey|key|secret|client_secret|password|passwd|pwd|cookie|set-cookie|authorization|auth|session_token|signature|sig)(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;&'"]+)"#, "$1$2<redacted>"),
        (#"\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+"#, "<redacted>"),
        (#"[\w.+-]+@[\w-]+(?:\.[\w-]+)+"#, "<email>"),
        (#"(?<![\w.])(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}(?![\w.])"#, "<ip>"),
        (#"(?i)(?<![\w:])(?:[0-9a-f]{1,4}:){4,7}[0-9a-f]{1,4}(?![\w:])"#, "<ip>"),
        (#"(?i)(?<![\w:])[0-9a-f]{0,4}(?::[0-9a-f]{0,4})*::[0-9a-f]{0,4}(?::[0-9a-f]{0,4})*(?![\w:])"#, "<ip>"),
        (#"(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:com|net|org|io|cn|xyz|cloud|local|lan|internal|arpa)\b"#, "<host>"),
        (#"(/Users|/home)/[^/\s'"]+"#, "~"),
        // Long runs of letters and digits together: keys, tokens, ids.
        (#"(?<![A-Za-z0-9+/_-])(?=[A-Za-z0-9+/_-]*\d)(?=[A-Za-z0-9+/_-]*[A-Za-z])[A-Za-z0-9+/_-]{32,}={0,2}"#, "<redacted>"),
    ].map { (try! NSRegularExpression(pattern: $0.0), $0.1) }

    /// Free text as it may go into the package: URLs cut to their path, secrets, addresses and long keys replaced,
    /// home folders as `~`, at most `maxText` bytes.
    static func text(_ raw: String) -> String {
        var s = raw
        for (rule, template) in rules {
            s = rule.stringByReplacingMatches(in: s, range: NSRange(s.startIndex..., in: s), withTemplate: template)
        }
        return clip(s, maxText)
    }

    /// `s` cut to at most `bytes` UTF-8 bytes on a character boundary, with an ellipsis when it was cut.
    static func clip(_ s: String, _ bytes: Int) -> String {
        guard s.utf8.count > bytes else { return s }
        var out = ""
        var used = 0
        for ch in s {
            let n = String(ch).utf8.count
            if used + n > bytes - 3 { break }
            out.append(ch)
            used += n
        }
        return out + "…"
    }
}

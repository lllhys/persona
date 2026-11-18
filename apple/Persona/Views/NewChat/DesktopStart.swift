import Foundation

/// New Agent with a launcher that starts the session in Claude's desktop app (`Launcher.startsInDesktopApp`,
/// docs/integrations/claude-integration.md「New sessions in the desktop app」, #5480): its folder may be left empty (the
/// session goes to the app's scratch folder, which the computer picks), browsing starts in the folder that holds the
/// app's projects, and a new folder may be named there (the computer makes it when the session starts).
nonisolated enum DesktopStart {
    /// The folder that holds most of the folders of the desktop app's sessions among `sessions` (one computer's): Claude's
    /// (as the computer places the scratch folder), or with `codex` the Codex App's projects (its chats in no project, in
    /// <home>/Documents/Codex/<date>/<name>, left out). Ties go to the first by name. nil when it has none.
    static func projectsFolder(_ sessions: [Session], codex: Bool = false) -> String? {
        var count: [String: Int] = [:]
        for s in sessions where s.agentSurface == "desktop" && (codex ? s.isCodex && !AppGroups.isCodexChatFolder(s.cwd) : s.isClaude)
            && s.cwd.hasPrefix("/") {
            let parent = (s.cwd as NSString).deletingLastPathComponent
            guard !parent.isEmpty, parent != "/" else { continue }
            count[parent, default: 0] += 1
        }
        return count.max { a, b in a.value != b.value ? a.value < b.value : a.key > b.key }?.key
    }

    /// The path of a new folder `name` in `folder`; nil when the name is not one folder's name.
    static func newFolder(_ name: String, in folder: String) -> String? {
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !n.isEmpty, n != ".", n != "..", !n.contains("/"), folder.hasPrefix("/") else { return nil }
        return (folder as NSString).appendingPathComponent(n)
    }
}

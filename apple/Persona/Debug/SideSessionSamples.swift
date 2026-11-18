#if DEBUG
import Foundation
import PersonaWire

/// `-showSideSessions <mode>` (DEBUG only, for screenshots; release.sh keeps the name out of Release): three sample side
/// sessions under the session `-openSession` names (else the newest agent), as a hub that folds them would list them
/// (#7944): one that asks a question (its card comes with it), one working, one stopped. `chip` (or `YES`) shows the
/// chip; `sheet` opens the sheet too; `side` opens the first side's chat over its parent; `<machine>/<session>` hangs
/// them under that session without opening it (the Agents list).
enum SideSessionSamples {
    static var mode: String? { UserDefaults.standard.string(forKey: "showSideSessions") }
    private static let prefix = "sample-side-"

    static func isSample(_ key: SessionKey) -> Bool { key.sessionID.hasPrefix(prefix) }

    /// The parent the samples go under, from the lists as they are: the session the switch names
    /// (`-showSideSessions <machine>/<session>`, for the Agents list), else `-openSession`'s, else the newest running agent
    /// that is no subtask.
    static func parent(_ sessions: [Session]) -> Session? {
        let named = mode.flatMap { $0.contains("/") ? $0 : nil } ?? UserDefaults.standard.string(forKey: "openSession")
        if let named, let s = sessions.first(where: { $0.machineRef.path + "/" + $0.sessionID == named || "\($0.machineID)/\($0.sessionID)" == named }) {
            return s
        }
        return sessions.filter { !$0.assistant && $0.state.isLive && !$0.isSubtask && !SideFold.isSideRow($0) && !isSample($0.key) }
            .max { ($0.updatedAt ?? .distantPast) < ($1.updatedAt ?? .distantPast) }
    }

    /// The sample rows under `sessions`' parent and its group, nil without the switch or a session to hang them under.
    static func rows(_ sessions: [Session]) -> (group: String, rows: [Session])? {
        guard mode != nil, let p = parent(sessions) else { return nil }
        let now = Date()
        let samples: [(id: String, title: String, state: String, ago: TimeInterval)] = [
            ("asks", "Check the release notes", "needs_you", 120),
            ("works", "Draft the migration plan", "working", 30),
            ("done", "Look up the API limits", "idle", 3600),
        ]
        let rows: [Session] = samples.compactMap { sample in
            let fields: [String: Any] = [
                "machine_id": p.machineID, "session_id": prefix + sample.id, "title": sample.title, "cwd": p.cwd,
                "status": sample.id == "done" ? "offline" : "idle", "state": sample.state, "agent": "claude", "agent_surface": "desktop",
                "updated_at": ISO8601DateFormatter().string(from: now.addingTimeInterval(-sample.ago)),
                "state_since": ISO8601DateFormatter().string(from: now.addingTimeInterval(-sample.ago)),
                "parent_agent_id": p.sessionID, "folded_under": p.sessionID, "folded_as": SideFold.side,
                "pending_interactions": sample.id == "asks" ? 1 : 0, "started_by_persona": false, "assistant": false,
            ]
            guard let data = try? JSONSerialization.data(withJSONObject: fields),
                  var s = try? JSON.decoder.decode(Session.self, from: data) else { return nil }
            s.group = p.group
            return s
        }
        return (p.group, rows)
    }

    /// The question the sample that asks waits on (`key`, under `parent`): shown in its own chat, and in its parent's as a
    /// side session's card.
    static func card(for key: SessionKey, parent: SessionKey) -> Interaction? {
        guard key.sessionID == prefix + "asks" else { return nil }
        let json = """
        {"id":"sample-side-question","machine_id":"\(key.machineID)","session_id":"\(key.sessionID)","kind":"question",
         "folded_under":"\(parent.sessionID)","folded_as":"side","questions":[{"question":"Which version should the notes cover?","header":"Version",
         "multi_select":false,"options":[{"label":"The next release","description":null},{"label":"Every release this year","description":null}]}]}
        """
        return try? JSON.decoder.decode(Interaction.self, from: Data(json.utf8))
    }
}
#endif

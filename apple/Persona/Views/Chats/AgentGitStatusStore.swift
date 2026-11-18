import SwiftUI

/// The repository status behind an Agent menu. Keyed by the session and its directory, not by
/// whichever chat happens to be current when a delayed response arrives.
@MainActor @Observable
final class AgentGitStatusStore {
    struct Key: Hashable, Sendable {
        let session: SessionKey
        let cwd: String
    }

    private var entries: [Key: GitStatus] = [:]
    private var generations: [Key: Int] = [:]

    func status(for key: Key) -> GitStatus? { entries[key] }

    @discardableResult
    func begin(_ key: Key) -> Int {
        let generation = (generations[key] ?? 0) + 1
        generations[key] = generation
        return generation
    }

    func accept(_ key: Key, generation: Int, status: GitStatus) {
        guard generations[key] == generation else { return }
        entries[key] = status
    }

    /// Page actions (a Git write or selecting another Agent) own the next generation too,
    /// so a reply for the old generation cannot overwrite them.
    func set(_ status: GitStatus?, for key: Key) {
        begin(key)
        entries[key] = status
    }

    /// Accept a response only if this key still belongs to the current Agent. `current` is
    /// captured at completion time, not at request start, and is required for cross-session loads.
    func accept(_ key: Key, generation: Int, status: GitStatus, current: SessionKey) {
        guard key.session == current else { return }
        accept(key, generation: generation, status: status)
    }

    func clear(_ key: Key) {
        begin(key)
        entries[key] = nil
    }

    /// The existing detail-page load, with an identity-checked completion. A delayed answer for an
    /// older directory or request cannot replace a newer one, regardless of Task cancellation.
    func load(_ key: Key, api: APIClient) async {
        await load(key) { try await api.gitStatus(machine: key.session.machineID, cwd: key.cwd) }
    }

    /// `fetch` is the network request in production and a controllable delay in tests.
    func load(_ key: Key, fetch: () async throws -> GitStatus) async {
        let generation = begin(key)
        let owner = key.session
        do {
            let status = try await fetch()
            accept(key, generation: generation, status: status, current: owner)
        } catch {
            // A failed load keeps the last accepted status. It never invents a new repository state.
        }
    }
}

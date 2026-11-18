import Foundation
import Testing
@testable import Persona
import PersonaWire

/// #9278: menu state belongs to a SessionKey and its directory, not to a retained ChatView.
@MainActor struct AgentGitStatusStoreTests {
    private func key(_ session: String, cwd: String = "/work/a") -> AgentGitStatusStore.Key {
        .init(session: SessionKey(machineID: "m", sessionID: session), cwd: cwd)
    }

    private func status(_ count: Int) throws -> GitStatus {
        let files = (0..<count).map { #"{"path":"file\#($0).swift","index":"M","worktree":"M","added":1,"deleted":0}"# }
        return try JSON.decoder.decode(GitStatus.self, from: Data(#"{"is_repo":true,"ahead":0,"behind":0,"files":[\#(files.joined(separator: ","))],"stashes":0,"commits":[]}"#.utf8))
    }

    @Test func reopenedDetailUsesTheSameLoadedStatusAsTheRow() async throws {
        let store = AgentGitStatusStore(), k = key("a")
        store.set(try status(3), for: k)
        // A new detail has no local status, and its request has not answered. Its menu reads the store,
        // just as the row does; it does not fall back to a second nil value.
        let menuStatus = store.status(for: k)
        #expect(menuStatus?.files.count == 3)
        await store.load(k) { try await Task.never() }
        #expect(store.status(for: k)?.files.count == 3)
        store.set(try status(5), for: k) // an update from the other entry is visible immediately.
        #expect(store.status(for: k)?.files.count == 5)
    }

    @Test func failedReloadKeepsTheAcceptedStatus() async throws {
        let store = AgentGitStatusStore(), k = key("a")
        store.set(try status(3), for: k)
        await store.load(k) { throw APIError(status: 503, message: "offline") }
        #expect(store.status(for: k)?.files.count == 3)
    }

    @Test func lateResultFromAnotherAgentCannotLandOnTheCurrentOne() async throws {
        let store = AgentGitStatusStore(), a = key("a"), b = key("b")
        store.set(try status(2), for: b)
        let aResult = Task { await store.load(a) { try await self.status(9) } }
        store.set(try status(4), for: b) // B is now current; A's late response has no claim on it.
        await aResult.value
        #expect(store.status(for: a)?.files.count == 9, "A's isolated result may complete, but never lands on B")
        #expect(store.status(for: b)?.files.count == 4)
    }

    @Test func lateResultFromTheOldDirectoryCannotReplaceTheNewOne() async throws {
        let store = AgentGitStatusStore(), old = key("a", cwd: "/work/old"), current = key("a", cwd: "/work/new")
        store.set(try status(2), for: current)
        let oldResult = Task { await store.load(old) { try await self.status(8) } }
        store.set(try status(6), for: current)
        await oldResult.value
        #expect(store.status(for: current)?.files.count == 6)
        #expect(store.status(for: old)?.files.count == 8, "a completed old-directory request is isolated, not applied to current")
    }

    @Test func aNewerRequestForTheSameDirectoryWinsOverAnOlderOne() async throws {
        let store = AgentGitStatusStore(), k = key("a")
        let older = store.begin(k)
        let newer = store.begin(k)
        store.accept(k, generation: newer, status: try status(5))
        store.accept(k, generation: older, status: try status(9))
        #expect(store.status(for: k)?.files.count == 5)
    }

    @Test func completionIsRejectedWhenItsSessionIsNoLongerCurrent() throws {
        let store = AgentGitStatusStore(), a = key("a"), b = key("b")
        store.set(try status(4), for: b)
        let generation = store.begin(a)
        store.accept(a, generation: generation, status: try status(9), current: b.session)
        #expect(store.status(for: a) == nil, "A's completion cannot write A when B is current")
        #expect(store.status(for: b)?.files.count == 4)
        store.accept(a, generation: generation, status: try status(9), current: a.session)
        #expect(store.status(for: a)?.files.count == 9)
    }
}

private extension Task where Success == Never, Failure == Never {
    static func never() async throws -> GitStatus {
        try await Task.sleep(for: .seconds(60))
        throw CancellationError()
    }
}

import Foundation
import Testing
@testable import Persona

private struct PersonalAgentPrefsEventTimeout: Error, CustomStringConvertible {
    let event: String
    var description: String { "Timed out waiting for \(event)" }
}

private func waitForPersonalAgentPrefsEvent(_ expected: String, in stream: AsyncStream<String>) async throws {
    try await waitForPersonalAgentPrefsEvents([expected], in: stream)
}

private func waitForPersonalAgentPrefsEvents(_ expected: Set<String>, in stream: AsyncStream<String>) async throws {
    try await withThrowingTaskGroup(of: Void.self) { group in
        group.addTask {
            var remaining = expected
            for await event in stream {
                remaining.remove(event)
                if remaining.isEmpty { return }
            }
            throw PersonalAgentPrefsEventTimeout(event: expected.sorted().joined(separator: ", ") + " (stream ended)")
        }
        group.addTask {
            try await Task.sleep(for: .seconds(2))
            throw PersonalAgentPrefsEventTimeout(event: expected.sorted().joined(separator: ", "))
        }
        _ = try await group.next()
        group.cancelAll()
    }
}

private actor PersonalAgentPrefsHub {
    private var stored: PersonalAgentPrefs
    private var refusePut = false
    private var refuseEveryPut = false
    private var cancelPut = false
    private var putStatus = 500
    private var getStatus: Int?
    private var holdGet = false
    private var holdPut = false
    private var markPut = false
    private var releaseHeldGet: AsyncStream<Void>.Continuation?
    private var releaseHeldPut: AsyncStream<Void>.Continuation?
    private let events: AsyncStream<String>.Continuation

    init(stored: PersonalAgentPrefs, getStatus: Int? = nil, events: AsyncStream<String>.Continuation) {
        self.stored = stored
        self.getStatus = getStatus
        self.events = events
    }

    func refuseNextPut(status: Int = 500) {
        putStatus = status
        refusePut = true
    }
    func refuseEveryPut(status: Int) {
        putStatus = status
        refusePut = true
        refuseEveryPut = true
    }
    func refuseGets(status: Int) { getStatus = status }
    func cancelNextPut() { cancelPut = true }
    func holdNextGet() { holdGet = true }
    func holdNextPut() { holdPut = true }
    func markNextPutResponse() { markPut = true }

    func releaseGet() {
        releaseHeldGet?.yield(())
        releaseHeldGet?.finish()
        releaseHeldGet = nil
    }
    func releasePut() {
        releaseHeldPut?.yield(())
        releaseHeldPut?.finish()
        releaseHeldPut = nil
    }

    func answer(_ request: LocalRequest) async throws -> Data {
        if request.method == "PUT" {
            let changed = try JSONDecoder().decode(PersonalAgentPrefs.self, from: request.body ?? Data())
            if holdPut {
                holdPut = false
                let gate = AsyncStream<Void>.makeStream()
                releaseHeldPut = gate.continuation
                events.yield("put-held")
                for await _ in gate.stream { break }
                releaseHeldPut = nil
            }
            if cancelPut {
                cancelPut = false
                events.yield("put-cancelled")
                throw CancellationError()
            }
            if refusePut {
                refusePut = refuseEveryPut
                events.yield("put-failed")
                throw APIError(status: putStatus, message: "offline")
            }
            stored = changed
            if markPut {
                stored.hidden = ["confirmed": 1]
                markPut = false
            }
            events.yield("PUT")
            return try JSONEncoder().encode(stored)
        }
        let snapshot = stored
        events.yield("GET")
        if holdGet {
            holdGet = false
            let gate = AsyncStream<Void>.makeStream()
            releaseHeldGet = gate.continuation
            events.yield("get-held")
            for await _ in gate.stream { break }
            releaseHeldGet = nil
        }
        if let getStatus { throw APIError(status: getStatus, message: "offline") }
        return try JSONEncoder().encode(snapshot)
    }
}

@MainActor
private func personalAgentPrefsFixture(stored: PersonalAgentPrefs = PersonalAgentPrefs(), getStatus: Int? = nil) -> (
    store: PersonalAgentPrefsStore, hub: PersonalAgentPrefsHub, api: APIClient,
    stream: AsyncStream<String>, events: AsyncStream<String>.Continuation, defaults: UserDefaults, name: String
) {
    let events = AsyncStream<String>.makeStream()
    let hub = PersonalAgentPrefsHub(stored: stored, getStatus: getStatus, events: events.continuation)
    let api = APIClient(config: ServerConfig(url: URL(string: "https://hub.invalid")!, token: "fixture"), transport: { try await hub.answer($0) })
    let name = "com.example.persona.prefs-tests." + UUID().uuidString
    let defaults = UserDefaults(suiteName: name)!
    return (PersonalAgentPrefsStore(defaults: defaults), hub, api, events.stream, events.continuation, defaults, name)
}

@MainActor struct PersonalAgentPrefsStoreTests {
    @Test func failedSaveRollsBackReportsAndReloads() async throws {
        var original = PersonalAgentPrefs()
        original.pins = ["before"]
        let f = personalAgentPrefsFixture(stored: original)
        defer { f.defaults.removePersistentDomain(forName: f.name) }
        await f.store.load(f.api)
        await f.hub.refuseNextPut()
        var reported = ""
        f.store.update(f.api, { $0.togglePin("after") }, onFailure: { reported = $0 })
        #expect(f.store.prefs.pins.contains("after"), "the edit is optimistic while the PUT is in flight")

        try await waitForPersonalAgentPrefsEvent("put-failed", in: f.stream)
        try await waitForPersonalAgentPrefsEvent("GET", in: f.stream)
        #expect(reported.contains("offline"))
        #expect(f.store.prefs == original, "a failed save restores the hub-backed value instead of looking saved")
        f.events.finish()
    }

    @Test func aStaleReadDoesNotOverwriteANewerEdit() async throws {
        let f = personalAgentPrefsFixture()
        defer { f.defaults.removePersistentDomain(forName: f.name) }
        await f.store.load(f.api)
        await f.hub.holdNextGet()
        await f.hub.holdNextPut()

        let staleRead = Task { await f.store.load(f.api) }
        try await waitForPersonalAgentPrefsEvent("get-held", in: f.stream)
        f.store.update(f.api, { $0.togglePin("new") })
        try await waitForPersonalAgentPrefsEvent("put-held", in: f.stream)
        await f.hub.releaseGet()
        await staleRead.value
        #expect(f.store.prefs.pins == ["new"], "a GET that began before the edit must not erase its optimistic value")
        await f.hub.releasePut()
        try await waitForPersonalAgentPrefsEvent("PUT", in: f.stream)
        f.events.finish()
    }

    @Test func aReadStartedDuringASaveCannotReturnBeforeThatSave() async throws {
        let f = personalAgentPrefsFixture()
        defer { f.defaults.removePersistentDomain(forName: f.name) }
        await f.store.load(f.api)
        await f.hub.holdNextPut()
        await f.hub.holdNextGet()
        await f.hub.markNextPutResponse()
        f.store.update(f.api, { $0.togglePin("new") })
        try await waitForPersonalAgentPrefsEvent("put-held", in: f.stream)
        let read = Task { await f.store.load(f.api) }
        let release = Task {
            try? await Task.sleep(for: .milliseconds(50))
            await f.hub.releasePut()
        }
        await release.value
        try await waitForPersonalAgentPrefsEvents(["PUT", "get-held"], in: f.stream)
        try await waitForRefreshTest { f.store.prefs.hidden?["confirmed"] == 1 }
        await f.hub.releaseGet()
        await read.value
        #expect(f.store.prefs.pins == ["new"])
        #expect(f.store.prefs.hidden?["confirmed"] == 1, "the read must not replace the confirmed save with its older snapshot")
        f.events.finish()
    }

    @Test func savesReachTheHubInEditOrder() async throws {
        let f = personalAgentPrefsFixture()
        defer { f.defaults.removePersistentDomain(forName: f.name) }
        await f.store.load(f.api)
        await f.hub.holdNextPut()
        f.store.update(f.api, { $0.togglePin("first") })
        try await waitForPersonalAgentPrefsEvent("put-held", in: f.stream)
        f.store.update(f.api, { $0.togglePin("second") })
        let release = Task {
            try? await Task.sleep(for: .milliseconds(50))
            await f.hub.releasePut()
        }
        await release.value
        try await waitForPersonalAgentPrefsEvent("PUT", in: f.stream)
        try await waitForPersonalAgentPrefsEvent("PUT", in: f.stream)
        await f.store.load(f.api)
        #expect(f.store.prefs.pins == ["second", "first"], "an older full snapshot must not be the last write the hub stores")
        f.events.finish()
    }

    @Test func consecutiveFailuresRestoreTheLastConfirmedValueEvenWhenReloadFails() async throws {
        var original = PersonalAgentPrefs()
        original.pins = ["before"]
        let f = personalAgentPrefsFixture(stored: original)
        defer { f.defaults.removePersistentDomain(forName: f.name) }
        await f.store.load(f.api)
        await f.hub.refuseEveryPut(status: 500)
        await f.hub.refuseGets(status: 500)
        var reports: [String] = []
        f.store.update(f.api, { $0.view.group = AgentGroupBy.none.rawValue }, onFailure: { _ in reports.append("first") })
        f.store.update(f.api, { $0.view.sort = AgentSortBy.name.rawValue }, onFailure: { _ in
            reports.append("second")
            f.events.yield("reported-second")
        })
        try await waitForPersonalAgentPrefsEvent("reported-second", in: f.stream)
        try await waitForPersonalAgentPrefsEvent("GET", in: f.stream)
        #expect(reports == ["second"], "the superseded PUT neither rolls back nor reports over the latest failure")
        #expect(f.store.prefs == original, "no unsaved edit remains even when the recovery GET is unavailable")
        f.events.finish()
    }

    @Test func anOlderFailureDoesNotRollBackANewerEdit() async throws {
        let f = personalAgentPrefsFixture()
        defer { f.defaults.removePersistentDomain(forName: f.name) }
        await f.store.load(f.api)
        await f.hub.refuseNextPut()
        await f.hub.markNextPutResponse()
        var reports: [String] = []
        f.store.update(f.api, { $0.view.group = AgentGroupBy.none.rawValue }, onFailure: { _ in reports.append("first") })
        f.store.update(f.api, { $0.view.sort = AgentSortBy.name.rawValue }, onFailure: { _ in reports.append("second") })
        try await waitForPersonalAgentPrefsEvent("PUT", in: f.stream)
        try await waitForRefreshTest { f.store.prefs.hidden?["confirmed"] == 1 }
        #expect(reports.isEmpty, "the newer full snapshot includes the earlier edit, so its older failure is superseded")
        #expect(f.store.prefs.view.groupBy == .none)
        #expect(f.store.prefs.view.sortBy == .name)
        f.events.finish()
    }

    @Test func cancellationRollsBackWithoutReportingAFailure() async throws {
        var original = PersonalAgentPrefs()
        original.pins = ["before"]
        let f = personalAgentPrefsFixture(stored: original)
        defer { f.defaults.removePersistentDomain(forName: f.name) }
        await f.store.load(f.api)
        await f.hub.cancelNextPut()
        var reported = ""
        f.store.update(f.api, { $0.togglePin("after") }, onFailure: { reported = $0 })
        try await waitForPersonalAgentPrefsEvent("put-cancelled", in: f.stream)
        try await waitForPersonalAgentPrefsEvent("GET", in: f.stream)
        #expect(reported.isEmpty)
        #expect(f.store.prefs == original)
        f.events.finish()
    }

    @Test func anOlderMissingEndpointDoesNotDisableANewerSave() async throws {
        let f = personalAgentPrefsFixture()
        defer { f.defaults.removePersistentDomain(forName: f.name) }
        await f.hub.refuseNextPut(status: 404)
        f.store.update(f.api, { $0.togglePin("first") })
        f.store.update(f.api, { $0.togglePin("second") })
        try await waitForPersonalAgentPrefsEvent("put-failed", in: f.stream)
        try await waitForPersonalAgentPrefsEvent("PUT", in: f.stream)
        await f.hub.refuseGets(status: 500)
        await f.store.load(f.api) // a failed read is a barrier that cannot re-enable hub-backed saves
        f.store.update(f.api, { $0.togglePin("third") })
        try await waitForPersonalAgentPrefsEvent("PUT", in: f.stream)
        f.events.finish()
    }

    @Test(arguments: [404, 405]) func anOlderHubKeepsLocalPreferences(status: Int) async throws {
        let f = personalAgentPrefsFixture(getStatus: status)
        defer { f.defaults.removePersistentDomain(forName: f.name) }
        await f.hub.refuseEveryPut(status: status)
        var reported = ""
        f.store.update(f.api, { $0.togglePin("local") }, onFailure: { reported = $0 })
        try await waitForPersonalAgentPrefsEvent("put-failed", in: f.stream)
        await f.store.load(f.api)
        #expect(reported.isEmpty, "an older hub has no save failure: its preferences are intentionally local")
        #expect(f.store.prefs.pins == ["local"])
        f.events.finish()
    }
}

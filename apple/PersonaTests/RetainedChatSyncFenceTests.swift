import Foundation
import Testing
@testable import Persona

private actor DeferredItemPages {
    enum Failure: Error { case requestDidNotStart, noPendingResponse }
    private var count = 0
    private var pending: [CheckedContinuation<Data, Error>] = []

    func answer(_ request: LocalRequest) async throws -> Data {
        guard request.path.contains("/sessions/box/") && request.path.hasSuffix("/items") else { return Data("{}".utf8) }
        count += 1
        return try await withCheckedThrowingContinuation { pending.append($0) }
    }

    func waitForRequests(_ expected: Int) async throws {
        for _ in 0..<200 {
            if count >= expected && !pending.isEmpty { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        throw Failure.requestDidNotStart
    }

    func release(_ json: String) throws {
        guard !pending.isEmpty else { throw Failure.noPendingResponse }
        pending.removeFirst().resume(returning: Data(json.utf8))
    }
}

private final class MutexBox<Value>: @unchecked Sendable {
    private let lock = NSLock()
    private var value: Value
    init(_ value: Value) { self.value = value }
    func withLock<T>(_ body: (inout Value) -> T) -> T { lock.withLock { body(&value) } }
}

/// Exercise real APIClient decoding and every asynchronous fill/reload boundary with manually released responses.
@MainActor struct RetainedChatSyncFenceTests {
    init() { ProductionContinuityShadow.shared.installForTests() }

    private func item(_ seq: Int) -> Item {
        Item(id: "\(seq)#0", seq: seq, ts: nil, role: "assistant", kind: .text, text: "Synthetic item \(seq)",
             toolUseID: nil, toolName: nil, toolInput: nil, isError: nil, image: nil)
    }

    private func setup() -> (AppModel, SessionKey, ChatStore, UUID, DeferredItemPages) {
        let model = AppModel()
        let key = SessionKey(group: "sync-fence-fixture", machineID: "box", sessionID: UUID().uuidString)
        let store = model.chat(key)
        store.apply([item(1)], span: ItemSpan(history: "old", from: nil, to: 1), backlog: true, hasMore: false)
        let owner = UUID(), gate = DeferredItemPages()
        var client = APIClient(config: ServerConfig(url: URL(string: "https://fake.invalid")!, token: "test-only"))
        client.transport = { request in try await gate.answer(request) }
        model.apiForTests = client
        model.subscribe(key, by: owner)
        return (model, key, store, owner, gate)
    }

    private func page(_ seq: Int, history: String = "old", more: Bool = false) -> String {
        """
        {"items":[{"id":"\(seq)#0","seq":\(seq),"role":"assistant","kind":"text","text":"Synthetic item \(seq)"}],
         "history":"\(history)","from_seq":\(seq - 1),"to_seq":\(seq),"has_more":false,"more_after":\(more)}
        """
    }

    @Test func prefetchReloadCancellationFinishesItsCoreRequest() async throws {
        let (model, key, store, _, gate) = setup()
        // A prefetch that arrives ahead of a replaced transcript uses an independent reload, outside the fill gate.
        _ = store.apply(.batch(.latest, items: [item(1)], span: ItemSpan(history: "replacement", from: 2, to: 3), backlog: true))
        let allowed = MutexBox(true)
        let task = Task { await model.reload(key, shouldApply: { allowed.withLock { $0 } }) }
        try await gate.waitForRequests(1)
        allowed.withLock { $0 = false }
        try await gate.release(page(2, history: "replacement"))
        await task.value
        _ = store.apply(.batch(.pushed, items: [item(5)], span: ItemSpan(history: "replacement", from: 5, to: 5)))
        let result = try await ProductionContinuityShadow.shared.compare(store)
        #expect(result.compared == 3)
        #expect(result.mismatches == 0)
        ProductionContinuityShadow.shared.close(store)
    }

    @Test func anInflightFirstPageCannotApplyAfterTheLastOwnerLeaves() async throws {
        let (model, key, store, owner, gate) = setup()
        let allowed: @MainActor () -> Bool = { model.isSubscribed(key) && model.existingChat(key) === store }
        let task = Task { await model.fill(key, shouldApply: allowed) }
        try await gate.waitForRequests(1)
        model.unsubscribe(key, by: owner)
        try await gate.release(page(2))
        await task.value
        #expect(store.covered == 1 && store.items.map(\.seq) == [1])
    }

    @Test func aLaterFillPageUsesTheSameVisibilityFence() async throws {
        let (model, key, store, owner, gate) = setup()
        let allowed: @MainActor () -> Bool = { model.isSubscribed(key) && model.existingChat(key) === store }
        let task = Task { await model.fill(key, shouldApply: allowed) }
        try await gate.waitForRequests(1)
        try await gate.release(page(2, more: true))
        try await gate.waitForRequests(2)
        #expect(store.covered == 2)
        model.unsubscribe(key, by: owner)
        try await gate.release(page(3))
        await task.value
        #expect(store.covered == 2 && store.items.map(\.seq) == [1, 2])
    }

    @Test func aHistoryReloadDoesNotDropTheVisibilityFence() async throws {
        let (model, key, store, owner, gate) = setup()
        let allowed: @MainActor () -> Bool = { model.isSubscribed(key) && model.existingChat(key) === store }
        let task = Task { await model.fill(key, shouldApply: allowed) }
        try await gate.waitForRequests(1)
        try await gate.release(page(2, history: "replacement"))
        try await gate.waitForRequests(2)
        // The first response arrived while visible: a different history intentionally resets the old transcript.
        #expect(store.covered == 0 && store.items.isEmpty && store.history == "replacement")
        model.unsubscribe(key, by: owner)
        try await gate.release(page(7, history: "replacement"))
        await task.value
        #expect(store.covered == 0 && store.items.isEmpty && store.history == "replacement")
    }

    @Test func anOldReloadCannotOverwriteAReplacementStore() async throws {
        let (model, key, store, _, gate) = setup()
        let allowed: @MainActor () -> Bool = { model.isSubscribed(key) && model.existingChat(key) === store }
        let task = Task { await model.reload(key, shouldApply: allowed) }
        try await gate.waitForRequests(1)
        model.dropChat(key)
        let replacement = model.chat(key)
        replacement.apply([item(42)], span: ItemSpan(history: "current", from: nil, to: 42), backlog: true, hasMore: false)
        let other = UUID()
        model.subscribe(key, by: other)
        try await gate.release(page(2, history: "obsolete"))
        await task.value
        #expect(model.existingChat(key) === replacement)
        #expect(replacement.covered == 42 && replacement.items.map(\.seq) == [42])
        model.unsubscribe(key, by: other)
    }

    @Test func anotherVisibleOwnerKeepsTheInflightCatchUpValid() async throws {
        let (model, key, store, owner, gate) = setup()
        let other = UUID()
        model.subscribe(key, by: other)
        let allowed: @MainActor () -> Bool = { model.isSubscribed(key) && model.existingChat(key) === store }
        let task = Task { await model.fill(key, shouldApply: allowed) }
        try await gate.waitForRequests(1)
        model.unsubscribe(key, by: owner)
        try await gate.release(page(2))
        await task.value
        #expect(store.covered == 2 && store.items.map(\.seq) == [1, 2])
        model.unsubscribe(key, by: other)
    }
}

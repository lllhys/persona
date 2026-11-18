import Foundation
import Testing
@testable import Persona

@MainActor
struct ProductionContinuityShadowTests {
    init() { ProductionContinuityShadow.shared.installForTests() }

    private func item(_ seq: Int, kind: ItemKind = .text) -> Item {
        Item(id: "\(seq)#0", seq: seq, ts: nil, role: "assistant", kind: kind, text: "Synthetic shadow fixture",
             toolUseID: nil, toolName: nil, toolInput: nil, isError: nil, image: nil)
    }

    private func fold(_ seq: Int, through: Int) -> Item {
        var item = item(seq, kind: .fold)
        item.id = "fold:\(seq)#0"
        item.fold = Fold(items: through - seq + 1, tools: through - seq + 1, thoughts: 0, until: nil,
                         throughSeq: through, throughID: "\(through)#0")
        return item
    }

    @Test func productionInputsComparePatchesAndState() async throws {
        let shadow = ProductionContinuityShadow.shared
        let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: "production-shadow"))
        let first = [item(1), fold(2, through: 3)]
        _ = store.apply(.batch(.latest, items: first, span: ItemSpan(history: "h", to: 2), backlog: true, hasMore: true))
        _ = store.apply(.event(.openFold, fold: first[1]))
        _ = store.apply(.range([item(2, kind: .toolUse), item(3, kind: .toolResult)], fold: first[1], more: false))
        shadow.loadOlder(store)
        _ = store.apply(.batch(.older, items: [item(0)], span: ItemSpan(), hasMore: false))
        _ = store.apply(.batch(.pushed, items: [item(4)], span: ItemSpan(history: "h", from: 2, to: 4)))
        let result = try await shadow.compare(store)
        #expect(result.compared >= 5)
        #expect(result.mismatches == 0)
        shadow.close(store)
    }

    @Test func restoredCacheAndIndependentFoldsKeepTheirOrder() async throws {
        let shadow = ProductionContinuityShadow.shared
        let key = SessionKey(machineID: "fixture", sessionID: "shadow-cache")
        let folder = FileManager.default.temporaryDirectory.appending(path: "shadow-cache-\(UUID())", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: folder) }
        ChatCache.save(key, items: [item(1), fold(2, through: 3), fold(5, through: 6)], hasMore: true,
                       history: "h", covered: 3, in: folder)
        let store = ChatStore(key: key, cacheFolder: folder)
        let folds = store.items.filter { $0.kind == .fold }
        _ = store.apply(.batch(.latest, items: store.items, span: ItemSpan(history: "h", to: 3), backlog: true, hasMore: true))
        _ = store.apply(.event(.openFold, fold: folds[0]))
        _ = store.apply(.event(.openFold, fold: folds[1]))
        _ = store.apply(.range([item(5, kind: .toolUse), item(6, kind: .toolResult)], fold: folds[1], more: false))
        _ = store.apply(.range([item(2, kind: .toolUse), item(3, kind: .toolResult)], fold: folds[0], more: false))
        let result = try await shadow.compare(store)
        #expect(result.compared >= 3)
        #expect(result.mismatches == 0)
        shadow.close(store)
    }

    @Test func sameInputTwiceDoesNotLosePatchState() async throws {
        let shadow = ProductionContinuityShadow.shared
        let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: "same-input"))
        let input = NativeContinuityInput.batch(.latest, items: [item(1)], span: ItemSpan(history: "h", to: 1), backlog: true, hasMore: false)
        _ = store.apply(input)
        _ = store.apply(input)
        let result = try await shadow.compare(store)
        #expect(result.mismatches == 0)
        shadow.close(store)
    }

    @Test func diagnosticsHaveOnlyDomainRuleAndShape() async throws {
        ProductionContinuityShadow.shared.installForTests()
        let reports = MutexBox<[ProductionContinuityShadow.Report]>([])
        let shadow = ProductionContinuityShadow { report in reports.withLock { $0.append(report) } }
        let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: "private-identifiers"))
        var secret = item(1)
        secret.text = "synthetic-message-content-that-must-not-be-reported"
        secret.id = "synthetic-item-identifier"
        _ = store.apply(.batch(.latest, items: [secret], span: ItemSpan(history: "h", to: 1), backlog: true, hasMore: false))
        store.setContinuityShadowEnabledForFixtureReplay(false)
        // A native result that deliberately disagrees proves the mismatch reporter rather than a silent comparison.
        var wrong = store.nativeResult()
        wrong = NativeContinuityResult(items: [], covered: 999, history: "synthetic-history", hasMore: true, loadedOnce: false)
        shadow.accept(.batch(.pushed, items: [item(2)], span: ItemSpan(history: "h", from: 1, to: 2)),
                      into: store, snapshot: store.nativeSnapshot(), native: wrong, gap: nil)
        _ = try await shadow.compare(store)
        let data = try JSONEncoder().encode(reports.withLock { $0 })
        let text = try #require(String(data: data, encoding: .utf8))
        for forbidden in [secret.text ?? "", secret.id, "synthetic-history", "private-identifiers", "999"] {
            #expect(!text.contains(forbidden))
        }
        #expect(reports.withLock { $0.allSatisfy { $0.domain == "continuity" && !$0.rule.isEmpty && $0.inputShape.contains("kind=pushed") } })
        ProductionContinuityShadow.shared.close(store)
    }

    @Test func directOlderResponseDoesNotInventASchedulingRequest() async throws {
        let shadow = ProductionContinuityShadow.shared
        let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: "direct-older"))
        _ = store.apply(.batch(.latest, items: [item(2)], span: ItemSpan(history: "h", to: 2), backlog: true, hasMore: true))
        // ChatView.jumpToInput calls prepend directly; there is no AppModel.loadOlder scheduling event.
        store.prepend([item(1)], hasMore: false)
        let result = try await shadow.compare(store)
        #expect(result.compared == 2)
        #expect(result.mismatches == 0)
        shadow.close(store)
    }

    @Test func pushWhileFillResponseIsOutstandingKeepsItsEvidence() async throws {
        let shadow = ProductionContinuityShadow.shared
        let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: "outstanding-fill"))
        _ = store.apply(.batch(.latest, items: [item(1)], span: ItemSpan(history: "h", to: 1), backlog: true))
        _ = store.apply(.batch(.pushed, items: [item(3)], span: ItemSpan(history: "h", from: 2, to: 3)))
        // Another push while the first fetchAfter response is held must reuse, not expire, the in-flight request.
        _ = store.apply(.batch(.pushed, items: [item(4)], span: ItemSpan(history: "h", from: 3, to: 4)))
        let result = try await shadow.compare(store)
        #expect(result.compared == 3)
        #expect(result.mismatches == 0)
        shadow.close(store)
    }

    @Test func completedFillThenGapUsesItsOwnAfterRequest() async throws {
        ProductionContinuityShadow.shared.installForTests()
        let reports = MutexBox<[ProductionContinuityShadow.Report]>([])
        let shadow = ProductionContinuityShadow { report in reports.withLock { $0.append(report) } }
        let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: "completed-fill"))
        store.setContinuityShadowEnabledForFixtureReplay(false)
        func mirror(_ input: NativeContinuityInput) {
            let before = store.nativeSnapshot()
            let gap = store.applyNativeOnly(input)
            shadow.accept(input, into: store, snapshot: before, native: store.nativeResult(), gap: gap)
        }
        mirror(.batch(.latest, items: [item(1)], span: ItemSpan(history: "h", to: 1), backlog: true, hasMore: false))
        mirror(.batch(.pushed, items: [item(3)], span: ItemSpan(history: "h", from: 1, to: 3)))
        mirror(.batch(.after, items: [item(2), item(3)], span: ItemSpan(history: "h", from: 0, to: 3)))
        // The completed catch-up clears filling; this skipped sequence must request another page, not reuse an old one.
        let pushed = NativeContinuityInput.batch(.pushed, items: [item(5)], span: ItemSpan(history: "h", from: 5, to: 5))
        let before = store.nativeSnapshot()
        let gap = store.applyNativeOnly(pushed)
        #expect(gap == .fill)
        shadow.accept(pushed, into: store, snapshot: before, native: store.nativeResult(), gap: gap)
        let result = try await shadow.compare(store)
        #expect(result.compared == 4)
        #expect(reports.withLock { $0.allSatisfy { $0.domain == "continuity" && $0.rule != "missing-request" } })
        #expect(result.mismatches == 0)
        ProductionContinuityShadow.shared.close(store)
    }

    @Test func independentFoldCompletionDoesNotEraseAnotherOutstandingFold() async throws {
        ProductionContinuityShadow.shared.installForTests()
        let shadow = ProductionContinuityShadow.shared
        let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: "independent-fold-state"))
        let first = fold(2, through: 3), second = fold(5, through: 6), third = fold(8, through: 9)
        _ = store.apply(.batch(.latest, items: [first, second, third], span: ItemSpan(), backlog: true))
        _ = store.apply(.event(.openFold, fold: first))
        _ = store.apply(.event(.openFold, fold: second))
        _ = store.apply(.event(.openFold, fold: third))
        // A delayed completion for one fold must not clear the other outstanding fold's scheduling evidence.
        _ = store.apply(.range([item(2, kind: .toolUse), item(3, kind: .toolResult)], fold: first, more: false))
        _ = store.apply(.range([item(5, kind: .toolUse), item(6, kind: .toolResult)], fold: second, more: false))
        _ = store.apply(.range([item(8, kind: .toolUse), item(9, kind: .toolResult)], fold: third, more: false))
        let result = try await shadow.compare(store)
        #expect(result.compared == 7)
        #expect(result.mismatches == 0)
        shadow.close(store)
    }

    @Test func completedPrefetchThenNewGapUsesItsOwnAfterRequest() async throws {
        let shadow = ProductionContinuityShadow.shared
        let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: "prefetch-fill"))
        _ = store.apply(.batch(.pushed, items: [item(3)], span: ItemSpan(history: "h", from: 1, to: 3)))
        // A prefetch can complete the core fill. It does not imply another native request after this synchronous call.
        _ = store.apply(.batch(.after, items: [item(2), item(3)], span: ItemSpan(history: "h", from: 0, to: 3)))
        let pushed = NativeContinuityInput.batch(.pushed, items: [item(5)], span: ItemSpan(history: "h", from: 5, to: 5))
        #expect(store.apply(pushed) == .fill)
        let result = try await shadow.compare(store)
        #expect(result.compared == 3)
        #expect(result.mismatches == 0)
        shadow.close(store)
    }

    @Test func failureAfterAReceivedPageClearsTheOutstandingCoreRequest() async throws {
        let folder = FileManager.default.temporaryDirectory.appending(path: "shadow-fill-failure-\(UUID())", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: folder) }
        let model = AppModel(stateFolder: folder)
        model.linksUpForTests = false
        model.connectOnly(.demo)
        let key = SessionKey(machineID: "fixture", sessionID: "fill-failure-after-page")
        let store = model.chat(key)
        _ = store.apply(.batch(.latest, items: [item(1)], span: ItemSpan(history: "h", from: 0, to: 1), backlog: true))
        _ = store.apply(.batch(.pushed, items: [item(3)], span: ItemSpan(history: "h", from: 2, to: 3)))
        let responses = ContinuityFillResponses()
        var client = APIClient(config: ServerConfig(url: URL(string: "https://fake.invalid")!, token: "test-only"))
        client.transport = { [weak store] request in
            await responses.setNextPage(store?.covered ?? 0)
            return try await responses.answer(request)
        }
        model.apiForTests = client
        let allowed = MutexBox(true)
        let first = Task { await model.fill(key, shouldApply: { allowed.withLock { $0 } }) }
        try await responses.waitForRequests(1)
        allowed.withLock { $0 = false }
        try await responses.release(after: 1, items: [item(2)], from: 1, to: 2, moreAfter: true)
        await first.value
        // A later fence cancellation must clear the core request even though its first response arrived.
        _ = store.apply(.batch(.pushed, items: [item(5)], span: ItemSpan(history: "h", from: 5, to: 5)))
        let result = try await ProductionContinuityShadow.shared.compare(store)
        #expect(result.compared == 4)
        #expect(result.mismatches == 0)
        ProductionContinuityShadow.shared.close(store)
    }

    @Test func prefetchAcrossAnOccupiedNativeFillGateKeepsActualRequestEvidence() async throws {
        let folder = FileManager.default.temporaryDirectory.appending(path: "shadow-fill-gate-\(UUID())", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: folder) }
        let model = AppModel(stateFolder: folder)
        model.linksUpForTests = false
        model.connectOnly(.demo)
        let key = SessionKey(machineID: "fixture", sessionID: "occupied-fill-gate")
        let store = model.chat(key)
        _ = store.apply(.batch(.latest, items: [item(1)], span: ItemSpan(history: "h", from: 0, to: 1), backlog: true))
        _ = store.apply(.batch(.pushed, items: [item(3)], span: ItemSpan(history: "h", from: 2, to: 3)))
        let responses = ContinuityFillResponses()
        var client = APIClient(config: ServerConfig(url: URL(string: "https://fake.invalid")!, token: "test-only"))
        client.transport = { [weak store] request in
            await responses.setNextPage(store?.covered ?? 0)
            return try await responses.answer(request)
        }
        model.apiForTests = client

        let first = Task { await model.fill(key) }
        try await responses.waitForRequests(1)
        // An independent HTTP prefetch completes the gap while the first native request remains outstanding.
        #expect(model.applyPrefetch(store, items: [item(2), item(3)], span: ItemSpan(history: "h", from: 1, to: 3), moreAfter: false) == .none)
        await model.fill(key) // The existing native fill owns this gate; there is no second transport request.
        #expect(await responses.count == 1)
        try await responses.release(after: 1, items: [item(2)], from: 1, to: 2)
        await first.value
        // Once the prefetch and the native fill have finished, this new gap needs its own core request.
        _ = store.apply(.batch(.pushed, items: [item(5)], span: ItemSpan(history: "h", from: 5, to: 5)))
        let result = try await ProductionContinuityShadow.shared.compare(store)
        #expect(store.items.map(\.seq) == [1, 2, 3, 5])
        #expect(result.compared == 6)
        #expect(result.mismatches == 0)
        ProductionContinuityShadow.shared.close(store)
    }

    @Test func lateInputForAClosedStoreCannotRecreateItsShadow() async throws {
        let shadow = ProductionContinuityShadow()
        shadow.installForTests()
        var store: ChatStore? = ChatStore(key: SessionKey(machineID: "fixture", sessionID: "retired-shadow"))
        _ = store?.apply(.batch(.latest, items: [item(1)], span: ItemSpan(history: "h", to: 1), backlog: true))
        if let store {
            shadow.accept(.event(.loadOlder), into: store, snapshot: store.nativeSnapshot(), native: store.nativeResult(), gap: nil)
            shadow.close(store)
        }
        let input = NativeContinuityInput.batch(.pushed, items: [item(3)], span: ItemSpan(history: "h", from: 2, to: 3))
        if let store {
            shadow.accept(input, into: store, snapshot: store.nativeSnapshot(), native: store.nativeResult(), gap: .fill)
        }
        weak var retired = store
        store = nil
        shadow.waitForTests()
        #expect(retired == nil)
        #expect(shadow.debugStateForTests.chats == 0, "unexpected lifecycle state: \(shadow.debugStateForTests)")
    }

    @Test func disabledShadowDoesNotCreateAClientForDirectSchedulingInputs() {
        let shadow = ProductionContinuityShadow()
        let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: "disabled-shadow"))
        shadow.install(enabled: false)
        _ = store.apply(.batch(.latest, items: [item(1)], span: ItemSpan(history: "h", to: 1), backlog: true, hasMore: true))
        shadow.loadOlder(store)
        shadow.failed(.fillFailed, in: store)
        #expect(shadow.coreClientForTests == nil)
    }

    @Test func diagnosticShapeCacheIsBounded() {
        let shadow = ProductionContinuityShadow()
        for count in 0..<600 {
            shadow.recordReportForTests(rule: "test-shape", shape: "items=\(count)")
        }
        #expect(shadow.reportShapeCountForTests <= 256)
    }

    @Test func shapeCountsAndPresenceFlagsNeverCarrySequences() {
        let input = NativeContinuityInput.batch(.pushed, items: [item(982451653)],
            span: ItemSpan(history: "private-history", from: 1234567, to: 9876543), backlog: true, hasMore: nil, moreAfter: true)
        let shape = ProductionContinuityShadow.shape(input)
        #expect(shape.contains("kind=pushed"))
        #expect(shape.contains("items=1"))
        #expect(shape.contains("history=set"))
        #expect(!shape.contains("private-history"))
        #expect(!shape.contains("1234567"))
        #expect(!shape.contains("982451653"))
    }
}

private actor ContinuityFillResponses {
    enum Failure: Error { case timeout, wrongRequest }
    private var pending: [(after: Int, resume: CheckedContinuation<Data, Error>)] = []
    private var requests = 0
    var nextPage = 0
    var count: Int { pending.count }
    func setNextPage(_ value: Int) { nextPage = value }

    func answer(_ request: LocalRequest) async throws -> Data {
        guard request.path.hasSuffix("/items"), let value = request.query["after_seq"], let after = Int(value) else {
            throw Failure.wrongRequest
        }
        requests += 1
        if requests > 2 {
            return try JSONSerialization.data(withJSONObject: [
                "items": [], "history": "h", "from_seq": nextPage, "to_seq": nextPage, "more_after": false,
            ])
        }
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { resume in pending.append((after, resume)) }
        } onCancel: {
            Task { await cancelAll() }
        }
    }

    func waitForRequests(_ expected: Int) async throws {
        for _ in 0..<200 {
            if requests >= expected { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        await cancelAll()
        throw Failure.timeout
    }

    func release(after: Int, items: [Item], from: Int, to: Int, moreAfter: Bool = false) throws {
        guard let index = pending.firstIndex(where: { $0.after == after }) else { throw Failure.wrongRequest }
        let pending = pending.remove(at: index)
        let encoded = try items.map { try JSONSerialization.jsonObject(with: JSONEncoder().encode($0)) }
        pending.resume.resume(returning: try JSONSerialization.data(withJSONObject: [
            "items": encoded, "history": "h", "from_seq": from, "to_seq": to, "more_after": moreAfter,
        ]))
    }

    private func cancelAll() {
        for request in pending { request.resume.resume(throwing: Failure.timeout) }
        pending.removeAll()
    }
}

private final class MutexBox<Value>: @unchecked Sendable {
    private let lock = NSLock()
    private var value: Value
    init(_ value: Value) { self.value = value }
    func withLock<T>(_ body: (inout Value) -> T) -> T { lock.withLock { body(&value) } }
}

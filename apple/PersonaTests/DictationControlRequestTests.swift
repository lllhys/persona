import Foundation
import SwiftUI
import Testing
@testable import Persona

/// Dictate to Persona's navigation and one-shot request, with no network or microphone (`FakeHub`, `FakeDictation`).
@MainActor @Suite(.serialized) struct DictationControlRequestTests {
    @Test func intentOpensANewConversationAndLeavesTheRequestBeforeTheShellExists() throws {
        let model = try Self.model()
        defer { Self.close(model) }
        let previous = SessionKey(machineID: "m", sessionID: "previous")
        model.selectedTab = .chats
        model.shellSettings = true
        model.assistantCurrent = previous

        OpenPersonaDictationIntent.open(model)

        #expect(model.tabRouter == nil) // cold launch: no shell or starter has appeared yet
        #expect(model.selectedTab == .assistant)
        #expect(!model.shellSettings)
        #expect(model.assistantCurrent == nil)
        #expect(model.dictationRequest != nil)
        #expect(UserDefaults.standard.string(forKey: "assistant.current") == "")
    }

    @Test func aColdAssistantRefreshKeepsTheNewConversationAndItsRequest() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant", json: #"{"machine_id":"m","launcher":"claude","cwd":"/","conversations":[{"machine_id":"m","session_id":"main","status":"idle","assistant":true}]}"#)
        let model = try Self.model(hub)
        defer { Self.close(model) }
        OpenPersonaDictationIntent.open(model)
        let request = try #require(model.dictationRequest)

        await model.refreshAssistant()

        #expect(model.assistantCurrent == nil)
        #expect(model.dictationRequest == request)
        #expect(hub.sent("GET", "/api/v1/assistant").count == 1)
    }

    @Test func aRefreshStartedBeforeTheControlCannotRestoreTheMainConversation() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant", json: #"{"machine_id":"m","launcher":"claude","cwd":"/","main":"main","conversations":[{"machine_id":"m","session_id":"main","status":"idle","assistant":true}]}"#)
        let model = try Self.model(hub)
        defer { Self.close(model) }
        await model.refreshAssistant()
        let refreshStartedAt = model.assistantNewChat

        OpenPersonaDictationIntent.open(model)
        model.selectAssistantMain(after: refreshStartedAt)

        #expect(model.assistantMain != nil)
        #expect(model.assistantCurrent == nil)
        #expect(model.dictationRequest != nil)
    }

    @Test func theControlRoutesToTheStarterThroughAnIPadRouter() throws {
        let model = try Self.model()
        defer { Self.close(model) }
        let pad = PadRouter()
        pad.attach(to: model)
        defer { pad.detach(from: model) }

        OpenPersonaDictationIntent.open(model)

        #expect(pad.request?.page == .newConversation)
        #expect(model.assistantCurrent == nil)
        #expect(model.dictationRequest != nil)
    }

    @Test func plainNewChatRoutesThroughBothRoutersAndClearsWithoutOne() throws {
        let previous = SessionKey(machineID: "m", sessionID: "previous")

        let phone = try Self.model()
        defer { Self.close(phone) }
        let shell = ShellState()
        shell.attach(to: phone)
        defer { shell.detach(from: phone) }
        shell.drawer.progress = 1
        phone.assistantCurrent = previous
        phone.openNewAssistant()
        #expect(shell.request?.page == .newConversation)
        #expect(!shell.drawer.isOpen)
        #expect(phone.selectedTab == .assistant)

        let wide = try Self.model()
        defer { Self.close(wide) }
        let pad = PadRouter()
        pad.attach(to: wide)
        defer { pad.detach(from: wide) }
        wide.assistantCurrent = previous
        wide.openNewAssistant()
        #expect(pad.request?.page == .newConversation)
        #expect(wide.selectedTab == .assistant)

        let cold = try Self.model()
        defer { Self.close(cold) }
        cold.assistantCurrent = previous
        cold.openNewAssistant()
        #expect(cold.tabRouter == nil)
        #expect(cold.assistantCurrent == nil)
        #expect(cold.selectedTab == .assistant)
    }

    @Test func theNewStarterTakesTheRequestOnlyOnce() throws {
        let model = try Self.model()
        defer { Self.close(model) }
        OpenPersonaDictationIntent.open(model)
        let request = try #require(model.dictationRequest)
        let dictation = FakeDictation()
        let bar = DictationBarModel()

        for _ in 0..<2 {
            if model.takeDictationRequest(request, uploadDest: nil) {
                bar.start(dictation, hints: [], reduceMotion: true)
            }
        }

        #expect(dictation.starts == 1)
        #expect(bar.on)
        #expect(model.dictationRequest == nil)
        #expect(!model.takeDictationRequest(request, uploadDest: nil))
    }

    @Test func anotherStarterOrAStaleRequestCannotTakeIt() throws {
        let model = try Self.model()
        defer { Self.close(model) }
        OpenPersonaDictationIntent.open(model)
        let request = try #require(model.dictationRequest)
        let dictation = FakeDictation()

        #expect(!model.takeDictationRequest(request, uploadDest: Session.healthSpace))
        #expect(!model.takeDictationRequest(DictationRequest(), uploadDest: nil))
        #expect(dictation.starts == 0)
        #expect(model.dictationRequest == request)
        #expect(model.takeDictationRequest(request, uploadDest: nil))
    }

    @Test func openingAnExistingConversationDropsTheRequestWithoutStarting() throws {
        let model = try Self.model()
        defer { Self.close(model) }
        OpenPersonaDictationIntent.open(model)
        let request = try #require(model.dictationRequest)

        model.assistantCurrent = SessionKey(machineID: "m", sessionID: "existing")

        #expect(model.dictationRequest == nil)
        #expect(!model.takeDictationRequest(request, uploadDest: nil))

        // Defend the boundary itself too: even a wrongly installed request cannot be taken by an existing conversation.
        let misplaced = DictationRequest()
        model.dictationRequest = misplaced
        #expect(!model.takeDictationRequest(misplaced, uploadDest: nil))
        #expect(model.dictationRequest == nil)
    }

    @Test func aFailedStartRestoresFocusAndCannotBeRetried() throws {
        let model = try Self.model()
        defer { Self.close(model) }
        OpenPersonaDictationIntent.open(model)
        let request = try #require(model.dictationRequest)
        let dictation = FakeDictation(failure: "synthetic permission denied")
        let bar = DictationBarModel()
        var focused = false

        #expect(model.takeDictationRequest(request, uploadDest: nil))
        bar.start(dictation, hints: [], reduceMotion: true, onFailure: { focused = true })

        #expect(dictation.starts == 1)
        #expect(focused)
        #expect(!bar.on)
        #expect(model.dictationRequest == nil)
        #expect(!model.takeDictationRequest(request, uploadDest: nil))
    }

    @Test func leavingAnActiveBarPreservesTheTypedAndSpokenDraftExactlyOnce() {
        let dictation = FakeDictation(heard: "spoken words")
        let bar = DictationBarModel()
        var draft = "typed"
        let text = Binding(get: { draft }, set: { draft = $0 })
        bar.start(dictation, hints: [], reduceMotion: true)

        bar.reset(dictation, text: text)
        #expect(draft == "typed spoken words")
        #expect(!bar.on)

        bar.reset(dictation, text: text)
        #expect(draft == "typed spoken words")
    }

    @Test func leavingAfterCancelAppendsNothing() {
        let dictation = FakeDictation(heard: "discarded words")
        let bar = DictationBarModel()
        var draft = "typed"
        let text = Binding(get: { draft }, set: { draft = $0 })
        bar.start(dictation, hints: [], reduceMotion: true)

        bar.cancel(dictation, reduceMotion: true)
        bar.reset(dictation, text: text)

        #expect(draft == "typed")
        #expect(dictation.stops == [true])
        #expect(!bar.on)
    }

    @Test func leavingAfterAFailedStartAppendsNothing() {
        let dictation = FakeDictation(failure: "synthetic permission denied", heard: "not heard")
        let bar = DictationBarModel()
        var draft = "typed"
        let text = Binding(get: { draft }, set: { draft = $0 })

        bar.start(dictation, hints: [], reduceMotion: true)
        bar.reset(dictation, text: text)

        #expect(draft == "typed")
        #expect(!bar.on)
    }

    @Test func leavingAfterStopDoesNotAppendTheWordsAgain() async {
        let dictation = FakeDictation(heard: "spoken words")
        let bar = DictationBarModel()
        var draft = "typed"
        let text = Binding(get: { draft }, set: { draft = $0 })
        bar.start(dictation, hints: [], reduceMotion: true)

        bar.end(.stop, dictation, text: text, reduceMotion: true, send: { false })
        for _ in 0..<100 where bar.on {
            try? await Task.sleep(for: .milliseconds(10))
        }
        #expect(draft == "typed spoken words")
        #expect(dictation.finishes == [true])
        #expect(!bar.on)

        bar.reset(dictation, text: text)
        #expect(draft == "typed spoken words")
    }

    @Test func leavingDuringSendWaitAfterJoinDoesNotAppendWordsAgain() async {
        let dictation = FakeDictation(heard: "spoken words")
        let bar = DictationBarModel()
        var draft = "typed"
        var sendCalls = 0
        let text = Binding(get: { draft }, set: { draft = $0 })
        bar.start(dictation, hints: [], reduceMotion: true)

        // The send has accepted the joined draft but not cleared it yet: the bar stays on for its short hand-off window.
        bar.end(.send, dictation, text: text, reduceMotion: true, send: { sendCalls += 1; return true })
        for _ in 0..<1000 where sendCalls == 0 { await Task.yield() }
        #expect(sendCalls == 1)
        #expect(draft == "typed spoken words")
        #expect(bar.on)
        #expect(bar.ending == .send)

        bar.reset(dictation, text: text)
        #expect(draft == "typed spoken words")
        #expect(!bar.on)
    }

    @Test func theNewestNewChatCompletionAdoptsNavigatesAndClearsAsBefore() {
        let op = UUID()
        let decision = AssistantStartDecision(startGeneration: 4, currentGeneration: 4, pendingOp: op, op: op,
                                              succeeded: true, space: nil, showsPage: true)

        #expect(decision.began)
        #expect(decision.open)
        #expect(decision.haptic == .success)
        #expect(decision.clearPending)
        #expect(decision.failure == .clear)
    }

    @Test func aCompletionOvertakenByNewerNewChatHasNoVisibleFeedback() {
        let op = UUID()
        let accepted = AssistantStartDecision(startGeneration: 4, currentGeneration: 5, pendingOp: op, op: op,
                                              succeeded: true, space: nil, showsPage: true)
        #expect(accepted.began)
        #expect(!accepted.open)
        #expect(accepted.haptic == nil)
        #expect(accepted.clearPending)
        #expect(accepted.failure == .none)

        let failed = AssistantStartDecision(startGeneration: 4, currentGeneration: 5, pendingOp: op, op: op,
                                            succeeded: false, space: nil, showsPage: true)
        #expect(!failed.began)
        #expect(!failed.open)
        #expect(failed.haptic == nil)
        #expect(failed.clearPending)
        #expect(failed.failure == .none)
    }

    @Test func aLateCompletionNeverClearsANewerSendsBubble() {
        let op = UUID(), newer = UUID()
        for succeeded in [true, false] {
            let decision = AssistantStartDecision(startGeneration: 4, currentGeneration: 4, pendingOp: newer, op: op,
                                                  succeeded: succeeded, space: nil, showsPage: true)
            #expect(!decision.clearPending)
        }
    }

    @Test func noInterveningNewChatMatchesEveryOriginalCompletionOutcome() {
        struct Case {
            let succeeded: Bool, space: String?, showsPage: Bool, open: Bool
            let haptic: AssistantStartDecision.Haptic, failure: AssistantStartDecision.FailureAction
        }
        let cases = [
            Case(succeeded: true, space: nil, showsPage: true, open: true, haptic: .success, failure: .clear),
            Case(succeeded: true, space: nil, showsPage: false, open: false, haptic: .success, failure: .clear),
            Case(succeeded: true, space: Session.healthSpace, showsPage: true, open: true, haptic: .success, failure: .clear),
            Case(succeeded: true, space: Session.healthSpace, showsPage: false, open: true, haptic: .success, failure: .clear),
            Case(succeeded: false, space: nil, showsPage: true, open: false, haptic: .error, failure: .show),
            Case(succeeded: false, space: nil, showsPage: false, open: false, haptic: .error, failure: .record),
            Case(succeeded: false, space: Session.healthSpace, showsPage: true, open: false, haptic: .error, failure: .show),
            Case(succeeded: false, space: Session.healthSpace, showsPage: false, open: false, haptic: .error, failure: .show),
        ]
        let op = UUID()
        for c in cases {
            let decision = AssistantStartDecision(startGeneration: 7, currentGeneration: 7, pendingOp: op, op: op,
                                                  succeeded: c.succeeded, space: c.space, showsPage: c.showsPage)
            #expect(decision.began == c.succeeded)
            #expect(decision.open == c.open)
            #expect(decision.haptic == c.haptic)
            #expect(decision.clearPending)
            #expect(decision.failure == c.failure)
        }
    }

    private static func model(_ hub: FakeHub = FakeHub()) throws -> AppModel {
        let model = AppModel(stateFolder: FileManager.default.temporaryDirectory.appending(path: "dictation-control-" + UUID().uuidString))
        model.linkHooksForTests = (transport: try #require(hub.client.transport), socket: { SyncSocket() })
        model.connectOnly(ServerConfig(url: hub.url, token: "t"))
        return model
    }

    private static func close(_ model: AppModel) {
        for link in model.links { link.live.disconnect() }
    }
}

@MainActor private final class FakeDictation: DictationStarting {
    private(set) var starts = 0
    private(set) var stops: [Bool] = []
    private(set) var finishes: [Bool] = []
    let failure: String?
    let heard: String

    init(failure: String? = nil, heard: String = "") {
        self.failure = failure
        self.heard = heard
    }

    func start(hints: [String], update: @escaping (String) -> Void, failed: @escaping (String) -> Void) {
        starts += 1
        if let failure { failed(failure) }
    }

    func finish(settling: Bool) async -> String {
        finishes.append(settling)
        return heard
    }

    func stop(settling: Bool) {
        stops.append(settling)
    }
}

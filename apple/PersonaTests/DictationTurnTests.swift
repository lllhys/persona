import SwiftUI
import Testing
@testable import Persona

/// A start of dictation waits while the system asks for the microphone; what the person does in that wait wins over
/// it (#3407, the orderings `apple/verify/composer-hold/` found). The sample stands in for the microphone and
/// `PermissionGate` for the system's question. Serialized: they share `RecordingMicrophone.shared`.
@MainActor @Suite(.serialized) struct DictationTurnTests {
    /// Finding 1: a tap, then a hold while the tap's start still waits. The hold dictates; no "microphone is in use"
    /// (which would end the hold), and the tap's start never writes into the draft.
    @Test func aHoldDuringATapsWaitTakesOver() async throws {
        let gate = PermissionGate()
        let dictation = Self.dictation(gate)
        defer { dictation.stop() }
        var text = "draft"
        let binding = Binding(get: { text }, set: { text = $0 })
        dictation.toggle(into: binding)
        let hold = ComposerHold()
        hold.begin(dictation, text: binding, reduceMotion: true)
        try await Self.waitUntil { gate.asks == 2 }
        gate.answer()
        try await Self.waitUntil { hold.heard == "one" }
        #expect(hold.holding)
        #expect(dictation.isListening)
        #expect(text == "draft")
        hold.finish(.keep, dictation, text: binding, reduceMotion: true)
        #expect(!dictation.isListening)
        #expect(text == "draft one")
    }

    /// The tap's start got in first and listens: the hold takes the microphone over from it instead of failing.
    @Test func aHoldTakesOverFromATapAlreadyListening() async throws {
        let dictation = Self.dictation(nil)
        defer { dictation.stop() }
        var text = ""
        let binding = Binding(get: { text }, set: { text = $0 })
        dictation.toggle(into: binding)
        try await Self.waitUntil { dictation.isListening }
        let hold = ComposerHold()
        hold.begin(dictation, text: binding, reduceMotion: true)
        try await Self.waitUntil { hold.heard == "one" }
        #expect(hold.holding)
        #expect(text.isEmpty) // the tap's dictation stopped before its first word
    }

    /// Finding 2: a send (or the composer going away) while a tapped mic still waits ends that start: it never listens,
    /// so nothing writes into the emptied composer, and it says nothing.
    @Test func stopEndsAStartStillWaiting() async throws {
        let gate = PermissionGate()
        let dictation = Self.dictation(gate)
        defer { dictation.stop() }
        var heard = 0, failures = 0
        dictation.start(update: { _ in heard += 1 }, failed: { _ in failures += 1 })
        try await Self.waitUntil { gate.asks == 1 }
        dictation.stop()
        gate.answer()
        try await Self.settle()
        #expect(!dictation.isListening)
        #expect(heard == 0 && failures == 0)
        #expect(Self.microphoneIsFree())
    }

    /// A hold let go while the microphone is still asked for: dictation stays off, as the person left it.
    @Test func aHoldLetGoInTheWaitNeverListens() async throws {
        let gate = PermissionGate()
        let dictation = Self.dictation(gate)
        defer { dictation.stop() }
        var text = "draft"
        let binding = Binding(get: { text }, set: { text = $0 })
        let hold = ComposerHold()
        hold.begin(dictation, text: binding, reduceMotion: true)
        try await Self.waitUntil { gate.asks == 1 }
        hold.finish(.keep, dictation, text: binding, reduceMotion: true)
        gate.answer()
        try await Self.settle()
        #expect(!dictation.isListening)
        #expect(!hold.holding)
        #expect(text == "draft")
        #expect(Self.microphoneIsFree())
    }

    /// A refusal is said only by the start whose turn it still is; one a newer start replaced says nothing.
    @Test func onlyTheNewestStartSaysItWasRefused() async throws {
        let gate = PermissionGate()
        let dictation = Self.dictation(gate)
        defer { dictation.stop() }
        var older = 0, newer = 0
        dictation.start(update: { _ in }, failed: { _ in older += 1 })
        dictation.start(update: { _ in }, failed: { _ in newer += 1 })
        try await Self.waitUntil { gate.asks == 2 }
        gate.answer(false)
        try await Self.waitUntil { newer == 1 }
        try await Self.settle()
        #expect(older == 0)
        #expect(!dictation.isListening)
    }

    private static func dictation(_ gate: PermissionGate?) -> Dictation {
        let dictation = Dictation()
        dictation.sample = "one two three"
        if let gate { dictation.permission = { await gate.ask() } }
        return dictation
    }

    private static func microphoneIsFree() -> Bool {
        let probe = UUID()
        defer { RecordingMicrophone.shared.release(probe) }
        return RecordingMicrophone.shared.claim(probe)
    }

    /// Lets the starts' tasks run to their end (a sample's first word comes after 350 ms).
    private static func settle() async throws { try await Task.sleep(for: .milliseconds(500)) }

    private static func waitUntil(_ condition: () -> Bool) async throws {
        for _ in 0..<300 {
            if condition() { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        Issue.record("timed out")
        throw CancellationError()
    }
}

/// The system's question for the microphone, held open until the test answers it.
@MainActor private final class PermissionGate {
    private var waiting: [CheckedContinuation<Bool, Never>] = []
    private(set) var asks = 0

    func ask() async -> Bool {
        asks += 1
        return await withCheckedContinuation { waiting.append($0) }
    }

    func answer(_ granted: Bool = true) {
        let answered = waiting
        waiting = []
        for continuation in answered { continuation.resume(returning: granted) }
    }
}

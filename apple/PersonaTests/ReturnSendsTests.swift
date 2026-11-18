import GameController
import SwiftUI
import Testing
@testable import Persona

/// Return Key Sends (#8178): which edits are Return typed, and what a hardware Return does with modifiers held.
struct ReturnSendsTests {
    @Test func oneLineBreakTypedAnywhereIsReturn() {
        #expect(ReturnSends.breakTyped(from: "hello", to: "hello\n"))
        #expect(ReturnSends.breakTyped(from: "hello", to: "he\nllo"))
        #expect(ReturnSends.breakTyped(from: "hello", to: "\nhello"))
        #expect(ReturnSends.breakTyped(from: "", to: "\n"))
        // A line break after another: still the one key.
        #expect(ReturnSends.breakTyped(from: "a\n", to: "a\n\n"))
        #expect(ReturnSends.breakTyped(from: "你好", to: "你好\n"))
    }

    @Test func otherEditsAreNotReturn() {
        #expect(!ReturnSends.breakTyped(from: "hello", to: "hello!"))
        #expect(!ReturnSends.breakTyped(from: "hello", to: "hello"))
        #expect(!ReturnSends.breakTyped(from: "hello\n", to: "hello"))
        // A paste that brings a line break and more, or a break that replaces a selection.
        #expect(!ReturnSends.breakTyped(from: "hello", to: "hello\nworld"))
        #expect(!ReturnSends.breakTyped(from: "hello", to: "hell\n"))
        // The keyboard confirming its composition (Pinyin's letters become characters): no line break.
        #expect(!ReturnSends.breakTyped(from: "ni", to: "你"))
        #expect(!ReturnSends.breakTyped(from: "nihao", to: "nihao "))
    }

    /// Review of 996f8a2: edits whose common prefix runs into an old line break return false without reading past the end.
    @Test func editsAroundAnExistingLineBreakStayInBounds() {
        #expect(!ReturnSends.breakTyped(from: "\n", to: "a\n"))
        #expect(!ReturnSends.breakTyped(from: "a\n", to: "ab\n"))
        #expect(!ReturnSends.breakTyped(from: "\n", to: "\na"))
        #expect(ReturnSends.breakTyped(from: "\n", to: "\n\n"))
        #expect(!ReturnSends.breakTyped(from: "", to: "a"))
        #expect(!ReturnSends.breakTyped(from: "é", to: "é\u{301}"))
    }

    /// Review of 996f8a2: a line break that arrives while the keyboard still has marked text (a Japanese or Chinese
    /// keyboard confirming its composition) never sends; the same edit without marked text does.
    @Test func aLineBreakWhileComposingNeverSends() {
        #expect(!ReturnSends.breakSends(from: "あ", to: "あ\n", composing: true))
        #expect(!ReturnSends.breakSends(from: "ni", to: "ni\n", composing: true))
        #expect(ReturnSends.breakSends(from: "あ", to: "あ\n", composing: false))
    }

    @Test func aLetThroughOrPausedLineBreakDoesNotSend() {
        #expect(!ReturnSends.breakSends(from: "a", to: "a\n", composing: false, allowed: true))
        #expect(!ReturnSends.breakSends(from: "a", to: "a\n", composing: false, paused: true))
        #expect(!ReturnSends.breakSends(from: "a", to: "ab", composing: false))
    }

    /// Marked text is asked for only once a line break was typed (the check runs on every keystroke).
    @Test func composingIsReadOnlyForALineBreak() {
        var asked = 0
        func composing() -> Bool { asked += 1; return false }
        _ = ReturnSends.breakSends(from: "a", to: "ab", composing: composing())
        #expect(asked == 0)
        _ = ReturnSends.breakSends(from: "a", to: "a\n", composing: composing())
        #expect(asked == 1)
    }

    /// Review of 75c30cc: a hardware Return whose press did not reach the field (the draft path) stays a new line with
    /// Shift or Option held on either side, as the press path does; any other key held does not change it.
    @Test(arguments: [GCKeyCode.leftShift, .rightShift, .leftAlt, .rightAlt])
    func theFallbackKeepsALineBreakWithShiftOrOptionHeld(_ key: GCKeyCode) {
        let held = ReturnSends.lineBreakKeyHeld { $0 == key }
        #expect(held)
        #expect(!ReturnSends.breakSends(from: "draft", to: "draft\n", composing: false, keyHeld: held))
    }

    @Test func theFallbackSendsWithNoModifierHeld() {
        #expect(!ReturnSends.lineBreakKeyHeld { _ in false })
        #expect(!ReturnSends.lineBreakKeyHeld { $0 == .leftControl || $0 == .leftGUI || $0 == .keyA })
        #expect(ReturnSends.breakSends(from: "draft", to: "draft\n", composing: false,
                                       keyHeld: ReturnSends.lineBreakKeyHeld { $0 == .keyA }))
    }

    /// The keyboard is asked for held keys only once a line break was typed (the check runs on every keystroke).
    @Test func heldKeysAreReadOnlyForALineBreak() {
        var asked = 0
        func held() -> Bool { asked += 1; return false }
        _ = ReturnSends.breakSends(from: "a", to: "ab", composing: false, keyHeld: held())
        #expect(asked == 0)
        _ = ReturnSends.breakSends(from: "a", to: "a\n", composing: false, keyHeld: held())
        #expect(asked == 1)
    }

    @Test func plainReturnSendsAndModifiedReturnDoesNot() {
        #expect(ReturnSends.keySends([], composing: false))
        #expect(ReturnSends.keySends(.capsLock, composing: false))
        #expect(ReturnSends.keySends(.numericPad, composing: false))
        #expect(!ReturnSends.keySends(.shift, composing: false))
        #expect(!ReturnSends.keySends(.option, composing: false))
        // ⌘Return is the send button's own shortcut.
        #expect(!ReturnSends.keySends(.command, composing: false))
    }

    @Test func returnWhileComposingConfirmsInsteadOfSending() {
        #expect(!ReturnSends.keySends([], composing: true))
    }

    @Test func returnDoesWhatTheSendButtonsPlaceDoes() {
        #expect(ReturnSends.outcome(canSend: true, gated: false) == .send)
        #expect(ReturnSends.outcome(canSend: false, gated: false) == .nothing)
        #expect(ReturnSends.outcome(canSend: true, gated: true) == .gate)
        #expect(ReturnSends.outcome(canSend: false, gated: true) == .gate)
    }

    @MainActor @Test func offByDefault() {
        let defaults = UserDefaults(suiteName: "ReturnSendsTests")!
        defaults.removePersistentDomain(forName: "ReturnSendsTests")
        #expect(StoredValue(wrappedValue: false, ReturnSends.enabledKey, store: defaults).wrappedValue == false)
    }
}

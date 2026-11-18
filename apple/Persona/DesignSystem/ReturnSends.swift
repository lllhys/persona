import SwiftUI
#if os(iOS)
import GameController
import UIKit
#endif

/// Settings › Appearance & Display › Return Key Sends (#8178), off by default: Return in a chat's composer sends, as the
/// web's Enter does with a precise pointer (`enterSends`) and the Mac's always does; Shift-Return is a new line. Off,
/// Return is a new line as before and the composer is unchanged. On the iPhone and iPad only: the Mac already sends.
///
/// The composers' fields are SwiftUI's vertical `TextField`, which takes Return as a line break and never submits. So
/// the software keyboard's Return is read off the draft: an edit that only adds one line break is Return typed. A
/// hardware keyboard's Return is caught as a key press first (`onKeyPress`), and when that does not reach the field,
/// read off the draft the same way, with Shift or Option held (`GCKeyboard`, either side) leaving the line break. While the keyboard is still
/// composing (marked text: Pinyin, kana) Return confirms the composition as it always has and sends nothing (#2203).
nonisolated enum ReturnSends {
    static let enabledKey = "ui.returnSends"

    /// `new` is `old` with one line break typed into it, and nothing else changed (one key's edit, not a paste).
    static func breakTyped(from old: String, to new: String) -> Bool {
        let o = old.utf8, n = new.utf8
        guard n.count == o.count + 1 else { return false }
        var i = o.startIndex, j = n.startIndex
        while i != o.endIndex, o[i] == n[j] {
            o.formIndex(after: &i)
            n.formIndex(after: &j)
        }
        // `j` moves with `i`, so it stops short of `n`'s end (one byte longer); the guard keeps that true by itself.
        guard j != n.endIndex, n[j] == UInt8(ascii: "\n") else { return false }
        return n[n.index(after: j)...].elementsEqual(o[i...])
    }

    /// Whether an edit to the draft is a Return that sends: one line break typed, with nothing else holding Return
    /// (`paused`), not a hardware Shift- or Option-Return let through (`allowed`), and never while the keyboard still
    /// has marked text (`composing`): a keyboard that confirms its composition with a line break must not send (#2203).
    /// `composing` is read only after a line break was typed: it asks the field (this runs on every keystroke).
    /// `keyHeld` is the hardware fallback: a key that makes Return a new line is held (`lineBreakKeyHeld`), for when the
    /// press itself did not reach the field; read last, like `composing`.
    static func breakSends(from old: String, to new: String, composing: @autoclosure () -> Bool,
                           keyHeld: @autoclosure () -> Bool = false, allowed: Bool = false, paused: Bool = false) -> Bool {
        !paused && !allowed && breakTyped(from: old, to: new) && !composing() && !keyHeld()
    }

    #if os(iOS)
    /// The hardware keys that, held, make Return a new line: Shift and Option, on either side (`keySends` reads the same
    /// two off a press that reaches the field).
    static let lineBreakKeys: [GCKeyCode] = [.leftShift, .rightShift, .leftAlt, .rightAlt]

    /// One of `lineBreakKeys` is held, as `isPressed` (a hardware keyboard's buttons) says.
    static func lineBreakKeyHeld(_ isPressed: (GCKeyCode) -> Bool) -> Bool {
        lineBreakKeys.contains(where: isPressed)
    }
    #endif

    /// What a hardware Return does with these modifiers held: send with none, else what the field does (Shift and
    /// Option a new line, ⌘ the send button's shortcut). Caps Lock and the keypad's Enter are no modifiers.
    static func keySends(_ modifiers: EventModifiers, composing: Bool) -> Bool {
        !composing && modifiers.subtracting([.capsLock, .numericPad]).isEmpty
    }

    /// What Return does when it would send.
    enum Outcome: Equatable {
        /// Send, as the send button would.
        case send
        /// The send button's place says what to do first (`SendGate`): Return does what tapping it does.
        case gate
        /// Nothing to send (an empty draft, an upload still running): nothing happens, no blank line either.
        case nothing
    }

    static func outcome(canSend: Bool, gated: Bool) -> Outcome {
        gated ? .gate : canSend ? .send : .nothing
    }
}

extension View {
    /// Return sends from this composer while Return Key Sends is on (`ReturnSends`). `send` is the send button's
    /// action, `gate` the action of what stands in its place; `paused` while Return belongs to something else (a list
    /// over the field that picks with it).
    func returnSends(text: Binding<String>, canSend: Bool, gate: (() -> Void)? = nil, paused: Bool = false,
                     send: @escaping () -> Void) -> some View {
        #if os(iOS)
        modifier(ReturnSendsModifier(text: text, canSend: canSend, gate: gate, paused: paused, send: send))
        #else
        self
        #endif
    }
}

#if os(iOS)
private struct ReturnSendsModifier: ViewModifier {
    @Binding var text: String
    var canSend: Bool
    var gate: (() -> Void)?
    var paused: Bool
    var send: () -> Void
    @StoredValue(ReturnSends.enabledKey) private var enabled = false
    /// A hardware Shift-Return just went to the field: the line break it types stays.
    @State private var breakAllowed = false

    func body(content: Content) -> some View {
        content
            // The software keyboard's key reads "send" while Return sends; `.return` is the field's own default.
            .submitLabel(enabled ? .send : .return)
            .onKeyPress(.return, phases: .down) { press in
                guard enabled, !paused else { return .ignored }
                guard ReturnSends.keySends(press.modifiers, composing: TypedText.composingFocused) else {
                    breakAllowed = press.modifiers.contains(.shift) || press.modifiers.contains(.option)
                    return .ignored
                }
                act()
                return .handled
            }
            .onChange(of: text) { old, new in
                guard enabled else { return }
                let allowed = breakAllowed
                breakAllowed = false
                // The cheap checks first: this runs on every keystroke (docs/apple/performance.md P4); the field's
                // marked text and the keyboard's Shift and Option are read only once a line break was typed.
                guard ReturnSends.breakSends(from: old, to: new, composing: TypedText.composingFocused,
                                             keyHeld: Self.lineBreakKeyHeld, allowed: allowed, paused: paused) else { return }
                // Take the line break back out of the field itself as well as the draft: sending confirms what the
                // field shows (`TypedText.commitFocused`), which would otherwise put it back.
                if let view = FirstResponder.current as? UITextView, view.text == new { view.text = old }
                text = old
                act()
            }
    }

    private func act() {
        switch ReturnSends.outcome(canSend: canSend, gated: gate != nil) {
        case .send: send()
        case .gate: gate?()
        case .nothing: break
        }
    }

    /// Shift or Option is held on a hardware keyboard (the key press did not reach the field, so its modifiers were
    /// not read). No hardware keyboard: nothing is held.
    private static var lineBreakKeyHeld: Bool {
        guard let keys = GCKeyboard.coalesced?.keyboardInput else { return false }
        return ReturnSends.lineBreakKeyHeld { keys.button(forKeyCode: $0)?.isPressed == true }
    }
}
#endif

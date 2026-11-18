import SwiftUI

/// A mic's touch, the same on every mic (#9619: `ComposerHoldMic`, `ComposerMicButton`, `FieldMicButton`). Let go before
/// `Theme.Hold.delay` it is a tap; held that long it is a hold, which lasts until the finger lifts. One touch is one or
/// the other: a hold never ends in a tap, and a tap never starts a hold. A touch the system takes away (a call, an
/// alert) or a mic that leaves the screen under the finger ends a hold as letting go does, keeping what was heard.
struct MicPress: ViewModifier {
    /// A hold can start (false while the mic already listens: every touch is then a tap, which ends it).
    var holds: Bool
    var tap: () -> Void
    /// Held long enough: dictation starts.
    var began: () -> Void
    /// Where the finger is from where it went down, while it holds.
    var moved: (CGSize) -> Void = { _ in }
    /// The hold is over: what was heard is kept or dropped (`DictationHold`).
    var ended: (DictationHold.Outcome) -> Void

    /// Counts the touch toward a hold; cancelled when the finger lifts first (a tap).
    @State private var pending: Task<Void, Never>?
    /// A touch is under way: its first change has come and its end has not.
    @State private var inTouch = false
    /// Dictating while held.
    @State private var holding = false
    /// This touch became a hold, even one a slide already dropped: letting go is no tap.
    @State private var held = false
    /// The finger is down; the gesture resets it when the system takes the touch away without an end.
    @GestureState private var touching = false

    func body(content: Content) -> some View {
        content
            .gesture(press)
            .onChange(of: touching) { _, down in
                if !down { abandon() }
            }
            .onDisappear { abandon() }
    }

    private var press: some Gesture {
        DragGesture(minimumDistance: 0, coordinateSpace: .global)
            .updating($touching) { _, down, _ in down = true }
            .onChanged { value in
                if !inTouch {
                    inTouch = true
                    held = false
                    if holds { count() }
                }
                guard holding else { return }
                moved(value.translation)
                if let outcome = DictationHold.whileHeld(value.translation) { end(outcome) }
            }
            .onEnded { value in
                inTouch = false
                pending?.cancel()
                pending = nil
                if holding {
                    end(DictationHold.onRelease(value.translation, velocity: value.velocity))
                } else if !held {
                    tap()
                }
            }
    }

    private func count() {
        pending = Task {
            try? await Task.sleep(for: Theme.Hold.delay)
            guard !Task.isCancelled, inTouch else { return }
            pending = nil
            held = true
            holding = true
            began()
        }
    }

    private func end(_ outcome: DictationHold.Outcome) {
        holding = false
        ended(outcome)
    }

    /// The touch ended without the gesture's end (taken away by the system, the mic gone): a hold keeps what was heard.
    /// After a normal end it does nothing (the end came first).
    private func abandon() {
        pending?.cancel()
        pending = nil
        inTouch = false
        if holding { end(.keep) }
    }
}

extension View {
    /// What VoiceOver says of a mic (#9619): its name, that it listens, and what a tap does now. Its activation is a tap.
    func micAccessibility(listening: Bool, id: String, tap: @escaping () -> Void) -> some View {
        accessibilityElement()
            .accessibilityAddTraits(.isButton)
            .accessibilityLabel(listening ? String(localized: "Stop Dictation") : String(localized: "Dictate"))
            .accessibilityValue(listening ? String(localized: "Listening…") : "")
            .accessibilityHint(listening ? String(localized: "Tap to stop.")
                                         : String(localized: "Tap to dictate, or touch and hold to dictate while you hold. Slide left to cancel."))
            .accessibilityAction { tap() }
            .accessibilityIdentifier(id)
    }
}

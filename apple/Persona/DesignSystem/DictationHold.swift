import CoreGraphics

/// Hold a mic to dictate while the finger is down (#9619; Telegram's voice button, its thresholds in `Theme.Hold`): a
/// touch held past `Theme.Hold.delay` starts dictating, letting go keeps what was heard, sliding left drops it. A tap
/// already keeps dictation on until the next tap, so there is no lock. Only left counts: a finger drifting right, up or
/// down is where it started.
nonisolated enum DictationHold {
    enum Outcome: Equatable {
        /// What was heard goes into the draft.
        case keep
        /// What was heard is dropped; the draft is as it was.
        case cancel
    }

    /// How far left the finger is from where it went down, never below zero.
    static func travel(_ translation: CGSize) -> CGFloat {
        max(0, -translation.width)
    }

    /// Decided while the finger is still down, as soon as it goes far enough; nil: not yet.
    static func whileHeld(_ translation: CGSize) -> Outcome? {
        travel(translation) > Theme.Hold.cancelDistance ? .cancel : nil
    }

    /// Decided when the finger lifts: a shorter slide left drops it, or a flick left. `velocity` is in points per second,
    /// negative leftward, as a drag gesture reports it.
    static func onRelease(_ translation: CGSize, velocity: CGSize) -> Outcome {
        if -velocity.width > Theme.Hold.flickSpeed || travel(translation) > Theme.Hold.releaseCancelDistance { return .cancel }
        return .keep
    }

    /// How far the "Slide to cancel" label has moved left: still for the first few points, then at half the finger's pace.
    static func labelShift(_ translation: CGSize) -> CGFloat {
        max(0, travel(translation) - Theme.Hold.labelDeadZone) * Theme.Hold.labelFollow
    }

    /// The label is gone by the time the slide drops what was heard, fading over the last `Theme.Hold.labelFade` points.
    static func labelOpacity(_ translation: CGSize) -> CGFloat {
        let atDrop = labelShift(CGSize(width: -Theme.Hold.cancelDistance, height: 0))
        return min(1, max(0, (atDrop - labelShift(translation)) / Theme.Hold.labelFade))
    }

    /// The circle under the finger shrinks as the finger goes toward cancel, never below `Theme.Hold.circleSmallest`.
    static func circleScale(_ translation: CGSize) -> CGFloat {
        max(Theme.Hold.circleSmallest, 1 - travel(translation) / Theme.Hold.circleSlide)
    }

    /// The draft with what was heard after it, a space between when the draft does not end in one.
    static func joined(_ draft: String, _ heard: String) -> String {
        guard !heard.isEmpty else { return draft }
        return (draft.isEmpty || draft.last?.isWhitespace == true ? draft : draft + " ") + heard
    }
}

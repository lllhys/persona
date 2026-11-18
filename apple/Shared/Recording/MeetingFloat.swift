import CoreGraphics
import Foundation

// The floating meeting window (#8731 V5, docs/design/DESIGN_SYSTEM.md "The floating meeting window"): while a meeting
// records and its room is put away, a small card says who speaks now. Who it can tell (E7): Persona while the phone says
// its answer (certain), someone while the microphone hears a voice, "you" instead when the microphone is a headset's (it
// hears little but its wearer), quiet otherwise. No numbered speakers: the phone can't tell people apart. Pure, for the
// tests.

/// Who speaks now in the floating window.
nonisolated enum MeetingFloatSpeaker: String, Hashable, Sendable {
    case persona, you, someone, quiet, paused

    /// The voice gate's state between looks: whether a voice is on, and when the level last stood above `off`.
    nonisolated struct Gate: Hashable, Sendable {
        var on = false
        var lastLoud: Double = -.infinity
    }

    /// The speaker at `now` (seconds, any steady clock): paused first, then Persona, then the voice gate on `level`
    /// (0...1). The gate opens at `onLevel`, and stays open while the level stays above `offLevel` or for `hold`
    /// seconds after, so a breath between words doesn't flicker to quiet.
    static func next(level: Double, personaSpeaking: Bool, paused: Bool, headset: Bool, gate: Gate, now: Double,
                     onLevel: Double, offLevel: Double, hold: Double) -> (speaker: MeetingFloatSpeaker, gate: Gate) {
        var gate = gate
        if level >= offLevel { gate.lastLoud = now }
        if level >= onLevel { gate.on = true } else if gate.on, now - gate.lastLoud > hold { gate.on = false }
        if paused { return (.paused, Gate()) }
        if personaSpeaking { return (.persona, gate) }
        return (gate.on ? (headset ? .you : .someone) : .quiet, gate)
    }
}

/// Where the floating window sits: on a side (left or right), at a height (its center, as a share of the room it may
/// use), or docked on a side as a bar. Pure geometry for the drag's end.
nonisolated struct MeetingFloatPlace: Hashable, Sendable {
    var leading: Bool
    var height: Double
    var docked: Bool

    static let start = MeetingFloatPlace(leading: false, height: 0.72, docked: false)

    /// Where a drag that ended with the card's center at `center` leaves it, in a space `width` wide whose usable height
    /// runs from `top` to `bottom`: docked on a side when the card was pulled `pull` of its width past it, else on the
    /// nearer side, at that height (kept inside the usable band).
    static func dropped(center: CGPoint, cardWidth: Double, width: Double, top: Double, bottom: Double,
                        pull: Double) -> MeetingFloatPlace {
        let past = cardWidth * pull
        let leading = center.x < width / 2
        let docked = center.x - cardWidth / 2 < -past || center.x + cardWidth / 2 > width + past
        let span = max(1, bottom - top)
        let height = min(1, max(0, (Double(center.y) - top) / span))
        return MeetingFloatPlace(leading: leading, height: height, docked: docked)
    }

    /// The card's center for this place.
    func center(cardWidth: Double, edge: Double, width: Double, top: Double, bottom: Double) -> CGPoint {
        let x = leading ? edge + cardWidth / 2 : width - edge - cardWidth / 2
        return CGPoint(x: x, y: top + (bottom - top) * height)
    }
}

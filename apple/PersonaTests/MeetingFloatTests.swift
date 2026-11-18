import CoreGraphics
import Foundation
import Testing
@testable import Persona

/// The floating meeting window (#8731 V5): who it says speaks, and where a drag leaves it.
struct MeetingFloatTests {
    private func step(_ level: Double, _ gate: MeetingFloatSpeaker.Gate, at now: Double, persona: Bool = false,
                      paused: Bool = false, headset: Bool = false) -> (speaker: MeetingFloatSpeaker, gate: MeetingFloatSpeaker.Gate) {
        MeetingFloatSpeaker.next(level: level, personaSpeaking: persona, paused: paused, headset: headset, gate: gate, now: now,
                                 onLevel: 0.18, offLevel: 0.10, hold: 1.2)
    }

    /// The gate opens at the on level, stays open through a breath (above off, or within the hold), then says quiet.
    @Test func aVoiceOpensTheGateAndABreathKeepsItOpen() {
        var g = MeetingFloatSpeaker.Gate()
        var r = step(0.05, g, at: 0)
        #expect(r.speaker == .quiet)
        g = r.gate
        r = step(0.20, g, at: 0.25)
        #expect(r.speaker == .someone)
        g = r.gate
        r = step(0.12, g, at: 0.5)
        #expect(r.speaker == .someone, "above the off level: still speaking")
        g = r.gate
        r = step(0.02, g, at: 1.0)
        #expect(r.speaker == .someone, "a breath within the hold")
        g = r.gate
        r = step(0.02, g, at: 1.8)
        #expect(r.speaker == .quiet, "past the hold after the last loud look")
    }

    /// Persona's answer comes first, a headset makes the voice "you", and paused says paused and closes the gate.
    @Test func personaThenTheHeadsetThenPaused() {
        let open = step(0.3, MeetingFloatSpeaker.Gate(), at: 0).gate
        #expect(step(0.3, open, at: 0.25, persona: true).speaker == .persona)
        #expect(step(0.3, open, at: 0.25, headset: true).speaker == .you)
        let paused = step(0.3, open, at: 0.25, paused: true)
        #expect(paused.speaker == .paused && paused.gate == MeetingFloatSpeaker.Gate())
    }

    /// A drag's end: the nearer side at that height, inside the band; pulled past a side, docked there.
    @Test func aDropSnapsToTheNearerSideOrDocks() {
        let drop = { (x: Double, y: Double) in
            MeetingFloatPlace.dropped(center: CGPoint(x: x, y: y), cardWidth: 200, width: 400, top: 100, bottom: 700, pull: 0.3)
        }
        #expect(drop(120, 400) == MeetingFloatPlace(leading: true, height: 0.5, docked: false))
        #expect(drop(300, 50) == MeetingFloatPlace(leading: false, height: 0, docked: false), "kept inside the band")
        #expect(drop(-60, 400).docked && drop(-60, 400).leading, "pulled past the left side")
        #expect(drop(460, 400).docked && !drop(460, 400).leading, "pulled past the right side")
        #expect(!drop(80, 400).docked, "at the side but not past it")
        let place = MeetingFloatPlace(leading: true, height: 0.5, docked: false)
        #expect(place.center(cardWidth: 200, edge: 12, width: 400, top: 100, bottom: 700) == CGPoint(x: 112, y: 400))
    }
}

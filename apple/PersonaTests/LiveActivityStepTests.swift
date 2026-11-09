import Foundation
import Testing
@testable import Persona

/// What the app does with the fleet while it draws the Live Activity itself (in front: the hub pushes nothing then).
/// Only the assistant answering is something to show on an island that shows it (#4628, the app's side of #4590).
struct LiveActivityStepTests {
    private func state(phase: FleetState.Phase = .none, assistant: Bool = false, island: IslandSlot = .assistant) -> FleetState {
        var s = FleetState(phase: phase, assistant: assistant)
        (s.left, s.right) = (.status, island)
        return s
    }

    struct Case: Sendable, CustomTestStringConvertible {
        let label: String
        var phase: FleetState.Phase = .none
        var assistant = false
        var island: IslandSlot = .assistant
        let running: Bool
        let active: Bool
        let expected: LiveActivityController.Step
        var testDescription: String { label }
    }

    static let cases: [Case] = [
        // No agent working, nothing unread: only the assistant answering, its side on the island (Thinking).
        Case(label: "assistant answering, running, active", assistant: true, running: true, active: true, expected: .update),
        Case(label: "assistant answering, running, inactive", assistant: true, running: true, active: false, expected: .update),
        // Once the assistant is done the minute runs as before.
        Case(label: "assistant done, running", running: true, active: true, expected: .endSoon),
        // An island that does not show the assistant has nothing to show.
        Case(label: "assistant on a tokens island", assistant: true, island: .tokens, running: true, active: true, expected: .endSoon),
        // A product decision left open in #4628: only an activity already on is kept.
        Case(label: "assistant alone, not running", assistant: true, running: false, active: true, expected: .nothing),
        // Agents start and update as before.
        Case(label: "working, not running, active", phase: .working, running: false, active: true, expected: .start),
        Case(label: "working, not running, inactive", phase: .working, running: false, active: false, expected: .nothing),
        Case(label: "done, not running", phase: .done, running: false, active: true, expected: .nothing),
        Case(label: "needs you, running", phase: .needsYou, running: true, active: true, expected: .update),
        Case(label: "empty, not running", running: false, active: true, expected: .nothing),
    ]

    @Test(arguments: cases)
    func stepFollowsTheFleetAndTheActivity(_ c: Case) {
        let s = state(phase: c.phase, assistant: c.assistant, island: c.island)
        #expect(LiveActivityController.step(s, running: c.running, active: c.active) == c.expected, "\(c.label)")
    }

    @Test func showsNothingFollowsTheIslandsSides() {
        #expect(state().showsNothing)
        #expect(!state(assistant: true).showsNothing)
        #expect(state(assistant: true, island: .none).showsNothing)
        #expect(!state(phase: .done).showsNothing)
    }
}

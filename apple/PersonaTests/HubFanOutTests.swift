import Foundation
import Testing
@testable import Persona
import PersonaWire

/// Asking every group's hub at once (#4705): a hub out of reach holds back none of the others, and a poll skips the
/// groups whose link is down unless none is up.
@MainActor
struct HubFanOutTests {
    /// Called from outside the main actor (a detached task, as a background refresh might): the answers still come on
    /// the main actor (#4777: they came on a pool thread, and the app stopped at launch).
    @Test func answersComeOnTheMainActorFromAnyCaller() async {
        let onMain = await Task.detached { () -> Bool in
            var all = true
            await HubFanOut.run(["", "work"], fetch: { $0 }) { _, _ in all = all && Thread.isMainThread }
            return all
        }.value
        #expect(onMain)
    }

    @Test func aPollAsksOnlyTheGroupsThatAreUp() {
        #expect(HubFanOut.asked(["", "work", "lab"], up: { $0 != "work" }) == ["", "lab"])
    }

    @Test func withNoGroupUpEveryGroupIsAsked() {
        #expect(HubFanOut.asked(["", "work"], up: { _ in false }) == ["", "work"])
    }

    /// The slow group (a hub whose requests wait out their timeout) answers last; the others' answers come before it.
    @Test func aSlowGroupHoldsBackNoOther() async {
        struct Down: Error {}
        var order: [String] = []
        var failed: [String] = []
        let started = Date.now
        await HubFanOut.run(["dead", "", "work"], fetch: { g in
            if g == "dead" {
                try await Task.sleep(for: .milliseconds(600))
                throw Down()
            }
            return g.count
        }) { g, result in
            MainActor.assertIsolated() // AppModel and views read in here (#4777)
            order.append(g)
            if case .failure = result { failed.append(g) }
        }
        #expect(order.last == "dead")
        #expect(Set(order) == ["dead", "", "work"])
        #expect(failed == ["dead"])
        // At once, not one after another: about the slow one's time, not the sum.
        #expect(Date.now.timeIntervalSince(started) < 1.5)
    }

    @Test func eachAnswerArrivesBeforeTheSlowestEnds() async {
        var firstAt: Date?
        let started = Date.now
        await HubFanOut.run(["fast", "slow"], fetch: { g in
            if g == "slow" { try await Task.sleep(for: .milliseconds(800)) }
            return g
        }) { g, _ in
            MainActor.assertIsolated()
            if g == "fast" { firstAt = .now }
        }
        #expect(firstAt.map { $0.timeIntervalSince(started) < 0.6 } == true)
    }
}

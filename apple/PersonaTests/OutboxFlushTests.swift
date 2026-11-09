import Foundation
import Testing
@testable import Persona

/// Which chats' waiting messages go out, by group (#4704): a group's messages go once its own link is up, whatever the
/// hub-level group's link is doing, and a group that is sending already or out of reach keeps no other group back.
struct OutboxFlushTests {
    private let home = SessionKey(machineID: "mac", sessionID: "a")
    private let homeB = SessionKey(machineID: "mac", sessionID: "b")
    private let work = SessionKey(group: "work", machineID: "box", sessionID: "c")

    @Test func aGroupSendsWhileTheHubLevelOneIsDown() {
        let out = OutboxFlush.batches([home, work], ready: { $0 == "work" }, busy: [])
        #expect(out == ["work": [work]])
    }

    @Test func everyGroupThatIsUpGetsItsOwnBatch() {
        let out = OutboxFlush.batches([work, homeB, home], ready: { _ in true }, busy: [])
        #expect(out == ["": [home, homeB], "work": [work]])
    }

    @Test func aGroupSendingAlreadyHoldsBackOnlyItself() {
        let out = OutboxFlush.batches([home, work], ready: { _ in true }, busy: [""])
        #expect(out == ["work": [work]])
    }

    @Test func nothingGoesWhileNoGroupIsUp() {
        #expect(OutboxFlush.batches([home, work], ready: { _ in false }, busy: []).isEmpty)
    }
}

/// A message that keeps coming back waiting while its group's link is up fails after `OutboxWait.connectedLimit`;
/// waiting while the link is down has no limit.
struct OutboxWaitTests {
    private let t0 = Date(timeIntervalSince1970: 1_000_000)

    @Test func aFirstWaitOnAnUpLinkStartsTheRun() {
        #expect(OutboxWait.next(.waiting, since: nil, now: t0, linkUp: true) == .init(outcome: .waiting, since: t0))
    }

    @Test func aWaitWithinTheLimitKeepsTheRunsStart() {
        let next = OutboxWait.next(.waiting, since: t0, now: t0.addingTimeInterval(OutboxWait.connectedLimit - 1), linkUp: true)
        #expect(next == .init(outcome: .waiting, since: t0))
    }

    @Test func aWaitAtTheLimitFailsTheMessage() {
        let next = OutboxWait.next(.waiting, since: t0, now: t0.addingTimeInterval(OutboxWait.connectedLimit), linkUp: true)
        #expect(next == .init(outcome: .failed, since: nil, gaveUp: true))
    }

    @Test func aWaitWhileTheLinkIsDownNeverFailsAndEndsTheRun() {
        let next = OutboxWait.next(.waiting, since: t0, now: t0.addingTimeInterval(3600), linkUp: false)
        #expect(next == .init(outcome: .waiting, since: nil))
    }

    @Test func aSendEndsTheRun() {
        #expect(OutboxWait.next(.sent, since: t0, now: t0.addingTimeInterval(5), linkUp: true) == .init(outcome: .sent, since: nil))
    }
}

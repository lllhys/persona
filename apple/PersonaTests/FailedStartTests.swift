import Foundation
import Testing
@testable import Persona

/// A first message whose start failed (#1368): whether it may have started, and the op id its Resend goes with (as a
/// chat's Resend: the old one only when the hub says that start is done or still running).
@MainActor
struct FailedStartTests {
    private func message(mayHaveStarted: Bool) -> FailedStart.Message {
        FailedStart.Message(text: "Plan a weekend trip.", images: [], space: nil, op: UUID(), mayHaveStarted: mayHaveStarted,
                            reason: nil, at: Date(timeIntervalSince1970: 1_700_000_000))
    }

    @Test func eachPageHoldsItsOwnFailedFirstMessage() {
        let home = message(mayHaveStarted: false)
        var work = home, health = home, healthFromWork = home
        work.manager = "m-1"
        health.space = "health"
        healthFromWork.space = "health"
        healthFromWork.manager = "m-1"
        #expect(!work.samePage(home), "another manager's Persona page")
        #expect(work.samePage(work) && health.samePage(healthFromWork), "Health is one page, whichever manager was shown")
        #expect(!health.samePage(home))
    }

    @Test func whatLeavesItPossiblyStarted() {
        #expect(FailedStart.mayHaveStarted(APIError(status: 502, message: "", fields: ["code": "assistant_in_doubt", "delivered": "unknown"])))
        #expect(FailedStart.mayHaveStarted(APIError(status: 202, message: "", fields: ["code": OpReceipt.unknownCode])))
        #expect(FailedStart.mayHaveStarted(URLError(.networkConnectionLost)))
        #expect(FailedStart.mayHaveStarted(URLError(.timedOut)))
        #expect(!FailedStart.mayHaveStarted(APIError(status: 409, message: "", fields: ["code": "launcher_missing"])))
        #expect(!FailedStart.mayHaveStarted(APIError(status: 503, message: "", fields: ["code": "assistant_offline"])))
        #expect(!FailedStart.mayHaveStarted(APIError(status: 400, message: "bad request")))
    }

    @Test func resendAsksTheHubOnlyWhenItMayHaveStarted() async {
        let hub = FakeHub()
        let m = message(mayHaveStarted: false)
        hub.on("GET", "/api/v1/ops/\(m.op.uuidString.lowercased())", json: #"{"state":"done"}"#)
        #expect(await FailedStart.resendOp(m, api: hub.client, receipts: true) != m.op)
        #expect(hub.requests.isEmpty)
        #expect(await FailedStart.resendOp(message(mayHaveStarted: true), api: hub.client, receipts: false) != m.op)
        #expect(hub.requests.isEmpty)
    }

    @Test func resendKeepsTheOpOnlyWhenTheHubSaysItStartedOrIsStarting() async {
        for (state, keeps) in [("done", true), ("pending", true), ("failed", false), ("unknown", false)] {
            let hub = FakeHub()
            let m = message(mayHaveStarted: true)
            hub.on("GET", "/api/v1/ops/\(m.op.uuidString.lowercased())", json: #"{"state":"\#(state)"}"#)
            let kept = await FailedStart.resendOp(m, api: hub.client, receipts: true) == m.op
            #expect(kept == keeps, "\(state)")
        }
        let hub = FakeHub() // no such op (404): a new one
        let m = message(mayHaveStarted: true)
        #expect(await FailedStart.resendOp(m, api: hub.client, receipts: true) != m.op)
    }
}

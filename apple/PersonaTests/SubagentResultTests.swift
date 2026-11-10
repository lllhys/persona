import Foundation
import Testing
@testable import Persona

@MainActor
struct SubagentResultTests {
    private func use(_ id: String, background: Bool = false) -> Item {
        Item(id: "use-" + id, seq: 1, role: "assistant", kind: .toolUse, toolUseID: id,
             toolName: "Agent", toolInput: .object(["description": "Review changes", "run_in_background": .bool(background)]))
    }

    private func result(_ id: String, text: String, error: Bool = false) -> Item {
        Item(id: "result-" + id, seq: 2, role: "user", kind: .toolResult, text: text, toolUseID: id, isError: error)
    }

    private func notice(_ id: String, status: String = "completed", detail: String? = "All checks passed.") -> Item {
        Item(id: "notice-" + id, seq: 3, role: "system", kind: .notice, text: "Agent finished", wake: true,
             task: TaskNote(kind: "agent", status: status, title: "Review changes", toolUseID: id, detail: detail))
    }

    @Test func foregroundResultsUseTheOriginalToolResult() {
        let u = use("a"), r = result("a", text: "All checks passed.")
        let call = ToolCall(use: u, result: r)
        #expect(SubagentResult.call(call, tasks: TaskIndex.build([u, r]), agent: nil) == call)
        #expect(SubagentResult.call(ToolCall(use: u), tasks: TaskIndex.build([u]), agent: nil) == nil)
    }

    @Test func aLaunchReceiptKeepsTheProgressCardUntilTheAgentFinishes() {
        let u = use("a", background: true), r = result("a", text: "Async agent launched successfully.\nagentId: worker-a")
        let call = ToolCall(use: u, result: r)
        let agent = Subagent(id: "worker-a", sessionID: "s@worker-a", toolUseID: "a", running: true)
        let tasks = TaskIndex.build([u, r])
        #expect(SubagentCardStatus.of(call, tasks: tasks, agent: agent) == .running)
        #expect(SubagentResult.call(call, tasks: tasks, agent: agent) == nil)
        let event = notice("a", status: "event", detail: "Checking another file.")
        #expect(SubagentResult.call(call, tasks: TaskIndex.build([u, r, event]), agent: agent) == nil)
    }

    @Test func aCompletedBackgroundResultUsesTheReportNotTheReceipt() {
        let u = use("a", background: true), r = result("a", text: "Async agent launched successfully.\nagentId: worker-a")
        var n = notice("a")
        n.ts = Date(timeIntervalSince1970: 123)
        n.fullSize = 4096
        let tasks = TaskIndex.build([u, r, n])
        let finished = SubagentResult.call(ToolCall(use: u, result: r), tasks: tasks, agent: nil)
        #expect(finished?.use == u)
        #expect(finished?.result?.kind == .toolResult)
        #expect(finished?.result?.text == "All checks passed.")
        #expect(finished?.result?.toolUseID == "a")
        #expect(finished?.result?.id == n.id)
        #expect(finished?.result?.ts == n.ts)
        #expect(finished?.result?.fullSize == nil)
        #expect(finished?.isRunning == false)
    }

    @Test func failuresKeepTheirErrorAndEmptyReportsDoNotExposeTheLaunchReceipt() {
        let u = use("a", background: true), r = result("a", text: "Async agent launched successfully.\nagentId: worker-a")
        let call = ToolCall(use: u, result: r)
        let failed = notice("a", status: "failed", detail: "One check failed.")
        #expect(SubagentResult.call(call, tasks: TaskIndex.build([u, r, failed]), agent: nil)?.isError == true)
        let empty = notice("a", detail: nil)
        #expect(SubagentResult.call(call, tasks: TaskIndex.build([u, r, empty]), agent: nil) == nil)
    }

    @Test func reportsAreAssociatedByToolUseIDNotBySharedTitles() {
        let a = use("a", background: true), b = use("b", background: true)
        let ra = result("a", text: "Async agent launched successfully.\nagentId: worker-a")
        let rb = result("b", text: "Async agent launched successfully.\nagentId: worker-b")
        let tasks = TaskIndex.build([a, b, ra, rb, notice("b", detail: "Second worker passed.")])
        #expect(SubagentResult.call(ToolCall(use: a, result: ra), tasks: tasks, agent: nil) == nil)
        #expect(SubagentResult.call(ToolCall(use: b, result: rb), tasks: tasks, agent: nil)?.result?.text == "Second worker passed.")
    }
}

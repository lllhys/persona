import Foundation
import Testing
@testable import Persona

/// A subagent's report folded to one line until opened: what that line says, which reports stay open, and which
/// messages from another session are a subagent's handback.
@MainActor
struct ReportFoldTests {
    /// A heading first is the line; otherwise the first eighty characters; a report that is one short line is not folded (nil).
    nonisolated static let foldedLines: [(input: String, expected: String?)] = [
        // A heading first is the line.
        (input: "## 结算回归：42 个用例全部通过\n\n- 三条主流程都过了。", expected: "结算回归：42 个用例全部通过"),
        (input: "# **Summary**\nAll green.", expected: "Summary"),
        // Otherwise its first eighty characters.
        (input: String(repeating: "很长的报告", count: 20), expected: String(String(repeating: "很长的报告", count: 20).prefix(80)) + "…"),
        // Its lines run on as one, without the marks they start with.
        (input: "42 passed.\n- **checkout** ok\n> note", expected: "42 passed. checkout ok note"),
        // "**bold**" opening the report is bold, not a list item.
        (input: "**Done**: 42 passed.\nagentId: x1", expected: "Done: 42 passed. agentId: x1"),
        // A report that is one short line is not folded.
        (input: "42 passed.", expected: nil),
        (input: "  \n", expected: nil),
        (input: "# Only a heading", expected: nil),
        (input: String(repeating: "a", count: 80), expected: nil),
        (input: String(repeating: "a", count: 81), expected: String(repeating: "a", count: 80) + "…"),
    ]

    @Test(arguments: foldedLines)
    func theFoldedLine(input: String, expected: String?) {
        #expect(ReportFold.line(input) == expected)
    }

    @Test func openReportsAreKeptByTheirCardAndOnlyTheLatestFew() {
        let open = OpenReports(persist: false)
        #expect(!open.isOpen("chat/agent-1"))
        open.set("chat/agent-1", true)
        #expect(open.isOpen("chat/agent-1"))
        #expect(!open.isOpen("other/agent-1"), "item ids repeat across chats")
        open.set("chat/agent-1", false)
        #expect(!open.isOpen("chat/agent-1"))
        for i in 0..<401 { open.set("chat/task-\(i)", true) }
        #expect(!open.isOpen("chat/task-0"))
        #expect(open.isOpen("chat/task-1") && open.isOpen("chat/task-400"))
    }

    @Test func aMessageFromOneOfTheSessionsSubagentsIsAHandback() {
        let agents = [Subagent(id: "a1b2c3", sessionID: "s@a1b2c3", type: "general-purpose", description: "回归：结算流程", running: false)]
        #expect(PeerMessage.isHandback(from: "general-purpose", agents: agents))
        #expect(PeerMessage.isHandback(from: "a1b2c3", agents: agents))
        #expect(PeerMessage.isHandback(from: "回归：结算流程", agents: agents))
        #expect(!PeerMessage.isHandback(from: "release-bot", agents: agents), "another session's message stays whole")
        #expect(!PeerMessage.isHandback(from: "general-purpose", agents: [Subagent]()))
        #expect(!PeerMessage.isHandback(from: " ", agents: agents))
    }
}

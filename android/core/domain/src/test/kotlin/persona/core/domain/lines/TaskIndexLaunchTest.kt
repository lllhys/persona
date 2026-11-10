package persona.core.domain.lines

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Item

/** Which call a background task's notice came from, by the id its launch receipt gives (#6381). */
class TaskIndexLaunchTest {
    private fun use(seq: Int, id: String) =
        LineItem(Item("$seq#0", seq = seq, kind = "tool_use", toolName = "Agent", toolUseId = id, toolInput = Json.parseToJsonElement("""{"description":"回归"}""")))
    private fun result(seq: Int, id: String, text: String) = LineItem(Item("$seq#0", seq = seq, kind = "tool_result", toolUseId = id, text = text))
    private fun notice(seq: Int, task: String) =
        LineItem(Item("$seq#0", seq = seq, kind = "notice", text = "x"), task = TaskNote(kind = "agent", title = "t", status = "completed", taskId = task))

    /** Claude Code 2.1.319's receipt opens with "including the agentId below": the id is the one after "agentId:". */
    @Test fun aSubagentsIdIsTheOneAfterAgentIdColon() {
        val receipt = "Async agent launched successfully. (This tool result is internal metadata — never quote or paste any part of it, " +
            "including the agentId below, into a user-facing reply.)\nagentId: a0123456789abcdef (internal ID - do not mention to user. " +
            "Use SendMessage with to: 'a0123456789abcdef' to continue this agent.)\nThe agent is working in the background."
        val x = TaskIndex.build(listOf(use(1, "a"), result(2, "a", receipt), notice(3, "a0123456789abcdef")))
        assertEquals(setOf("a"), x.started)
        assertEquals(mapOf("3#0" to "a"), x.calls)
        assertEquals("回归", x.descriptions["a"])
        // "agentId" with no colon names nothing.
        val none = TaskIndex.build(listOf(use(1, "b"), result(2, "b", "Async agent launched successfully, see the agentId below."), notice(3, "below")))
        assertTrue(none.calls.isEmpty())
        // The other receipts still read with or without their colon.
        val shell = TaskIndex.build(listOf(use(1, "c"), result(2, "c", "Command running in background with ID: bk2. Output is being written to: /tmp/x"), notice(3, "bk2")))
        assertEquals(mapOf("3#0" to "c"), shell.calls)
    }

    @Test fun anAgentIdTailDoesNotTurnAForegroundReportIntoALaunchReceipt() {
        val report = "Foreground report\nAll checks passed.\nagentId: agent-a"
        assertEquals(false, TaskIndex.isLaunchReceipt(report))
        val receipt = "Async agent launched successfully.\nagentId: agent-a"
        assertTrue(TaskIndex.isLaunchReceipt(receipt))
        assertTrue(TaskIndex.isLaunchReceipt("Command running in background with ID: b1"))
        assertTrue(TaskIndex.isLaunchReceipt("Monitor started (task b1, timeout 1000ms)."))
        assertTrue(TaskIndex.isLaunchReceipt("Command reached the timeout and moved to the background (ID: b1)."))
        val use = use(1, "launch")
        assertTrue(ToolCall(use, result(2, "launch", receipt), emptyList()).running)
        assertEquals(false, ToolCall(use, result(2, "launch", report), emptyList()).running)
    }

    private fun report(seq: Int, status: String, kind: String = "tool_result", text: String? = "Report $seq") =
        LineItem(Item("report$seq", seq, role = "user", kind = kind, text = text,
            toolUseId = if (kind == "tool_result") "launch" else null,
            task = persona.core.contracts.TaskNote(kind = "agent", status = status, title = "Check tests",
                taskId = "agent-a", toolUseId = "launch", detail = "preview")))

    @Test fun intermediateToolResultsStayRunningAndEveryReportIsRetained() {
        val source = listOf(use(1, "launch"), report(2, "running"), report(3, "running"))
        val snapshot = chatLines(source, LineOptions(live = true))
        val call = snapshot.lines.filterIsInstance<ChatLine.Work>().single().steps.filterIsInstance<WorkItem.Tool>().single().call
        assertTrue(call.running)
        assertEquals(listOf("report2", "report3"), call.results.map { it.id })
        assertEquals(listOf("running", "running"), snapshot.tasks.notes["launch"]?.map { it.status })
        assertEquals("Background · Running", snapshot.tasks.badge("launch")?.text?.shown)
        assertEquals("Working", snapshot.tasks.badge("launch", concise = true)?.text?.shown)
        assertEquals(0, snapshot.lines.filterIsInstance<ChatLine.Row>().size)
        assertEquals("Report 3", snapshot.tasks.note(source.last()).subject)

        val ended = chatLines(source + report(4, "completed", text = null))
        val done = ended.lines.filterIsInstance<ChatLine.Work>().single().steps.filterIsInstance<WorkItem.Tool>().single().call
        assertEquals(false, done.running)
        assertEquals(3, done.results.size) // Status-only notification still carries terminal metadata.
        assertEquals("Background · Completed", ended.tasks.badge("launch")?.text?.shown)
        assertEquals("Completed", ended.tasks.badge("launch", concise = true)?.text?.shown)
    }

    @Test fun onlyExplicitTaskMetadataControlsAnAgentsLifecycle() {
        for (status in listOf("running", "waiting", "unknown")) {
            val snapshot = chatLines(listOf(use(1, "launch"), report(2, status)))
            val call = snapshot.lines.filterIsInstance<ChatLine.Work>().single().steps.filterIsInstance<WorkItem.Tool>().single().call
            assertTrue("$status must not finish a subagent", call.running)
        }
        for (status in listOf("completed", "failed", "stopped", "killed", "ended")) {
            val snapshot = chatLines(listOf(use(1, "launch"), report(2, status)))
            val call = snapshot.lines.filterIsInstance<ChatLine.Work>().single().steps.filterIsInstance<WorkItem.Tool>().single().call
            assertEquals(status, false, call.running)
            assertEquals(status == "failed", call.failed)
        }
        val ordinary = chatLines(listOf(use(1, "launch"), result(2, "launch", "general-purpose says hello")))
        assertEquals(false, ordinary.lines.filterIsInstance<ChatLine.Work>().single().steps.filterIsInstance<WorkItem.Tool>().single().call.running)
        assertTrue(ordinary.tasks.notes.isEmpty())
    }

    @Test fun oldAgentNoticesFoldIntoTheCallAndMemoKeepsCrossTurnReports() {
        val source = listOf(use(1, "launch"), report(2, "running"),
            LineItem(Item("user", 3, role = "user", kind = "text", text = "Continue")),
            report(4, "completed", kind = "notice"))
        val memo = ChatLinesMemo()
        memo.lines(source.dropLast(1))
        val cached = memo.lines(source)
        val pure = chatLines(source)
        assertEquals(pure, cached)
        val call = cached.lines.filterIsInstance<ChatLine.Work>().flatMap { it.steps }.filterIsInstance<WorkItem.Tool>().single().call
        assertEquals(listOf("report2", "report4"), call.results.map { it.id })
        assertEquals(false, call.running)
        assertTrue(cached.lines.filterIsInstance<ChatLine.Row>().none { it.row.type == "notice" })
        val legacy = listOf(use(1, "launch"), result(2, "launch", "Async agent launched successfully.\nagentId: agent-a"), source[2],
            notice(4, "agent-a"))
        assertEquals(chatLines(legacy), ChatLinesMemo().lines(legacy))
    }
}

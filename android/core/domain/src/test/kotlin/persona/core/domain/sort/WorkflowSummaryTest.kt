package persona.core.domain.sort

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.BackgroundTask
import persona.core.contracts.Session
import persona.core.contracts.Workflow
import persona.core.domain.models.keepingDetails
import persona.core.domain.zh
import java.time.Instant

/** The workflow line and card parts (#2935), as iOS and the web write them. */
class WorkflowSummaryTest {
    private val now = Instant.parse("2026-11-05T10:14:30Z")
    private val audit = Workflow(taskId = "t1", runId = "wf_1", name = "parity-audit", status = "running", running = true,
        phases = listOf("Audit", "Verify"), phase = "Verify", phaseIndex = 2, agentsStarted = 185, agentsDone = 169, agentsRunning = 16,
        startedAt = "2026-11-05T10:00:00Z")
    private val base = Session(machineId = "m1", sessionId = "s1", status = "idle", state = "working")

    @Test fun theLineNamesTheNewestRunItsPhaseProgressAndTime() {
        val s = base.copy(workflows = listOf(audit))
        assertEquals("后台 workflow「parity-audit」 · Verify 2/2 · 169/185+ · 14 分", WorkflowSummary.line(s, now)?.zh())
        assertEquals(listOf("后台 workflow「parity-audit」", "Verify 2/2", "169/185+", "14 分"),
            WorkflowSummary.lineFields(s, now)?.map { it.zh() })
    }

    @Test fun moreRunsAddHowMany() {
        val s = base.copy(workflows = listOf(audit, audit.copy(taskId = "t2", name = null, summary = "second"), audit.copy(taskId = "t3"),
            audit.copy(taskId = "t4", status = "completed", running = false)))
        assertEquals("后台 workflow「parity-audit」 · Verify 2/2 · 169/185+ · 14 分 · 等 2 个", WorkflowSummary.line(s, now)?.zh())
    }

    @Test fun aSummaryRowReadsItsWorkflowProgress() {
        // summary=1 rows (#5888): no workflows, the newest running run without its agents and phases
        val s = base.copy(waiting = true, runningWorkflowsCount = 1, workflowProgress = audit.copy(phases = null, phaseCount = 2))
        assertEquals("后台 workflow「parity-audit」 · Verify 2/2 · 169/185+ · 14 分", WorkflowSummary.line(s, now)?.zh())
        assertEquals(true, AgentSession(s).isBackgroundWorking)
        // a count without progress (an older runtime's background entry) still reads as background work
        assertEquals(true, AgentSession(base.copy(waiting = true, runningWorkflowsCount = 1)).isBackgroundWorking)
        // a complete row reads its workflows
        assertNull(WorkflowSummary.line(base.copy(workflows = emptyList()), now))
        assertEquals(false, AgentSession(base.copy(workflows = emptyList())).isBackgroundWorking)
    }

    @Test fun aSummaryOverAnOpenedChatReadsItsOwnCounts() {
        // keepingDetails keeps an older complete row's workflows; the summary's own counts still win (#3588)
        val started = base.copy(waiting = true, runningWorkflowsCount = 1, workflowProgress = audit)
            .keepingDetails(base.copy(workflows = emptyList()))
        assertEquals(emptyList<Workflow>(), started.workflows)
        assertEquals("后台 workflow「parity-audit」 · Verify 2/2 · 169/185+ · 14 分", WorkflowSummary.line(started, now)?.zh())
        assertEquals(true, AgentSession(started).isBackgroundWorking)
        val ended = base.copy(waiting = false).keepingDetails(base.copy(workflows = listOf(audit)))
        assertEquals(1, ended.workflows?.size)
        assertNull(WorkflowSummary.line(ended, now))
        assertEquals(false, AgentSession(ended).isBackgroundWorking)
    }

    @Test fun nothingRunningOrAnOlderHub() {
        assertNull(WorkflowSummary.line(base, now))
        assertNull(WorkflowSummary.line(base.copy(workflows = listOf(audit.copy(status = "killed", running = false))), now))
        // an older hub: only the background entry says a workflow runs
        assertEquals("后台运行中", WorkflowSummary.line(base.copy(background = listOf(BackgroundTask("t1", "workflow"))), now)?.zh())
        assertNull(WorkflowSummary.line(base.copy(background = listOf(BackgroundTask("b1", "shell"))), now))
    }

    @Test fun aRunWithoutNameOrPhasesStillReads() {
        val bare = Workflow(taskId = "t1", status = "running", startedAt = "2026-11-05T10:14:00Z")
        assertEquals("后台 workflow · 1 分", WorkflowSummary.of(bare, now).zh())
        // the phase index alone names the phase
        assertEquals("Audit 1/2", WorkflowSummary.phase(audit.copy(phase = null, phaseIndex = 1))?.zh())
        // a phase whose place is not known
        assertEquals("Verify", WorkflowSummary.phase(audit.copy(phaseIndex = 0))?.zh())
    }

    @Test fun elapsedRunsToTheEndOnceItEnded() {
        val ended = audit.copy(status = "completed", running = false, endedAt = "2026-11-05T12:05:00Z")
        assertEquals("2 小时 5 分", WorkflowSummary.elapsed(ended, now)?.zh())
        assertEquals("1 小时", WorkflowSummary.elapsed(ended.copy(endedAt = "2026-11-05T11:00:30Z"), now)?.zh())
        assertNull(WorkflowSummary.elapsed(audit.copy(startedAt = null), now))
    }

    @Test fun statusAndWhatTheCountsSay() {
        assertEquals(listOf("在跑", "完成", "失败", "已停止", "没动静了", "完成"),
            listOf("running", "completed", "failed", "killed", "stalled", "unknown").map { WorkflowSummary.status(audit.copy(status = it)).zh() })
        assertEquals("169/185 个 agent 完成", WorkflowSummary.agentsDone(audit).zh())
    }
}

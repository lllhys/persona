package persona.core.domain.sort

import persona.core.contracts.CoreText
import persona.core.contracts.CoreText.Key
import persona.core.contracts.Session
import persona.core.contracts.Workflow
import persona.core.contracts.WorkflowStatus
import persona.core.contracts.parseTime
import java.time.Duration
import java.time.Instant

/**
 * The words for an agent's background Workflow runs (docs/protocol/agent-states.md "Background workflows and Monitor", #2935): the one line
 * the Agents list and the task cards add under an agent, and the status chip and time of a run's card ([WorkflowTimeline]
 * draws the rest). The same as iOS and the web: "Background workflow 'parity-audit' · Verify 2/2 · 169/185+ · 14 min".
 */
object WorkflowSummary {
    private const val SEPARATOR = " · "

    /**
     * The line for [session]'s running workflows: the newest one, then "and N others" for the others. An older hub that sends
     * no workflows but has one in its background says "Running in background". Null when no workflow runs.
     */
    fun line(session: Session?, now: Instant): CoreText? = lineFields(session, now)?.let { CoreText.joined(SEPARATOR, it) }

    /** The same line as separate fields, so an Agents row can lay them out with its facts instead of receiving a joined string. */
    fun lineFields(session: Session?, now: Instant): List<CoreText>? {
        session ?: return null
        val running = session.runningWorkflows
        if (running.isEmpty()) return if (session.legacyWorkflowRunning) listOf(CoreText.of(Key.WorkflowBackgroundRunning)) else null
        return fields(running.first(), now) + listOfNotNull((running.size - 1).takeIf { it > 0 }?.let { CoreText.of(Key.WorkflowMore, it) })
    }

    /** One run on one line: its name, phase, done / started agents ("169/185+" while it runs, #3435) and how long it has run. */
    fun of(workflow: Workflow, now: Instant): CoreText = CoreText.joined(SEPARATOR, fields(workflow, now))

    /** The fields of [of], before presentation joins them. */
    private fun fields(workflow: Workflow, now: Instant): List<CoreText> = listOfNotNull(
        workflow.title?.let { CoreText.of(Key.WorkflowLine, it) } ?: CoreText.of(Key.WorkflowLineUntitled),
        phase(workflow),
        workflow.agentsStarted.takeIf { it > 0 }?.let { CoreText.plain(WorkflowTimeline.counts(workflow)) },
        elapsed(workflow, now),
    )

    /** "Verify 2/2" where the hub knows its place among the phases, else the phase's name alone. */
    fun phase(workflow: Workflow): CoreText? {
        val phases = workflow.phases.orEmpty()
        // a summary row's run has no phases, only how many (`phase_count`, #5888)
        val count = phases.size.takeIf { it > 0 } ?: workflow.phaseCount ?: 0
        val index = workflow.phaseIndex?.takeIf { it in 1..count }
        val name = workflow.phase?.trim()?.takeIf(String::isNotEmpty) ?: index?.let { phases.getOrNull(it - 1)?.trim() }?.takeIf(String::isNotEmpty)
            ?: return null
        return if (index != null) CoreText.of(Key.WorkflowPhase, name, index, count) else CoreText.plain(name)
    }

    /** How long it ran: until it ended, or until [now] while it runs; null without a start. */
    fun elapsedSeconds(workflow: Workflow, now: Instant): Long? {
        val start = parseTime(workflow.startedAt) ?: return null
        val end = parseTime(workflow.endedAt) ?: now
        return Duration.between(start, end).seconds.coerceAtLeast(0)
    }

    /** [elapsedSeconds] in whole minutes (it is redrawn once a minute): at least 1 min, then hours and minutes. */
    fun elapsed(workflow: Workflow, now: Instant): CoreText? = elapsedSeconds(workflow, now)?.let { s ->
        when {
            s < 3600 -> CoreText.of(Key.DurationMinutes, (s / 60).coerceAtLeast(1))
            s % 3600 / 60 == 0L -> CoreText.of(Key.DurationHours, s / 3600)
            else -> CoreText.of(Key.DurationHoursMinutes, s / 3600, s % 3600 / 60)
        }
    }

    /** The status chip's words: "Running", "Done", "Failed", "Stopped", "Stalled". */
    fun status(workflow: Workflow): CoreText = CoreText.of(when (workflow.state) {
        WorkflowStatus.Running -> Key.WorkflowRunning
        WorkflowStatus.Completed -> Key.WorkflowCompleted
        WorkflowStatus.Failed -> Key.WorkflowFailed
        WorkflowStatus.Killed -> Key.WorkflowKilled
        WorkflowStatus.Stalled -> Key.WorkflowStalled
    })

    /** "169/185 agents done": what the card's counts and progress bar say to a screen reader; the total comes first, as the plural's count. */
    fun agentsDone(workflow: Workflow): CoreText = CoreText.of(Key.WorkflowAgentsDone, workflow.agentsStarted, workflow.agentsDone)
}

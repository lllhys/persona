package persona.core.domain.sort

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.AgentState
import persona.core.contracts.LoopInfo
import persona.core.contracts.Session
import java.time.Instant

/**
 * A computer's projects page (#4338 A11), written from iOS `MachineSpacesView`: which agents it lists, how its projects are
 * ordered and split into Working and Idle (`SpaceGroup.build`, `AgentListOrder.isQuiet`), what a project's row says
 * (`BusySpaceRow`, `QuietSpaceRow`) and the counts in its summary (`StateCounts`).
 */
class MachineProjectsTest {
    private fun agent(
        id: String, space: String? = "app", state: String = "working", machine: String = "mac", updated: String = "2026-01-02T00:00:00Z",
        tab: String? = null, index: Int? = null, waiting: Boolean = false, awaits: Boolean = false, assistant: Boolean = false,
        loop: Boolean = false, pane: Int? = null, name: String? = space,
    ) = AgentSession(Session(machineId = machine, sessionId = id, title = "Title of $id", cwd = "/code/${name ?: "loose"}", state = state, spaceId = space,
        spaceName = name, slotId = tab?.let { "slot-$it" }, slotName = tab, slotIndex = index, paneIndex = pane, updatedAt = updated,
        backgroundWaiting = waiting, awaitsReply = awaits, assistant = assistant, loop = if (loop) LoopInfo(kind = "loop", prompt = "check") else null))

    private fun projects(rows: List<AgentSession>, pins: List<String> = emptyList(), mutes: List<String> = emptyList(), machine: String = "mac") =
        machineProjects(rows, machine, AgentPreferences(spaces = pins), AgentPreferences(spaces = mutes))

    private fun List<MachineProject>.names() = map { it.space.name }

    @Test fun itListsTheComputersLiveAgentsOnlyAndLeavesOutTheAssistantAndSubtasksAParentHolds() {
        val subtask = AgentSession(Session(machineId = "mac", sessionId = "sub", state = "working", spaceId = "app", spaceName = "app", agent = "codex",
            parentSessionId = "a", updatedAt = "2026-01-02T00:00:00Z"))
        val needsYouSubtask = AgentSession(Session(machineId = "mac", sessionId = "sub2", state = "needs_you", spaceId = "app", spaceName = "app", agent = "codex",
            parentSessionId = "a", updatedAt = "2026-01-02T00:00:00Z"))
        val rows = listOf(agent("a"), subtask, needsYouSubtask, agent("b", "api", machine = "box"), agent("c", "chat", assistant = true),
            agent("d", "gone", state = "offline"), agent("e", "notes", state = "idle"))
        val mine = projects(rows)
        // The subtask a parent holds is no agent; one that needs you is (a row of its own, counted: DESIGN_SYSTEM 3.4).
        assertEquals(3, mine.agents)
        assertEquals(listOf("app", "notes"), (mine.busy + mine.idle).names().sorted())
        assertEquals(2, mine.projects)
        assertEquals(StateTally(needsYou = 1, working = 1, idle = 1), mine.tally)
        assertEquals(listOf("api"), projects(rows, machine = "box").busy.names())
        assertEquals(0, projects(rows, machine = "nowhere").projects)
    }

    @Test fun aProjectIsIdleWhenEveryAgentIsIdleAndNothingWaitsToBeWokenOrForAReply() {
        val rows = listOf(
            agent("1", "calm", state = "idle"), agent("2", "calm", state = "idle"),
            agent("3", "loop", state = "idle", loop = true),
            agent("4", "asks", state = "idle", awaits = true),
            agent("5", "mixed", state = "idle"), agent("6", "mixed", state = "unread"),
            agent("7", "busy"),
        )
        val shown = projects(rows)
        assertEquals(listOf("calm"), shown.idle.names())
        // A loop that has not fired, a question nobody answered, an unread agent and a working one keep a project busy.
        assertEquals(setOf("loop", "asks", "mixed", "busy"), shown.busy.names().toSet())
        // A muted agent waiting for a reply is plain Seen: its project is idle (`awaitsReply` with the mutes).
        val muted = machineProjects(listOf(agent("4", "asks", state = "idle", awaits = true)), "mac", AgentPreferences(),
            AgentPreferences(sessions = listOf("mac/4")))
        assertEquals(listOf("asks"), muted.idle.names())
    }

    @Test fun pinnedProjectsComeFirstAndStayInWorkingEvenWhenQuiet() {
        val rows = listOf(
            agent("1", "quiet-pinned", state = "idle"), agent("2", "plain", state = "working", updated = "2026-01-05T00:00:00Z"),
            agent("3", "needy", state = "needs_you", updated = "2026-01-01T00:00:00Z"),
            agent("4", "quiet", state = "idle"),
        )
        val shown = projects(rows, pins = listOf("mac/quiet-pinned"))
        assertEquals(listOf("quiet-pinned", "needy", "plain"), shown.busy.names())
        assertTrue(shown.busy.first().pinned)
        assertEquals(listOf("quiet"), shown.idle.names())
        assertEquals(4, shown.agents)
    }

    @Test fun projectsGoByTheirMostUrgentStateThenTheirNewestAgentThenTheirId() {
        val rows = listOf(
            agent("1", "older", updated = "2026-01-01T00:00:00Z"), agent("2", "newer", updated = "2026-01-03T00:00:00Z"),
            agent("3", "b-tie", updated = "2026-01-02T00:00:00Z"), agent("4", "a-tie", updated = "2026-01-02T00:00:00Z"),
            agent("5", "unread", state = "unread", updated = "2025-12-01T00:00:00Z"),
            agent("6", "needs", state = "needs_you", updated = "2025-11-01T00:00:00Z"),
        )
        // Needs you, unread, then the working ones by time, the ones with the same time by id.
        assertEquals(listOf("needs", "unread", "newer", "a-tie", "b-tie", "older"), projects(rows).busy.names())
    }

    @Test fun anAgentThatOnlyWaitsInTheBackgroundCountsAsIdleForItsProjectsOrderAndColorAndNeverAsWorking() {
        val rows = listOf(agent("1", "waits", waiting = true), agent("2", "works", updated = "2026-01-01T00:00:00Z"))
        val shown = projects(rows)
        // `countedState`: the waiting agent's project ranks as idle (3), after the one that works (2).
        assertEquals(listOf("works", "waits"), shown.busy.names())
        val waits = shown.busy.last()
        assertEquals(AgentState.Idle, waits.state)
        assertEquals(StateTally(waiting = 1), waits.tally)
        assertEquals(AgentState.Working, shown.busy.first().state)
        // Not idle for the split: a working agent waiting in the background keeps its project busy.
        assertTrue(shown.idle.isEmpty())
        assertEquals(StateTally(working = 1, waiting = 1), shown.tally)
    }

    @Test fun aProjectsRowSaysItsLeadItsCountsAndWhenItLastMoved() {
        val rows = listOf(
            agent("1", "app", state = "working", updated = "2026-01-04T00:00:00Z", tab = "build", index = 1),
            agent("2", "app", state = "unread", updated = "2026-01-02T00:00:00Z", tab = "docs", index = 2),
            agent("3", "app", state = "unread", updated = "2026-01-03T00:00:00Z", tab = "tests", index = 3),
            agent("4", "app", state = "idle", updated = "2026-01-01T00:00:00Z", tab = "old", index = 4),
        )
        val app = projects(rows).busy.single()
        // The lead is the most urgent state, and the latest among those: the unread one that moved last.
        assertEquals("3", app.lead?.session?.sessionId)
        assertEquals(StateTally(working = 1, unread = 2, idle = 1), app.tally)
        assertEquals(AgentState.Unread, app.state)
        assertEquals(Instant.parse("2026-01-04T00:00:00Z"), app.updatedAt)
        assertFalse(app.outsideSpace)
        assertFalse(app.muted)
    }

    @Test fun agentsOutsideAnySpaceAreOneProjectThatListsEachAsARow() {
        val rows = listOf(agent("1", null, name = null), agent("2", null, name = null, state = "idle"), agent("3", "app"))
        val shown = projects(rows)
        val loose = (shown.busy + shown.idle).single { it.outsideSpace }
        assertEquals(setOf("1", "2"), loose.space.members.map { it.session.sessionId }.toSet())
        // It counts as one project, as iOS's summary does, and its agents are in the page's agent count.
        assertEquals(2, shown.projects)
        assertEquals(3, shown.agents)
    }

    @Test fun aMutedProjectIsFlaggedAndKeepsItsPlace() {
        val rows = listOf(agent("1", "quiet-muted"), agent("2", "plain", updated = "2026-01-01T00:00:00Z"))
        val shown = projects(rows, mutes = listOf("mac/quiet-muted"))
        assertEquals(listOf("quiet-muted", "plain"), shown.busy.names())
        assertTrue(shown.busy.first().muted)
        assertFalse(shown.busy.last().muted)
    }

    @Test fun theTallyCountsIdleAsSeenOrUnknownWithoutTheAgentsThatWait() {
        val rows = listOf(agent("1", state = "idle"), agent("2", state = "unknown"), agent("3", waiting = true), agent("4", state = "needs_you"),
            agent("5", state = "unread"), agent("6"))
        assertEquals(StateTally(needsYou = 1, working = 1, unread = 1, waiting = 1, idle = 2), rows.stateTally())
        assertEquals(StateTally(), emptyList<AgentSession>().stateTally())
    }
}

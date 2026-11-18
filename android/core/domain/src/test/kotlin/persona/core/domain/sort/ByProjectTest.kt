package persona.core.domain.sort

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.AgentState
import persona.core.contracts.Machine
import persona.core.contracts.Session

/**
 * By project's spaces (#4338 A7, A8), written from iOS: the agents that only wait in the background are counted apart from
 * Working on a space's heading (`SpaceGroup.waitingInBackground`, `StateCounts`), and a busy space keeps its idle agents for
 * the row that folds them again (`agentRows`, `collapseRow`: "Hide N Idle").
 */
class ByProjectTest {
    private val machines = listOf(Machine("laptop", "Laptop", online = true))
    private val byProject = AgentListSettings(grouping = AgentGrouping.MachineSpace)

    private fun row(id: String, space: String, state: String = "working", waiting: Boolean = false, tab: String? = null) =
        AgentSession(Session(machineId = "laptop", sessionId = id, spaceId = space, spaceName = space, state = state, title = id,
            backgroundWaiting = waiting, slotId = tab?.let { "slot-$it" }, slotName = tab, updatedAt = "2026-01-01T00:00:00Z"))

    private fun list(rows: List<AgentSession>, settings: AgentListSettings = byProject) = buildAgentList(rows, machines, settings = settings)
    private fun AgentListResult.space(name: String) = groups.single { it.title.shown == name }

    @Test fun aSpaceCountsTheAgentsThatOnlyWaitInTheBackgroundApartFromWorking() {
        val rows = listOf(
            row("build", "app"), row("watch", "app", waiting = true), row("poll", "app", waiting = true), row("asks", "app", "needs_you"),
            row("calm", "notes"),
        )
        val app = list(rows).space("app")
        // The hub keeps them working; the heading draws them with a clock instead, and Working leaves them out.
        assertEquals(2, app.waitingInBackground)
        assertEquals(3, app.stateCounts!![AgentState.Working])
        assertEquals(0, list(rows).space("notes").waitingInBackground)
        // The computer's heading counts them the same way.
        assertEquals(2, list(rows).machines.single().waitingInBackground)
        // Only a working agent can wait in the background.
        assertEquals(0, list(listOf(row("seen", "app", "idle", waiting = true), row("done", "app", "unread", waiting = true))).space("app").waitingInBackground)
    }

    @Test fun theSectionOfPinnedAgentsCountsThemApartToo() {
        val rows = listOf(row("watch", "app", waiting = true), row("build", "app"), row("other", "notes"))
        val pins = AgentPreferences(sessions = listOf("laptop/watch", "laptop/build"))
        for (grouping in listOf(AgentGrouping.MachineSpace, AgentGrouping.Repo, AgentGrouping.State)) {
            val pinned = buildAgentList(rows, machines, pins = pins, settings = AgentListSettings(grouping = grouping)).groups.single { it.id == "pinned" }
            assertEquals("$grouping", 1, pinned.waitingInBackground)
        }
    }

    @Test fun aRepositoryAndItsTreesCountThemApartToo() {
        fun at(id: String, cwd: String, waiting: Boolean) =
            AgentSession(Session(machineId = "laptop", sessionId = id, cwd = cwd, state = "working", backgroundWaiting = waiting))
        val byRepo = AgentListSettings(grouping = AgentGrouping.Repo)
        // A repository with its main checkout alone is one group; one with working trees has a heading and a group a tree.
        assertEquals(1, list(listOf(at("a", "/code/app", true), at("b", "/code/app", false)), byRepo).groups.single().waitingInBackground)
        val trees = list(listOf(at("a", "/code/app", true), at("b", "/code/app/.worktrees/x", false), at("c", "/code/app/.worktrees/x", true)), byRepo)
        assertEquals(2, trees.groups.single { it.repo == RepoLevel.Repository }.waitingInBackground)
        assertEquals(1, trees.groups.single { it.repo == RepoLevel.Worktree }.waitingInBackground)
        assertEquals(1, trees.groups.single { it.repo == RepoLevel.MainCheckout }.waitingInBackground)
    }

    @Test fun aBusySpaceKeepsItsIdleAgentsForTheRowThatFoldsThemAgain() {
        val rows = listOf(
            row("build", "app"), row("review", "app", "idle", tab = "review"), row("research", "app", "idle", tab = "research"),
            row("calm", "notes", "idle"),
        )
        val closed = list(rows).space("app")
        assertEquals(setOf("review", "research"), closed.idleFold!!.rows.map { it.session.sessionId }.toSet())
        assertFalse(closed.idleFold!!.open)
        assertEquals(listOf("build"), closed.visibleRows.map { it.session.sessionId })
        // Opened by "N more idle", every agent shows and the fold says so.
        val opened = list(rows, byProject.copy(expandedIdle = setOf(closed.id))).space("app")
        assertTrue(opened.idleFold!!.open)
        assertEquals(3, opened.visibleRows.size)
        // A search shows them with no row, so does a quick filter; a space with nothing going on folds whole.
        assertNull(list(rows, byProject.copy(query = "re")).space("app").idleFold)
        assertNull(list(rows, byProject.copy(quick = AgentQuickFilter.Working)).space("app").idleFold)
        assertNull(list(rows).space("notes").idleFold)
    }
}

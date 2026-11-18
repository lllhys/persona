package persona.core.domain.sort

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.LoopInfo
import java.time.Instant

/**
 * By Repository's idle repositories and idle line (#4338 A8), written from iOS: which repositories are idle
 * (`AgentListProjection` `quiet`, `AgentListOrder.stays`), an idle repository's row (`QuietSpaceRow`: "N agents · tab
 * names" and when it last moved), and a working tree's idle agents behind one line (`agentRows`, `RepoIdleLine`:
 * "2 idle · review · research", then "Hide 2 Idle").
 */
class IdleRepositoriesTest {
    private val machines = listOf(Machine("laptop", "Laptop", online = true))
    private val byRepo = AgentListSettings(grouping = AgentGrouping.Repo)

    private fun at(id: String, cwd: String, state: String, tab: String? = null, index: Int? = null,
                   updated: String = "2026-01-01T00:00:00Z", awaitsReply: Boolean = false, loop: Boolean = false) =
        AgentSession(Session(machineId = "laptop", sessionId = id, title = "Title of $id", cwd = cwd, state = state,
            slotId = tab?.let { "slot-$it" }, slotName = tab, slotIndex = index, updatedAt = updated, awaitsReply = awaitsReply,
            loop = if (loop) LoopInfo(kind = "loop", prompt = "check") else null))

    private fun list(rows: List<AgentSession>, settings: AgentListSettings = byRepo) = buildAgentList(rows, machines, settings = settings)
    private fun AgentListResult.group(id: String) = groups.single { it.id == id }

    @Test fun aRepositoryIsIdleWhenEveryAgentInItIsIdleAndNothingWaits() {
        val rows = listOf(
            at("busy", "/code/app", "working"), at("seen", "/code/app", "idle"),
            at("notes", "/code/notes", "idle"), at("todo", "/code/notes", "idle"),
            at("unread", "/code/blog", "unread"),
            at("asks", "/code/ask", "idle", awaitsReply = true),
            at("loops", "/code/loop", "idle", loop = true),
        )
        val idle = list(rows).groups.filter { it.isIdleRepository }.map { it.title.shown }
        // Working, unread, awaiting the user's reply or waiting on a loop keeps a repository busy, as iOS's `stays`.
        assertEquals(listOf("notes"), idle)
        // Under a quick filter nothing is idle: every repository it leaves is listed.
        assertTrue(list(rows, byRepo.copy(quick = AgentQuickFilter.Unread)).groups.none { it.isIdleRepository })
        // A working tree with nothing going on keeps its line in its repository: it is no idle repository.
        val tree = list(listOf(at("main", "/code/app", "working"), at("old", "/code/app/.worktrees/old", "idle")))
        assertTrue(tree.groups.none { it.isIdleRepository })
        assertEquals("quiet", tree.group("tree/laptop//code/app/.worktrees/old").parent)
    }

    @Test fun anIdleRepositorysRowSaysHowManyAgentsTheirFirstTabsAndWhenItLastMoved() {
        val rows = listOf(
            at("a", "/code/notes", "idle", tab = "minutes", updated = "2026-01-03T00:00:00Z"),
            at("b", "/code/notes", "idle", updated = "2026-01-05T00:00:00Z"),
            at("c", "/code/notes", "idle", tab = "2", index = 2, updated = "2026-01-01T00:00:00Z"),
            at("d", "/code/notes", "idle", tab = "planning", updated = "2026-01-02T00:00:00Z"),
            at("e", "/code/notes", "idle", tab = "todo", updated = "2026-01-04T00:00:00Z"),
        )
        val line = idleRepoLine(list(rows).groups.single { it.isIdleRepository })
        assertEquals(5, line.agents)
        // The tab names only (an agent without one names none), the first three in the list's order (latest first);
        // herdr's bare number reads "#2".
        assertEquals(listOf("todo", "minutes", "planning"), line.tabs)
        assertEquals(Instant.parse("2026-01-05T00:00:00Z"), line.updatedAt)
        assertEquals(listOf("#2"), idleRepoLine(list(listOf(rows[2])).groups.single()).tabs)
    }

    @Test fun aBusyTreeFoldsItsIdleAgentsBehindOneLineThatOpensAndFoldsAgain() {
        val rows = listOf(
            at("build", "/code/app", "working", tab = "build"),
            at("review", "/code/app", "idle", tab = "review", updated = "2026-01-03T00:00:00Z"),
            at("research", "/code/app", "idle", tab = "research", updated = "2026-01-02T00:00:00Z"),
            at("untabbed", "/code/app", "idle", updated = "2026-01-01T00:00:00Z"),
            at("older", "/code/app", "idle", tab = "older", updated = "2025-12-01T00:00:00Z"),
        )
        val id = "tree/laptop//code/app"
        val folded = list(rows).group(id)
        val fold = folded.idleFold!!
        assertEquals(listOf("review", "research", "untabbed", "older"), fold.rows.map { it.session.sessionId })
        assertFalse(fold.open)
        // "4 idle · review · research · Title of untabbed": the first three, a title where there is no tab name.
        assertEquals(listOf("review", "research", "Title of untabbed"), fold.names)
        assertEquals(listOf("build"), folded.visibleRows.map { it.session.sessionId })
        // Opened, every agent shows and the line folds them again.
        val opened = list(rows, byRepo.copy(expandedIdle = setOf(id))).group(id)
        assertTrue(opened.idleFold!!.open)
        assertEquals(5, opened.visibleRows.size)
        // A search shows them all with no line; so does a quick filter, which lists only what it leaves.
        assertNull(list(rows, byRepo.copy(query = "Title")).group(id).idleFold)
        assertNull(list(rows, byRepo.copy(quick = AgentQuickFilter.Working)).group(id).idleFold)
        // A tree with nothing going on folds whole: no line of its own.
        assertNull(list(rows.drop(1)).groups.single().idleFold)
        // By project's spaces carry their idle agents too; its tree draws them only as the row that folds them again.
        assertTrue(list(rows, AgentListSettings(grouping = AgentGrouping.MachineSpace)).groups.all { it.repo == null })
    }
}

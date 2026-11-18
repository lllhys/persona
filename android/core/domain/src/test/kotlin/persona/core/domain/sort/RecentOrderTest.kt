package persona.core.domain.sort

import persona.core.contracts.Session
import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.Instant

/** Recently Used (#9199): port of iOS `AgentListOrderTests` recent cases. */
class RecentOrderTest {
    private fun row(id: String, machine: String = "m", updated: Long?, state: String = "idle", status: String = state) =
        AgentSession(
            Session(
                machineId = machine, sessionId = id, title = id, status = status, state = state,
                // parseTime drops times before 2000; offset from a 2026 base like the other list tests.
                updatedAt = updated?.let { Instant.parse("2026-01-01T00:00:00Z").plusSeconds(it).toString() },
            ),
        )

    @Test fun recentActivityMixesProjectsStatesAndOfflineHistory() {
        val list = listOf(
            row("needs", updated = 10, state = "needs_you"),
            row("seen", updated = 30, state = "idle"),
            row("work", updated = 40, state = "working"),
            row("history", updated = 50, state = "offline", status = "offline"),
        )
        assertEquals(listOf("history", "work", "seen", "needs"), recentAgentOrder(list).map { it.session.sessionId })
    }

    @Test fun readingAReplyKeepsItsRecentPosition() {
        val older = row("older", updated = 10, state = "working")
        val latest = row("latest", updated = 40, state = "unread")
        assertEquals(listOf("latest", "older"), recentAgentOrder(listOf(older, latest)).map { it.session.sessionId })
        val read = row("latest", updated = 40, state = "idle")
        assertEquals(listOf("latest", "older"), recentAgentOrder(listOf(older, read)).map { it.session.sessionId })
    }

    @Test fun recentTiesUseTheWholeIdentityAndMissingTimesComeLast() {
        val a = row("same", machine = "a", updated = 7)
        val b = row("same", machine = "b", updated = 7)
        val unknown = row("unknown", updated = null)
        assertEquals(listOf("a/same", "b/same", "m/unknown"), recentAgentOrder(listOf(unknown, b, a)).map { it.id })
        assertEquals(listOf("a/same", "b/same", "m/unknown"), recentAgentOrder(listOf(a, unknown, b)).map { it.id })
    }

    @Test fun recentlyUsedIsOneFlatSectionLatestFirstIncludingOffline() {
        val rows = listOf(
            row("old", updated = 10, state = "working"),
            row("new", updated = 50, state = "offline", status = "offline"),
            row("mid", updated = 30, state = "idle"),
        )
        val list = buildAgentList(rows, settings = AgentListSettings(grouping = AgentGrouping.Recent))
        assertEquals(listOf(RecentGroup), list.groups.filter { it.id != "pinned" }.map { it.id })
        assertEquals(listOf("new", "mid", "old"), list.groups.single { it.id == RecentGroup }.rows.map { it.session.sessionId })
        assertEquals("Recently Used", list.groups.single { it.id == RecentGroup }.title.english())
    }

    @Test fun pinnedStaysAboveRecentlyUsed() {
        val rows = listOf(row("a", updated = 10), row("b", updated = 20))
        val list = buildAgentList(
            rows, pins = AgentPreferences(sessions = listOf("m/a")),
            settings = AgentListSettings(grouping = AgentGrouping.Recent),
        )
        assertEquals(listOf("pinned", RecentGroup), list.groups.map { it.id })
        assertEquals(listOf("a"), list.groups[0].rows.map { it.session.sessionId })
        assertEquals(listOf("b"), list.groups[1].rows.map { it.session.sessionId })
    }

    @Test fun emptyRecentListEmitsNoSection() {
        val list = buildAgentList(emptyList(), settings = AgentListSettings(grouping = AgentGrouping.Recent))
        assertEquals(emptyList<String>(), list.groups.map { it.id })
    }

    @Test fun recentlyUsedSectionNeverFolds() {
        val rows = listOf(row("a", updated = 10))
        val list = buildAgentList(
            rows,
            settings = AgentListSettings(grouping = AgentGrouping.Recent, folded = setOf(RecentGroup)),
        )
        assertEquals(listOf(RecentGroup), list.groups.map { it.id })
        assertEquals(false, list.groups.single().folded)
        assertEquals(1, list.groups.single().visibleRows.size)
    }
}

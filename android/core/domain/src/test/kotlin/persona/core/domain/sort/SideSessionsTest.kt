package persona.core.domain.sort

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.domain.answer.requestSourceLabel
import persona.core.domain.tasks.dockAgents

/**
 * Side sessions (#7944): a Claude desktop app's side chat folds under its topmost listed parent (`folded_as: "side"`)
 * and is never a row or a count of its own; the parent shows the sides it holds in its folded state.
 */
class SideSessionsTest {
    private val parent = Session("m", "p", title = "Plan the release", state = "idle", status = "idle",
        children = SessionChildren(total = 4, working = 1, needsYou = 1, sides = 3, sidesWorking = 1, sidesNeedsYou = 1, sidesUnread = 1))
    private val asking = side("ask", "needs_you", updated = "2026-11-15T09:00:00Z")
    private val working = side("work", "working", updated = "2026-11-15T08:00:00Z")
    private val finished = side("done", "unread", updated = "2026-11-15T10:00:00Z")
    /** The working side's own Codex subtask: folded through it, under the same topmost parent. */
    private val subtask = Session("m", "codex", agent = "codex", sourceKind = "codex_app_server", isSubagent = true,
        parentSessionId = "work", foldedUnder = "p", foldedSide = "work", state = "working", status = "working")
    private val rows = listOf(parent, asking, working, finished, subtask)

    private fun side(id: String, state: String, updated: String? = null, status: String = if (state == "working") "working" else "idle") =
        Session("m", id, title = "Side $id", state = state, status = status, foldedUnder = "p", foldedAs = FoldedAs.SIDE,
            parentAgentId = "p", updatedAt = updated)

    /** The one source line's key ("From side session · …"), out of the joined label. */
    private fun Interaction.sourceKey() = (requestSourceLabel()?.args?.getOrNull(1) as? CoreText)?.key

    @Test fun sidesAndTheirSubtasksAreNoRowsAndTheParentFoldsWhatItHolds() {
        val listed = foldRows(rows.map(::AgentSession))
        assertEquals(listOf("p"), listed.map { it.session.sessionId })
        assertEquals(AgentState.NeedsYou, listed.single().state)
        assertEquals("idle", parent.state)
        // The side that waited is answered here: the held rows speak at once, ahead of the hub's next rollup.
        val answered = rows.map { if (it.key == asking.key) it.copy(state = "working", status = "working") else it }
        assertEquals(AgentState.Working, foldRows(answered.map(::AgentSession)).single().state)
        // Every side seen and stopped: nothing left to say, the parent's own state.
        val quiet = rows.filter { it.key != subtask.key }.map { if (it.isFoldedSide) it.copy(state = "idle", status = "idle") else it }
        assertEquals(AgentState.Idle, foldRows(quiet.map(::AgentSession)).single().state)
    }

    @Test fun anUnreadSideMakesItsParentUnreadButNotACodexChildOrAWorkingParent() {
        val onlyUnread = listOf(parent.copy(children = null), finished)
        assertEquals(AgentState.Unread, foldRows(onlyUnread.map(::AgentSession)).single().state)
        // Seen on this phone (the list's own copy says idle): the parent is no longer unread.
        assertEquals(AgentState.Idle, foldRows(listOf(parent.copy(children = null), finished.copy(state = "idle")).map(::AgentSession)).single().state)
        // A paged parent whose sides this list does not hold reads the hub's side rollup.
        assertEquals(AgentState.Unread, parent.copy(children = SessionChildren(1, sides = 1, sidesUnread = 1)).foldedState)
        assertEquals(AgentState.Working, parent.copy(state = "working", children = SessionChildren(1, sides = 1, sidesUnread = 1)).foldedState)
        assertEquals(AgentState.Idle, parent.copy(children = SessionChildren(1)).foldedState)
        val unreadChild = Session("m", "c", state = "unread", foldedUnder = "p")
        assertEquals(AgentState.Idle, foldRows(listOf(parent.copy(children = SessionChildren(1)), unreadChild).map(::AgentSession)).single().state)
    }

    @Test fun thePanelListsSidesFlatNeedsYouThenWorkingThenNewestStoppedLast() {
        val stopped = side("gone", "idle", updated = "2026-11-15T11:00:00Z", status = "offline")
        val sideOfSide = side("deep", "idle", updated = "2026-11-15T07:00:00Z").copy(parentAgentId = "done")
        val all = rows + stopped + sideOfSide + Session("other", "x", foldedUnder = "p", foldedAs = FoldedAs.SIDE)
        assertEquals(listOf("ask", "work", "done", "deep", "gone"), sideSessions(all, parent.key).map { it.sessionId })
        assertEquals(SideRollup(5, needsYou = true), sideRollup(parent, sideSessions(all, parent.key), all))
        // A side whose own subtask needs the user waits for the user in the panel too.
        val waitingThrough = listOf(parent, working.copy(state = "idle", status = "idle"), subtask.copy(state = "needs_you"))
        assertEquals(AgentState.NeedsYou, sideState(waitingThrough[1], waitingThrough))
        // Nothing held (a paged parent): the hub's counts.
        assertEquals(SideRollup(3, needsYou = true), sideRollup(parent, emptyList()))
        assertEquals(SideRollup(), sideRollup(parent.copy(children = SessionChildren(2)), emptyList()))
    }

    @Test fun cardsAndSubtaskCardsBelongToTheSideAndCountOnceUnderTheParent() {
        val asks = Interaction("q1", machineId = "m", sessionId = "ask", kind = "question", foldedUnder = "p", foldedAs = FoldedAs.SIDE,
            sessionTitle = "Which changelog format?")
        val subtaskAsks = Interaction("q2", machineId = "m", sessionId = "codex", kind = "permission", foldedUnder = "p", foldedSide = "work")
        assertEquals(parent.key, asks.homeKey)
        assertEquals(asking.key, asks.sideHome)
        assertEquals(working.key, subtaskAsks.sideHome)
        assertNull(asks.copy(foldedAs = null).sideHome)
        assertEquals(CoreText.Key.RequestFromSide, asks.sourceKey())
        assertEquals(CoreText.Key.RequestFromSubagent, subtaskAsks.copy(sessionTitle = "Codex").sourceKey())
        val waiting = needsYouRows(listOf(asks, subtaskAsks), needsYouWaiting(rows))
        assertEquals(listOf(parent.key), waiting.map { it.key })
        assertEquals(2, waiting.single().cards.size)
    }

    @Test fun noListDockCountOrMachineBringsASideOrItsSubtaskBackAsARoot() {
        // By Machine only sections computers from the hub's machine list; pass the parent's host (#9319).
        val hosts = listOf(Machine("m", "Example"))
        for (grouping in AgentGrouping.selectable) {
            val list = buildAgentList(rows.map(::AgentSession), hosts, settings = AgentListSettings(grouping = grouping, hierarchy = true))
            assertEquals(grouping.name, setOf("p"), list.groups.flatMap { it.rows }.map { it.session.sessionId }.toSet())
            assertTrue(grouping.name, list.children.values.flatten().isEmpty())
            assertEquals(grouping.name, 1, list.counts[AgentQuickFilter.NeedsYou])
            assertEquals(grouping.name, 0, list.counts[AgentQuickFilter.Working])
        }
        assertEquals(listOf("p"), dockAgents("main", "main", rows, emptyList(), hierarchy = true).map { it.sessionId })
        assertEquals(StateTally(needsYou = 1), foldRows(rows.map(::AgentSession)).stateTally())
        assertEquals(1, machineProjects(rows.map(::AgentSession), "m", AgentPreferences(), AgentPreferences()).agents)
        // The parent gone from the list: its sides are rows again, as the hub lists them.
        assertEquals(setOf("ask", "work", "done"), foldRows(rows.drop(1).map { it.copy(foldedUnder = null, foldedAs = null) }
            .filter { it.key != subtask.key }.map(::AgentSession)).map { it.session.sessionId }.toSet())
    }

    @Test fun theParentsChatHasNoSideCardsAndASideChatHasItsOwnSubtasks() {
        val child = Session("m", "child", foldedUnder = "p", state = "idle", startedAt = "2026-11-15T06:00:00Z")
        val all = rows + child
        assertEquals(listOf("child"), childSessions(all, parent.key, sides = true).map { it.sessionId })
        assertEquals(setOf("ask", "work", "done", "codex", "child"), childSessions(all, parent.key).map { it.sessionId }.toSet())
        assertEquals(listOf("codex"), sideChildren(all, working.key).map { it.sessionId })
        assertTrue(sideChildren(all, asking.key).isEmpty())
    }

    @Test fun aSearchInsideASideKeepsTheParentsRowAndLandsOnTheSide() {
        val settings = AgentListSettings(query = "changelog", hierarchy = true)
        val named = rows.map { if (it.key == asking.key) it.copy(title = "Which changelog format?") else it }
        val list = buildAgentList(named.map(::AgentSession), settings = settings)
        assertEquals(listOf("p"), list.groups.flatMap { it.rows }.map { it.session.sessionId })
        assertEquals(asking.key, settings.sideHit(parent, named))
        assertNull(settings.sideHit(parent.copy(title = "Changelog"), named))
        assertNull(AgentListSettings().sideHit(parent, named))
        assertNull(settings.copy(query = "zzz").sideHit(parent, named))
        assertTrue(buildAgentList(named.map(::AgentSession), settings = settings.copy(query = "nothing here")).groups.all { it.rows.isEmpty() })
    }
}

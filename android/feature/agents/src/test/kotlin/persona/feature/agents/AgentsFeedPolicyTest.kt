package persona.feature.agents

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Interaction
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.domain.sort.AgentListSettings
import persona.core.domain.sort.AgentMachineGroup
import persona.core.domain.sort.AgentQuickFilter

/** The Agents feed's rules with no list under them: when a hold stands, what lands at once, what counts as news, and where a rebuilt list resumes. */
class AgentsFeedPolicyTest {
    private val mac = Machine("mac", "Mac", online = true)
    private val box = Machine("box", "Box", online = true)

    private fun session(id: String, state: String = "working", machine: String = "mac", title: String = id, since: String? = null) =
        feedSession(id, state, machine, title, since)

    private fun page(
        vararg sessions: Session, machines: List<Machine> = listOf(mac), paged: Boolean = false, loaded: Boolean = true,
        settings: AgentListSettings = AgentListSettings(), folded: Boolean = false,
    ) = feedPage(sessions.toList(), machines, paged, loaded, settings, folded)

    private val scope = listOf<Any?>("grouping", 360, 800)
    private fun frozen(page: AgentsPageState, scope: List<Any?> = this.scope) = AgentsFeedFrozen(page, scope)
    private fun rowStates(p: AgentsPageState) = p.list.groups.flatMap { it.rows }.map { it.session.sessionId to it.session.state }

    // The hold

    @Test fun aReorderANewRowAndALastLineWaitWhileTheRowsHeldAreStillThere() {
        val held = page(session("a"), session("b"), session("c"))
        assertTrue(holdsAgainst(frozen(held), page(session("c", title = "c, more"), session("n"), session("b"), session("a")), scope))
    }

    @Test fun aChangedControlOrWindowLetsTheHoldGo() {
        val held = page(session("a"), session("b"))
        assertFalse("grouping, sort, filter, search and folds are in the scope", holdsAgainst(frozen(held), held, listOf("sort", 360, 800)))
        assertFalse("a resized window starts the list over", holdsAgainst(frozen(held), held, listOf("grouping", 360, 600)))
        assertTrue(holdsAgainst(frozen(held), held, scope))
    }

    @Test fun anEmptiedListOrOneWithNoRowHeldLeftLetsTheHoldGo() {
        val held = page(session("a"), session("b"))
        assertFalse("nothing is left to keep still", holdsAgainst(frozen(held), page(), scope))
        assertFalse("every key held is gone", holdsAgainst(frozen(held), page(session("x"), session("y")), scope))
        assertTrue("one survivor is enough to keep the rest still", holdsAgainst(frozen(held), page(session("x"), session("b")), scope))
    }

    @Test fun aHoldOverAnEmptyListHasNothingToKeepStill() {
        assertFalse(holdsAgainst(frozen(page()), page(), scope))
        assertFalse("rows arriving in a list that had none land at once", holdsAgainst(frozen(page()), page(session("a")), scope))
    }

    @Test fun aFirstPageThatIsNotInYetIsNeverHeld() {
        assertFalse(holdsAgainst(frozen(page(session("a"))), page(session("a"), loaded = false), scope))
    }

    @Test fun aFetchedPageLandsAtOnceButAPushedSessionDoesNot() {
        val held = page(session("a"), session("b"), paged = true)
        assertFalse("the old window is the start of the new one", holdsAgainst(frozen(held), page(session("a"), session("b"), session("c"), paged = true), scope))
        assertTrue("a session a push put in front is a live insertion", holdsAgainst(frozen(held), page(session("n"), session("a"), session("b"), paged = true), scope))
        assertTrue("so is one in the middle", holdsAgainst(frozen(held), page(session("a"), session("n"), session("b"), paged = true), scope))
        assertTrue("a window that moved some other way waits", holdsAgainst(frozen(held), page(session("b"), session("a"), paged = true), scope))
        assertTrue("a list that is not paged only gets pushes", holdsAgainst(frozen(page(session("a"), session("b"))), page(session("a"), session("b"), session("c")), scope))
        assertFalse("the list went from whole to paged", holdsAgainst(frozen(page(session("a"))), page(session("a"), paged = true), scope))
    }

    @Test fun growsPageNeedsTheOldWindowAsTheStartOfTheNewOneInTheSameOrder() {
        val held = page(session("a"), session("b"), paged = true)
        assertTrue(growsPage(held, page(session("a"), session("b"), session("z"), paged = true)))
        assertFalse("same size", growsPage(held, page(session("a"), session("c"), paged = true)))
        assertFalse("longer but in front", growsPage(held, page(session("z"), session("a"), session("b"), paged = true)))
        assertFalse("longer but reordered", growsPage(held, page(session("b"), session("a"), session("z"), paged = true)))
        assertFalse("longer but a held session is gone", growsPage(held, page(session("a"), session("c"), session("d"), paged = true)))
    }

    // What lands at once

    @Test fun aSessionThatStartsNeedingYouGetsItsStateOnItsRowAndNothingElseMoves() {
        val held = page(session("a", title = "old a"), session("b"), session("c"))
        val latest = page(session("c"), session("a", "needs_you", title = "new a"), session("b", "working", title = "new b"))
        val landed = landCritical(held, latest)
        assertEquals("the order and the groups wait", listOf("a", "b", "c"), landed.list.groups.single().rows.map { it.session.sessionId })
        assertEquals("needs_you", landed.list.groups.single().rows[0].session.state)
        assertEquals("the rest of the row waits", "old a", landed.list.groups.single().rows[0].session.title)
        assertEquals("the same row in the drawn rows", "needs_you", landed.list.groups.single().visibleRows[0].session.state)
        assertEquals("needs_you", landed.sessions.single { it.sessionId == "a" }.state)
        assertEquals("the sessions take the state alone too", "old a", landed.sessions.single { it.sessionId == "a" }.title)
        assertEquals("a row whose state did not change stays as it was", "b", landed.list.groups.single().rows[1].session.title)
    }

    @Test fun aSessionThatGoesOfflineLandsAndOneThatComesBackLandsToo() {
        val held = page(session("a"), session("b", "offline"))
        val landed = landCritical(held, page(session("a", "offline"), session("b", "working")))
        assertEquals(listOf("a" to "offline", "b" to "working"), rowStates(landed))
    }

    @Test fun anOrdinaryChangeLandsNothingAndLeavesThePageItself() {
        val held = page(session("a"), session("b", "unread"))
        assertSame(held, landCritical(held, page(session("b", "idle"), session("a", "unread", title = "more"))))
    }

    @Test fun aSessionThatNeededYouAndIsGoneLeavesButOneThatWorkedStays() {
        val held = page(session("a", "needs_you"), session("b"), session("c", "idle"))
        val landed = landCritical(held, page(session("c", "idle")))
        assertEquals("b stays until the rest lands; a is answered", listOf("b" to "working", "c" to "idle"), rowStates(landed))
        assertEquals(listOf("b", "c"), landed.sessions.map { it.sessionId })
    }

    @Test fun aSessionThatNowNeedsYouAndIsNotListedYetIsAddedToTheSessionsButNotToTheGroups() {
        val held = page(session("a"))
        val landed = landCritical(held, page(session("n", "needs_you"), session("a")))
        assertEquals(listOf("a"), landed.list.groups.single().rows.map { it.session.sessionId })
        assertEquals(setOf("a", "n"), landed.sessions.map { it.sessionId }.toSet())
    }

    @Test fun theComputersLandWhileTheRowsWait() {
        val heading = AgentMachineGroup(mac, listOf("g"), sessionCount = 1, spaceCount = 1, stateCounts = emptyMap(), folded = false)
        val held = page(session("a")).let { it.copy(list = it.list.copy(machines = listOf(heading))) }
        val down = page(session("a"), machines = listOf(mac.copy(online = false)))
        val landed = landCritical(held, down)
        assertNotSame(held, landed)
        assertEquals(down.machines, landed.machines)
        assertFalse("the computer heading's online dot lands too", landed.list.machines.single().machine.online)
        assertEquals("the heading's other grouping facts wait", heading.stateCounts, landed.list.machines.single().stateCounts)
        assertEquals(rowStates(held), rowStates(landed))
    }

    @Test fun aGroupWhoseOnlyNeedsYouRowLeavesDoesNotKeepAnEmptyHeading() {
        val original = page(session("ask", "needs_you"), session("work"))
        val all = original.list.groups.single()
        val held = original.copy(list = original.list.copy(groups = listOf(
            all.copy(id = "ask", rows = all.rows.take(1), visibleRows = all.visibleRows.take(1)),
            all.copy(id = "work", rows = all.rows.drop(1), visibleRows = all.visibleRows.drop(1)),
        )))
        val landed = landCritical(held, page(session("work")))
        assertEquals(listOf("work"), landed.list.groups.map { it.id })
        assertEquals(listOf("work"), landed.list.groups.single().rows.map { it.session.sessionId })
    }

    // What counts as news

    private fun needsYouOf(p: AgentsPageState, requests: List<Interaction> = emptyList(), closed: Set<SessionKey> = emptySet()) =
        agentsFeedNeedsYou(p, requests) { it in closed }

    private fun mark(p: AgentsPageState, requests: List<Interaction> = emptyList()) = agentsFeedMark(p, needsYouOf(p, requests))

    @Test fun theNeedsYouSectionIsWhatNeedsYouMeansHereAndOnlyWhileItIsDrawn() {
        val p = page(session("a", "needs_you"), session("b"))
        assertEquals(setOf("agent:mac/a"), needsYouOf(p))
        assertEquals("a search hides the section", emptySet<String>(), needsYouOf(page(session("a", "needs_you"), settings = AgentListSettings(query = "a"))))
        assertEquals("so does another quick filter", emptySet<String>(),
            needsYouOf(page(session("a", "needs_you"), settings = AgentListSettings(quick = AgentQuickFilter.Working))))
        assertEquals("a session closed on this phone is not in it", emptySet<String>(), needsYouOf(p, closed = setOf(SessionKey("mac", "a"))))
    }

    @Test fun theComputerFilterAppliesSoAnotherComputersNeedsYouIsNotNews() {
        val onlyMac = AgentListSettings(machineFilter = "mac")
        val before = page(session("a"), settings = onlyMac, machines = listOf(mac, box))
        val after = page(session("a"), session("x", "needs_you", machine = "box"), settings = onlyMac, machines = listOf(mac, box))
        assertTrue(needsYouOf(after).isEmpty())
        assertTrue(agentsFeedNotice(mark(before), mark(after)).isEmpty)
        assertEquals("with no filter the same session is news", 1,
            agentsFeedNotice(mark(page(session("a"), machines = listOf(mac, box))),
                mark(page(session("a"), session("x", "needs_you", machine = "box"), machines = listOf(mac, box)))).needsYou)
    }

    @Test fun aRequestOfASessionIsOneRowAndACardOnNoSessionIsARowOfItsOwn() {
        val p = page(session("a"), session("b"))
        val ask = Interaction(id = "i1", machineId = "mac", sessionId = "a", kind = "permission")
        val orphan = Interaction(id = "i2", machineId = "mac", sessionId = "", kind = "permission")
        assertEquals(setOf("agent:mac/a", "card:i2"), needsYouOf(p, listOf(ask, ask.copy(id = "i3"), orphan)))
    }

    @Test fun nothingIsNewBeforeTheUserHasLeftTheTop() {
        assertTrue(agentsFeedNotice(null, mark(page(session("a", "needs_you")))).isEmpty)
    }

    @Test fun aSessionThatStartsNeedingYouIsUrgentAndOnesAlreadyWaitingAreNot() {
        val before = mark(page(session("a", "needs_you"), session("b")))
        val notice = agentsFeedNotice(before, mark(page(session("a", "needs_you"), session("b", "needs_you"))))
        assertEquals(1, notice.needsYou)
        assertTrue(notice.urgent)
        assertEquals("a new Needs You is told once, as the urgent kind", 0, notice.fresh)
        val withNew = agentsFeedNotice(before, mark(page(session("n", "needs_you"), session("a", "needs_you"), session("b"))))
        assertEquals(1, withNew.needsYou)
        assertEquals(0, withNew.fresh)
    }

    @Test fun aRequestOfASessionThatAlreadyNeededYouIsNotNewButAnotherSessionsIs() {
        val p = page(session("a", "needs_you"), session("b"))
        val before = mark(p)
        val ask = { s: String -> Interaction(id = "i-$s", machineId = "mac", sessionId = s, kind = "permission") }
        assertTrue(agentsFeedNotice(before, mark(p, listOf(ask("a")))).isEmpty)
        assertEquals(1, agentsFeedNotice(before, mark(p, listOf(ask("a"), ask("b")))).needsYou)
    }

    @Test fun newRowsAreCountedOnlyWhereATapShowsThemAndOnlyInAWholeList() {
        val before = page(session("a"), session("b"))
        val few = page(session("n1"), session("n2"), session("a"), session("b"))
        assertEquals(2, agentsFeedNotice(mark(before), mark(few)).fresh)
        val many = page(*((1..6).map { session("n$it") } + session("a") + session("b")).toTypedArray())
        assertEquals("only the first rows are sure to be seen", AGENTS_FEED_TOP_ROWS, agentsFeedNotice(mark(before), mark(many)).fresh)
        val four = page(session("a"), session("b"), session("c"), session("d"))
        val deep = page(session("a"), session("b"), session("c"), session("d"), *((1..6).map { session("n$it") }).toTypedArray())
        assertEquals("a row further down is not announced", 0, agentsFeedNotice(mark(four), mark(deep)).fresh)
        val pagedBefore = page(session("a"), paged = true)
        assertEquals("a fetched page is history", 0,
            agentsFeedNotice(mark(pagedBefore), mark(page(session("n1"), session("a"), paged = true))).fresh)
    }

    @Test fun aRowFoldedAwayIsNotANewRowAndOneThatLeavesIsNotNews() {
        val before = page(session("a"), session("b"))
        assertTrue("its group is folded: nothing is drawn", agentsFeedNotice(mark(before), mark(page(session("n"), session("a"), folded = true))).isEmpty)
        assertTrue(agentsFeedNotice(mark(before), mark(page(session("a")))).isEmpty)
    }

    @Test fun aSessionThatStopsRunningNearTheTopCountsButHistoryDoesNot() {
        val before = mark(page(session("a", "working"), session("b")))
        val stopped = agentsFeedNotice(before, mark(page(session("a", "offline"), session("b"))))
        assertEquals(1, stopped.offline)
        assertTrue(stopped.urgent)
        val history = agentsFeedNotice(before, mark(page(session("a", "working"), session("b"), session("old", "offline"))))
        assertEquals("a session the user never saw running did not stop", 0, history.offline)
        val deep = page(*((1..6).map { session("n$it") } + session("c", "offline")).toTypedArray())
        assertEquals("one far down is not shown by a tap", 0,
            agentsFeedNotice(mark(page(session("c"), *((1..6).map { session("n$it") }).toTypedArray())), mark(deep)).offline)
    }

    @Test fun aComputerGoingDownCountsOnceWithItsSessions() {
        val up = page(session("a"), session("b"), machines = listOf(mac))
        val down = page(session("a", "offline"), session("b", "offline"), machines = listOf(mac.copy(online = false)))
        assertEquals("one computer, not three", 1, agentsFeedNotice(mark(up), mark(down)).offline)
        assertEquals("a computer already down is not news", 0, agentsFeedNotice(mark(down), mark(down)).offline)
    }

    @Test fun aComputerOutsideTheFilterGoingDownIsNotNews() {
        val onlyMac = AgentListSettings(machineFilter = "mac")
        val up = page(session("a"), settings = onlyMac, machines = listOf(mac, box))
        val boxDown = page(session("a"), settings = onlyMac, machines = listOf(mac, box.copy(online = false)))
        assertTrue(agentsFeedNotice(mark(up), mark(boxDown)).isEmpty)
    }

    // Resuming a rebuilt list

    @Test fun aSavedRowIsPutBackWhereItStood() {
        val keys = listOf("machine-header:mac", "session:mac/a", "session:mac/b")
        assertEquals(ReadingRestore.Scroll(2, -120), readingRestore(SavedReadingAnchor("session:mac/b", -120), keys, loaded = true))
        assertEquals("still loading: the row may yet come", ReadingRestore.Scroll(1, 0), readingRestore(SavedReadingAnchor("session:mac/a", 0), keys, loaded = false))
    }

    @Test fun aRowThatIsNotThereStartsFromTheTopOnceTheListIsIn() {
        val keys = listOf("session:mac/a")
        val saved = SavedReadingAnchor("session:mac/gone", 40)
        assertEquals(ReadingRestore.Top, readingRestore(saved, keys, loaded = true))
        assertEquals("a loaded list that emptied has no place to return to", ReadingRestore.Top, readingRestore(saved, emptyList(), loaded = true))
        assertEquals(ReadingRestore.Wait, readingRestore(saved, keys, loaded = false))
        assertEquals(ReadingRestore.Wait, readingRestore(saved, emptyList(), loaded = false))
    }

    @Test fun theSavedPlaceIsTheFirstTreeRowInViewNotAHeaderAboveTheTree() {
        val treeKeys = setOf("session:mac/a", "session:mac/b")
        val items = listOf<Pair<Any, Int>>("search" to -30, "notice" to -10, "session:mac/a" to 24, "session:mac/b" to 96)
        assertEquals(SavedReadingAnchor("session:mac/a", 24), savedAnchorOf(items, treeKeys))
        assertNull("only headers in view", savedAnchorOf(items.take(2), treeKeys))
        assertNull(savedAnchorOf(emptyList(), treeKeys))
    }

    @Test fun aRowClippedAboveTheViewportKeepsItsNegativeOffset() {
        assertEquals(SavedReadingAnchor("session:mac/a", -35), savedAnchorOf(listOf("session:mac/a" to -35), setOf("session:mac/a")))
    }

    @Test fun theHoldIsBoundedAndSettlesBeforeItsCap() {
        // Configured targets, not measurements: the list rests for half a second, and nothing waits past five seconds.
        assertEquals(500L, AgentsFeedTiming.SETTLE_MILLIS)
        assertEquals(5_000L, AgentsFeedTiming.CAP_MILLIS)
        assertTrue(AgentsFeedTiming.SETTLE_MILLIS < AgentsFeedTiming.CAP_MILLIS)
    }

    @Test fun aStormOfPushesLandsAsTheLastOneAndCountsEachNewThingOnce() {
        val start = page(*((0 until 20).map { session("s$it") }).toTypedArray())
        val baseline = mark(start)
        var latest = start
        repeat(40) { step ->
            // Each push reorders, changes a last line, and every tenth one brings a session that needs the user.
            val rotated = latest.sessions.drop(1) + latest.sessions.first().copy(title = "line $step")
            val extra = if (step % 10 == 9) listOf(session("ask$step", "needs_you")) else emptyList()
            latest = page(*((extra + rotated).toTypedArray()))
        }
        assertTrue("every push of the storm waits for the same hold", holdsAgainst(frozen(start), latest, scope))
        val notice = agentsFeedNotice(baseline, mark(latest))
        assertEquals("each session that came needing the user is told once, however often the list moved", 4, notice.needsYou)
        assertTrue(notice.urgent)
    }
}

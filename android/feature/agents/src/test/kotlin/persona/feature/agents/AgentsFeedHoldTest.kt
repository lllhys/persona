package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.foundation.gestures.ScrollScope
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertTextContains
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.designsystem.PersonaTheme
import persona.core.domain.sort.AgentListSettings

/**
 * The Agents feed over a real list, through the same functions the screen uses: the page is held while the list is
 * scrolled away from the top and lands when it rests, at the cap, at the top or when a control changes; what needs the user
 * lands on the rows at once; what came meanwhile is a pill that never moves the list. Time is the test clock's, so the waits
 * are the configured ones ([AgentsFeedTiming]), not measurements.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AgentsFeedHoldTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val base = (0 until 30).map { feedSession("s%02d".format(it)) }

    private var latest by mutableStateOf(feedPage(base))
    private var scope by mutableStateOf<List<Any?>>(listOf("controls"))
    /** The window's size, which the screen puts in the scope as the size it reads from the configuration. */
    private var windowHeight by mutableStateOf(400.dp)

    private lateinit var state: LazyListState
    private lateinit var uiScope: CoroutineScope
    private lateinit var shownPage: AgentsPageState
    private lateinit var view: AgentsFeedView
    private var gate = CompletableDeferred<Unit>()
    /** How often the screen's own scope was composed: a hold that changes nothing drawn must not add to it. */
    private var screenCompositions = 0

    /** Stands for AgentsScreen: it reads the latest page and the feed, never the scroll state. */
    @Composable private fun Screen() {
        state = rememberLazyListState()
        uiScope = rememberCoroutineScope()
        val feed = rememberAgentsFeed(latest, { p -> agentsFeedNeedsYou(p, emptyList()) { false } }, listOf(scope, windowHeight), state)
        view = feed
        SideEffect {
            shownPage = feed.page
            screenCompositions++
        }
        Box {
            LazyColumn(Modifier.height(windowHeight), state = state) {
                items(feed.page.visibleRowIds(), key = { it }) { id -> Box(Modifier.fillMaxWidth().height(60.dp).testTag(id)) }
            }
            AgentsFeedNote(feed, latest, Modifier.align(Alignment.BottomCenter).testTag("note"))
        }
    }

    private fun start(reduceMotion: Boolean = true) {
        rule.mainClock.autoAdvance = false
        rule.setContent { PersonaTheme(darkTheme = false, reduceMotion = reduceMotion) { Screen() } }
        advance(100)
    }

    private fun advance(millis: Long) {
        rule.mainClock.advanceTimeBy(millis)
        rule.waitForIdle()
    }

    private fun scrollAway() {
        rule.runOnUiThread { uiScope.launch { state.scrollToItem(12) } }
        advance(1_000)
        assertTrue("the list rests below the top", state.canScrollBackward)
    }

    /** A scroll that stays in progress until [release]; [steps] run first, inside it. */
    private fun holdScroll(steps: suspend ScrollScope.() -> Unit = {}) {
        gate = CompletableDeferred()
        val mine = gate
        rule.runOnUiThread { uiScope.launch { state.scroll { steps(); mine.await() } } }
        advance(100)
        assertTrue(state.isScrollInProgress)
    }

    private fun release() {
        gate.complete(Unit)
        advance(50)
    }

    private fun update(change: () -> Unit) {
        rule.runOnUiThread(change)
        // The mutation is seen on one frame; the following frame applies its composition and side effects.
        advance(100)
        advance(100)
    }

    private fun push(change: (Session) -> Session = { it }, extra: List<Session> = emptyList(), reorder: Boolean = false) {
        update {
            val changed = base.map(change)
            latest = feedPage((if (reorder) changed.reversed() else changed) + extra)
        }
    }

    private fun titleOf(id: String) = shownPage.sessions.single { it.sessionId == id }.title
    private fun stateOf(id: String) = shownPage.sessions.single { it.sessionId == id }.state
    private fun order() = shownPage.visibleRowIds()

    private fun line(id: String, title: String = "$id, more") = { s: Session -> if (s.sessionId == id) s.copy(title = title) else s }
    private fun needsYou(id: String) = { s: Session -> if (s.sessionId == id) s.copy(state = "needs_you") else s }

    // The hold

    @Test fun anOrdinaryPushDuringAScrollWaitsUntilTheListHasRestedThenLands() {
        start()
        scrollAway()
        holdScroll()
        push(line("s03"))
        assertEquals("held while the list moves", "s03", titleOf("s03"))
        release()
        advance(AgentsFeedTiming.SETTLE_MILLIS - 150)
        assertEquals("still inside the rest", "s03", titleOf("s03"))
        advance(300)
        assertEquals("landed after the rest", "s03, more", titleOf("s03"))
    }

    @Test fun aHoldLandsAtTheCapWhileTheListKeepsMovingAndTheNextPushWaitsAgain() {
        start()
        scrollAway()
        holdScroll()
        push(line("s03"))
        advance(AgentsFeedTiming.CAP_MILLIS - 1_000)
        assertEquals("not yet", "s03", titleOf("s03"))
        assertTrue("the list is still being scrolled", state.isScrollInProgress)
        advance(1_500)
        assertEquals("the cap passed", "s03, more", titleOf("s03"))

        push(line("s03", "s03, again"))
        assertEquals("the same scroll holds the next push", "s03, more", titleOf("s03"))
        advance(AgentsFeedTiming.CAP_MILLIS + 500)
        assertEquals("and it lands at its own cap, not never", "s03, again", titleOf("s03"))
        release()
    }

    @Test fun aPushAfterTheCapStillLandsWhenTheScrollEnds() {
        start()
        scrollAway()
        holdScroll()
        push(line("s03"))
        advance(AgentsFeedTiming.CAP_MILLIS + 500)
        push(line("s03", "s03, again"))
        assertEquals("s03, more", titleOf("s03"))
        release()
        advance(AgentsFeedTiming.SETTLE_MILLIS + 200)
        assertEquals("s03, again", titleOf("s03"))
    }

    @Test fun aChangedControlLandsAtOnceWhileScrollingAndTheNextPushWaitsAgain() {
        start()
        scrollAway()
        holdScroll()
        push(line("s03"))
        assertEquals("s03", titleOf("s03"))
        update { scope = listOf("another grouping") }
        assertEquals("a control changed: what the list draws is the new list", "s03, more", titleOf("s03"))
        push(line("s03", "s03, again"))
        assertEquals("the scroll goes on, so the next push waits", "s03, more", titleOf("s03"))
        release()
    }

    @Test fun aResizedWindowLandsAtOnceWhileScrollingAndTheNextPushWaitsAgain() {
        start()
        scrollAway()
        holdScroll()
        push(line("s03"))
        assertEquals("s03", titleOf("s03"))
        update { windowHeight = 300.dp }
        assertEquals("the window changed: the hold starts over from the latest", "s03, more", titleOf("s03"))
        push(line("s03", "s03, again"))
        assertEquals("the scroll goes on, so the next push waits", "s03, more", titleOf("s03"))
        release()
    }

    @Test fun aFetchedPageLandsAtOnceWhileScrollingAndTheNextPushWaitsAgain() {
        start()
        update { latest = feedPage(base, paged = true) }
        scrollAway()
        holdScroll()
        update { latest = feedPage(base + feedSession("more1") + feedSession("more2"), paged = true) }
        assertTrue("the page is in under the finger, with no gap", shownPage.sessions.any { it.sessionId == "more2" })
        update { latest = feedPage(base.map(line("s03")) + feedSession("more1") + feedSession("more2"), paged = true) }
        assertEquals("a push after it waits", "s03", titleOf("s03"))
        release()
    }

    @Test fun aSessionAPushPutsInFrontOfAPagedListWaitsAndIsNotTakenForAPage() {
        start()
        update { latest = feedPage(base, paged = true) }
        scrollAway()
        holdScroll()
        update { latest = feedPage(listOf(feedSession("fresh")) + base, paged = true) }
        assertFalse("held, not released as a fetched page", shownPage.sessions.any { it.sessionId == "fresh" })
        release()
        advance(AgentsFeedTiming.SETTLE_MILLIS + 200)
        assertTrue(shownPage.sessions.any { it.sessionId == "fresh" })
    }

    @Test fun aReplacedOrEmptiedListLandsAtOnceWhileScrolling() {
        start()
        scrollAway()
        holdScroll()
        update { latest = feedPage(listOf(feedSession("x1"), feedSession("x2"))) }
        assertEquals(setOf("x1", "x2"), shownPage.sessions.map { it.sessionId }.toSet())
        update { latest = feedPage(emptyList()) }
        assertTrue(shownPage.sessions.isEmpty())
        release()
    }

    @Test fun reachingTheTopLetsTheHoldGoWhileTheFingerIsStillDown() {
        start()
        scrollAway()
        val toTop = CompletableDeferred<Unit>()
        holdScroll { toTop.await(); scrollBy(-1_000_000f) }
        push(line("s03"))
        assertEquals("s03", titleOf("s03"))
        toTop.complete(Unit)
        advance(100)
        assertFalse(state.canScrollBackward)
        assertEquals("the latest is what the top shows", "s03, more", titleOf("s03"))
        release()
    }

    @Test fun aDragAtTheTopHoldsNothing() {
        start()
        holdScroll()
        push(line("s03"))
        assertEquals("the top shows new arrivals as they come", "s03, more", titleOf("s03"))
        release()
    }

    // What needs the user

    @Test fun whatNeedsTheUserLandsOnItsRowAtOnceWhileTheRestWaits() {
        start()
        scrollAway()
        holdScroll()
        val before = order()
        push({ s -> needsYou("s03")(line("s04")(s)) }, reorder = true)
        assertEquals("the row shows what is so", "needs_you", stateOf("s03"))
        assertEquals("its order and the others' last lines wait", before, order())
        assertEquals("s04", titleOf("s04"))
        release()
        advance(AgentsFeedTiming.SETTLE_MILLIS + 200)
        assertEquals("the rest lands when the list rests", "s04, more", titleOf("s04"))
        assertEquals(before.reversed(), order())
    }

    @Test fun aSessionThatGoesOfflineLandsAtOnceWhileScrolling() {
        start()
        scrollAway()
        holdScroll()
        push({ s -> if (s.sessionId == "s05") s.copy(state = "offline") else s })
        assertEquals("offline", stateOf("s05"))
        release()
    }

    // The pill

    @Test fun aNewNeedsYouAwayFromTheTopIsAPillAtOnceAndTheListStaysPut() {
        start()
        scrollAway()
        val (index, offset) = state.firstVisibleItemIndex to state.firstVisibleItemScrollOffset
        rule.onNodeWithTag("note").assertDoesNotExist()
        push(needsYou("s20"))
        rule.onNodeWithTag("note").assertIsDisplayed().assertTextContains("1 needs you")
        assertEquals(index, state.firstVisibleItemIndex)
        assertEquals(offset, state.firstVisibleItemScrollOffset)
    }

    @Test fun anotherComputersNeedsYouIsNoPillWhenTheListIsFilteredToOne() {
        start()
        val onlyMac = AgentListSettings(machineFilter = "mac")
        update { latest = feedPage(base, settings = onlyMac) }
        scrollAway()
        update { latest = feedPage(base + feedSession("far", "needs_you", machine = "box"), settings = onlyMac) }
        rule.onNodeWithTag("note").assertDoesNotExist()
    }

    @Test fun aNewRowNearTheTopIsTheQuietKindOfNote() {
        start()
        scrollAway()
        update { latest = feedPage(listOf(feedSession("fresh")) + base) }
        rule.onNodeWithTag("note").assertIsDisplayed().assertTextContains("1 new")
    }

    @Test fun nothingIsToldAtTheTop() {
        start()
        push(needsYou("s02"), extra = listOf(feedSession("n1")))
        rule.onNodeWithTag("note").assertDoesNotExist()
    }

    @Test fun theNoteStartsOverFromWhereTheUserIsWhenAControlChanges() {
        start()
        scrollAway()
        update { latest = feedPage(listOf(feedSession("fresh")) + base) }
        rule.onNodeWithTag("note").assertIsDisplayed()
        update { scope = listOf("another grouping") }
        rule.onNodeWithTag("note").assertDoesNotExist()
    }

    // The way back to the latest

    @Test fun theJumpLandsWhatWaitedAndItsOwnScrollHoldsNothing() {
        start(reduceMotion = false)
        scrollAway()
        holdScroll()
        push({ s -> needsYou("s20")(line("s05")(s)) })
        assertEquals("waiting while the list moves", "s05", titleOf("s05"))
        rule.onNodeWithTag("note").assertIsDisplayed()

        // The production path: the pill's own action, which scrolls the list itself.
        rule.runOnUiThread { view.showLatest() }
        advance(50)
        assertEquals("what waited has landed before the list moves", "s05, more", titleOf("s05"))
        assertTrue("the list is on its way up", state.isScrollInProgress && state.canScrollBackward)
        push(line("s06"))
        assertEquals("the screen's own scroll is not the user reading: a push lands at once", "s06, more", titleOf("s06"))

        advance(3_000)
        assertFalse("back at the latest", state.canScrollBackward)
        rule.onNodeWithTag("note").assertDoesNotExist()
        assertEquals("s06, more", titleOf("s06"))
    }

    @Test fun repeatedJumpsKeepTheWholeProgrammaticScrollFreeOfHolds() {
        start(reduceMotion = false)
        scrollAway()
        push(needsYou("s20"))
        rule.runOnUiThread { view.showLatest() }
        advance(30)
        rule.runOnUiThread { view.showLatest() }
        advance(30)
        assertTrue("the second jump is still moving", state.isScrollInProgress && state.canScrollBackward)
        push(line("s07"))
        assertEquals("the first jump ending does not make the second one a user scroll", "s07, more", titleOf("s07"))
        advance(3_000)
        assertFalse(state.canScrollBackward)
    }

    @Test fun tappingThePillGoesToTheLatestAndTheNoteGoes() {
        start()
        scrollAway()
        push(needsYou("s20"))
        rule.onNodeWithTag("note").performClick()
        advance(500)
        assertFalse("back at the latest", state.canScrollBackward)
        rule.onNodeWithTag("note").assertDoesNotExist()
        assertEquals("needs_you", stateOf("s20"))
    }

    @Test fun theUserTakingTheListOverMidJumpHoldsAgain() {
        start(reduceMotion = false)
        scrollAway()
        push(needsYou("s20"))
        rule.runOnUiThread { view.showLatest() }
        advance(30)
        // The user's own scroll preempts the animation, away from the top.
        holdScroll()
        push(line("s07"))
        assertEquals("a push during the user's own scroll waits", "s07", titleOf("s07"))
        release()
    }

    // The screen's own scope

    @Test fun aScrollThatChangesNothingDrawnDoesNotComposeTheScreenAgain() {
        start()
        scrollAway()
        val before = screenCompositions
        holdScroll()
        advance(100)
        release()
        advance(AgentsFeedTiming.SETTLE_MILLIS + 200)
        assertEquals("the hold's state is not observed where the page is drawn", before, screenCompositions)
    }
}

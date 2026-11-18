package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.domain.sort.AgentSession

/**
 * A rebuilt screen (a killed process, a restored back stack) resumes by the row's key and offset, not by the index the
 * list's own saver keeps: the hub's order moves while the app is away. The hub's change is made when the old screen is
 * taken down, after its place was saved and before the new one reads the list, as it would happen while the app is away;
 * making it while the old screen is alive would only teach the old screen a new place.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AgentsReadingRestoreTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun entry(number: Int) =
        TreeRowEntry(AgentSession(Session("demo", "s-$number")), depth = 0, first = false, last = false, located = false)

    private val original = (0 until 60).map(::entry)

    private var rows by mutableStateOf(original)
    private var loaded by mutableStateOf(true)
    private lateinit var state: LazyListState
    private lateinit var uiScope: CoroutineScope
    /** What the hub did while the app was away: run as the old screen goes, before the new one is built. */
    private var whileAway: (() -> Unit)? = null

    private fun show(restoration: StateRestorationTester) = restoration.setContent {
        state = rememberLazyListState()
        uiScope = rememberCoroutineScope()
        DisposableEffect(Unit) { onDispose { whileAway?.invoke(); whileAway = null } }
        val restoring = RestoreAgentReadingPosition(state, rows, paginationFooter = false, loaded = loaded)
        KeepAgentReadingPosition(state, rows, paginationFooter = false, scope = emptyList(), restoring = restoring)
        // A heading above the tree, as the page has, so the tree does not start at the list's first item.
        LazyColumn(Modifier.height(300.dp), state = state, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            item("search") { Box(Modifier.fillMaxWidth().height(40.dp)) }
            items(rows, key = { it.key }) { Box(Modifier.fillMaxWidth().height(52.dp)) }
        }
    }

    /** The first tree row in view and where it is, in the list's own measure. */
    private fun firstRow(): Pair<Any, Int> = state.layoutInfo.visibleItemsInfo.first { it.key != "search" }.let { it.key to it.offset }

    /** Rests the list with [key] first in view, 17 px of it already scrolled out; the place is saved once the list is idle. */
    private fun scrollTo(key: String) {
        val index = rows.indexOfFirst { it.key == key } + 1
        rule.runOnUiThread { uiScope.launch { state.scrollToItem(index, 17) } }
        rule.waitForIdle()
        rule.waitForIdle()
    }

    private fun restore(restoration: StateRestorationTester, away: (() -> Unit)? = null) {
        whileAway = away
        restoration.emulateSavedInstanceStateRestore()
        rule.waitForIdle()
        rule.waitForIdle()
    }

    @Test fun theRowThatWasFirstInViewIsBackWhereItStoodEvenThoughTheOrderMoved() {
        val restoration = StateRestorationTester(rule)
        show(restoration)
        scrollTo("session:demo/s-25")
        val (key, offset) = firstRow()
        assertEquals("session:demo/s-25", key)

        // While the app was away the hub put that row near the top and moved the others around it.
        restore(restoration) { rows = listOf(original[25]) + original.filterIndexed { index, _ -> index != 25 }.reversed() }

        val (after, afterOffset) = firstRow()
        assertEquals("the same row, found by its key and not by the index it had", key, after)
        assertEquals("at the same place in the window", offset, afterOffset)
        assertFalse("and it is not where its old index would have put another row", state.firstVisibleItemIndex == 26)
    }

    @Test fun aRowThatIsGoneStartsTheListFromTheTop() {
        val restoration = StateRestorationTester(rule)
        show(restoration)
        scrollTo("session:demo/s-25")
        val (key, _) = firstRow()

        restore(restoration) { rows = original.filterNot { it.key == key } }

        assertEquals(0, state.firstVisibleItemIndex)
        assertEquals(0, state.firstVisibleItemScrollOffset)
    }

    @Test fun aListThatIsNotInYetWaitsForTheRowAndPutsItBackWhenItArrives() {
        val restoration = StateRestorationTester(rule)
        show(restoration)
        scrollTo("session:demo/s-40")
        val (key, offset) = firstRow()

        restore(restoration) { rows = emptyList(); loaded = false }
        assertEquals("nothing to scroll to yet, nothing decided", 0, state.firstVisibleItemIndex)

        rule.runOnIdle { rows = original; loaded = true }
        rule.waitForIdle()
        rule.waitForIdle()
        assertEquals("the place saved before the rebuild is the one put back", key to offset, firstRow())
    }

    @Test fun aListThatLoadedEmptyOpensAtTheTopWithoutFailingAndTheNextLoadIsNotMovedBack() {
        val restoration = StateRestorationTester(rule)
        show(restoration)
        scrollTo("session:demo/s-25")

        restore(restoration) { rows = emptyList() }

        assertEquals(0, state.firstVisibleItemIndex)
        assertEquals(0, state.firstVisibleItemScrollOffset)
        // The row is given a bounded moment to follow; past it the place is let go.
        rule.mainClock.advanceTimeBy(RESTORE_SETTLE_MILLIS + 100)
        rule.waitForIdle()
        rule.runOnIdle { rows = original }
        rule.waitForIdle()
        assertEquals("the list is the list as it is, from the top", 0, state.firstVisibleItemIndex)
    }

    /**
     * The failure seen on a device: the hub's status says the list is loaded while the page the list draws is still the
     * build before it, with no rows. That used to give the place up at once, and the rows that followed opened at the top.
     */
    @Test fun aListThatSaysLoadedBeforeItsRowsAreInStillGetsItsRowBackWhenTheyFollow() {
        val restoration = StateRestorationTester(rule)
        show(restoration)
        scrollTo("session:demo/s-40")
        val (key, offset) = firstRow()

        restore(restoration) { rows = emptyList(); loaded = true }
        assertEquals("nothing to scroll to yet", 0, state.firstVisibleItemIndex)

        rule.runOnIdle { rows = original }
        rule.waitForIdle()
        rule.waitForIdle()
        assertEquals("the row that followed the loaded line is put back", key to offset, firstRow())
    }

    @Test fun aRowThatNeverFollowsIsGivenUpAfterTheBoundedWaitAndTheListStaysWhereItIs() {
        val restoration = StateRestorationTester(rule)
        show(restoration)
        scrollTo("session:demo/s-40")
        val (key, _) = firstRow()

        restore(restoration) { rows = original.filterNot { it.key == key }; loaded = true }
        assertEquals(0, state.firstVisibleItemIndex)
        rule.mainClock.advanceTimeBy(RESTORE_SETTLE_MILLIS + 100)
        rule.waitForIdle()
        // A late return of the row after that is the list as it is: it does not drag the reader back.
        rule.runOnIdle { rows = original }
        rule.waitForIdle()
        rule.waitForIdle()
        assertEquals(0, state.firstVisibleItemIndex)
    }

    @Test fun aListLeftAtTheTopOpensAtTheTop() {
        val restoration = StateRestorationTester(rule)
        show(restoration)
        rule.waitForIdle()

        restore(restoration) { rows = original.reversed() }

        assertEquals(0, state.firstVisibleItemIndex)
        assertFalse(state.canScrollBackward)
    }

    @Test fun aTouchBeforeTheRowIsInKeepsTheUsersOwnPlace() {
        val restoration = StateRestorationTester(rule)
        show(restoration)
        scrollTo("session:demo/s-40")

        restore(restoration) { rows = emptyList(); loaded = false }
        val userScroll = CompletableDeferred<Unit>()
        rule.runOnUiThread { uiScope.launch { state.scroll { userScroll.await() } } }
        rule.waitForIdle()
        try {
            org.junit.Assert.assertTrue("the production scroll state stays visible to the restoration observer", state.isScrollInProgress)
            rule.runOnIdle { rows = original; loaded = true }
            rule.waitForIdle()
            rule.waitForIdle()

            assertEquals("the user scrolled before the list came: it is not undone", 0, state.firstVisibleItemIndex)
        } finally {
            userScroll.complete(Unit)
            rule.waitForIdle()
        }
    }
}

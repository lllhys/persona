package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasScrollToKeyAction
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performScrollToKey
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.domain.sort.AgentSession

/**
 * The list is one column in any window: when its height changes (a rotation, a split screen, a fold) the reading row is
 * chosen again from what the new layout shows, and live updates after that keep it still, as before the change.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AgentsReadingResizeTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val initialRows = (0 until 50).map { number ->
        TreeRowEntry(AgentSession(Session("demo", "s-$number")), depth = 0, first = false, last = false, located = false)
    }
    private var rows by mutableStateOf(initialRows)
    private var height by mutableStateOf(310.dp)

    private val isRow = SemanticsMatcher("is a session row") {
        it.config.getOrNull(SemanticsProperties.TestTag)?.startsWith("session:") == true
    }

    private fun show() = rule.setContent {
        val state = rememberLazyListState()
        // The window's size is part of the reading position's scope, as the screen passes it.
        KeepAgentReadingPosition(state, rows, paginationFooter = false, scope = listOf(height), restoring = false)
        LazyColumn(Modifier.height(height), state = state, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(rows, key = { it.key }) { row -> Box(Modifier.fillMaxWidth().height(52.dp).testTag(row.key)) }
        }
    }

    /** The row a reader would be at: the one covering the list's midpoint among those wholly in view. */
    private fun rowAtMidpoint(): String {
        val list = rule.onNode(hasScrollToKeyAction()).fetchSemanticsNode().boundsInRoot
        val whole = rule.onAllNodes(isRow).fetchSemanticsNodes().filter { it.boundsInRoot.top >= list.top && it.boundsInRoot.bottom <= list.bottom }
        val middle = list.center.y
        val node = whole.firstOrNull { it.boundsInRoot.top <= middle && middle < it.boundsInRoot.bottom }
            ?: whole.minBy { kotlin.math.abs(it.boundsInRoot.center.y - middle) }
        return node.config[SemanticsProperties.TestTag]
    }

    private fun topOf(tag: String) = rule.onNodeWithTag(tag).assertIsDisplayed().fetchSemanticsNode().boundsInRoot.top

    @Test fun afterTheListIsMadeShorterALiveReorderStillKeepsTheRowBeingRead() {
        show()
        rule.onNode(hasScrollToKeyAction()).performScrollToKey(initialRows[20].key)
        rule.runOnIdle { height = 210.dp }
        rule.waitForIdle()

        val reading = rowAtMidpoint()
        val before = topOf(reading)
        val moved = rows.first { it.key == reading }
        rule.runOnIdle { rows = rows.filterNot { it == moved }.toMutableList().apply { add(4, moved) } }
        rule.waitForIdle()

        assertEquals("the row read after the resize keeps its place under a reorder", before, topOf(reading), 2f)
    }

    @Test fun afterTheListIsMadeTallerALiveRemovalAboveKeepsTheRowBeingRead() {
        show()
        rule.onNode(hasScrollToKeyAction()).performScrollToKey(initialRows[20].key)
        rule.runOnIdle { height = 450.dp }
        rule.waitForIdle()

        val reading = rowAtMidpoint()
        val before = topOf(reading)
        val above = rows.take(6).toSet()
        rule.runOnIdle { rows = rows.filterNot { it in above } }
        rule.waitForIdle()

        assertEquals(before, topOf(reading), 2f)
    }

    @Test fun resizingBackAndForthNeverLeavesTheListWithoutARowInView() {
        show()
        rule.onNode(hasScrollToKeyAction()).performScrollToKey(initialRows[30].key)
        repeat(3) {
            rule.runOnIdle { height = 180.dp }
            rule.waitForIdle()
            assertTrue(rule.onAllNodes(isRow).fetchSemanticsNodes().isNotEmpty())
            rule.runOnIdle { height = 500.dp }
            rule.waitForIdle()
            assertTrue(rule.onAllNodes(isRow).fetchSemanticsNodes().isNotEmpty())
        }
    }
}

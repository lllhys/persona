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
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.getUnclippedBoundsInRoot
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

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AgentReadingPositionTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    @Test fun liveReorderKeepsTheNearestRowWhenTheMidpointFallsInAGap() {
        val initialRows = (0 until 50).map { number ->
            TreeRowEntry(AgentSession(Session("demo", "s-$number")), depth = 0, first = false, last = false, located = false)
        }
        val (first, preceding, reader) = initialRows.subList(20, 23)
        val heights = mapOf(first to 40.dp, preceding to 80.dp, reader to 32.dp)
        var rows by mutableStateOf(initialRows)
        rule.setContent {
            val state = rememberLazyListState()
            KeepAgentReadingPosition(state, rows, paginationFooter = false, scope = emptyList(), restoring = false)
            LazyColumn(Modifier.height(310.dp), state = state, verticalArrangement = Arrangement.spacedBy(24.dp)) {
                items(rows, key = { it.key }) { row ->
                    Box(Modifier.fillMaxWidth().height(heights[row] ?: 52.dp).testTag(row.key))
                }
            }
        }

        val list = rule.onNode(hasScrollToKeyAction())
        list.performScrollToKey(first.key)
        val middle = list.fetchSemanticsNode().boundsInRoot.center.y
        val precedingBounds = rule.onNodeWithTag(preceding.key).fetchSemanticsNode().boundsInRoot
        val readingRow = rule.onNodeWithTag(reader.key).assertIsDisplayed()
        val readingBounds = readingRow.fetchSemanticsNode().boundsInRoot
        assertTrue("the midpoint falls in the gap between the two rows", precedingBounds.bottom < middle && middle < readingBounds.top)
        assertTrue("the following row has the closer center", readingBounds.center.y - middle < middle - precedingBounds.center.y)

        rule.runOnIdle { rows = rows.filterNot { it == reader }.toMutableList().apply { add(8, reader) } }

        val after = readingRow.assertIsDisplayed().fetchSemanticsNode().boundsInRoot.top
        assertEquals("the row nearest the midpoint keeps its old position", readingBounds.top, after, 2f)
    }

    @Test fun liveRemovalKeepsTheClosestSurvivingRowAtItsOldHeight() {
        val initialRows = (0 until 50).map { number ->
            TreeRowEntry(AgentSession(Session("demo", "s-$number")), depth = 0, first = false, last = false, located = false)
        }
        val first = initialRows[20]
        val neighbor = initialRows[21]
        val reader = initialRows[22]
        var tree by mutableStateOf(initialRows)
        var renderedRows by mutableStateOf(initialRows)
        rule.setContent {
            val state = rememberLazyListState()
            KeepAgentReadingPosition(state, tree, paginationFooter = false, scope = emptyList(), restoring = false)
            LazyColumn(Modifier.height(276.dp), state = state) {
                item("header") { Box(Modifier.height(52.dp)) }
                items(renderedRows, key = { it.key }) { row ->
                    Box(Modifier.fillMaxWidth().height(if (row == neighbor) 40.dp else 52.dp).testTag(row.key))
                }
            }
        }

        val list = rule.onNode(hasScrollToKeyAction())
        list.performScrollToKey(first.key)
        val viewport = list.fetchSemanticsNode().boundsInRoot
        val oldReader = rule.onNodeWithTag(reader.key).assertIsDisplayed()
        val oldReaderBounds = oldReader.fetchSemanticsNode().boundsInRoot
        assertTrue("the removed row crosses the viewport midpoint", viewport.center.y in oldReaderBounds.top..oldReaderBounds.bottom)
        val closest = rule.onNodeWithTag(neighbor.key).assertIsDisplayed()
        val before = closest.fetchSemanticsNode().boundsInRoot.top

        // The shorter row above the reader was closer than the row below. Move it beyond the old first key's viewport.
        val updatedRows = initialRows.filterNot { it == reader || it == neighbor }.toMutableList().apply { add(8, neighbor) }
        // The new tree can be published while the previous layout is still measured.
        rule.runOnIdle { tree = updatedRows }
        oldReader.assertIsDisplayed()
        assertEquals("the previous layout stays in place until the tree matches", before, closest.fetchSemanticsNode().boundsInRoot.top, 2f)
        rule.runOnIdle { renderedRows = updatedRows }

        oldReader.assertDoesNotExist()
        val after = closest.assertIsDisplayed().fetchSemanticsNode().boundsInRoot.top
        assertEquals("the nearest surviving row keeps its old position", before, after, 2f)
    }

    @Test fun liveRemovalSkipsACloserClippedRowWhenAWholeRowSurvives() {
        val initialRows = (0 until 50).map { number ->
            TreeRowEntry(AgentSession(Session("demo", "s-$number")), depth = 0, first = false, last = false, located = false)
        }
        val (first, neighbor, reader, clipped) = initialRows.subList(20, 24)
        val heights = mapOf(first to 24.dp, neighbor to 106.dp, reader to 92.dp, clipped to 76.dp)
        var rows by mutableStateOf(initialRows)
        rule.setContent {
            val state = rememberLazyListState()
            KeepAgentReadingPosition(state, rows, paginationFooter = false, scope = emptyList(), restoring = false)
            LazyColumn(Modifier.height(276.dp), state = state) {
                items(rows, key = { it.key }) { row ->
                    Box(Modifier.fillMaxWidth().height(heights[row] ?: 52.dp).testTag(row.key))
                }
            }
        }

        val list = rule.onNode(hasScrollToKeyAction())
        list.performScrollToKey(first.key)
        val viewport = list.getUnclippedBoundsInRoot()
        val oldReader = rule.onNodeWithTag(reader.key).assertIsDisplayed()
        val oldReaderBounds = oldReader.fetchSemanticsNode().boundsInRoot
        val middle = list.fetchSemanticsNode().boundsInRoot.center.y
        assertTrue("the removed row crosses the viewport midpoint", middle in oldReaderBounds.top..oldReaderBounds.bottom)
        val clippedRow = rule.onNodeWithTag(clipped.key).assertIsDisplayed()
        assertTrue("the closer row is clipped at the bottom", clippedRow.getUnclippedBoundsInRoot().bottom > viewport.bottom)
        val wholeRow = rule.onNodeWithTag(neighbor.key).assertIsDisplayed()
        val before = wholeRow.fetchSemanticsNode().boundsInRoot.top

        // Compose's first visible key remains, while the fully visible neighbor moves out of its viewport.
        rule.runOnIdle {
            rows = rows.filterNot { it == reader || it == neighbor }.toMutableList().apply { add(8, neighbor) }
        }

        oldReader.assertDoesNotExist()
        val after = wholeRow.assertIsDisplayed().fetchSemanticsNode().boundsInRoot.top
        assertEquals("fallback preserves the fully visible neighbor", before, after, 2f)
    }

    @Test fun liveReorderKeepsAWholeNeighborAfterTheViewportClipsTheReadingRow() {
        val initialRows = (0 until 50).map { number ->
            TreeRowEntry(AgentSession(Session("demo", "s-$number")), depth = 0, first = false, last = false, located = false)
        }
        val (first, neighbor, reader) = initialRows.subList(20, 23)
        val heights = mapOf(neighbor to 65.dp, reader to 112.dp)
        var rows by mutableStateOf(initialRows)
        var viewportHeight by mutableStateOf(300.dp)
        rule.setContent {
            val state = rememberLazyListState()
            KeepAgentReadingPosition(state, rows, paginationFooter = false, scope = emptyList(), restoring = false)
            LazyColumn(Modifier.height(viewportHeight), state = state) {
                items(rows, key = { it.key }) { row ->
                    Box(Modifier.fillMaxWidth().height(heights[row] ?: 52.dp).testTag(row.key))
                }
            }
        }

        val list = rule.onNode(hasScrollToKeyAction())
        list.performScrollToKey(first.key)
        val readingRow = rule.onNodeWithTag(reader.key).assertIsDisplayed()
        val initialBounds = readingRow.getUnclippedBoundsInRoot()
        val initialViewport = list.getUnclippedBoundsInRoot()
        val initialMiddle = (initialViewport.top + initialViewport.bottom) / 2
        assertTrue("the whole reading row initially crosses the midpoint",
            initialBounds.bottom < initialViewport.bottom && initialMiddle in initialBounds.top..initialBounds.bottom)
        val wholeRow = rule.onNodeWithTag(neighbor.key).assertIsDisplayed()
        val before = wholeRow.fetchSemanticsNode().boundsInRoot.top

        rule.runOnIdle { viewportHeight = 185.dp }
        val shortenedViewport = list.getUnclippedBoundsInRoot()
        assertTrue("the old anchor is now clipped at the bottom", readingRow.assertIsDisplayed().getUnclippedBoundsInRoot().bottom > shortenedViewport.bottom)
        assertTrue("its neighbor still fits", wholeRow.getUnclippedBoundsInRoot().bottom < shortenedViewport.bottom)

        // Both sessions survive. Only the whole neighbor moves out of the first key's viewport.
        rule.runOnIdle { rows = rows.filterNot { it == neighbor }.toMutableList().apply { add(8, neighbor) } }

        val after = wholeRow.assertIsDisplayed().fetchSemanticsNode().boundsInRoot.top
        assertEquals("the whole neighbor keeps its old position through the reorder", before, after, 2f)
    }
}

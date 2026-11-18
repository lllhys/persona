package persona.feature.machines

import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.height
import androidx.compose.ui.unit.width
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.JCard
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens

class MachineGridTest {
    @Test
    fun columnsFollowContentWidthRatherThanADeviceBranch() {
        assertEquals(1, machineGridColumns(0.dp))
        assertEquals(1, machineGridColumns(329.dp))
        assertEquals(1, machineGridColumns(330.dp))
        assertEquals(1, machineGridColumns(659.dp))
        assertEquals(1, machineGridColumns(660.dp))
        assertEquals(1, machineGridColumns(671.dp))
        assertEquals(2, machineGridColumns(672.dp))
        assertEquals(2, machineGridColumns(1013.dp))
        assertEquals(3, machineGridColumns(1014.dp))
        assertEquals(3, machineGridColumns(1355.dp))
        assertEquals(4, machineGridColumns(1356.dp))
    }

    @Test
    fun rowsAreLazyListChunksWithStableKeysAndEmptySlots() {
        val ids = listOf("a", "b", "c", "d", "e", "f", "g")
        val rows = machineGridRows(ids, 3)

        assertEquals(listOf(listOf("a", "b", "c"), listOf("d", "e", "f"), listOf("g")), rows)
        assertEquals(1, rows.last().size)
        assertEquals(rows.map(::machineGridRowKey), machineGridRows(ids, 3).map(::machineGridRowKey))
        assertTrue(machineGridRowKey(listOf("a", "bc")) != machineGridRowKey(listOf("ab", "c")))
        assertEquals(Tokens.Size.cardColumn, 330.dp)
    }
}

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class MachineGridLayoutTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    @Config(sdk = [35], qualifiers = "en-w700dp-h500dp-xhdpi")
    @Test
    fun cardsInOneRowShareTheirHeightAndTheLastRowKeepsItsEmptySlot() {
        rule.setContent {
            PersonaTheme {
                Column {
                    MachineGridRow(listOf("short", "tall"), columns = 2, modifier = Modifier.testTag("full-row")) { id, modifier ->
                        JCard(modifier.testTag("card-$id")) {
                            Text(id)
                            if (id == "tall") Spacer(Modifier.height(120.dp))
                        }
                    }
                    MachineGridRow(listOf("only"), columns = 2, modifier = Modifier.testTag("last-row")) { id, modifier ->
                        JCard(modifier.testTag("last-card")) { Text(id) }
                    }
                }
            }
        }

        val short = rule.onNodeWithTag("card-short").getUnclippedBoundsInRoot()
        val tall = rule.onNodeWithTag("card-tall").getUnclippedBoundsInRoot()
        assertEquals(tall.height, short.height)
        val row = rule.onNodeWithTag("last-row").getUnclippedBoundsInRoot()
        val card = rule.onNodeWithTag("last-card").getUnclippedBoundsInRoot()
        assertTrue(card.width < row.width)
    }

    @Config(sdk = [35], qualifiers = "en-w700dp-h500dp-mdpi")
    @Test
    fun columnChangesKeepTheFirstMachineRowAndPixelOffset() {
        val ids = (0 until 30).map { "machine-$it" }
        var columns by mutableIntStateOf(1)
        lateinit var state: LazyListState
        rule.setContent {
            state = rememberMachineGridListState(ids, columns)
            LazyColumn(Modifier.height(200.dp), state = state) {
                machineGrid(ids, when (columns) {
                    1 -> 330.dp
                    2 -> 672.dp
                    else -> 1014.dp
                }) { id, modifier ->
                    Text(id, modifier.height(40.dp))
                }
            }
        }
        rule.runOnIdle { runBlocking { state.scrollToItem(9, 7) } }

        for (nextColumns in listOf(2, 3, 1)) {
            val anchor = machineGridRows(ids, columns)[state.firstVisibleItemIndex].first()
            rule.runOnIdle { columns = nextColumns }
            rule.waitForIdle()
            val row = machineGridRows(ids, nextColumns)[state.firstVisibleItemIndex]
            assertTrue("lost row anchor $anchor at $nextColumns columns: ${state.firstVisibleItemIndex}", row.contains(anchor))
            assertEquals(7, state.firstVisibleItemScrollOffset)
        }
    }
}

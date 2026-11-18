package persona.feature.machines

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.Dp
import persona.core.designsystem.Tokens
import persona.core.designsystem.pagePadding
import persona.core.designsystem.readingInset
import kotlin.math.floor

/** Measures the actual pane, then mirrors the horizontal content padding JListPage will apply. */
@Composable
internal fun MachineGridWidth(content: @Composable (Dp) -> Unit) {
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val horizontalPadding = pagePadding + readingInset(maxWidth)
        val contentWidth = (maxWidth - horizontalPadding * 2).coerceAtLeast(Tokens.Space.flush)
        content(contentWidth)
    }
}

internal fun machineGridColumns(
    contentWidth: Dp,
    cardColumn: Dp = Tokens.Size.cardColumn,
    spacing: Dp = Tokens.Space.m,
): Int {
    if (!contentWidth.value.isFinite() || contentWidth <= Tokens.Space.flush) return 1
    return floor((contentWidth.value + spacing.value) / (cardColumn.value + spacing.value)).toInt().coerceAtLeast(1)
}

internal fun <T> machineGridRows(ids: List<T>, columns: Int): List<List<T>> = ids.chunked(columns.coerceAtLeast(1))

internal fun machineGridRowKey(row: List<String>): String =
    row.joinToString("|", prefix = "machine-row|") { "${it.length}:$it" }

private class MachineGridViewport(
    var ids: List<String>,
    var columns: Int,
    var visibleKey: Any? = null,
    var visibleIndex: Int = -1,
    var visibleOffset: Int = 0,
    var gridStart: Int = 0,
)

internal data class MachineGridScrollRequest(val index: Int, val offset: Int)

/** Maps the first visible row's leftmost machine into the new chunking, retaining its absolute pixel offset. */
internal fun machineGridScrollRequest(
    previousIds: List<String>,
    previousColumns: Int,
    ids: List<String>,
    columns: Int,
    visibleKey: Any?,
    visibleIndex: Int,
    visibleOffset: Int,
    previousGridStart: Int = 0,
): MachineGridScrollRequest? {
    val key = visibleKey as? String ?: return null
    if (!key.startsWith("machine-row|")) return null
    val previousRows = machineGridRows(previousIds, previousColumns)
    val keyRow = previousRows.indexOfFirst { machineGridRowKey(it) == key }
    val indexedRow = visibleIndex - previousGridStart
    val previousRow = if (indexedRow in previousRows.indices) indexedRow else keyRow
    if (previousRow !in previousRows.indices) return null
    val anchor = previousRows[previousRow].firstOrNull() ?: return null
    val newMachineIndex = ids.indexOf(anchor)
    if (newMachineIndex < 0) return null
    val gridStart = visibleIndex - previousRow
    if (gridStart < 0) return null
    return MachineGridScrollRequest(gridStart + newMachineIndex / columns.coerceAtLeast(1), visibleOffset)
}

/** Keeps the same machine row, not merely the old lazy item index, when a pane crosses a column boundary. */
@Composable
internal fun rememberMachineGridListState(ids: List<String>, columns: Int): LazyListState {
    val state = rememberLazyListState()
    val previous = remember { MachineGridViewport(ids, columns) }
    val visible = state.layoutInfo.visibleItemsInfo.firstOrNull()
    val visibleOffset = state.firstVisibleItemScrollOffset
    val request = remember(columns) {
        if (previous.columns == columns) null else {
            // Item keys may already reflect the new chunking here; only the previous stable snapshot has the old anchor.
            machineGridScrollRequest(previous.ids, previous.columns, ids, columns,
                previous.visibleKey, previous.visibleIndex, previous.visibleOffset, previous.gridStart)
        }
    }
    LaunchedEffect(columns, request) {
        if (request != null) state.scrollToItem(request.index, request.offset)
    }
    SideEffect {
        val oldRows = machineGridRows(previous.ids, previous.columns)
        val oldRow = oldRows.indexOfFirst { machineGridRowKey(it) == visible?.key }
        if (oldRow >= 0 && visible != null) previous.gridStart = visible.index - oldRow
        previous.ids = ids
        previous.columns = columns
        previous.visibleKey = visible?.key
        previous.visibleIndex = visible?.index ?: -1
        previous.visibleOffset = visibleOffset
    }
    return state
}

/** Adds rows to the page's existing LazyColumn; no nested grid or second scrolling container. */
internal fun LazyListScope.machineGrid(
    ids: List<String>,
    contentWidth: Dp,
    card: @Composable (String, Modifier) -> Unit,
) {
    val columns = machineGridColumns(contentWidth)
    items(machineGridRows(ids, columns), key = ::machineGridRowKey, contentType = { "machine-row" }) { row ->
        MachineGridRow(row, columns, card = card)
    }
}

@Composable
internal fun MachineGridRow(
    row: List<String>,
    columns: Int,
    modifier: Modifier = Modifier,
    card: @Composable (String, Modifier) -> Unit,
) {
    Row(modifier.fillMaxWidth().height(IntrinsicSize.Max), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        row.forEach { id ->
            Box(Modifier.weight(1f).fillMaxHeight()) {
                card(id, Modifier.fillMaxHeight())
            }
        }
        repeat((columns - row.size).coerceAtLeast(0)) {
            Spacer(Modifier.weight(1f))
        }
    }
}

package persona.core.designsystem

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.focusTarget
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.DpOffset
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntRect
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import kotlin.math.roundToInt

/**
 * A [JMenu] anchored to a pointer in window coordinates. The existing menu owns its action rows, surface and scrolling;
 * this wrapper only supplies the anchor, safe-window placement and Escape dismissal.
 */
@Composable
fun JPointerMenu(
    anchor: Offset,
    onDismiss: () -> Unit,
    actions: List<JAction>,
    modifier: Modifier = Modifier,
) {
    val density = LocalDensity.current
    val windowSize = LocalWindowInfo.current.containerSize
    val safeBounds = pointerMenuSafeBounds(windowSize, WindowInsets.safeDrawing, density, LocalLayoutDirection.current)
    var hostOrigin by remember { mutableStateOf(Offset.Zero) }
    var menuSize by remember { mutableStateOf(IntSize.Zero) }
    val origin = pointerMenuOrigin(anchor, menuSize, safeBounds)
    val menuOffset = with(density) {
        DpOffset((origin.x - anchor.x).toDp(), (origin.y - anchor.y).toDp())
    }
    val focusRequester = remember { FocusRequester() }
    var focusRequested by remember { mutableStateOf(false) }

    Box(Modifier.onGloballyPositioned { hostOrigin = it.localToWindow(Offset.Zero) }) {
        Box(Modifier.offset {
            IntOffset((anchor.x - hostOrigin.x).roundToInt(), (anchor.y - hostOrigin.y).roundToInt())
        }) {
            JMenu(
                expanded = true,
                onDismiss = onDismiss,
                actions = actions,
                offset = menuOffset,
                modifier = modifier
                    .onSizeChanged { menuSize = it }
                    .onPreviewKeyEvent { event ->
                        if (event.key == Key.Escape && event.type == KeyEventType.KeyDown) {
                            onDismiss()
                            true
                        } else false
                    }
                    .focusRequester(focusRequester)
                    .focusTarget()
                    .onGloballyPositioned {
                        if (!focusRequested) {
                            focusRequested = true
                            focusRequester.requestFocus()
                        }
                    },
            )
        }
    }
}

/** Safe physical window bounds; getLeft/getRight stay physical in either layout direction. */
internal fun pointerMenuSafeBounds(
    windowSize: IntSize,
    insets: WindowInsets,
    density: Density,
    layoutDirection: LayoutDirection,
): IntRect {
    val left = insets.getLeft(density, layoutDirection)
    val rightInset = insets.getRight(density, layoutDirection)
    val top = insets.getTop(density)
    return IntRect(left, top,
        (windowSize.width - rightInset).coerceAtLeast(left),
        (windowSize.height - insets.getBottom(density)).coerceAtLeast(top))
}

/** The menu's top-left, growing right/down first and flipping where the other side has room. */
internal fun pointerMenuOrigin(anchor: Offset, menuSize: IntSize, window: IntRect): IntOffset = IntOffset(
    pointerMenuAxis(anchor.x, menuSize.width, window.left, window.right),
    pointerMenuAxis(anchor.y, menuSize.height, window.top, window.bottom),
)

private fun pointerMenuAxis(anchor: Float, menuExtent: Int, safeStart: Int, safeEnd: Int): Int {
    val after = anchor.roundToInt()
    val before = (anchor - menuExtent).roundToInt()
    val preferred = when {
        after + menuExtent <= safeEnd -> after
        before >= safeStart -> before
        else -> after
    }
    val lastStart = (safeEnd - menuExtent).coerceAtLeast(safeStart)
    return preferred.coerceIn(safeStart, lastStart)
}

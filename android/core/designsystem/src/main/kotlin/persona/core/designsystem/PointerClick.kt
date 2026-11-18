package persona.core.designsystem

import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.composed
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.PointerEventType
import androidx.compose.ui.input.pointer.isSecondaryPressed
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.LayoutCoordinates
import androidx.compose.ui.layout.onGloballyPositioned

/** Where a pointer-aware element is, for converting its local event positions to window coordinates. */
private class PointerClickPlacement {
    var coordinates: LayoutCoordinates? = null
}

/**
 * A secondary click at its window position. Only the press is consumed; primary clicks, hover, movement and touch keep
 * their existing behavior. The parameterless [onSecondaryClick] remains the variant for callers that need no position.
 */
fun Modifier.onSecondaryClickAt(action: ((Offset) -> Unit)?): Modifier = if (action == null) this else composed {
    val placement = remember { PointerClickPlacement() }
    Modifier.onGloballyPositioned { placement.coordinates = it }.pointerInput(action) {
        awaitPointerEventScope {
            while (true) {
                val event = awaitPointerEvent(PointerEventPass.Initial)
                if (event.type == PointerEventType.Press && event.buttons.isSecondaryPressed) {
                    event.changes.forEach { it.consume() }
                    val position = event.changes.first().position
                    placement.coordinates?.let { action(it.localToWindow(position)) }
                }
            }
        }
    }
}

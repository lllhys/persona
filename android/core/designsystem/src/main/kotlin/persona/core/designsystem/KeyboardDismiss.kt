package persona.core.designsystem

import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.SuspendingPointerInputModifierNode
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.node.CompositionLocalConsumerModifierNode
import androidx.compose.ui.node.DelegatingNode
import androidx.compose.ui.node.ModifierNodeElement
import androidx.compose.ui.node.currentValueOf
import androidx.compose.ui.platform.InspectorInfo
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.platform.LocalTextToolbar
import androidx.compose.ui.semantics.onLongClick
import androidx.compose.ui.semantics.semantics

/**
 * A tap on blank space inside this area puts the keyboard away. It only watches: the tap reaches what is under the
 * finger first, and anything that handles it (a link, a button, a card, a text field, a long press, a scroll) consumes
 * it, which leaves the keyboard up. Nothing here consumes an event, so no click below is lost.
 */
fun Modifier.dismissKeyboardOnBlankTap(onClearFocus: () -> Unit = {}): Modifier =
    this then DismissKeyboardElement(onClearFocus)

/**
 * Keeps taps on a composer's padding from reaching a page behind it or a surrounding page's dismiss listener.
 * Children handle the gesture first; only otherwise unhandled taps are consumed here, and run [onTap] (a composer
 * focuses its field, so its whole frame takes a tap, not only the text line).
 */
fun Modifier.keepsKeyboardOnTap(onTap: () -> Unit = {}): Modifier = pointerInput(Unit) { detectTapGestures { onTap() } }

/**
 * A long press runs [action] and keeps the rest of its press to itself; a shorter tap is left unconsumed, so a tap on
 * something that only has a long press (a message, all the row's width) still reaches [dismissKeyboardOnBlankTap].
 * `detectTapGestures(onLongPress = …)` consumes every tap, which kept the keyboard up over a whole conversation.
 * Accessibility services (a screen reader, switch access) cannot long press: they get it as the node's long-click
 * action, named [label].
 */
fun Modifier.onLongPress(key: Any?, label: String? = null, action: () -> Unit): Modifier =
    semantics { onLongClick(label) { action(); true } }.pointerInput(key) {
    awaitEachGesture {
        // A link or a button inside took the press: theirs.
        awaitFirstDown()
        // Lifted, scrolled away or left the area in time: not a long press, and nothing taken from it.
        val ended = withTimeoutOrNull(viewConfiguration.longPressTimeoutMillis) { waitForUpOrCancellation(); true }
        if (ended != null) return@awaitEachGesture
        action()
        do {
            val event = awaitPointerEvent()
            event.changes.forEach { it.consume() }
        } while (event.changes.any { it.pressed })
    }
}

/**
 * When [key] changes (another tab is shown), lets go of text input: the selection's paste menu, the focus and the
 * keyboard. A page kept composed but hidden would otherwise keep all three over the page now shown (#894). The first
 * value is not a change.
 */
@Composable
fun ReleaseTextInputOnChange(key: Any) {
    val focusManager = LocalFocusManager.current
    val keyboard = LocalSoftwareKeyboardController.current
    val textToolbar = LocalTextToolbar.current
    var shown by remember { mutableStateOf(key) }
    LaunchedEffect(key) {
        if (shown != key) {
            textToolbar.hide()
            focusManager.clearFocus(force = true)
            keyboard?.hide()
            shown = key
        }
    }
}

private data class DismissKeyboardElement(val onClearFocus: () -> Unit) : ModifierNodeElement<DismissKeyboardNode>() {
    override fun create() = DismissKeyboardNode(onClearFocus)
    override fun update(node: DismissKeyboardNode) { node.onClearFocus = onClearFocus }
    override fun InspectorInfo.inspectableProperties() { name = "dismissKeyboardOnBlankTap" }
}

private class DismissKeyboardNode(var onClearFocus: () -> Unit) : DelegatingNode(), CompositionLocalConsumerModifierNode {
    init {
        delegate(SuspendingPointerInputModifierNode {
            awaitEachGesture {
                // Final: the content under the finger has seen this gesture already and said whether it took it.
                awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Final)
                // Null when the gesture was consumed (a drag that scrolled, a long press) or left the area.
                val up = waitForUpOrCancellation(PointerEventPass.Final) ?: return@awaitEachGesture
                if (!up.isConsumed) {
                    onClearFocus()
                    currentValueOf(LocalFocusManager).clearFocus()
                    currentValueOf(LocalSoftwareKeyboardController)?.hide()
                }
            }
        })
    }
}

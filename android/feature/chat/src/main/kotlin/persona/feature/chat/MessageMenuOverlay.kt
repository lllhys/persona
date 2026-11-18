package persona.feature.chat

import androidx.compose.runtime.Composable
import androidx.compose.ui.geometry.Offset
import persona.core.designsystem.JAction
import persona.core.designsystem.JActionSheet
import persona.core.designsystem.JPointerMenu
import persona.core.designsystem.LocalLargeWindowInput

/** How the same message actions are presented for the current input and window. */
internal enum class MessageMenuPresentation { Pointer, ActionSheet }

internal fun messageMenuPresentation(largeWindow: Boolean, anchor: Offset?): MessageMenuPresentation =
    if (largeWindow && anchor != null) MessageMenuPresentation.Pointer else MessageMenuPresentation.ActionSheet

/** One action model and callback, whether a pointer opened the message or a touch / accessibility action did. */
@Composable
internal fun MessageMenuOverlay(anchor: Offset?, actions: List<JAction>, onDismiss: () -> Unit) {
    if (messageMenuPresentation(LocalLargeWindowInput.current, anchor) == MessageMenuPresentation.Pointer) {
        JPointerMenu(anchor = requireNotNull(anchor), actions = actions, onDismiss = onDismiss)
    } else {
        JActionSheet(actions = actions, onDismiss = onDismiss)
    }
}

package persona.android.ui

import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import persona.android.navigation.PaneHinge
import persona.android.navigation.PaneSplits
import persona.android.navigation.sidebarSplit
import persona.core.designsystem.JSidebarColumn

/** One owner outside the Assistant entry stack keeps the shared sidebar alive while right-hand pages change. */
@Composable
internal fun AssistantPaneFrame(
    regular: Boolean,
    showSidebar: Boolean,
    shown: Boolean,
    modifier: Modifier = Modifier,
    sidebar: (@Composable () -> Unit)?,
    hinges: List<PaneHinge>,
    overlay: Boolean = false,
    onDismiss: () -> Unit = {},
    drags: Boolean = false,
    detailAtRoot: Boolean = true,
    /** False while the Persona tab is composed but not selected (`Modifier.unplaced()` in `Root.kt`). */
    paneShown: Boolean = true,
    onDragSettle: (Boolean) -> Unit = {},
    content: @Composable () -> Unit,
) {
    BoxWithConstraints(modifier.fillMaxSize()) {
        val split = sidebarSplit(maxWidth.value, hinges)
        JSidebarColumn(if (regular && showSidebar) sidebar else null,
            width = (if (overlay) PaneSplits.DefaultLeft else split.sidebar).dp, pageStart = split.page.dp,
            shown = shown, overlay = overlay, onDismiss = onDismiss, drags = drags, detailAtRoot = detailAtRoot,
            paneShown = paneShown, onDragSettle = onDragSettle, content = content)
    }
}

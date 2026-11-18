package persona.core.designsystem

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.tween
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription

/**
 * Hardware actions available to an [ExpandedComposer] inside a large-window chat. Null outside that integration keeps
 * phone, preview and standalone-editor behavior unchanged. [send] is null whenever the ordinary Send button is disabled.
 */
data class ComposerHardwareActions(
    val send: (() -> Unit)? = null,
    /** Collapse for sending without the Esc path's focus restoration. */
    val collapseForSend: ((onCollapse: () -> Unit) -> Unit)? = null,
    /** Collapse for Escape and restore the small field's focused state. */
    val collapseForEscape: ((onCollapse: () -> Unit) -> Unit)? = null,
)

val LocalComposerHardware = staticCompositionLocalOf<ComposerHardwareActions?> { null }

/**
 * A non-layout-changing drop outline around the large-window composer. The caller attaches the actual drag target;
 * this component owns only the style-specific edge, motion and spoken state.
 */
@Composable
fun ComposerDropTargetFrame(
    active: Boolean,
    importing: Boolean,
    label: String,
    importingLabel: String,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    content: @Composable () -> Unit,
) {
    val shape = when (PersonaTheme.style) {
        AppStyle.Glass -> RoundedCornerShape(HomeTokens.composerRadius)
        AppStyle.Brief -> RoundedCornerShape(Tokens.Radius.bubble)
        AppStyle.Board -> RoundedCornerShape(Tokens.Radius.control)
    }
    val spec = if (PersonaTheme.reduceMotion) snap<Color>() else tween(PersonaTheme.motion.standard)
    val inactiveEdge = PersonaTheme.palette.accent.copy(alpha = 0f)
    val edge by animateColorAsState(if (enabled && active) PersonaTheme.palette.accent else inactiveEdge, spec, label = "composer drop")
    Box(modifier.border(Tokens.Size.lineMedium, edge, shape)
        .then(if (enabled) Modifier.testTag("composer-drop-target") else Modifier).semantics {
            if (enabled && active) stateDescription = if (importing) importingLabel else label
        }, content = { content() })
}

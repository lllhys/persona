package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import persona.core.designsystem.icons.PhIcons

/**
 * A quiet note over a list that is being read further down: [text] says what came meanwhile, a tap goes back to the
 * latest ([actionLabel] is what a screen reader says the tap does). It floats where the list's own edge is, never takes
 * focus or moves the list, and is announced politely when its text changes. [status] puts that state's dot before the
 * words, for what needs the user. One line: a long [text] is cut short, never wrapped. Shaped like [JumpToLatest] in each style.
 */
@Composable
fun JLatestPill(text: String, actionLabel: String, onClick: () -> Unit, modifier: Modifier = Modifier, status: Status? = null) {
    val palette = PersonaTheme.palette
    val style = PersonaTheme.style
    val shape = if (style == AppStyle.Board) RoundedCornerShape(Tokens.Radius.control) else RoundedCornerShape(50)
    val ink = if (style == AppStyle.Brief) palette.onAction else palette.ink
    Row(
        modifier.minimumInteractiveComponentSize().heightIn(min = JChipTokens.height)
            .then(when (style) {
                AppStyle.Glass -> Modifier.floatingSurface(RoundedCornerShape(50))
                AppStyle.Brief -> Modifier.clip(shape).background(palette.action)
                AppStyle.Board -> Modifier.clip(shape).background(palette.surface).border(JChipTokens.border, palette.line, shape)
            })
            .clickable(onClickLabel = actionLabel, role = Role.Button, onClick = onClick)
            .semantics { liveRegion = LiveRegionMode.Polite }
            .padding(horizontal = JChipTokens.horizontalPadding),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (status != null) {
            Spacer(Modifier.size(JChipTokens.dotSize).clip(CircleShape).background(palette.status(status).dot))
            Spacer(Modifier.width(JChipTokens.dotGap))
        }
        // The words give way, never the arrow: a long text or a large font is cut short at the end.
        Text(text, Modifier.weight(1f, fill = false), style = PersonaTheme.type.labelSmall.copy(fontWeight = FontWeight.SemiBold), color = ink,
            maxLines = 1, softWrap = false, overflow = TextOverflow.Ellipsis)
        Spacer(Modifier.width(JChipTokens.iconGap))
        JIcon(PhIcons.ArrowUp, null, size = JChipTokens.iconSize, tint = ink, decorative = true)
    }
}

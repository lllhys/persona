package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight

/**
 * A number in iOS's disc (`Theme.Size.badge`, its words in the accent on the accent's soft fill): a plan card's task number
 * (#4339 C13). It grows with the text size as iOS's `scaledFrame` does, up to 1.6 times, as the queue strip's number does.
 */
@Composable
fun JNumberDisc(number: Int, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    Box(
        modifier.size(Tokens.Size.badge * LocalDensity.current.fontScale.coerceIn(1f, HomeTokens.shellScaleMaximum))
            .clip(CircleShape).background(palette.accentSoft),
        contentAlignment = Alignment.Center,
    ) { Text("$number", style = PersonaTheme.type.micro.copy(fontWeight = FontWeight.Bold), color = palette.accent, maxLines = 1) }
}

package persona.core.designsystem

import androidx.compose.runtime.Immutable
import androidx.compose.ui.unit.Dp

/** Settings keeps its existing navigation stack; only its outer presentation changes with the window. */
enum class SettingsPresentation { FullWindow, PageSized }

/** Named page-sizing values. The existing list-detail directive still decides when two panes fit. */
object SettingsPageSizing {
    val maximumWidth: Dp get() = ReadingWidth
    val maximumHeight: Dp get() = ReadingWidth
    val minimumWindowMargin: Dp get() = Tokens.Space.xl
}

@Immutable
data class SettingsPageBounds(val width: Dp, val height: Dp)

fun settingsPresentation(largeWindow: Boolean): SettingsPresentation =
    if (largeWindow) SettingsPresentation.PageSized else SettingsPresentation.FullWindow

/** A null result is the compact fallback: there is no usable page inside the required window margins. */
fun settingsPageBounds(availableWidth: Dp, availableHeight: Dp): SettingsPageBounds? {
    val margin = SettingsPageSizing.minimumWindowMargin * 2
    val width = availableWidth - margin
    val height = availableHeight - margin
    if (width <= Tokens.Space.flush || height <= Tokens.Space.flush) return null
    return SettingsPageBounds(
        width = minOf(width, SettingsPageSizing.maximumWidth),
        height = minOf(height, SettingsPageSizing.maximumHeight),
    )
}

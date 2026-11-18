package persona.core.designsystem

import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.Dp

/**
 * Settings › Appearance › Layout density (#5564): how much room goes around and between things. Compact tightens every
 * [Tokens.Space] value by its [scale]; text, icons, radii and touch targets keep their sizes. Standard is the default and
 * draws exactly the spec's values.
 */
enum class LayoutDensity(
    val key: String,
    val scale: Float,
    /** Page side margin ([pagePadding]); gentler than [scale] so text never touches the screen edge. */
    val pageScale: Float,
    /** Corner radii ([Tokens.Radius], [PersonaTheme.shapes], [corner]); pills stay pills. */
    val radiusScale: Float,
    /** Minimum row and button heights ([row]); text and icons still fit, the extra air around them goes. */
    val rowScale: Float,
    /** Chat bubbles' corners ([bubbleCorner]): Compact squares them off more than other radii, a big round bubble reads loose. */
    val bubbleScale: Float,
    @StringRes val labelRes: Int,
) {
    Standard("standard", 1f, 1f, 1f, 1f, 1f, R.string.designsystem_density_standard),
    Compact("compact", 0.5f, 0.625f, 0.6f, 0.8f, 0.3f, R.string.designsystem_density_compact);

    val label: String @Composable get() = stringResource(labelRes)

    companion object {
        val Default = Standard
        fun fromKey(key: String?): LayoutDensity = entries.firstOrNull { it.key == key } ?: Default

        /**
         * What [Tokens.Space] multiplies by, for every window of the app. Snapshot state, so a composition that read a
         * spacing token draws again when the choice changes; [AppStyleStore] sets it.
         */
        var spaceScale by mutableFloatStateOf(Default.scale)
            internal set
        var pageScale by mutableFloatStateOf(Default.pageScale)
            internal set
        var radiusScale by mutableFloatStateOf(Default.radiusScale)
            internal set
        var rowScale by mutableFloatStateOf(Default.rowScale)
            internal set
        var bubbleScale by mutableFloatStateOf(Default.bubbleScale)
            internal set

        internal fun apply(density: LayoutDensity) {
            spaceScale = density.scale
            pageScale = density.pageScale
            radiusScale = density.radiusScale
            rowScale = density.rowScale
            bubbleScale = density.bubbleScale
        }
    }
}

/** An off-scale gap (a bubble's 14 x 10 inset, say) that still follows [LayoutDensity]; on-scale gaps use [Tokens.Space]. */
val Dp.dense: Dp get() = this * LayoutDensity.spaceScale

/** A row or button's minimum height (`heightIn(min = 40.dp.row)`) that Compact lowers by [LayoutDensity.rowScale]. */
val Dp.row: Dp get() = this * LayoutDensity.rowScale

/** A corner radius (`RoundedCornerShape(12.dp.corner)`) that Compact rounds less by [LayoutDensity.radiusScale]. */
val Dp.corner: Dp get() = this * LayoutDensity.radiusScale

/** A chat bubble's corner radius, which Compact rounds less by [LayoutDensity.bubbleScale]. */
val Dp.bubbleCorner: Dp get() = this * LayoutDensity.bubbleScale

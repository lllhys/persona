package persona.core.designsystem

import androidx.compose.ui.unit.Dp
import kotlin.math.ceil

/** Space left between scrolling content and the upper edge of a horizontal hinge. */
internal object TabletopHingeSpacing {
    val contentBottom: Dp = Tokens.Space.m
}

/** The geometry of a page after considering its measured chrome and the window's horizontal hinge. */
internal sealed interface TabletopPageLayout {
    data object SinglePage : TabletopPageLayout

    data class Split(
        val bodyHeight: Int,
        val hingeBottom: Int,
        val dockPlacement: TabletopDockPlacement,
    ) : TabletopPageLayout
}

/** A split page without a dock has no interactive content below the hinge. */
internal sealed interface TabletopDockPlacement {
    data object BackgroundOnly : TabletopDockPlacement
    data class BottomBar(val top: Int) : TabletopDockPlacement
}

/**
 * Resolves tabletop geometry in window pixels. Both bars are measured before this is called, so a small lower half
 * falls back to the ordinary single page instead of clipping an estimated dock.
 */
internal fun tabletopPageLayout(
    window: WindowLayout,
    topBarHeight: Int,
    bottomBarHeight: Int?,
    totalHeight: Int,
    minimumContentHeight: Int,
    bottomInset: Int = 0,
): TabletopPageLayout {
    val height = totalHeight.coerceAtLeast(0)
    if (!window.tabletop || height == 0) return TabletopPageLayout.SinglePage

    val hingeTop = window.hingeTop.toInt().coerceIn(0, height)
    val hingeBottom = ceil(window.hingeBottom.toDouble()).toInt().coerceIn(hingeTop, height)
    if (hingeBottom <= hingeTop) return TabletopPageLayout.SinglePage

    val requiredAbove = topBarHeight.coerceAtLeast(0).toLong() + minimumContentHeight.coerceAtLeast(0)
    if (requiredAbove > hingeTop) return TabletopPageLayout.SinglePage

    if (bottomBarHeight == null) {
        return TabletopPageLayout.Split(hingeTop, hingeBottom, TabletopDockPlacement.BackgroundOnly)
    }

    val dockHeight = bottomBarHeight.coerceAtLeast(0)
    val lowerHeight = height - hingeBottom
    val safeLowerHeight = lowerHeight - bottomInset.coerceIn(0, lowerHeight)
    if (dockHeight > safeLowerHeight) return TabletopPageLayout.SinglePage
    val dockTop = hingeBottom + (safeLowerHeight - dockHeight) / 2
    return TabletopPageLayout.Split(hingeTop, hingeBottom, TabletopDockPlacement.BottomBar(dockTop))
}

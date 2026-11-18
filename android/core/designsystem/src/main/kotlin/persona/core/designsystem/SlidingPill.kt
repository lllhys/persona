package persona.core.designsystem

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorProducer
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.layout.Placeable
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.hideFromAccessibility
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.LayoutDirection
import kotlin.math.roundToInt

/**
 * False on [HomeNavigationLayout]'s unplaced measuring copies. Those copies only decide which
 * switch fits; a spring there would run twice more on every tap and never be seen.
 */
internal val LocalSlidingPillAnimate = staticCompositionLocalOf { true }

/**
 * One pill behind the chosen segment, Glass only.
 *
 * A tap moves the pill on [PersonaTheme.motion.enter], the iPhone's `Theme.Motion.state`
 * (`apple/Shared/Design/Theme.swift` line 726, `Animation.snappy(duration: 0.25)`), through
 * `Tokens.Motion.State`. That is the shell switch's pill (`apple/Persona/DesignSystem/ShellComponents.swift`
 * `SwitchMotion`: the Duo tab bar line 325, across the top line 336) and the `matchedGeometryEffect` id
 * `"pill"` (lines 206 and 221, only when the switch is outside a shell). A segmented picker on iOS is the
 * system's sliding thumb (`Picker` `.segmented`, for example `apple/Persona/Views/Session/ReviewPage.swift`);
 * #9586 maps that tap onto the same state spring. The thumb's own UIKit curve is not in this repo.
 *
 * The animated value is the selected index, read while measuring, so the frames of the spring do not
 * recompose the labels. It is born on that index, so the first frame already draws the pill, the way the
 * switch is already on the chosen segment before a tap (`SwitchMotion` animates `chosen` only after it
 * changes). [pillFrame] then reads the segment placeables in the same measure pass. Nothing selected
 * (an index outside `0 until count`) draws no pill. System animations off put the pill on the choice at
 * once, as the Duo tab bar does (`SwitchMotion` line 325, `reduceMotion ? nil`). The horizontal switch
 * does not read Reduce Motion (line 336); Android still stops, with the rest of its motion.
 */
@Composable
internal fun GlassSlidingPill(
    selected: Int,
    count: Int,
    color: Color,
    modifier: Modifier = Modifier,
    segmentModifier: (Int) -> Modifier = { Modifier },
    segment: @Composable (Int) -> Unit,
) {
    val reduce = PersonaTheme.reduceMotion
    val glass = PersonaTheme.style == AppStyle.Glass
    val showPill = selected in 0 until count
    // Stable for one composition: the measuring copies set the local once and never change it.
    val animate = LocalSlidingPillAnimate.current && !reduce && glass
    // Born on the choice, so the first frame already matches. -1 means nothing was selected yet:
    // the next real choice appears there, it does not travel in from segment 0.
    val indexAnim = remember {
        Animatable(if (selected in 0 until count) selected.toFloat() else -1f)
    }
    val hadSelection = remember { mutableStateOf(selected in 0 until count) }
    val motion = PersonaTheme.motion
    LaunchedEffect(selected, count, animate) {
        val inRange = selected in 0 until count
        if (!inRange) {
            hadSelection.value = false
            return@LaunchedEffect
        }
        val target = selected.toFloat()
        val appear = !hadSelection.value || indexAnim.value !in 0f..(count - 1).toFloat()
        if (!animate || appear) indexAnim.snapTo(target)
        else if (indexAnim.targetValue != target) indexAnim.animateTo(target, motion.enter())
        hadSelection.value = true
    }
    val indexState: State<Float> = indexAnim.asState()
    // One layout, absolute left edges. A Row would mirror in RTL and a Box would mirror a shorter pill
    // again (`Alignment.TopStart`), so the pill left the segment. iOS keeps the pill on the segment when
    // the control mirrors (the system segmented thumb, and `ShellSwitch`'s pill).
    Layout(
        modifier = modifier,
        content = {
            if (showPill) {
                Box(
                    Modifier.clip(CircleShape).background(color)
                        .testTag("sliding-pill").semantics { hideFromAccessibility() },
                )
            }
            // [segmentModifier] goes on the box this layout places, so a modifier that reads the segment's place in the
            // row (`revealWhenSelected`) sees where it sits, not 0 inside its own box.
            repeat(count) { index -> Box(segmentModifier(index)) { segment(index) } }
        },
    ) { measurables, constraints ->
        // Read here, not in composition: a tap then invalidates measure, not the whole control.
        // A stale index (nothing was selected) is not a place to spring from; use the choice itself.
        val raw = indexState.value
        val inRange = raw in 0f..(count - 1).toFloat()
        val animatedIndex = if (showPill && animate && hadSelection.value && inRange) raw else selected.toFloat()
        val segmentMeasurables = if (showPill) measurables.drop(1) else measurables
        // The old Glass `JSegmented` was a `Row`. Each child gets the width still left, so a later
        // label ellipsizes instead of being placed past the end (and past the start in RTL).
        var cursor = 0
        val segmentPlaceables = segmentMeasurables.map { measurable ->
            // Infinity is Int.MAX_VALUE. Subtracting from it is not a legal max width.
            val maxChild = if (constraints.hasBoundedWidth) (constraints.maxWidth - cursor).coerceAtLeast(0)
                else Constraints.Infinity
            val placeable = measurable.measure(constraints.copy(minWidth = 0, minHeight = 0, maxWidth = maxChild))
            cursor += placeable.width
            placeable
        }
        val contentWidth = segmentPlaceables.sumOf { it.width }
        val contentHeight = segmentPlaceables.maxOfOrNull { it.height } ?: 0
        // `fillMaxWidth` makes minWidth equal maxWidth. The row fills that width and packs the
        // segments at the start (the right edge in RTL).
        val width = maxOf(contentWidth, constraints.minWidth).coerceAtMost(constraints.maxWidth)
        val height = maxOf(contentHeight, constraints.minHeight).coerceAtMost(constraints.maxHeight)
        val rtl = layoutDirection == LayoutDirection.Rtl
        val starts = IntArray(segmentPlaceables.size)
        val widths = IntArray(segmentPlaceables.size)
        val heights = IntArray(segmentPlaceables.size)
        cursor = 0
        segmentPlaceables.forEachIndexed { i, child ->
            starts[i] = if (rtl) width - cursor - child.width else cursor
            widths[i] = child.width
            heights[i] = child.height
            cursor += child.width
        }
        val frame = if (showPill) {
            pillFrame(animatedIndex, starts.asList(), widths.asList(), heights.asList())
        } else null
        val pillPlaceable = if (frame != null) {
            measurables.first().measure(
                Constraints.fixed(frame.width.coerceAtLeast(0), frame.height.coerceAtLeast(0)),
            )
        } else null
        layout(width, height) {
            // Behind the labels. The last place() draws on top.
            if (pillPlaceable != null && frame != null) pillPlaceable.place(frame.x, 0)
            segmentPlaceables.forEachIndexed { i, child -> child.place(starts[i], 0) }
        }
    }
}

/**
 * The label on one segment. Glass fades its colour on [PersonaTheme.motion.enter], the same spec as
 * the pill: iOS changes `foregroundStyle` inside `SwitchMotion`'s `animation(Theme.Motion.state, value: chosen)`
 * (`ShellComponents.swift` lines 325 and 336; the styles are `beautifulSegment` lines 198–199 and
 * `plainSegment` lines 214–215).
 *
 * A weight change is a second text crossfaded on that spring. SwiftUI crossfades the glyphs when the
 * font changes; it does not draw a half-bold word. [idleStyle] defaults to [style], and then only the
 * colour moves (`JSegmented` keeps one weight, as the old row did). At rest the label is one `Text`,
 * so the settled control matches that row. The amount is read while drawing, not in composition.
 */
@Composable
internal fun GlassSegmentLabel(
    text: String,
    selected: Boolean,
    selectedColor: Color,
    idleColor: Color,
    style: TextStyle,
    modifier: Modifier = Modifier,
    idleStyle: TextStyle = style,
    softWrap: Boolean = true,
    onComposed: () -> Unit = {},
) {
    onComposed()
    val animate = LocalSlidingPillAnimate.current && !PersonaTheme.reduceMotion && PersonaTheme.style == AppStyle.Glass
    if (!animate) {
        Text(
            text, modifier, color = if (selected) selectedColor else idleColor,
            style = if (selected) style else idleStyle,
            maxLines = 1, softWrap = softWrap, overflow = TextOverflow.Ellipsis,
        )
        return
    }
    // `shown` flips when the spring finishes, so the settled label is the same `Text` as before.
    // It does not change on the frames in between.
    var shown by remember { mutableStateOf(selected) }
    val amount = animateFloatAsState(
        targetValue = if (selected) 1f else 0f,
        animationSpec = PersonaTheme.motion.enter(),
        label = "segment-label",
        finishedListener = { _ -> shown = selected },
    )
    if (shown == selected) {
        Text(
            text, modifier, color = if (selected) selectedColor else idleColor,
            style = if (selected) style else idleStyle,
            maxLines = 1, softWrap = softWrap, overflow = TextOverflow.Ellipsis,
        )
        return
    }
    val sameGlyphs = style == idleStyle
    if (sameGlyphs) {
        BasicText(
            text = text,
            modifier = modifier,
            style = style,
            overflow = TextOverflow.Ellipsis,
            softWrap = softWrap,
            maxLines = 1,
            color = ColorProducer { lerp(idleColor, selectedColor, amount.value) },
        )
        return
    }
    Layout(
        modifier = modifier,
        content = {
            Text(
                text, color = idleColor, style = idleStyle, maxLines = 1, softWrap = softWrap,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.graphicsLayer { alpha = 1f - amount.value }.clearAndSetSemantics {},
            )
            Text(
                text, color = selectedColor, style = style, maxLines = 1, softWrap = softWrap,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.graphicsLayer { alpha = amount.value },
            )
        },
    ) { measurables, constraints ->
        val loose = constraints.copy(minWidth = 0, minHeight = 0)
        val idle = measurables[0].measure(loose)
        val chosen = measurables[1].measure(loose)
        // The resting width is the target style's width, so the end of the spring matches one `Text`.
        val main = if (selected) chosen else idle
        layout(main.width, main.height) {
            fun place(child: Placeable) {
                child.place((main.width - child.width) / 2, (main.height - child.height) / 2)
            }
            place(idle)
            place(chosen)
        }
    }
}

/** The pill's place at [index]: 0 on the first segment, [n - 1] on the last. Values between lerp. */
internal data class PillFrame(val x: Int, val width: Int, val height: Int)

internal fun pillFrame(index: Float, starts: List<Int>, widths: List<Int>, height: Int): PillFrame {
    if (starts.isEmpty() || widths.isEmpty()) return PillFrame(0, 0, height.coerceAtLeast(0))
    return pillFrame(index, starts, widths, List(minOf(starts.size, widths.size)) { height })
}

/** Height follows the segment under the pill. The highlight used to be that segment's own background. */
internal fun pillFrame(index: Float, starts: List<Int>, widths: List<Int>, heights: List<Int>): PillFrame {
    val n = minOf(starts.size, widths.size, heights.size)
    if (n == 0) return PillFrame(0, 0, 0)
    val last = n - 1
    val clamped = index.coerceIn(0f, last.toFloat())
    if (last == 0) return PillFrame(starts[0], widths[0].coerceAtLeast(0), heights[0].coerceAtLeast(0))
    val i = clamped.toInt().coerceAtMost(last - 1)
    val t = clamped - i
    val x = starts[i] + (starts[i + 1] - starts[i]) * t
    val w = widths[i] + (widths[i + 1] - widths[i]) * t
    val h = heights[i] + (heights[i + 1] - heights[i]) * t
    return PillFrame(x.roundToInt(), w.roundToInt().coerceAtLeast(0), h.roundToInt().coerceAtLeast(0))
}

package persona.core.designsystem

import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.composed
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import persona.core.designsystem.icons.PhIcons

/** How a drag on a message quotes it (iOS `MessageSwipe`, #1058). */
object SwipeReply {
    /** How far left the message goes before letting go replies: past it, a tap of the haptics and the icon fills. */
    val threshold = Tokens.MinTouch.ios * 1.5f
    /** Past the threshold the message follows the finger at this rate (a rubber band). */
    const val stretch = 0.25f

    /** Where the message sits for a drag of [x] (≤ 0), both in px. */
    fun offset(x: Float, threshold: Float): Float = if (x >= -threshold) x else -threshold + (x + threshold) * stretch

    /** Whether letting go at [x] replies. */
    fun armed(x: Float, threshold: Float): Boolean = -x >= threshold
}

/**
 * Drag a message left to reply to it (iOS #1058): it follows the finger, the reply icon comes in at its right and
 * fills with the style's action colour once past [SwipeReply.threshold] (with a haptic tap); letting go there calls [onReply], and the
 * message springs back either way on [Tokens.Motion.SpringBack], copying `withAnimation(Theme.Motion.springBack)` at
 * `MessageMenu.swift:470`. That call does not read Reduce Motion, on iPhone and iPad alike, so Android springs in
 * every style with animations off too. Accessibility services get [label] as a custom action instead of the drag.
 * Vertical drags still scroll the list: only a sideways move past the touch slop is taken.
 */
fun Modifier.swipeToReply(label: String, onReply: () -> Unit): Modifier = composed {
    val haptics = LocalHapticFeedback.current
    val scope = rememberCoroutineScope()
    val reply by rememberUpdatedState(onReply)
    val offset = remember { Animatable(0f) }
    var drag by remember { mutableStateOf(0f) }
    var armed by remember { mutableStateOf(false) }
    val palette = PersonaTheme.palette
    val icon = rememberVectorPainter(if (PersonaTheme.style == AppStyle.Board) PhIcons.ArrowBendUpLeft.bold else PhIcons.ArrowBendUpLeft.regular)
    val well = 32.dp
    val glyph = 18.dp
    val inset = Tokens.Space.s
    val spring = IosSpring(Tokens.Motion.SpringBack.duration, Tokens.Motion.SpringBack.bounce).spec<Float>()
    this
        // Merged so the screen reader focuses the message as one node, the one that carries the action (links and
        // buttons inside stay their own stops: clickable nodes are not merged in).
        .semantics(mergeDescendants = true) { customActions = listOf(CustomAccessibilityAction(label) { reply(); true }) }
        .pointerInput(Unit) {
            val limit = SwipeReply.threshold.toPx()
            fun settle() {
                if (armed) reply()
                armed = false
                drag = 0f
                scope.launch { offset.animateTo(0f, spring) }
            }
            detectHorizontalDragGestures(
                onDragEnd = ::settle,
                onDragCancel = ::settle,
            ) { change, amount ->
                drag = (drag + amount).coerceAtMost(0f)
                if (drag < 0f) change.consume()
                val past = SwipeReply.armed(drag, limit)
                if (past != armed) {
                    armed = past
                    if (past) haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove)
                }
                scope.launch { offset.snapTo(SwipeReply.offset(drag, limit)) }
            }
        }
        .drawWithContent {
            val x = offset.value
            translate(left = x) { this@drawWithContent.drawContent() }
            if (x < 0f) {
                val progress = (-x / SwipeReply.threshold.toPx()).coerceIn(0f, 1f)
                val radius = well.toPx() / 2 * (0.6f + 0.4f * progress)
                val center = Offset(size.width - inset.toPx() - well.toPx() / 2, size.height / 2)
                drawCircle(if (armed) palette.action else palette.sunken, radius, center, alpha = progress)
                val g = glyph.toPx() * (0.6f + 0.4f * progress)
                translate(center.x - g / 2, center.y - g / 2) {
                    with(icon) {
                        draw(androidx.compose.ui.geometry.Size(g, g), alpha = progress,
                            colorFilter = ColorFilter.tint(if (armed) palette.onAction else palette.ink2))
                    }
                }
            }
        }
}

package persona.core.designsystem

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer

/**
 * What opens in a chat, as iOS `.unfolding` (`ChatPin.swift:707`).
 *
 * A composable [Modifier], not a wrapper: the opened block already has a place in the tree, and a `graphicsLayer`
 * alpha does not change the size it measures. The row is its full height on the first frame and only fades in
 * (`Theme.Motion.reveal`, easeOut 0.28 s, `Theme.swift:754`; [Tokens.Motion.Reveal]). What closes leaves the
 * composition at once, its space with it (`toggleInPlace` sets `disablesAnimations`, `ChatPin.swift:686-688`, #3005).
 * Several iOS sites also write `.transition(.opacity.combined(with: .move(edge: .top)))` on that block. The insertion
 * runs inside that disabled transaction, so this modifier does not slide. [rememberOpenEdge] is true only on the
 * composition where `open` flips from closed to open, matching `Unfolding.revealing` (`ChatPin.swift:684-692`): a row
 * that was already open, or that a lazy list composed again, does not fade again. Reduced motion draws it at once
 * (`ChatPin.swift:721-722`). iPhone and regular width (iPad, iPhone Duo) use the same modifier; no size class skips it.
 */
@Stable
class OpenEdge {
    private var previous: Boolean? = null

    /**
     * True only on the composition where [open] changes from closed to open and motion is allowed.
     * The previous value updates after this composition, so a second pass in the same frame still sees the edge.
     */
    @Composable
    fun revealing(open: Boolean): Boolean {
        val reduce = PersonaTheme.reduceMotion
        val show = previous == false && open && !reduce
        SideEffect { previous = open }
        return show
    }
}

/** One [OpenEdge] for this composition. [key] starts a fresh edge when the row's identity changes. */
@Composable
fun rememberOpenEdge(key: Any? = null): OpenEdge = remember(key) { OpenEdge() }

/**
 * Fade this node in over the space it already takes, when [revealing]. Otherwise draw it as it is.
 * Put it on a node that enters the composition when the row opens.
 */
@Composable
fun Modifier.unfolding(revealing: Boolean): Modifier {
    val reduce = PersonaTheme.reduceMotion
    val fade = remember { revealing && !reduce }
    val alpha = remember { Animatable(if (fade) 0f else 1f) }
    LaunchedEffect(fade, reduce) {
        if (!fade || reduce) alpha.snapTo(1f)
        else alpha.animateTo(
            1f,
            tween(Tokens.Motion.Reveal.duration, easing = ComposerMotion.curve(Tokens.Motion.Reveal.curve)),
        )
    }
    // Read alpha only while drawing. Reading it here recomposes the caller on every frame of the fade.
    if (!fade || reduce) return this
    return graphicsLayer { this.alpha = alpha.value }
}

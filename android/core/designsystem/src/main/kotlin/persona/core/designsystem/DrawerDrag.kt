package persona.core.designsystem

import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.systemGestures
import androidx.compose.foundation.systemGestureExclusion
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.input.pointer.PointerEvent
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.PointerInputChange
import androidx.compose.ui.input.pointer.PointerInputScope
import androidx.compose.ui.input.pointer.SuspendingPointerInputModifierNode
import androidx.compose.ui.input.pointer.util.VelocityTracker
import androidx.compose.ui.input.pointer.util.addPointerInputChange
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInRoot
import androidx.compose.ui.layout.positionInWindow
import androidx.compose.ui.node.DelegatingNode
import androidx.compose.ui.node.ModifierNodeElement
import androidx.compose.ui.node.RootForTest
import androidx.compose.ui.platform.InspectorInfo
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.SemanticsNode
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import kotlin.coroutines.cancellation.CancellationException
import kotlin.math.abs
import kotlin.math.hypot
import kotlin.math.max
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * The phone drawer's finger tracking, copied from the iPhone.
 *
 * Release: [AssistantDrawer.settlesOpen] (`apple/Persona/Views/Assistant/AssistantSidebar.swift:141-143`) is
 * [SwipeRelease.goes] (`apple/Persona/Views/Shell/ShellPager.swift:193-196`). A flick faster than 300 pt/s goes that
 * way; otherwise progress plus 0.2 s of velocity, over the width, opens past one half. Velocity is positive toward
 * open. 300 is not scale-invariant, so the threshold is in dp/s and the width in dp.
 *
 * Share: `AssistantSidebar.swift:119-121` is `velocity / way` (the page moves with the finger). The pager's settle
 * (`ShellPager.swift:156-157`) negates that because the strip moves against the finger; the drawer does not.
 * SwiftUI's initial velocity is the share of the remaining distance per second. Compose's is in value units per
 * second, so a 0..1 progress spring takes `share * (target - progress)`.
 *
 * The finger's settle uses iOS `Theme.Motion.drawer` (`apple/Shared/Design/Theme.swift:799-801`), which does not
 * read Reduce Motion, so that spring runs with the finger's velocity. A button, Back, or another write of the open
 * flag keeps [PersonaTheme.reduceMotion]. Unfolding to the regular-width column snaps. A system animator scale of 0
 * is the platform's own switch and makes the finger's spring instant.
 */
internal object DrawerDrag {
    /** Below this, the remaining distance is too short to turn into a share (`AssistantSidebar.swift:120`). */
    const val minimumWay = 1f

    fun settlesOpen(progress: Float, velocityDpPerSecond: Float, widthDp: Float): Boolean {
        if (abs(velocityDpPerSecond) > HomeTokens.drawerFlickDpPerSecond) return velocityDpPerSecond > 0f
        val width = max(widthDp, minimumWay)
        return progress + velocityDpPerSecond * HomeTokens.drawerProjectionSeconds / width > 0.5f
    }

    /**
     * Share of the remaining distance the finger covers per second. Always ≥ 0; a finger heading the other way is 0.
     * [velocity] and [width] must share a unit (both dp, or both px).
     */
    fun velocityShare(progress: Float, target: Float, velocity: Float, width: Float): Float {
        val way = (target - progress) * max(width, minimumWay)
        return if (abs(way) > minimumWay) max(0f, velocity / way) else 0f
    }

    /** [velocityShare] as Compose progress units per second, signed toward [target]. */
    fun progressPerSecond(progress: Float, target: Float, velocity: Float, width: Float): Float =
        velocityShare(progress, target, velocity, width) * (target - progress)

    /** `AssistantDrawer.drag` (`AssistantSidebar.swift:100-104`): no animation, clamped to 0..1. */
    fun tracked(dragStart: Float, translationPx: Float, widthPx: Float): Float =
        (dragStart + translationPx / max(widthPx, minimumWay)).coerceIn(0f, 1f)

    /**
     * `AssistantDrawer.takes` (`AssistantSidebar.swift:147-149`). Within 45° of horizontal (strictly more horizontal
     * than vertical), and while closed only to the right. [isOpen] is "out at all" (`progress > 0`), not the resting flag.
     */
    fun takes(dx: Float, dy: Float, isOpen: Boolean): Boolean = abs(dx) > abs(dy) && (isOpen || dx > 0f)

    /**
     * A down in a system back-gesture zone that this drawer does not own. The trailing inset always refuses.
     * The leading inset is claimed only inside the [RootEdgeExclusion] band (#9803); outside that band Android
     * still owns Back, so taking the down would fight it. Matching iOS: no Back at the Assistant root
     * (`SwipeBack.swift:11-16`; `DrawerPan` covers the whole page, `AssistantSidebar.swift:153-154`); a pushed
     * page keeps the drag off (`DrawerPan`, `AssistantSidebar.swift:505-509`). A zero inset (three-button
     * navigation) yields nothing.
     */
    fun inSystemBackInset(
        x: Float,
        y: Float,
        leadingPx: Float,
        trailingPx: Float,
        widthPx: Float,
        heightPx: Float,
        bandHeightPx: Float,
    ): Boolean {
        if (trailingPx > 0f && x > widthPx - trailingPx) return true
        if (leadingPx <= 0f || x >= leadingPx) return false
        // Inside the leading inset but outside the excluded band: leave it to system Back.
        return !RootEdgeExclusion.inLeadingBand(
            x = x,
            y = y,
            leadingPx = leadingPx,
            layoutWidthPx = widthPx,
            layoutHeightPx = heightPx,
            bandHeightPx = bandHeightPx,
        )
    }

    /** Why a drag was refused, the words `DrawerPan` records (`AssistantSidebar.swift:484-527`). */
    enum class Decision { Take, Direction, Pushed, SwipeRow, SidewaysScroller, SystemBack, Child }

    /**
     * [onSwipeRow], [sidewaysRoom] and [childOwns] are resolved by the caller. iOS hit-tests the scroller and the
     * swipe row (`AssistantSidebar.swift:500`, `:551-563`). A sideways scroller is checked only while closed (line
     * 526), and only when it has room toward its start (line 558). [childOwns] is a move some other recognizer has
     * already taken (a slider, a selection, a chart, a swipe row). An Android view keeps a rightward drag
     * only while it can scroll toward its start, so it is not one of those. Scroll views wait instead
     * (`shouldBeRequiredToFailBy`, lines 540-544), so a list consuming a horizontal move is not one of those.
     */
    fun decision(
        dx: Float,
        dy: Float,
        isOpen: Boolean,
        atRoot: Boolean,
        onSwipeRow: Boolean,
        sidewaysRoom: Boolean,
        inSystemBack: Boolean,
        childOwns: Boolean = false,
    ): Decision {
        if (inSystemBack) return Decision.SystemBack
        if (!atRoot) return Decision.Pushed
        if (isOpen) {
            if (dx < 0f && abs(dx) > abs(dy) && onSwipeRow) return Decision.SwipeRow
            if (childOwns && abs(dx) > abs(dy)) return Decision.Child
            return if (takes(dx, dy, isOpen = true)) Decision.Take else Decision.Direction
        }
        if (abs(dx) <= abs(dy)) return Decision.Direction
        if (dx <= 0f) return Decision.Direction
        if (childOwns) return Decision.Child
        if (sidewaysRoom) return Decision.SidewaysScroller
        return Decision.Take
    }

    /**
     * How far the sidebar sits below its resting place, in px. `applyToUIKit` (`AssistantSidebar.swift:69`) is
     * `(1 - progress) * Theme.Motion.drawerLift`, and `drawerLift` is `Theme.Space.s` (`Theme.swift:805`). At rest
     * open the offset is zero so no layer is added. At rest closed the sidebar is not composed.
     */
    fun liftPx(progress: Float, fullLiftPx: Float): Float =
        if (progress >= 1f) 0f else (1f - progress.coerceAtLeast(0f)) * fullLiftPx

    /**
     * Hairline opacity. `DrawerPageMotion` (`AssistantSidebar.swift:406` and `:421`) strokes only while out, at
     * opacity `progress`. Null hides the line (closed). At 1 the line keeps its own color, unscaled.
     */
    fun hairlineAlpha(progress: Float, lineAlpha: Float): Float? = when {
        progress <= 0f -> null
        progress >= 1f -> lineAlpha
        else -> lineAlpha * progress
    }
}

private class DrawerSettle(val open: Boolean, val velocity: Float, val token: Int, val finger: Boolean = false)

/**
 * Finger state for [rememberPhoneDrawerDrag]. The pointer scope cannot call [Animatable.stop] (it is a restricted
 * suspend scope), so a drag writes [dragProgress] and a normal coroutine snaps the animatable when the finger lets go.
 */
private class DrawerRuntime(opened: Boolean) {
    val anim = Animatable(if (opened) 1f else 0f)
    var settle by mutableStateOf(DrawerSettle(opened, 0f, 0))
    var dragging by mutableStateOf(false)

    /**
     * True from the moment a finger takes the page until that settle animation finishes. The shell reads progress
     * in draw lambdas while this is set, so the drag does not recompose the drawer every frame.
     */
    var live by mutableStateOf(false)
    var dragProgress by mutableFloatStateOf(if (opened) 1f else 0f)
    var widthPx = 1f
    var leadingPx = 0f
    var trailingPx = 0f
    /** Centred exclusion band height in px (`RootEdgeExclusion.heightDp`); used to refuse left-inset downs outside it. */
    var bandHeightPx = 0f
    var opened = opened
    var carryVelocity = false
    var style: AppStyle = AppStyle.Glass
    var reduceMotion = false

    /** False while the window is regular: an external close then is the unfold, and it snaps. */
    var dragEnabled = false

    /** Horizontal scroll value 0 is the left edge. In RTL the start of that scroller is the high end. */
    var rtl = false
    var onOpen: () -> Unit = {}
    var onDismiss: () -> Unit = {}
    var haptics: (Haptic) -> Unit = {}
    var clearText: () -> Unit = {}
    var fingerDown = false

    /** Top-left of this drawer in the semantics root, so a pointer lines up with [SemanticsNode.boundsInRoot]. */
    var originInRoot by mutableStateOf(Offset.Zero)

    /** The compose view. Its semantics owner and its child Android views are the public hit test. */
    var host: View? = null

    /** An external open or close arrived while the finger was down. That target wins over the release. */
    var superseded = false
    lateinit var scope: CoroutineScope

    /** While the finger is down, the page is wherever the finger put it. A finished animation is exactly 0 or 1. */
    val shown: Float
        get() = if (dragging) dragProgress else anim.value
}

internal class PhoneDrawerDrag(
    private val progressOf: () -> Float,
    private val liveOf: () -> Boolean,
    val modifier: Modifier,
    private val widthOf: () -> Float,
    private val setWidth: (Float) -> Unit,
) {
    val progress: Float get() = progressOf()

    /** A finger is tracking, or the settle animation from that tracking is still running. */
    val live: Boolean get() = liveOf()

    /** The drawer's width in px, measured by the shell. Tracking divides by it (`AssistantSidebar.swift:103`). */
    var widthPx: Float
        get() = widthOf()
        set(value) { setWidth(value) }
}

/**
 * Progress of the phone drawer, plus the modifier that tracks a finger when [dragEnabled].
 *
 * The spring runs only from [settle], so a finger that takes over mid-flight (`dragBegan`,
 * `AssistantSidebar.swift:92-97`) is not restarted at velocity 0 when the parent flag catches up.
 * The finger's settle passes the share as the spring's initial velocity, including when Reduce Motion is on
 * (`Theme.Motion.drawer`, `Theme.swift:799-801`, does not read it). Brief and Board pass 0; their tweens ignore it.
 * A button, Back, or another write of the open flag keeps [PersonaTheme.reduceMotion]. Unfolding snaps.
 */
@Composable
internal fun rememberPhoneDrawerDrag(
    opened: Boolean,
    onOpen: () -> Unit,
    onDismiss: () -> Unit,
    dragEnabled: Boolean,
): PhoneDrawerDrag {
    val runtime = remember { DrawerRuntime(opened) }
    runtime.scope = rememberCoroutineScope()
    runtime.opened = opened
    runtime.onOpen = onOpen
    runtime.onDismiss = onDismiss
    // iOS `drawer(velocity:)` does not read Reduce Motion (`Theme.swift:799-801`). Pass the style's spec with the
    // reduced-motion flag off so Glass keeps the spring and the share. `drawerSpec`'s own flag still serves the
    // resting-pixel oracle, which is not this finger settle.
    runtime.style = PersonaTheme.style
    runtime.reduceMotion = PersonaTheme.reduceMotion
    runtime.dragEnabled = dragEnabled
    runtime.carryVelocity = runtime.style == AppStyle.Glass
    runtime.haptics = rememberHaptics()
    runtime.host = LocalView.current
    val focus = LocalFocusManager.current
    val keyboard = LocalSoftwareKeyboardController.current
    runtime.clearText = {
        // `dragBegan` resigns the first responder when the drag starts closed (`AssistantSidebar.swift:94-96`).
        focus.clearFocus()
        keyboard?.hide()
    }
    val density = LocalDensity.current
    val direction = LocalLayoutDirection.current
    runtime.rtl = direction == LayoutDirection.Rtl
    val gestures = WindowInsets.systemGestures
    runtime.leadingPx = gestures.getLeft(density, direction).toFloat()
    runtime.trailingPx = gestures.getRight(density, direction).toFloat()

    // Back, or any other write of [opened], ends a drag in progress and wins (`AssistantSidebar.swift` has no
    // second writer mid-pan; Android's BackHandler does). Skipping this while the finger is down let the release
    // open the drawer again.
    LaunchedEffect(opened) {
        if (runtime.fingerDown) {
            runtime.superseded = true
            if (runtime.dragging) {
                val progress = runtime.dragProgress
                runtime.anim.stop()
                runtime.anim.snapTo(progress)
                runtime.dragging = false
            }
        }
        if (runtime.settle.open != opened) {
            runtime.settle = DrawerSettle(opened, 0f, runtime.settle.token + 1)
        }
    }
    LaunchedEffect(runtime.settle.token) {
        val settle = runtime.settle
        // The finger's spring ignores Reduce Motion. A button or Back keeps the theme flag. Unfolding, which
        // turns the gesture off while closing, snaps: the sidebar column is already there.
        val velocity = if (settle.finger && runtime.carryVelocity) settle.velocity else 0f
        val spec = when {
            settle.finger -> drawerSpec<Float>(runtime.style, reduceMotion = false)
            !settle.open && !runtime.dragEnabled -> tween<Float>(Tokens.Motion.reduced)
            else -> drawerSpec<Float>(runtime.style, runtime.reduceMotion)
        }
        val target = if (settle.open) 1f else 0f
        if (runtime.anim.value != target || velocity != 0f) {
            runtime.anim.animateTo(target, spec, initialVelocity = velocity)
        }
        // `anim.stop()` cancels this effect, so a finger taking over does not clear [DrawerRuntime.live] here.
        runtime.live = false
    }

    // At a root with the drawer closed, reclaim the left system-gestures strip so a finger there opens the
    // drawer (`SwipeBack.swift:11-16`, `AssistantSidebar.swift:153-154`). Opened, pushed, or drag off: no exclusion.
    // dragEnabled is already false on another tab (`Root.kt`), so a composed-but-hidden shell publishes nothing.
    val bandHeightPx = with(density) { RootEdgeExclusion.heightDp.dp.toPx() }
    runtime.bandHeightPx = bandHeightPx
    val excludeEdge = if (dragEnabled && !opened) {
        val leadingPx = runtime.leadingPx
        Modifier.systemGestureExclusion { coordinates ->
            RootEdgeExclusion.rect(
                active = true,
                stripWidthPx = leadingPx,
                layoutWidthPx = coordinates.size.width.toFloat(),
                layoutHeightPx = coordinates.size.height.toFloat(),
                bandHeightPx = bandHeightPx,
                layoutLeftInWindowPx = coordinates.positionInWindow().x,
            ) ?: Rect.Zero
        }
    } else {
        Modifier
    }
    val modifier = Modifier
        .onGloballyPositioned { runtime.originInRoot = it.positionInRoot() }
        .then(excludeEdge)
        .then(DrawerDragElement(runtime, dragEnabled))
    return PhoneDrawerDrag(
        progressOf = { runtime.shown },
        liveOf = { runtime.live },
        modifier = modifier,
        widthOf = { runtime.widthPx },
        setWidth = { runtime.widthPx = it },
    )
}

/**
 * What is under the finger, from the frontmost placed semantics node upward, the way `sidewaysScroller` walks
 * (`AssistantSidebar.swift:551-563`).
 *
 * [Waiting] is a scroller that should let the drawer go first: a vertical list, or a horizontal one with no room
 * toward the start (line 558). Scroll views wait (`shouldBeRequiredToFailBy`, lines 540-544). [Sideways] is a
 * horizontal scroller that still has that room, and only while the drawer is closed and the finger pulls toward
 * the start (line 526). [Child] is a slider ([SemanticsActions.SetProgress]) or a text selection whose handles are
 * showing. A collapsed cursor is not one, and neither is a progress indicator. [SwipeRow] is [SwipeRowMark]: the
 * open drawer yields a leftward drag on it (line 500) instead of the list that contains it. A tap target is none of
 * these. An Android view is [Sideways] only when it can scroll toward the start (`View.canScrollHorizontally(-1)`,
 * the room at line 558).
 *
 * [button] is a clickable in front (the open scrim). Its consumption is not a drag, and a view under it is not the hit.
 * Semantics does not carry opacity, so only that button counts as covering the view.
 */
private enum class UnderFinger { None, Waiting, Sideways, Child, SwipeRow }

private data class FingerTarget(val under: UnderFinger, val button: Boolean)

private class DrawerDragNode(private val runtime: DrawerRuntime, enabled: Boolean) : DelegatingNode() {
    private var enabled = enabled
    private val pointer = delegate(SuspendingPointerInputModifierNode {
        if (!this@DrawerDragNode.enabled) return@SuspendingPointerInputModifierNode
        trackDrawer(runtime, this@DrawerDragNode)
    })

    fun setEnabled(value: Boolean) {
        if (enabled == value) return
        enabled = value
        // Restarting the pointer job is a configuration change, not a finger cancel (`PointerInputResetException`).
        pointer.resetPointerInputHandler()
    }

    fun underFinger(at: Offset, open: Boolean, dx: Float): FingerTarget {
        if (!isAttached) return FingerTarget(UnderFinger.None, button = false)
        val rootAt = runtime.originInRoot + at
        val semantics = (runtime.host as? RootForTest)?.semanticsOwner?.unmergedRootSemanticsNode
        val front = if (semantics != null) frontmost(semantics, rootAt) else null
        var kind = classifyChain(front, open, dx, runtime.rtl)
        val host = runtime.host
        val view = if (host != null) androidViewAt(host, rootAt) else null
        val covered = host != null && view != null && composeCovers(front, rootAt)
        // Room toward the start, and only while closed (`AssistantSidebar.swift:526` and `:558`).
        if (view != null && host != null && !covered && kind != UnderFinger.Child && kind != UnderFinger.SwipeRow &&
            !open && dx > 0f && roomTowardStart(view, host)
        ) {
            kind = UnderFinger.Sideways
        }
        return FingerTarget(kind, button = front != null && isButton(front))
    }
}

/**
 * Frontmost placed node whose bounds contain [at]. Children come from the semantics tree in z-order (last is on
 * top). A reuse-pool or prefetched node that is not placed is skipped, so its stale coordinates cannot win.
 */
private fun frontmost(node: SemanticsNode, at: Offset): SemanticsNode? {
    val info = node.layoutInfo
    if (!info.isAttached || !info.isPlaced || info.isDeactivated) return null
    if (!node.boundsInRoot.contains(at)) return null
    val kids = node.children
    for (index in kids.lastIndex downTo 0) {
        val found = frontmost(kids[index], at)
        if (found != null) return found
    }
    return node
}

/**
 * Walk from the hit up, as `sidewaysScroller` walks superviews (`AssistantSidebar.swift:551-563`). A slider or a
 * text selection with its handles showing stops the walk. A swipe row stops it too, before the list that contains
 * the row (`SwipeRowMark`, line 500; the list waits, lines 540-544). A horizontal scroller with room toward the
 * start stops it as well (line 558). One with no room, and a vertical list, wait and the walk continues, so an
 * outer scroller that still has room can win (lines 556-562).
 *
 * [horizontalScroll] stores the caller's `reverseScrolling` on the axis and does not fold in layout direction.
 * Value 0 is the left edge. A rightward drag in RTL increases that value, so the start, where the drag has no
 * room, is the high end. The flag and RTL each flip which end that is.
 */
private fun classifyChain(front: SemanticsNode?, open: Boolean, dx: Float, rtl: Boolean): UnderFinger {
    var node = front
    var waiting = false
    while (node != null) {
        val config = node.config
        if (config.contains(SemanticsActions.SetProgress)) return UnderFinger.Child
        if (config.contains(SemanticsProperties.TextSelectionRange)) {
            val range = config[SemanticsProperties.TextSelectionRange]
            if (!range.collapsed) return UnderFinger.Child
        }
        if (config.contains(SwipeRowMark)) return UnderFinger.SwipeRow
        val horizontal = SemanticsProperties.HorizontalScrollAxisRange
        if (config.contains(horizontal)) {
            val axis = config[horizontal]
            val value = axis.value()
            val atStart = atScrollStart(axis.reverseScrolling, value, axis.maxValue(), rtl)
            if (!open && dx > 0f && !atStart) return UnderFinger.Sideways
            waiting = true
        } else if (config.contains(SemanticsProperties.VerticalScrollAxisRange)) {
            waiting = true
        }
        node = node.parent
    }
    return if (waiting) UnderFinger.Waiting else UnderFinger.None
}

/** The end a rightward drag cannot scroll past. RTL and `reverseScrolling` each move that end. */
private fun atScrollStart(reverseScrolling: Boolean, value: Float, max: Float, rtl: Boolean): Boolean {
    val highEnd = reverseScrolling xor rtl
    return if (highEnd) value >= max - 1f else value <= 1f
}

/**
 * The open scrim is a button. It is in front of the page, so a view under it is not what the finger is on.
 * Semantics does not carry opacity, so a plain box above an Android view does not count as covering it.
 */
private fun isButton(node: SemanticsNode): Boolean {
    val config = node.config
    if (config.contains(SemanticsActions.OnClick)) return true
    return config.contains(SemanticsProperties.Role) && config[SemanticsProperties.Role] == Role.Button
}

private fun composeCovers(front: SemanticsNode?, at: Offset): Boolean =
    front != null && isButton(front) && front.boundsInRoot.contains(at)

/** Deepest Android child of [host] whose window bounds contain the semantics-root point. The host itself is not one. */
private fun androidViewAt(host: View, rootAt: Offset): View? {
    val window = IntArray(2)
    host.getLocationInWindow(window)
    val hit = deepestView(host, window[0] + rootAt.x, window[1] + rootAt.y)
    return if (hit == null || hit === host) null else hit
}

private fun deepestView(view: View, windowX: Float, windowY: Float): View? {
    if (view.visibility != View.VISIBLE || !view.isAttachedToWindow) return null
    val loc = IntArray(2)
    view.getLocationInWindow(loc)
    val localX = windowX - loc[0]
    val localY = windowY - loc[1]
    if (localX < 0f || localY < 0f || localX >= view.width || localY >= view.height) return null
    if (view is ViewGroup) {
        for (index in view.childCount - 1 downTo 0) {
            val found = deepestView(view.getChildAt(index), windowX, windowY)
            if (found != null) return found
        }
    }
    return view
}

/** `canScrollHorizontally(-1)` is room back toward the start (`AssistantSidebar.swift:558`, content offset past the inset). */
private fun roomTowardStart(view: View, host: View): Boolean {
    var current: View? = view
    while (current != null && current !== host) {
        if (current.canScrollHorizontally(-1)) return true
        current = current.parent as? View
    }
    return false
}

private data class DrawerDragElement(val runtime: DrawerRuntime, val enabled: Boolean) : ModifierNodeElement<DrawerDragNode>() {
    override fun create() = DrawerDragNode(runtime, enabled)
    override fun update(node: DrawerDragNode) { node.setEnabled(enabled) }
    override fun InspectorInfo.inspectableProperties() { name = "phoneDrawerDrag" }
}

private suspend fun PointerInputScope.trackDrawer(runtime: DrawerRuntime, hit: DrawerDragNode) {
    val slop = viewConfiguration.touchSlop
    awaitEachGesture {
        // The previous gesture's external close must not block the next finger.
        runtime.superseded = false
        val down = awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
        // One finger (`DrawerPan` maximumNumberOfTouches, `AssistantSidebar.swift:642`).
        if (DrawerDrag.inSystemBackInset(
                x = down.position.x,
                y = down.position.y,
                leadingPx = runtime.leadingPx,
                trailingPx = runtime.trailingPx,
                widthPx = size.width.toFloat(),
                heightPx = size.height.toFloat(),
                bandHeightPx = runtime.bandHeightPx,
            )
        ) {
            return@awaitEachGesture
        }
        runtime.fingerDown = true
        val pointer = down.id
        val origin = down.position
        val tracker = VelocityTracker()
        // `addPointerInputChange` keeps the move samples and skips the finger-up, matching the tracker's
        // own contract. Adding the up (same place, later time) makes the flick look like it stopped.
        tracker.addPointerInputChange(down)
        var took = false
        var settled = false
        var tornDown = false
        var dragStart = 0f
        try {
            while (true) {
                val event = awaitPointerEvent(PointerEventPass.Initial)
                if (runtime.superseded) {
                    // Back (or another writer) already chose. Do not let the finger's release overwrite it.
                    settled = true
                    break
                }
                val change = event.changes.firstOrNull { it.id == pointer }
                if (change == null || !change.pressed) {
                    if (took) {
                        if (pointerCancelled(event, change)) {
                            // A synthetic up after cancel must not settle open with the finger's speed
                            // (`AssistantSidebar.swift:676-680` calls `close()`, velocity 0).
                        } else {
                            settled = true
                            releaseDrawer(runtime, tracker, dragStart, density)
                            change?.consume()
                        }
                    }
                    break
                }
                val dx = change.position.x - origin.x
                val dy = change.position.y - origin.y
                tracker.addPointerInputChange(change)
                if (!took) {
                    if (hypot(dx, dy) < slop) continue
                    // A long-press has already started a selection. Crossing the slop after that must not open the drawer.
                    if (change.uptimeMillis - down.uptimeMillis >= viewConfiguration.longPressTimeoutMillis) break
                    val open = runtime.shown > 0f
                    val horizontal = abs(dx) > abs(dy)
                    // `sidewaysScroller` (`AssistantSidebar.swift:551-563`). A vertical list consumes a horizontal
                    // move without owning it; iOS makes that list wait (`shouldBeRequiredToFailBy`, lines 540-544),
                    // so claim the move before its Main pass. A marked swipe row does not wait (line 500): the row
                    // gets the leftward drag. Consumption counts only for a node that is not that list and not a
                    // button: a slider, a selection, a chart. A tap does not consume the move.
                    val target = hit.underFinger(change.position, open, dx)
                    val under = target.under
                    val sidewaysRoom = under == UnderFinger.Sideways
                    val markedRow = under == UnderFinger.SwipeRow
                    val claimScroller = !markedRow && under == UnderFinger.Waiting && horizontal && (open || dx > 0f)
                    if (claimScroller) change.consume()
                    val seen = awaitPointerEvent(PointerEventPass.Final).changes.firstOrNull { it.id == pointer }
                    if (runtime.superseded) {
                        settled = true
                        break
                    }
                    val childConsumed = seen?.isConsumed == true && !claimScroller
                    val childKeeps = horizontal && (
                        under == UnderFinger.Child ||
                            (!markedRow && childConsumed && !target.button &&
                                under != UnderFinger.Sideways && under != UnderFinger.Waiting)
                        )
                    val onSwipeRow = horizontal && open && dx < 0f && (markedRow || childKeeps)
                    val childOwns = childKeeps && !onSwipeRow
                    val choice = DrawerDrag.decision(
                        dx, dy, isOpen = open, atRoot = true,
                        onSwipeRow = onSwipeRow, sidewaysRoom = sidewaysRoom, inSystemBack = false,
                        childOwns = childOwns,
                    )
                    if (choice != DrawerDrag.Decision.Take) break
                    took = true
                    // Take over wherever the page is, including mid-spring (`dragBegan`, `:92-93`).
                    dragStart = runtime.shown
                    runtime.live = true
                    runtime.dragProgress = DrawerDrag.tracked(dragStart, dx, runtime.widthPx)
                    runtime.dragging = true
                    runtime.scope.launch { runtime.anim.stop() }
                    if (dragStart == 0f) runtime.clearText()
                    change.consume()
                } else {
                    change.consume()
                    runtime.dragProgress = DrawerDrag.tracked(dragStart, dx, runtime.widthPx)
                }
            }
        } catch (cancelled: CancellationException) {
            // The pointer job was reset (unfold, a pushed page, another tab). Not a finger cancel.
            tornDown = true
            throw cancelled
        } finally {
            runtime.fingerDown = false
            if (took && !settled && !runtime.superseded) {
                if (tornDown) {
                    // The pointer job was reset because the gesture left (unfold, a pushed page, another tab).
                    // That is not a finger cancel: do not call onDismiss, or the open drawer never becomes the column.
                    parkDrawer(runtime, runtime.opened, notify = false)
                } else {
                    // Cancelled or failed: close, with no leftover speed (`AssistantSidebar.swift:676-680`).
                    parkDrawer(runtime, open = false, notify = true)
                }
            }
        }
    }
}

/**
 * A finger-up from the platform carries [MotionEvent.ACTION_UP]. A cancel is delivered as a synthetic up with no
 * motion event (`onCancelPointerInput`), same time and place, so it must not be treated as a release.
 */
private fun pointerCancelled(event: PointerEvent, change: PointerInputChange?): Boolean {
    if (change == null) return true
    if (change.pressed) return false
    val motion = event.motionEvent ?: return change.previousPressed
    return motion.actionMasked == MotionEvent.ACTION_CANCEL
}

/**
 * `letGo` (`AssistantSidebar.swift:107-115`). [density] is px per dp, so the flick threshold stays 300 dp/s.
 * The share uses dp for both the velocity and the width. A drag that started closed and settles open plays
 * `Haptics.tap` (line 110); the button's open does not.
 */
private fun releaseDrawer(runtime: DrawerRuntime, tracker: VelocityTracker, dragStart: Float, density: Float) {
    val pxPerDp = max(density, DrawerDrag.minimumWay)
    val widthDp = max(runtime.widthPx, DrawerDrag.minimumWay) / pxPerDp
    val velocityDp = tracker.calculateVelocity().x / pxPerDp
    val progress = runtime.dragProgress
    val opens = DrawerDrag.settlesOpen(progress, velocityDp, widthDp)
    val target = if (opens) 1f else 0f
    val velocity = DrawerDrag.progressPerSecond(progress, target, velocityDp, widthDp)
    if (dragStart == 0f && opens) runtime.haptics(Haptic.Tap)
    runtime.scope.launch {
        if (runtime.superseded) return@launch
        runtime.anim.stop()
        if (runtime.superseded) return@launch
        runtime.anim.snapTo(progress)
        if (runtime.superseded) return@launch
        runtime.dragging = false
        runtime.settle = DrawerSettle(opens, velocity, runtime.settle.token + 1, finger = true)
        if (runtime.superseded) return@launch
        if (opens && !runtime.opened) runtime.onOpen()
        else if (!opens && runtime.opened) runtime.onDismiss()
    }
}

/**
 * Leave the finger, with no speed (`drawer.close()`, `AssistantSidebar.swift:89` and `:676-680`).
 * [notify] tells the shell when this close is the finger's cancel. A torn-down gesture does not: the flag already
 * says whether the drawer is open, and dismissing it races the unfold that turns it into the sidebar column.
 */
private fun parkDrawer(runtime: DrawerRuntime, open: Boolean, notify: Boolean) {
    val progress = runtime.dragProgress
    val wasOpen = runtime.opened
    runtime.scope.launch {
        if (runtime.superseded) return@launch
        runtime.anim.stop()
        if (runtime.superseded) return@launch
        runtime.anim.snapTo(progress)
        if (runtime.superseded) return@launch
        runtime.dragging = false
        // A finger cancel still uses the finger's spring. Unfolding clears [DrawerRuntime.dragEnabled] first and snaps.
        runtime.settle = DrawerSettle(open, 0f, runtime.settle.token + 1, finger = runtime.dragEnabled)
        if (notify && wasOpen && !open) runtime.onDismiss()
    }
}

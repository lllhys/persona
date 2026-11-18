package persona.feature.assistant

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.LazyGridLayoutInfo
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.SubcomposeLayout
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.IntSize
import persona.core.designsystem.Brand
import persona.core.designsystem.JBrandMark
import persona.core.designsystem.JIcon
import persona.core.designsystem.PageBackdrop
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PersonaIcons

/** The most lines the blank page's Needs you shows, room permitting (iOS `ShellNeedsYou`). */
internal const val ShellNeedsYouMost = 3

/**
 * How many of [total] lines show (iOS `ShellNeedsYou`'s `ViewThatFits`): three at most, fewer when [room] has no room
 * for them, one at least. [height] is how tall that many lines are with the line for the rest under them.
 */
internal fun needsYouShown(total: Int, room: Int, height: (Int) -> Int): Int {
    if (total <= 0) return 0
    val most = minOf(ShellNeedsYouMost, total)
    return (most downTo 1).firstOrNull { height(it) <= room } ?: 1
}

/**
 * The line for what does not show (iOS `ShellNeedsYou.more`): how many are left, and how many of them are agents. With an
 * agent among them it says they need you; only agents left, it opens Agents, else the Inbox (which holds only its own).
 */
internal data class NeedsYouMore(val count: Int, val agents: Int) {
    val needYou: Boolean get() = agents > 0
    val opensAgents: Boolean get() = agents == count
}

/** What is left after the first [shown] of [lines]; null when they all show. */
internal fun needsYouMore(lines: List<NeedsYouLine>, shown: Int): NeedsYouMore? {
    val hidden = lines.drop(shown)
    return if (hidden.isEmpty()) null else NeedsYouMore(hidden.size, hidden.count { it is NeedsYouLine.Agent })
}

/**
 * How tall Needs you may be on the blank page (iOS `NeedsRoom`, #283 there): what is left between the end of the first
 * two rows of things to ask and the composer, with the keyboard down, less what else sits over the composer. In the
 * root's pixels; no limit until both ends are measured.
 */
@Stable
internal class NeedsRoom {
    /** Where the first two rows of things to ask end, read with the page at its top (`NewChatPage`). */
    var fold by mutableIntStateOf(0)
    /** The readiness card over the rows, while it shows. */
    var above by mutableIntStateOf(0)
    /** The draft's error between the rows and the composer, while it shows. */
    var below by mutableIntStateOf(0)
    private var floor by mutableIntStateOf(0)
    private var page = IntSize.Zero

    /**
     * The page's size, which the keyboard does not change (the page pads itself for it): another size (turning the phone,
     * unfolding it, another window size) measures the composer again. Told while measuring, before [composerAt].
     */
    fun pageSize(size: IntSize) {
        if (size != page) floor = 0
        page = size
    }

    /** The composer's top: the lowest it has been on this page. The keyboard and a longer draft only lift it, and must not take rows away. */
    fun composerAt(top: Int) {
        floor = maxOf(floor, top)
    }

    /** How tall the rows may be, [fade] left clear over them where the things to ask fade out. */
    fun height(fade: Int): Int = if (fold <= 0 || floor <= 0) Int.MAX_VALUE else floor - below - above - fold - fade
}

/**
 * The grid items whose ends make the fold (iOS `WelcomeFold`, `cardRows: 2`): the second row of things to ask, two a
 * row, or the last of them when there are fewer; with none, the lead before them. [first] is the first one's index.
 */
internal fun welcomeFold(first: Int, count: Int): IntRange {
    if (count <= 0) return (first - 1)..(first - 1)
    val last = minOf(count, WelcomeRows * 2) - 1
    return (first + last / 2 * 2)..(first + last)
}

/**
 * Where the [fold] items end, from the grid's top; the grid's end when they are not laid out (they are below a short
 * window), null before anything is.
 */
internal fun foldBottom(info: LazyGridLayoutInfo, fold: IntRange): Int? {
    if (info.visibleItemsInfo.isEmpty()) return null
    val items = info.visibleItemsInfo.filter { it.index in fold }
    // Item offsets start after the top content padding (the grid's viewport starts at minus that padding).
    return if (items.isEmpty()) info.viewportSize.height else info.beforeContentPadding + items.maxOf { it.offset.y + it.size.height }
}

/** How many rows of things to ask stay clear of Needs you, as on iOS. */
private const val WelcomeRows = 2

/**
 * What needs the user, over the blank page's composer on Glass (#4339 C17, iOS `ShellNeedsYou`'s plain rows): one quiet
 * line each, the agents waiting on them (a session each, as the Agents tab's badge counts) then the inbox's "Needs you",
 * as many as [room] has space for (three at most, one at least), then a line for the rest. A tap opens a line where it
 * lives; the rest's line opens Agents when only agents are left, else the Inbox.
 */
@Composable
internal fun ShellNeedsYou(
    lines: List<NeedsYouLine>, room: () -> Int, onOpen: (NeedsYouLine) -> Unit, onMore: (NeedsYouMore) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (lines.isEmpty()) return
    SubcomposeLayout(modifier.testTag("shell-needs-you")) { constraints ->
        val loose = constraints.copy(minHeight = 0, maxHeight = Constraints.Infinity)
        // Every line is one line of text at the same height: the first one measures them all.
        val first = subcompose("line-0") { NeedsYouRow(lines[0], onOpen) }.map { it.measure(loose) }
        val line = first.sumOf { it.height }
        val shown = needsYouShown(lines.size, room()) { n -> line * (n + if (n < lines.size) 1 else 0) }
        val rest = (1 until shown).flatMap { i -> subcompose("line-$i") { NeedsYouRow(lines[i], onOpen) }.map { it.measure(loose) } }
        val more = needsYouMore(lines, shown)?.let { left -> subcompose("more") { MoreRow(left, onMore) }.map { it.measure(loose) } }.orEmpty()
        val placed = first + rest + more
        layout(constraints.maxWidth, placed.sumOf { it.height }) {
            var y = 0
            placed.forEach { it.place(0, y); y += it.height }
        }
    }
}

/**
 * Needs you while the composer's @ list is [open] (iOS `MentionMenuOpen` and `ShellHome`'s
 * `withAnimation(Theme.Motion.standard)`): above the keyboard the lines, the list and the composer did not fit together, so
 * the lines fade and fold away as the list comes in, and come back as it goes. The motion is iOS's state spring
 * (`Tokens.Motion.State` through [PersonaTheme.motion]); with animations off they are there or not at once.
 */
@Composable
internal fun NeedsStepAside(open: Boolean, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    val still = PersonaTheme.reduceMotion
    val motion = PersonaTheme.motion
    AnimatedVisibility(!open, modifier,
        enter = if (still) EnterTransition.None else fadeIn(motion.enter()) + expandVertically(motion.enter(), Alignment.Bottom),
        exit = if (still) ExitTransition.None else fadeOut(motion.enter()) + shrinkVertically(motion.enter(), Alignment.Bottom)) {
        content()
    }
}

/**
 * Under Needs you (and the readiness card over it), the page's own backdrop, so what scrolls under them does not read
 * through; over [fade] above them it fades in, so the things to ask fade out there rather than being cut at an edge
 * (iOS `ShellHome`'s gradient over its bottom stack). Fills its parent and draws [fade] above it.
 */
@Composable
internal fun NeedsBackdrop(fade: Dp, modifier: Modifier = Modifier) {
    // Only its alpha counts: the backdrop is kept where the mask is opaque.
    val mask = PersonaTheme.palette.ink.copy(alpha = 1f)
    Box(modifier.testTag("needs-backdrop").layout { measurable, constraints ->
        val above = fade.roundToPx()
        val placeable = measurable.measure(Constraints.fixed(constraints.maxWidth, constraints.maxHeight + above))
        layout(constraints.maxWidth, constraints.maxHeight) { placeable.place(0, -above) }
    }) {
        Box(Modifier.fillMaxSize().graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }.drawWithContent {
            drawContent()
            val solid = (fade.toPx() / size.height).coerceIn(0f, 1f)
            drawRect(Brush.verticalGradient(0f to mask.copy(alpha = 0f), solid to mask), blendMode = BlendMode.DstIn)
        }) { PageBackdrop() }
    }
}

@Composable
private fun NeedsYouRow(line: NeedsYouLine, onOpen: (NeedsYouLine) -> Unit) {
    val palette = PersonaTheme.palette
    LineFrame(onClick = { onOpen(line) }, tag = if (line is NeedsYouLine.Agent) "shell-needs-agent" else "shell-needs-inbox", mark = {
        if (line is NeedsYouLine.Inbox && line.lark && line.itemKind != "doc" && line.itemKind != "moment") {
            JBrandMark(Brand.Lark, Tokens.Size.iconS, contentDescription = null)
        } else {
            JIcon(needsYouIcon(line), null, tint = if (line is NeedsYouLine.Agent) palette.needsYou.dot else palette.ink2)
        }
    }, text = whoAndWhat(needsYouWho(line), needsYouWhat(line), palette.ink.copy(alpha = NameAlpha), palette.ink2))
}

@Composable
private fun MoreRow(more: NeedsYouMore, onMore: (NeedsYouMore) -> Unit) {
    val palette = PersonaTheme.palette
    val text = if (more.needYou) pluralStringResource(R.plurals.assistant_needs_you_more_agents, more.count, more.count)
        else pluralStringResource(R.plurals.assistant_needs_you_more, more.count, more.count)
    LineFrame(onClick = { onMore(more) }, tag = "shell-needs-more", mark = { JIcon(PersonaIcons.inbox, null, tint = palette.ink2) },
        text = AnnotatedString(text), color = palette.ink2)
}

/** A line: its mark in an icon's width, then one line of body text, at least a row's height (iOS `LineFrame`). */
@Composable
private fun LineFrame(onClick: () -> Unit, tag: String, mark: @Composable () -> Unit, text: AnnotatedString, color: Color = Color.Unspecified) {
    Row(Modifier.fillMaxWidth().heightIn(min = LineHeight).clickable(role = Role.Button, onClick = onClick).testTag(tag)
        .padding(horizontal = Tokens.Space.l),
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.width(Tokens.Size.icon), contentAlignment = Alignment.Center) { mark() }
        Text(text, Modifier.weight(1f), style = PersonaTheme.type.body, color = color, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** ChatGPT's rows, as iOS's: one line of gray text, the name a step darker to find it at a glance. */
private fun whoAndWhat(who: String, what: String, name: Color, rest: Color): AnnotatedString = buildAnnotatedString {
    withStyle(SpanStyle(color = name)) { append(who) }
    if (what.isNotBlank()) withStyle(SpanStyle(color = rest)) { append("  "); append(what) }
}

/** What a line wants: a session waiting only in its terminal says so, in the words of its Agents row. */
@Composable
internal fun needsYouWhat(line: NeedsYouLine): String =
    if (line is NeedsYouLine.Agent && line.terminal) stringResource(persona.core.designsystem.R.string.designsystem_decision_screen)
    else line.what

/** How far the things to ask fade out over Needs you, kept clear of the lines (iOS `ShellHome.fade`). */
internal val NeedsFade: Dp = Tokens.Space.xl

/** iOS draws the name in the primary color at this opacity, the rest in the secondary gray. */
private const val NameAlpha = 0.75f

/** iOS `Theme.Size.box56`: a touch target with a little room around one line of text. */
private val LineHeight = Tokens.MinTouch.android + Tokens.Space.s

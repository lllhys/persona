package persona.core.designsystem

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import persona.core.designsystem.icons.PersonaIcons
import persona.core.designsystem.icons.PhIcon

// The space page as iOS draws it (`SpaceView`, #4338 A11): where the space is, then its agents as cards two to a row,
// the most urgent first, and a dashed card that opens another tab there.

/** How the space page lays out its agents: iOS's two columns of cards in Glass; Brief and Board keep their rows (decision 8 of #4194). */
object AgentSpaceLayout {
    val cards: Boolean @Composable get() = PersonaTheme.style == AppStyle.Glass
}

/**
 * The space page's first line (iOS `SpaceView`): the computer as an accent capsule with its device [icon], its name and,
 * when [onMachine] opens its projects (iOS `MachineSpacesView`, read as "All projects on <computer>"), a chevron; then the space's folder, already shortened with ~, cut at its start so
 * its end shows.
 */
@Composable
fun AgentSpaceLocation(
    machine: String,
    path: String,
    modifier: Modifier = Modifier,
    icon: PhIcon = PersonaIcons.machine,
    onMachine: (() -> Unit)? = null,
) {
    val palette = PersonaTheme.palette
    val accent = palette.accent
    val words = PersonaTheme.type.caption
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        Row(
            Modifier.then(if (onMachine != null) Modifier.minimumInteractiveComponentSize() else Modifier)
                .clip(CircleShape).background(accent.copy(alpha = SpaceCardTokens.machineFillAlpha))
                .then(if (onMachine != null) Modifier.clickable(onClickLabel = stringResource(R.string.designsystem_machine_projects, machine), role = Role.Button, onClick = onMachine) else Modifier)
                .padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.sm),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.sm),
        ) {
            JIcon(icon, null, size = Tokens.Space.l, tint = accent, decorative = true)
            Text(machine, Modifier.weight(1f, fill = false), style = words.copy(fontWeight = FontWeight.SemiBold), color = accent,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (onMachine != null) JIcon(PersonaIcons.disclosure, null, size = Tokens.Space.m, tint = accent, decorative = true)
        }
        Text(path, Modifier.weight(1f, fill = false), style = words, color = palette.ink2, maxLines = 1, overflow = TextOverflow.StartEllipsis)
    }
}

/**
 * One row of the space page's grid (iOS's `LazyVGrid` of two flexible columns, [Tokens.Space.m] apart): both cells are
 * as tall as the taller one; a row with one card leaves the second column empty.
 */
@Composable
fun AgentCardRow(modifier: Modifier = Modifier, end: @Composable () -> Unit = {}, start: @Composable () -> Unit) {
    Row(modifier.fillMaxWidth().height(IntrinsicSize.Max), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        Box(Modifier.weight(1f).fillMaxHeight()) { start() }
        Box(Modifier.weight(1f).fillMaxHeight()) { end() }
    }
}

/**
 * An agent's card on the space page (iOS `SlotCard`): its tab's chip in its state's colors (the agent's name after the
 * tab's when it shares the tab with other agents, [pane]), else an outlined [agent] chip, and its state's dot; the
 * [title] in two lines; its [state] in words, "Started it" for the tab's [starter] and how many requests are [pending];
 * up to three lines of its latest words ([preview]); the [model] at the foot. A needs-you card has a ring in the
 * needs-you color. The agent chip goes first when the column is narrow, then the tab's name is cut.
 *
 * [status] is the agent's own state (dot, chip, ring); [stateTone] colors the words (Awaiting reply takes its own);
 * [waiting]: it only waits in the background, so its clock replaces the dot and the clock, the chip and the words take
 * that state's lighter tone ([PersonaPalette.backgroundWaiting], iOS `stateTone`).
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun AgentSlotCard(
    title: String,
    status: Status,
    state: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    stateTone: Status = status,
    tab: String? = null,
    pane: String? = null,
    agent: String? = null,
    starter: Boolean = false,
    pending: Int = 0,
    preview: String? = null,
    model: String? = null,
    waiting: Boolean = false,
    onLongClick: (() -> Unit)? = null,
) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val shape = PersonaTheme.shapes.cardShape
    val chip = palette.status(status, waiting)
    val words = palette.status(stateTone, waiting)
    var line = state
    if (starter) line = stringResource(R.string.designsystem_space_card_starter, line)
    if (pending > 0) line = pluralStringResource(R.plurals.designsystem_space_card_pending, pending, line, pending)
    Column(
        modifier.fillMaxWidth().clip(shape).background(palette.surfaceOpaque)
            .border(Tokens.Size.hairline, palette.line, shape)
            .then(if (status == Status.NeedsYou) Modifier.border(Tokens.Size.lineMedium, palette.needsYou.dot.copy(alpha = SpaceCardTokens.needsYouRingAlpha), shape) else Modifier)
            .combinedClickable(role = Role.Button, onClick = onClick, onLongClick = onLongClick)
            .padding(Tokens.Space.m).heightIn(min = Tokens.Size.box150),
        verticalArrangement = Arrangement.spacedBy(Tokens.Space.s),
    ) {
        CardHeader(
            tab = { CardChip(listOfNotNull(tab, pane).joinToString(" · ").ifEmpty { "—" }, chip.text, chip.tint) },
            agent = agent?.takeIf { pane == null }?.let { name -> { CardChip(name, palette.ink2, outline = palette.ink2.copy(alpha = SpaceCardTokens.outlineAlpha)) } },
            dot = {
                if (waiting) JIcon(PersonaIcons.backgroundWaiting, null, size = Tokens.Size.dotL * SpaceCardTokens.clockScale, tint = palette.backgroundWaiting.dot, decorative = true)
                else JStatusDot(status, size = Tokens.Size.dotL)
            },
        )
        Text(title, style = type.callout.copy(fontWeight = FontWeight.SemiBold), color = palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
        Text(line, style = type.micro.copy(fontWeight = FontWeight.SemiBold), color = words.text)
        preview?.takeIf(String::isNotBlank)?.let { Text(it, style = type.micro, color = palette.ink2, maxLines = 3, overflow = TextOverflow.Ellipsis) }
        Spacer(Modifier.weight(1f))
        model?.takeIf(String::isNotBlank)?.let { Text(it, style = type.micro, color = palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis) }
    }
}

/**
 * The last card of the space page (iOS `SpaceView`): a plus and [label] in the accent on a dashed card as tall as the
 * card beside it, which opens a new tab in the space.
 */
@Composable
fun AgentNewTabCard(label: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
    val accent = PersonaTheme.palette.accent
    val radius = PersonaTheme.shapes.card
    Column(
        modifier.fillMaxWidth().clip(RoundedCornerShape(radius)).background(accent.copy(alpha = SpaceCardTokens.newTabFillAlpha))
            .drawBehind {
                val width = Tokens.Size.line.toPx()
                val dash = Tokens.Space.xs.toPx()
                drawRoundRect(accent.copy(alpha = SpaceCardTokens.newTabStrokeAlpha), Offset(width / 2, width / 2), Size(size.width - width, size.height - width),
                    CornerRadius(radius.toPx() - width / 2), style = Stroke(width, pathEffect = PathEffect.dashPathEffect(floatArrayOf(dash, dash))))
            }
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .stableAlpha(if (enabled) 1f else SpaceCardTokens.disabledAlpha)
            .padding(Tokens.Space.m).heightIn(min = Tokens.Size.box150),
        verticalArrangement = Arrangement.spacedBy(Tokens.Space.s, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        JIcon(PersonaIcons.add, null, size = Tokens.Size.iconS, tint = accent, decorative = true)
        Text(label, style = PersonaTheme.type.caption.copy(fontWeight = FontWeight.Medium), color = accent, textAlign = TextAlign.Center)
    }
}

/** iOS `Chip`: a capsule of the label's size, in a state's colors or outlined. */
@Composable
private fun CardChip(text: String, ink: Color, fill: Color? = null, outline: Color? = null) {
    val shape = CircleShape
    Text(text, Modifier.clip(shape).then(if (fill != null) Modifier.background(fill) else Modifier)
        .then(if (outline != null) Modifier.border(Tokens.Size.line, outline, shape) else Modifier)
        .padding(horizontal = Tokens.Space.s, vertical = Tokens.Space.xxs),
        style = PersonaTheme.type.micro.copy(fontWeight = FontWeight.Medium), color = ink, maxLines = 1, softWrap = false, overflow = TextOverflow.Ellipsis)
}

/**
 * The card's first line, iOS's `ViewThatFits`: the tab's chip, the agent's and the dot at the end; where that is too
 * wide the agent's chip goes, and where the tab's alone is too wide it is cut.
 */
@Composable
private fun CardHeader(tab: @Composable () -> Unit, agent: (@Composable () -> Unit)?, dot: @Composable () -> Unit) {
    Layout(content = { tab(); agent?.invoke(); dot() }) { measurables, constraints ->
        val gap = Tokens.Space.s.roundToPx()
        val before = Tokens.Space.xs.roundToPx()
        val dotPlaceable = measurables.last().measure(Constraints())
        val agentMeasurable = if (agent != null) measurables[1] else null
        val tabWidth = measurables[0].maxIntrinsicWidth(Constraints.Infinity)
        val agentWidth = agentMeasurable?.maxIntrinsicWidth(Constraints.Infinity) ?: 0
        val natural = tabWidth + (if (agentMeasurable != null) gap + agentWidth else 0) + before + dotPlaceable.width
        val width = if (constraints.hasBoundedWidth) constraints.maxWidth else natural
        val showAgent = agentMeasurable != null && natural <= width
        val agentPlaceable = if (showAgent) agentMeasurable?.measure(Constraints()) else null
        val room = width - dotPlaceable.width - before - (agentPlaceable?.let { it.width + gap } ?: 0)
        val tabPlaceable = measurables[0].measure(Constraints(maxWidth = room.coerceAtLeast(0)))
        val height = maxOf(tabPlaceable.height, agentPlaceable?.height ?: 0, dotPlaceable.height)
        layout(width, height) {
            tabPlaceable.place(0, (height - tabPlaceable.height) / 2)
            agentPlaceable?.place(tabPlaceable.width + gap, (height - agentPlaceable.height) / 2)
            dotPlaceable.place(width - dotPlaceable.width, (height - dotPlaceable.height) / 2)
        }
    }
}

/** The space page's alphas, as iOS `SpaceView` and `SlotCard` write them. */
private object SpaceCardTokens {
    const val machineFillAlpha = 0.1f
    const val needsYouRingAlpha = 0.5f
    const val outlineAlpha = 0.4f
    const val newTabFillAlpha = 0.06f
    const val newTabStrokeAlpha = 0.3f
    const val disabledAlpha = 0.4f
    /** The background-waiting clock is as wide as the working ring around a dot. */
    const val clockScale = 1.7f
}

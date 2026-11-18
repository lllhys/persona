package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.layout.layoutId
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import persona.core.designsystem.icons.PersonaIcons
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons

// A computer's projects page (iOS `MachineSpacesView`, #4338 A11; docs/android/android-design.md 2.4): the computer's summary at its
// top, a line for each project with something going on and one for each idle project. The page sets the lines in `JGroupItem`
// cards and `JRow`s; these are what goes inside them.

/** The computer's tile as iOS tints it: its color at this opacity behind the icon. */
private const val TileAlpha = 0.12f
/** A muted project's row, as iOS's `opacity(0.8)`, and its counts at `opacity(0.6)`. */
private const val MutedRowAlpha = 0.8f
private const val MutedCountsAlpha = 0.6f

/**
 * The computer at the top of its projects page (iOS `MachineSpacesView.summary`): a tile with its [icon] (the accent while it
 * is [online], else quiet), whether it is online, its [host] name in code type, [detail] under them ("3 projects · 9
 * agents") and the state [counts] at the end; [waiting] draws the agents that only wait in the background with a clock,
 * apart from the rest ([StateCounts]). Where the counts would leave the words too little room they go under them, as iOS
 * moves them at its largest text sizes.
 */
@Composable
fun AgentMachineSummary(
    icon: PhIcon,
    online: Boolean,
    host: String?,
    detail: String,
    counts: List<Pair<Status, Int>>,
    modifier: Modifier = Modifier,
    waiting: Int = 0,
) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val tint = if (online) palette.accent else palette.ink2
    val words = stringResource(if (online) R.string.designsystem_online else R.string.designsystem_offline)
    Layout(
        content = {
            Box(
                Modifier.layoutId(SummaryTile).size(Tokens.MinTouch.android).clip(RoundedCornerShape(Tokens.Radius.control)).background(tint.copy(alpha = TileAlpha)),
                contentAlignment = Alignment.Center,
            ) { JIcon(icon, null, size = Tokens.Size.icon, tint = tint, decorative = true) }
            Column(Modifier.layoutId(SummaryWords), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.sm)) {
                    Box(Modifier.size(Tokens.Size.dotS).clip(CircleShape).background(if (online) palette.positive else palette.offline.dot))
                    Text(words, style = type.labelSmall.copy(fontWeight = FontWeight.SemiBold), color = palette.ink, maxLines = 1)
                    host?.let { Text(it, Modifier.weight(1f, fill = false), style = type.codeSmall, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                }
                Text(detail, style = type.micro, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            // StateCounts is a run of siblings: a Row lays them out.
            Row(Modifier.layoutId(SummaryCounts), verticalAlignment = Alignment.CenterVertically) { StateCounts(counts, waiting) }
        },
        modifier = modifier.fillMaxWidth().semantics(mergeDescendants = true) {}.padding(horizontal = rowPadding, vertical = Tokens.Space.m),
    ) { measurables, constraints ->
        val byId = measurables.associateBy { it.layoutId }
        val gap = Tokens.Space.m.roundToPx()
        val tile = byId.getValue(SummaryTile).measure(Constraints())
        val countsP = byId.getValue(SummaryCounts).measure(Constraints(maxWidth = constraints.maxWidth))
        val room = constraints.maxWidth - tile.width - gap
        // Each count starts with a gap of its own; the first one's is the space before the run.
        val lead = (Tokens.Space.s + Tokens.Space.xxs).roundToPx()
        val inline = room - countsP.width >= SummaryWordsLeast.roundToPx()
        val wordsP = byId.getValue(SummaryWords).measure(Constraints(maxWidth = (if (inline) room - countsP.width else room).coerceAtLeast(0)))
        val first = maxOf(tile.height, wordsP.height, if (inline) countsP.height else 0)
        val height = if (inline) first else first + Tokens.Space.xs.roundToPx() + countsP.height
        layout(constraints.maxWidth, height) {
            tile.placeRelative(0, (first - tile.height) / 2)
            wordsP.placeRelative(tile.width + gap, (first - wordsP.height) / 2)
            if (inline) countsP.placeRelative(constraints.maxWidth - countsP.width, (first - countsP.height) / 2)
            else countsP.placeRelative(tile.width + gap - lead, first + Tokens.Space.xs.roundToPx())
        }
    }
}

private const val SummaryTile = "tile"
private const val SummaryWords = "words"
private const val SummaryCounts = "counts"
/** The least room the summary's words keep beside the counts: "3 projects · 9 agents" fits in it. */
private val SummaryWordsLeast get() = Tokens.Size.box120

/**
 * A project with something going on, the content of its row (iOS `BusySpaceRow`): its mark in the color of its most urgent
 * state ([status]; quiet while [muted]), its [name] with a mark when it is [pinned] or [muted], the state [counts] at the end
 * ([waiting] with a clock, apart); under it the [lead] agent's tab or title, what it last said ([words]) and when the project
 * last moved ([time]). A muted project is dimmed.
 */
@Composable
fun AgentProjectLine(
    name: String,
    status: Status,
    counts: List<Pair<Status, Int>>,
    modifier: Modifier = Modifier,
    waiting: Int = 0,
    pinned: Boolean = false,
    muted: Boolean = false,
    lead: String? = null,
    words: String? = null,
    time: String? = null,
) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    Column(
        modifier.fillMaxWidth().stableAlpha(if (muted) MutedRowAlpha else 1f).padding(vertical = Tokens.Space.xxs),
        verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JIcon(PersonaIcons.spaceFilled, null, size = Tokens.Size.iconXS, tint = if (muted) palette.ink2 else palette.status(status).dot, decorative = true)
                Text(name, Modifier.weight(1f, fill = false), style = type.headline, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (pinned) JIcon(PersonaIcons.pinned, stringResource(R.string.designsystem_project_pinned), size = Tokens.Space.l, tint = palette.accent)
                // Android's muted mark is the prohibit sign: the generated icons have no bell with a slash.
                if (muted) JIcon(PhIcons.Prohibit, stringResource(R.string.designsystem_status_muted), size = Tokens.Space.l, tint = palette.ink2)
            }
            // StateCounts is a run of siblings: a Row lays them out (a Box would stack them).
            Row(Modifier.stableAlpha(if (muted) MutedCountsAlpha else 1f), verticalAlignment = Alignment.CenterVertically) { StateCounts(counts, waiting) }
        }
        if (lead != null) ProjectLead(lead, words, time, Modifier.padding(start = Tokens.Size.iconXS + Tokens.Space.s))
    }
}

/**
 * An idle project, the content of its row (iOS `QuietSpaceRow`): its mark, its [name], under it [detail] ("2 agents · review
 * · research") and at the end when it last moved ([time]).
 */
@Composable
fun AgentIdleProjectLine(
    name: String,
    detail: String,
    modifier: Modifier = Modifier,
    time: String? = null,
) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        JIcon(PersonaIcons.space, null, size = Tokens.Size.iconXS, tint = palette.ink2, decorative = true)
        Column(Modifier.weight(1f).padding(vertical = Tokens.Space.xs), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
            Text(name, style = type.headline, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(detail, style = type.micro, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        time?.takeIf { it.isNotEmpty() }?.let { Text(it, style = type.micro, color = palette.ink2, maxLines = 1) }
    }
}

/**
 * A project's second line: the lead's [title] first (it keeps its width while it can), then what it [words] last said in
 * what is left, and at the end the [time], which never gives way (iOS lays it out as `layoutPriority(1)`, a secondary
 * text, a spacer and a fixed-size time).
 */
@Composable
private fun ProjectLead(title: String, words: String?, time: String?, modifier: Modifier) {
    val palette = PersonaTheme.palette
    val micro = PersonaTheme.type.micro
    Layout(
        content = {
            Text(title, Modifier.layoutId(LeadTitle), style = micro.copy(fontWeight = FontWeight.Medium), color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
            words?.takeIf(String::isNotBlank)?.let {
                Text(it.replace('\n', ' '), Modifier.layoutId(LeadWords), style = micro, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            time?.takeIf { it.isNotEmpty() }?.let { Text(it, Modifier.layoutId(LeadTime), style = micro, color = palette.ink3, maxLines = 1) }
        },
        modifier = modifier.fillMaxWidth(),
    ) { measurables, constraints ->
        val gap = Tokens.Space.sm.roundToPx()
        val byId = measurables.associateBy { it.layoutId }
        var left = constraints.maxWidth
        fun fit(id: Any): androidx.compose.ui.layout.Placeable? =
            byId[id]?.measure(Constraints(maxWidth = left.coerceAtLeast(0)))?.also { left -= it.width + gap }
        val timeP = fit(LeadTime)
        val titleP = fit(LeadTitle)
        val wordsP = fit(LeadWords)
        val height = listOfNotNull(titleP, wordsP, timeP).maxOfOrNull { it.height } ?: 0
        layout(constraints.maxWidth, height) {
            var x = 0
            titleP?.let { it.placeRelative(x, (height - it.height) / 2); x += it.width + gap }
            wordsP?.let { it.placeRelative(x, (height - it.height) / 2) }
            timeP?.let { it.placeRelative(constraints.maxWidth - it.width, (height - it.height) / 2) }
        }
    }
}

private const val LeadTitle = "title"
private const val LeadWords = "words"
private const val LeadTime = "time"

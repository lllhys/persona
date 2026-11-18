package persona.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.key
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import persona.core.contracts.api.ExtensionView
import persona.core.contracts.api.ExtensionViewItem
import persona.core.contracts.api.ExtensionViewSection
import persona.core.designsystem.*
import kotlin.math.floor

// UI version 8 (#9158, docs/extensions/extensions.md "UI version 8"): a dashboard page's columns layout, and a section's
// icon, link and rows side by side. Drawn with the design system's section header, group panel, text button and the
// adaptive card grid's rule (docs/design/DESIGN_SYSTEM.md "Adaptive card grids"), never a nested scrolling grid.

/** The layout decisions, apart from the drawing, so they are checked on the JVM. */
internal object ExtensionColumns {
    /** At most three columns of cards, and at most three rows side by side. */
    const val Most = 3

    /**
     * How many columns of cards fit in [contentWidth]: the native card grid's rule, `floor((width + gap) / (cardColumn + gap))`,
     * one at least (a phone) and [Most] at most. The width is the pane's content width, never the device's.
     */
    fun count(contentWidth: Dp, column: Dp = Tokens.Size.cardColumn, gap: Dp = Tokens.Space.m): Int {
        if (!contentWidth.value.isFinite() || contentWidth <= Tokens.Space.flush) return 1
        return floor((contentWidth.value + gap.value) / (column.value + gap.value)).toInt().coerceIn(1, Most)
    }

    /** A view drawn as a grid of cards: a columns view on a page with room for two columns or more; else a stack. */
    fun grid(view: ExtensionView?, columns: Int): Boolean = view?.layout == "columns" && columns > 1

    /**
     * The sections in rows of [columns] cards, by index, in reading order (left to right, then down, as a phone stacks
     * them). A section with span `full` has a row of its own; a short row keeps its empty places.
     */
    fun rows(sections: List<ExtensionViewSection>, columns: Int): List<List<Int>> {
        val out = mutableListOf<List<Int>>()
        var row = mutableListOf<Int>()
        sections.forEachIndexed { index, section ->
            if (section.span == "full") {
                if (row.isNotEmpty()) out += row.also { row = mutableListOf() }
                out += listOf(index)
            } else {
                row += index
                if (row.size >= columns.coerceAtLeast(1)) out += row.also { row = mutableListOf() }
            }
        }
        if (row.isNotEmpty()) out += row
        return out
    }

    /** How many of a section's rows sit side by side in [width]: as many as it asks (1 to 3) that keep `cardColumn` each. */
    fun perLine(columns: Int?, width: Dp): Int {
        val asked = (columns ?: 1).coerceIn(1, Most)
        return if (asked == 1) 1 else count(width, gap = Tokens.Space.flush).coerceAtMost(asked)
    }

    /** A run of a section's items in lines of [perLine] rows, in reading order; anything but a row takes a line of its own. */
    fun lines(items: List<ExtensionViewItem>, perLine: Int): List<List<ExtensionViewItem>> {
        val out = mutableListOf<List<ExtensionViewItem>>()
        var line = mutableListOf<ExtensionViewItem>()
        items.forEach { item ->
            if (item.type != "row") {
                if (line.isNotEmpty()) out += line.also { line = mutableListOf() }
                out += listOf(item)
            } else {
                line += item
                if (line.size >= perLine.coerceAtLeast(1)) out += line.also { line = mutableListOf() }
            }
        }
        if (line.isNotEmpty()) out += line
        return out
    }

    /** The section has a head beyond its plain title: an icon or a link. */
    fun head(section: ExtensionViewSection): Boolean = !section.icon.isNullOrBlank() || hasLink(section)

    /** A link that can be pressed: words and an action (or a page it pushes). */
    fun hasLink(section: ExtensionViewSection): Boolean =
        section.link?.let { it.title.isNotBlank() && (it.action.id.isNotEmpty() || it.action.push != null) } == true

    /** Its rows go side by side where there is room ([perLine]): a run of them is one block of lines. */
    fun sideBySide(section: ExtensionViewSection): Boolean = (section.columns ?: 1) > 1
}

/** The content width of a page whose content takes the pane's whole width ([JListPage] `fullWidth`) in a pane [maxWidth] wide. */
@Composable
internal fun extensionPageContentWidth(maxWidth: Dp): Dp {
    // pagePadding includes the reading inset of the page around this one, which a full-width page leaves out.
    val side = pagePadding - LocalReadingInset.current
    return (maxWidth - side * 2).coerceAtLeast(Tokens.Space.flush)
}

/**
 * Measures the pane a page of [view] fills and hands its list how many columns of cards it has room for, and whether
 * its content takes the pane's whole width (a columns view does; [ExtensionColumns.count] measures that width).
 * The pane is always measured, so the page's list keeps its place when the view changes layout.
 */
@Composable
internal fun ExtensionPageWidth(view: ExtensionView?, content: @Composable (columns: Int, fullWidth: Boolean) -> Unit) {
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val columnsView = view?.layout == "columns"
        val columns = if (columnsView) ExtensionColumns.count(extensionPageContentWidth(maxWidth)) else 1
        content(columns, columnsView)
    }
}

/** Inside a card of a columns view: an item does not draw a panel of its own (the card is one). */
internal val LocalExtensionCard = staticCompositionLocalOf { false }

/** An item's panel: the group's surface on a page, none inside a card. */
@Composable
internal fun ItemPanel(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    if (LocalExtensionCard.current) Column(modifier.fillMaxWidth(), content = content)
    else JGroupItem(GroupPosition.Single, modifier, content = content)
}

/** A section's icon in its named colour, before its title (UI version 8). */
@Composable
private fun SectionIcon(section: ExtensionViewSection) {
    val name = section.icon?.takeIf { it.isNotBlank() } ?: return
    JIcon(ExtensionIcons.of(name), null, size = Tokens.Space.l, tint = PersonaTheme.palette.hueOr(ExtensionViewText.hue(section.iconColor)), decorative = true)
}

/**
 * The link beside a section's title: the design system's text button with the link's own words, 48 dp to touch, read
 * out as its words, spinning while its action is on its way; pressed as a row is (its confirm first, a push opens the page).
 */
@Composable
internal fun SectionLink(section: ExtensionViewSection, state: ExtensionViewState, handle: ExtensionViewHandle, small: Boolean = false) {
    val link = section.link?.takeIf { ExtensionColumns.hasLink(section) } ?: return
    val action = link.action
    JButton(link.title, { handle.press(ExtensionViewItem(type = "button", title = link.title), action) },
        Modifier.semantics { contentDescription = link.title },
        kind = JButtonKind.Quiet, size = if (small) JButtonSize.Small else JButtonSize.Medium,
        enabled = !state.busy, loading = action.id.isNotEmpty() && state.acting == action.id)
}

/**
 * A section's title where a page draws it: the plain section header, with the section's icon before it and its link at
 * its end (UI version 8). A section with neither is drawn exactly as before.
 */
@Composable
internal fun SectionHead(section: ExtensionViewSection, state: ExtensionViewState, handle: ExtensionViewHandle, modifier: Modifier = Modifier) {
    val title = section.title?.takeIf { it.isNotBlank() }.orEmpty()
    val icon = !section.icon.isNullOrBlank()
    val link = ExtensionColumns.hasLink(section)
    JSectionHeader(title, modifier, leading = if (icon) ({ SectionIcon(section) }) else null,
        action = if (link) ({ SectionLink(section, state, handle) }) else null)
}

/**
 * A section's title in a card (an overview card, an assistant's view, a card of a columns view): its icon, the title
 * and its link on one line. [card] is a columns view's card, whose head reads as an overview card's.
 */
@Composable
internal fun SectionCardHead(section: ExtensionViewSection, state: ExtensionViewState, handle: ExtensionViewHandle, modifier: Modifier = Modifier, card: Boolean = false) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val title = section.title?.takeIf { it.isNotBlank() }
    if (title == null && !ExtensionColumns.head(section)) return
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        if (!section.icon.isNullOrBlank()) { SectionIcon(section); Spacer(Modifier.width(Tokens.Space.s)) }
        Text(title.orEmpty(), Modifier.weight(1f).then(if (title != null) Modifier.semantics { heading() } else Modifier), style = if (card) type.headline else type.caption,
            color = if (card) palette.ink else palette.ink2, maxLines = if (card) 2 else 1, overflow = TextOverflow.Ellipsis)
        SectionLink(section, state, handle, small = !card)
    }
}

/** An item's handle: a native stream's node's own, else the page's. */
internal fun ExtensionViewHandle.of(item: ExtensionViewItem): ExtensionViewHandle = forNode?.invoke(item.key.orEmpty()) ?: this

/** A row, a link, a switch or a choice in a card: on its own, a hairline above every one but the first. */
@Composable
private fun CardDivided(index: Int, content: @Composable () -> Unit) {
    if (index > 0) JDivider(Modifier.padding(horizontal = rowPadding))
    content()
}

/**
 * A section's run of rows (and the links, switches and choices among them) with rows side by side where there is room
 * ([ExtensionColumns.perLine]): in lines, each line one entry of the group (a hairline between lines, none between the
 * rows of a line), anything but a row a line of its own. [card]: inside a card, which is the group.
 */
@Composable
internal fun SideBySideRows(section: ExtensionViewSection, items: List<ExtensionViewItem>, state: ExtensionViewState, handle: ExtensionViewHandle,
                            modifier: Modifier = Modifier, card: Boolean = false) {
    BoxWithConstraints(modifier.fillMaxWidth()) {
        val per = ExtensionColumns.perLine(section.columns, maxWidth)
        val lines = ExtensionColumns.lines(items, per)
        Column(Modifier.fillMaxWidth()) {
            lines.forEachIndexed { index, line ->
                val draw: @Composable () -> Unit = {
                    // the gap keeps rows apart where a style's rows have no side padding of their own (brief)
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                        line.forEach { item -> Box(Modifier.weight(1f)) { key(item.key) { GroupedItem(item, state, handle.of(item)) } } }
                        if (line.first().type == "row") repeat(per - line.size) { Spacer(Modifier.weight(1f)) }
                    }
                }
                if (card) CardDivided(index, draw) else JGroupItem(GroupPosition.of(index, lines.size)) { draw() }
            }
        }
    }
}

/** What a card of a columns view draws: the section's items, as a page draws them, inside the card. */
@Composable
private fun CardItems(section: ExtensionViewSection, items: List<ExtensionViewItem>, state: ExtensionViewState, handle: ExtensionViewHandle) {
    var i = 0
    while (i < items.size) {
        val start = i
        if (ExtensionViewText.grouped(items[i])) while (i < items.size && ExtensionViewText.grouped(items[i])) i++ else i++
        val run = items.subList(start, i)
        val top = if (start == 0) Modifier else Modifier.padding(top = Tokens.Space.s)
        if (ExtensionViewText.grouped(run.first())) {
            if (ExtensionColumns.sideBySide(section)) SideBySideRows(section, run, state, handle, top, card = true)
            else Column(top.fillMaxWidth()) { run.forEachIndexed { index, item -> key(item.key) { CardDivided(index) { GroupedItem(item, state, handle.of(item)) } } } }
        } else run.forEach { item ->
            key(item.key) {
                // Text, markdown and the panels' content pad themselves to the rows' text; the rest keep the card's edge off.
                val inset = if (item.type in SelfPadded) Modifier else Modifier.padding(horizontal = rowPadding)
                Box(inset) { StandaloneItem(item, state, handle.of(item), first = start == 0) }
            }
        }
    }
}

private val SelfPadded = setOf("text", "markdown", "metric", "progress", "chart", "stat_grid", "table")

/**
 * A card of a columns view: the section's head inside it (its icon, title and link), its items as a page draws them, its
 * "Load more" and its footer. The card's surface is drawn by [ExtensionCardRow], as tall as the row's tallest card.
 */
@Composable
internal fun ExtensionSectionCard(index: Int, section: ExtensionViewSection, state: ExtensionViewState, handle: ExtensionViewHandle,
                                  sectionHandle: ExtensionViewHandle, stream: Boolean) {
    val items = if (stream) section.items else section.items.filter(ExtensionViewText::drawable)
    CompositionLocalProvider(LocalExtensionCard provides true) {
        Column(Modifier.fillMaxWidth().padding(bottom = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            SectionCardHead(section, state, sectionHandle, Modifier.padding(start = rowPadding, end = rowPadding, top = Tokens.Space.s), card = true)
            if (items.isNotEmpty()) Column(Modifier.fillMaxWidth()) { CardItems(section, items, state, handle) }
            if (section.more?.action?.id?.isNotEmpty() == true) {
                JDivider(Modifier.padding(horizontal = rowPadding))
                ExtensionMore(index, section, state, sectionHandle)
            }
            section.footer?.takeIf { it.isNotBlank() }?.let { SettingsCaption(it, Modifier.padding(horizontal = rowPadding)) }
        }
    }
}

/** The tag of a card's surface in a columns view, for tests. */
internal const val ExtensionCardSurfaceTag = "extension-card"

/**
 * One row of a columns view's grid: [count] cards across [columns] places (a full-width card is one across one), the
 * design system's gap between them, each card's surface (the group panel) as tall as the row's tallest card, so rows
 * of cards line up. Measured once each, without intrinsic sizes (a card may hold a list or a chart that has none).
 */
@Composable
internal fun ExtensionCardRow(count: Int, columns: Int, modifier: Modifier = Modifier, card: @Composable (Int) -> Unit) {
    val gap = Tokens.Space.m
    Layout(
        contents = listOf(
            { repeat(count) { JGroupItem(GroupPosition.Single, Modifier.fillMaxSize().testTag(ExtensionCardSurfaceTag)) { Spacer(Modifier.fillMaxSize()) } } },
            { repeat(count) { index -> Box { card(index) } } },
        ),
        modifier = modifier.fillMaxWidth(),
    ) { (surfaces, cards), constraints ->
        val gapPx = gap.roundToPx()
        val places = columns.coerceAtLeast(1)
        val width = ((constraints.maxWidth - gapPx * (places - 1)) / places).coerceAtLeast(0)
        val drawn = cards.map { it.measure(Constraints(minWidth = width, maxWidth = width)) }
        val height = drawn.maxOfOrNull { it.height } ?: 0
        val backs = surfaces.map { it.measure(Constraints.fixed(width, height)) }
        layout(constraints.maxWidth, height) {
            drawn.forEachIndexed { index, placeable ->
                val x = index * (width + gapPx)
                backs[index].place(x, 0)
                placeable.place(x, 0)
            }
        }
    }
}

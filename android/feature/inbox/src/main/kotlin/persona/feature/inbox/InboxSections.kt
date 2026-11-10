package persona.feature.inbox

import android.content.res.Resources
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.lazy.LazyItemScope
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.launch
import persona.core.domain.inbox.InboxGroups
import persona.core.domain.inbox.InboxRow
import persona.core.domain.inbox.InboxRowType

// The inbox's groups and Done's history on Android (#2576, as #2059 on the web and iOS; docs/assistant/inbox.md
// "Grouping"): the rules are persona.core.domain.inbox.InboxGroups, this file gives them the rows' records and titles.

/**
 * Done's history beyond the list's few days (#2200, #2576): each manager pages its own (a cursor is one manager's), and
 * the pages are merged by when the items were done. [records] grows with each [load]; [more] while some manager has more.
 */
@Stable
internal class InboxDoneHistory(private val repository: InboxRepository, private val scope: CoroutineScope) {
    var records by mutableStateOf(emptyList<InboxRecord>()); private set
    var loading by mutableStateOf(false); private set
    var failed by mutableStateOf(false); private set
    var started by mutableStateOf(false); private set
    var more by mutableStateOf(true); private set
    private val next = HashMap<String, InboxDoneCursor>()
    private val ended = HashSet<String>()

    /** The next page of each of [managers] (null or "": the default) that has one. */
    fun load(managers: List<String?>) {
        if (loading) return
        val asked = managers.map { it.orEmpty() }.distinct().filter { it !in ended }
        if (asked.isEmpty()) { more = false; return }
        loading = true; failed = false; started = true
        scope.launch {
            try {
                val pages = asked.map { m -> async { m to repository.doneHistory(m.ifEmpty { null }, next[m]) } }.awaitAll()
                val known = records.mapTo(HashSet()) { it.item.id }
                records = records + pages.flatMap { it.second.records }.filter { known.add(it.item.id) }
                pages.forEach { (m, page) -> if (page.next == null) { ended += m; next.remove(m) } else next[m] = page.next }
                more = asked.any { it !in ended }
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                failed = true
            } finally {
                loading = false
            }
        }
    }
}

/**
 * A row in a selection (#2576): [checked] while selecting (null outside one), whether it can be picked at all (not an
 * agent's card, a done item or one Persona is working on), [onToggle] while selecting, and [onStart] for a long-press
 * that starts a selection with it.
 */
internal data class InboxRowSelection(val checked: Boolean?, val selectable: Boolean, val onToggle: () -> Unit, val onStart: () -> Unit) {
    val selecting: Boolean get() = checked != null

    /** A tap: picks or unpicks while selecting, else [open]. */
    fun click(open: () -> Unit) { if (!selecting) open() else if (selectable) onToggle() }

    /** A long-press: starts a selection with a row that can be picked, picks or unpicks while selecting, else [menu]. */
    fun longClick(menu: () -> Unit) { when { selecting -> if (selectable) onToggle(); selectable -> onStart(); else -> menu() } }
}

/** A group of a section as drawn: its key, title and rows. */
internal data class InboxShownGroup(val key: String, val title: String, val rows: List<InboxRow>)

/**
 * The groups of section [section] (#2576): Done by when its items were done; Needs you, FYI and Collapsed by [grouping]
 * (the agents' cards first either way); none for In progress, or when there would be only one.
 */
internal fun inboxSectionGroups(
    section: String, rows: List<InboxRow>, grouping: String, now: Instant, zone: ZoneId, res: Resources, locale: Locale,
    recordsOf: (InboxRow) -> List<InboxRecord>, cardAt: (InboxRow) -> Instant?,
): List<InboxShownGroup> {
    if (section == "working" || rows.isEmpty()) return emptyList()
    fun time(row: InboxRow, field: (InboxRecord) -> String?): Instant? =
        if (row.type == InboxRowType.Interaction) cardAt(row) else recordsOf(row).mapNotNull { parseInstant(field(it)) }.maxOrNull()
    val groups = when {
        section == "done" -> InboxGroups.done(rows, now, zone) { time(it) { r -> r.item.doneAt } }
        grouping == InboxGroups.TIME -> {
            val (agents, rest) = rows.partition { it.type == InboxRowType.Interaction }
            listOfNotNull(agents.takeIf { it.isNotEmpty() }?.let { InboxGroups.Group(InboxGroups.AGENTS, it) }) +
                InboxGroups.byTime(rest, now, zone) { time(it) { r -> r.item.updatedAt } }
        }
        else -> InboxGroups.bySource(rows) { row -> inboxSourceKey(row, recordsOf) }
    }
    return InboxGroups.shown(groups).map { g -> InboxShownGroup(g.key, inboxGroupTitle(g.key, g.items, res, locale, recordsOf), g.items) }
}

/** A row's source group: its agent card, Lark, its mail account, its connector, else its source. */
internal fun inboxSourceKey(row: InboxRow, recordsOf: (InboxRow) -> List<InboxRecord>): String {
    if (row.type == InboxRowType.Interaction) return InboxGroups.AGENTS
    val record = recordsOf(row).firstOrNull() ?: return "other"
    if (record.feedId != null || record.source == "lark") return "lark"
    return InboxGroups.sourceKey(record.source.ifBlank { if (record.mail != null || record.item.kind == "mail") "mail" else "" },
        record.item.account, record.isConnector)
}

/** A group's title: the agents, Lark, "Mail · account", a connector's or source's own name, or the time group's. */
internal fun inboxGroupTitle(key: String, rows: List<InboxRow>, res: Resources, locale: Locale, recordsOf: (InboxRow) -> List<InboxRecord>): String {
    val first = rows.firstNotNullOfOrNull { recordsOf(it).firstOrNull() }
    InboxGroups.dayOf(key)?.let { day ->
        val pattern = android.text.format.DateFormat.getBestDateTimePattern(locale, "EEEMMMd")
        return day.format(DateTimeFormatter.ofPattern(pattern, locale))
    }
    return when {
        key == InboxGroups.AGENTS -> res.getString(R.string.inbox_group_agents)
        key == "lark" -> res.getString(R.string.inbox_source_lark)
        key.startsWith("mail:") -> res.getString(R.string.inbox_group_mail, first?.sourceLabel?.takeIf { it.isNotBlank() } ?: key.removePrefix("mail:"))
        key == "just" -> res.getString(R.string.inbox_group_just)
        key == "today" -> res.getString(R.string.inbox_group_today)
        key == "yesterday" -> res.getString(R.string.inbox_group_yesterday)
        key == "week" -> res.getString(R.string.inbox_group_week)
        key == "month" -> res.getString(R.string.inbox_group_month)
        else -> first?.sourceLabel?.takeIf { it.isNotBlank() } ?: key.removePrefix("c:")
    }
}

private fun parseInstant(value: String?): Instant? = value?.let { runCatching { Instant.parse(it) }.getOrNull() }

/**
 * A list scope that notes where each keyed item and header lands, so "Jump to" can scroll to a group's header: a lazy
 * list scrolls only by index. [counted] is how many items the page's own content put in.
 */
internal class KeyedListScope(private val inner: LazyListScope, private val positions: MutableMap<Any, Int>) : LazyListScope by inner {
    var counted = 0; private set
    override fun item(key: Any?, contentType: Any?, content: @Composable LazyItemScope.() -> Unit) {
        if (key != null) positions[key] = counted
        counted++
        inner.item(key, contentType, content)
    }
    override fun items(count: Int, key: ((index: Int) -> Any)?, contentType: (index: Int) -> Any?, itemContent: @Composable LazyItemScope.(index: Int) -> Unit) {
        counted += count
        inner.items(count, key, contentType, itemContent)
    }
    override fun stickyHeader(key: Any?, contentType: Any?, content: @Composable LazyItemScope.(index: Int) -> Unit) {
        if (key != null) positions[key] = counted
        counted++
        inner.stickyHeader(key, contentType, content)
    }
}

/** Where the page's keyed items landed in its last build (KeyedListScope), for "Jump to". */
internal class InboxListIndex {
    val positions = HashMap<Any, Int>()
    var counted = 0
}

/** "Jump to" of one Done (#2576): its section under [prefix] (a manager's, or none) and the groups it lists. */
internal data class InboxJumpMenu(val prefix: String, val section: String, val groups: List<InboxShownGroup>)

/** A group's tick in a selection: none, some or all of its pickable items picked; a tap picks them all or none. */
@Composable
internal fun InboxGroupTick(ids: List<String>, picked: Set<String>, name: String, onPick: (Set<String>) -> Unit) {
    val state = when (InboxGroups.tick(ids, picked)) {
        InboxGroups.Tick.None -> androidx.compose.ui.state.ToggleableState.Off
        InboxGroups.Tick.Some -> androidx.compose.ui.state.ToggleableState.Indeterminate
        InboxGroups.Tick.All -> androidx.compose.ui.state.ToggleableState.On
    }
    persona.core.designsystem.JTriCheckbox(state, { onPick(InboxGroups.pickGroup(ids, picked)) },
        label = androidx.compose.ui.res.stringResource(R.string.inbox_pick_group, name))
}

/** The end of Done while there is older history: "Show older", loading, or why it couldn't with Retry. */
@Composable
internal fun InboxOlderRow(history: InboxDoneHistory, onLoad: () -> Unit) {
    when {
        history.loading -> persona.core.designsystem.JLoading(text = androidx.compose.ui.res.stringResource(R.string.inbox_loading_older))
        history.failed -> persona.core.designsystem.JNotice(androidx.compose.ui.res.stringResource(R.string.inbox_older_failed),
            actionLabel = androidx.compose.ui.res.stringResource(R.string.inbox_retry), onAction = onLoad)
        else -> androidx.compose.foundation.layout.Box(Modifier.fillMaxWidth(), contentAlignment = androidx.compose.ui.Alignment.Center) {
            persona.core.designsystem.JTextButton(androidx.compose.ui.res.stringResource(R.string.inbox_load_older), onLoad)
        }
    }
}

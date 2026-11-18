package persona.feature.agents

import persona.core.contracts.AgentState
import persona.core.contracts.Interaction
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.domain.sort.AgentSession
import persona.core.domain.sort.AgentQuickFilter

// What the Agents list shows of the hub while someone reads it (docs/android/android.md "Reading position"): the page as
// it was when they began to scroll, for a bounded time, with what needs them landed at once, and a quiet note of what
// came meanwhile. Pure rules live here; AgentsFeedHold.kt drives them from the list's scroll state. The rows' own position
// is kept by AgentReadingPosition.kt.

/**
 * How long the page waits, as configured: neither is a measured latency.
 * A scroll that rests for [SETTLE_MILLIS] lets what waited land, and nothing waits longer than [CAP_MILLIS] however long
 * the list keeps moving (iOS `Hold.settle` / `Hold.cap`).
 */
internal object AgentsFeedTiming {
    const val SETTLE_MILLIS = 500L
    const val CAP_MILLIS = 5_000L
}

/**
 * How many rows from the top of the list the pill counts as new or stopped: what a tap on it is sure to show. A row
 * further down may be anywhere in the tree, so it is not announced.
 */
internal const val AGENTS_FEED_TOP_ROWS = 4

/** What the list drew when the hold began, and the controls it was drawn under ([scope], as the reading position's). */
internal class AgentsFeedFrozen(val page: AgentsPageState, val scope: List<Any?>)

/** The ids of the rows the list draws, in list order (a row folded away behind "N more idle" is not one). */
internal fun AgentsPageState.visibleRowIds(): List<String> = list.groups.flatMap { group -> group.visibleRows.map { it.id } }

private fun AgentsPageState.rowIds(): List<String> = list.groups.flatMap { group -> group.rows.map { it.id } }

/**
 * Whether the page [frozen] still stands for [latest]. False lets the hold go for good, so the list shows [latest] and the
 * reading position starts over from it:
 * - a control changed (grouping, sort, filter, search, folds, window): [scope] differs;
 * - the first page is not in yet, or the held list had no rows, or the list emptied, or none of the rows held is left
 *   (nothing to keep still);
 * - a paged list grew at its end: a fetched page is not a live insertion, and it must not leave a gap under the finger.
 * Everything else waits, except what [landCritical] lands: reorders, new rows and last lines.
 */
internal fun holdsAgainst(frozen: AgentsFeedFrozen, latest: AgentsPageState, scope: List<Any?>): Boolean {
    if (frozen.scope != scope || !latest.loaded) return false
    val held = frozen.page.rowIds().toSet()
    val rows = latest.rowIds()
    if (held.isEmpty() || rows.isEmpty() || rows.none { it in held }) return false
    if (frozen.page.pagination.paged != latest.pagination.paged) return false
    return !(latest.pagination.paged && growsPage(frozen.page, latest))
}

/**
 * The hub's loaded window got longer at its end, with the old window as the start of the new one in the same order: that
 * is a fetched page. A session a push put anywhere else, even in front, is a live insertion and waits like the rest.
 */
internal fun growsPage(held: AgentsPageState, latest: AgentsPageState): Boolean {
    if (latest.sessions.size <= held.sessions.size) return false
    return held.sessions.indices.all { held.sessions[it].key == latest.sessions[it].key }
}

private fun AgentState.urgent() = this == AgentState.NeedsYou || this == AgentState.Offline

/**
 * What must not wait (iOS `AgentsFeed.landCritical`): the sessions that start or stop needing the user or go offline are
 * given their new state on the rows as they stand, a session that needed the user and is gone leaves, one that now needs
 * the user is added (the Needs You section shows it), and the computers' states land. The order, the groups, the counts
 * and everything else about a row wait for the rest of [latest], so the list does not move under the finger. Returns
 * [held] itself when nothing of the kind changed.
 */
internal fun landCritical(held: AgentsPageState, latest: AgentsPageState): AgentsPageState {
    val now = latest.sessions.associateBy { it.key }
    val changed = HashMap<SessionKey, Session>()
    val gone = HashSet<SessionKey>()
    for (old in held.sessions) {
        val new = now[old.key]
        when {
            new == null -> if (old.foldedState.urgent()) gone += old.key
            new.foldedState != old.foldedState && (new.foldedState.urgent() || old.foldedState.urgent()) -> changed[old.key] = new
        }
    }
    val heldKeys = held.sessions.mapTo(HashSet()) { it.key }
    val arrived = latest.sessions.filter { it.key !in heldKeys && it.foldedState.urgent() }
    if (changed.isEmpty() && gone.isEmpty() && arrived.isEmpty() && held.machines == latest.machines) return held

    val fresh = changed.mapValues { it.value.listSession(latest.preferences) }
    val latestMachines = latest.machines.associateBy { it.id }
    fun AgentSession.landed(): AgentSession {
        val row = fresh[session.key] ?: return this
        return copy(session = session.copy(state = row.session.state, status = row.session.status,
            pendingInteractions = row.session.pendingInteractions, children = row.session.children), stateSince = row.stateSince)
    }
    fun List<AgentSession>.landed() = mapNotNull { if (it.session.key in gone) null else it.landed() }
    val landedGroups = held.list.groups.map { it.copy(rows = it.rows.landed(), visibleRows = it.visibleRows.landed()) }
    val groups = landedGroups.filter { group -> group.rows.isNotEmpty() || landedGroups.any { it.under == group.id && it.rows.isNotEmpty() } }
    return held.copy(
        sessions = held.sessions.mapNotNull { old ->
            if (old.key in gone) null
            else changed[old.key]?.let { new -> old.copy(state = new.state, status = new.status, pendingInteractions = new.pendingInteractions, children = new.children) } ?: old
        } + arrived,
        machines = latest.machines,
        list = held.list.copy(
            groups = groups,
            machines = held.list.machines.map { group -> latestMachines[group.machine.id]?.let { group.copy(machine = it) } ?: group },
            children = held.list.children.mapValues { it.value.landed() },
        ),
    )
}

/**
 * The sessions the Needs You section of [page] draws, by the section's own row id, under the controls [page] was built
 * with: the same rows the section and its count are made of ([agentNeedsYouRows]), for the requests [requests] that are
 * shown. Nothing while the section is not drawn (a search, or a quick filter other than All and Needs You), since a tap
 * on the pill would not show it.
 */
internal fun agentsFeedNeedsYou(page: AgentsPageState, requests: List<Interaction>, closed: (SessionKey) -> Boolean): Set<String> {
    val settings = page.settings
    if (settings.query.isNotEmpty() || (settings.quick != AgentQuickFilter.All && settings.quick != AgentQuickFilter.NeedsYou)) return emptySet()
    return agentNeedsYouRows(requests, page.sessions.map { it.listSession(page.preferences).session },
        settings.copy(quick = AgentQuickFilter.All, query = ""), closed).mapTo(HashSet()) { it.id }
}

/** What the user has been told about, for [agentsFeedNotice] to tell what is new from. */
internal data class AgentsFeedMark(
    /** The Needs You section's rows. */
    val needsYou: Set<String>,
    /** Every row the list draws. */
    val rows: Set<String>,
    /** The first rows from the top ([AGENTS_FEED_TOP_ROWS]): the ones a tap on the pill is sure to show. */
    val top: Set<String>,
    /** The drawn rows that are not offline. */
    val live: Set<String>,
    /** The drawn rows that are offline, with their computer. */
    val offline: Map<String, String>,
    /** The computers that are down, under the computer filter. */
    val down: Set<String>,
    val paged: Boolean,
)

/** What [page] says now; [needsYou] is [agentsFeedNeedsYou] of it. */
internal fun agentsFeedMark(page: AgentsPageState, needsYou: Set<String>): AgentsFeedMark {
    val drawn = page.list.groups.flatMap { it.visibleRows }.distinctBy { it.id }
    val filter = page.settings.machineFilter
    return AgentsFeedMark(
        needsYou = needsYou,
        rows = drawn.mapTo(HashSet()) { it.id },
        top = drawn.take(AGENTS_FEED_TOP_ROWS).mapTo(HashSet()) { it.id },
        live = drawn.filter { it.state != AgentState.Offline }.mapTo(HashSet()) { it.id },
        offline = drawn.filter { it.state == AgentState.Offline }.associate { it.id to it.session.machineId },
        down = page.machines.filter { (filter.isEmpty() || it.id == filter) && (!it.online || !it.error.isNullOrEmpty()) }
            .mapTo(HashSet()) { it.id },
        paged = page.pagination.paged,
    )
}

/** What came while the list was read further down: the counts say what, and [urgent] ones need the user or are down. */
internal data class AgentsFeedNotice(val needsYou: Int = 0, val offline: Int = 0, val fresh: Int = 0) {
    val isEmpty: Boolean get() = needsYou == 0 && offline == 0 && fresh == 0
    val urgent: Boolean get() = needsYou > 0 || offline > 0
}

/**
 * What [latest] has that [baseline] (the mark when the user left the top) did not, counting only what a tap on the pill
 * shows: sessions the Needs You section now has; computers that went down, and drawn rows near the top that stopped
 * running (not those of a computer that is down, which it counts once); new rows near the top. A paged list counts no new
 * rows, since a fetched page is older history and a pushed session cannot be told from it by id alone. Nothing before the
 * user has left the top.
 */
internal fun agentsFeedNotice(baseline: AgentsFeedMark?, latest: AgentsFeedMark): AgentsFeedNotice {
    if (baseline == null) return AgentsFeedNotice()
    val needsYou = latest.needsYou - baseline.needsYou
    val down = latest.down - baseline.down
    // A new Needs You is both a section row and a new row; it counts once, as the urgent kind.
    val sectionRows = latest.needsYou.mapTo(HashSet()) { it.removePrefix("agent:") }
    val stopped = latest.offline.count { (id, machine) -> id in latest.top && id in baseline.live && machine !in latest.down }
    return AgentsFeedNotice(
        needsYou = needsYou.size,
        offline = down.size + stopped,
        fresh = if (latest.paged || baseline.paged) 0 else (latest.top - baseline.rows - latest.offline.keys - sectionRows).size,
    )
}

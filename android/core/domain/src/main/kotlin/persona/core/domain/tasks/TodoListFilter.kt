package persona.core.domain.tasks

import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.Todo
import persona.core.contracts.parseTime
import java.time.DayOfWeek
import java.time.Instant
import java.time.ZoneId
import java.time.temporal.TemporalAdjusters

// The To-dos list's filter and sort (#3740, docs/assistant/todos.md "List filter and sort"), as iOS
// apple/Shared/TodoListFilter.swift: a view on the device, never sent to the hub. A filter narrows the to-dos before the
// status sections place them (as a search does), so each section and its count follow it; a sort orders the rows inside
// each section, the sections stay in their order.

/** Which to-dos the list shows, by due date ([key] is what the device keeps). Today, This week and This month also take open to-dos already overdue. */
enum class TodoListFilter(val key: String) {
    All("all"), NoDue("no_due"), Today("today"), Week("week"), Month("month");

    companion object {
        fun of(key: String?): TodoListFilter = entries.firstOrNull { it.key == key } ?: All
    }
}

/** How rows are ordered inside each section. Recent (most recently active first) is the list's own order. */
enum class TodoListSort(val key: String) {
    Recent("recent"), Due("due"), Priority("priority");

    companion object {
        fun of(key: String?): TodoListSort = entries.firstOrNull { it.key == key } ?: Recent
    }
}

/**
 * What the list's sections are ([key] is what the device keeps): the status sections (the default), or one section per
 * direction of the hub's tree (#4352), offered only while the hub has the tree.
 */
enum class TodoListGrouping(val key: String) {
    Status("status"), Direction("direction");

    companion object {
        fun of(key: String?): TodoListGrouping = entries.firstOrNull { it.key == key } ?: Status
    }
}

/** When a to-do is due, for the filter: its moment, or the start of its day in [zone]; null without either. */
fun Todo.filterDue(zone: ZoneId): Instant? = parseTime(dueAt) ?: dueDay?.atStartOfDay(zone)?.toInstant()

/**
 * Whether the to-do is in [filter] at [now]: due today, this week or this month (a due day by its start, a due moment by
 * itself), or open and already overdue. The week starts on [firstDay], the calendar's ([weekStart]).
 */
fun Todo.matches(filter: TodoListFilter, now: Instant, zone: ZoneId, firstDay: DayOfWeek): Boolean {
    val at = filterDue(zone)
    if (filter == TodoListFilter.All) return true
    if (filter == TodoListFilter.NoDue) return at == null
    if (at == null) return false
    if (isOverdue(now, zone)) return true
    val today = now.atZone(zone).toLocalDate()
    val (from, until) = when (filter) {
        TodoListFilter.Today -> today to today.plusDays(1)
        TodoListFilter.Week -> today.with(TemporalAdjusters.previousOrSame(firstDay)).let { it to it.plusWeeks(1) }
        else -> today.withDayOfMonth(1).let { it to it.plusMonths(1) }
    }
    return !at.isBefore(from.atStartOfDay(zone).toInstant()) && at.isBefore(until.atStartOfDay(zone).toInstant())
}

/** How many to-dos each filter would show (the menu says it under each). */
fun filterCounts(todos: List<Todo>, now: Instant, zone: ZoneId, firstDay: DayOfWeek): Map<TodoListFilter, Int> =
    TodoListFilter.entries.associateWith { f -> todos.count { it.matches(f, now, zone, firstDay) } }

/** High first, then normal, then low. */
fun Todo.priorityRank(): Int = when (priority) {
    "high" -> 0
    "low" -> 2
    else -> 1
}

/**
 * A section's rows in [sort]'s order. A merged row sorts by its earliest due / highest priority member; ties keep the
 * list's order (most recently active first). Recent leaves the rows as they are.
 */
fun sortedRows(rows: List<TriageGroup>, sort: TodoListSort, zone: ZoneId): List<TriageGroup> {
    if (sort == TodoListSort.Recent) return rows
    fun due(g: TriageGroup) = g.members.mapNotNull { it.dueBy(zone) }.minOrNull() ?: Instant.MAX
    fun rank(g: TriageGroup) = g.members.minOfOrNull { it.priorityRank() } ?: 1
    val order = if (sort == TodoListSort.Priority) compareBy<TriageGroup>(::rank).thenBy(::due) else compareBy(::due)
    return rows.sortedWith(order) // stable: ties keep the list's order
}

/** To-dos listed under a section: its rows' members, the related ones merged into a row included (the section's count). */
fun TriageResult.listed(section: TriageSection): Int = rows(section).sumOf { it.members.size }

/**
 * Of [todos], those waiting on the owner (Needs you) that [shown] leaves out: the line over a filtered list.
 * [superseded] is decided on every to-do ([supersededFailedStarts]), so a failed start whose finished work the filter
 * leaves out is not counted as waiting (#3713).
 */
fun hiddenWaiting(
    todos: List<Todo>,
    shown: List<Todo>,
    lookup: (SessionKey) -> Session?,
    now: Long,
    restored: Map<String, Long> = emptyMap(),
    zone: ZoneId = ZoneId.systemDefault(),
    superseded: Set<String> = supersededFailedStarts(todos),
): Int {
    val ids = shown.mapTo(HashSet()) { it.id }
    return todos.count {
        it.id !in ids && it.triage(lookup, now, restored[it.id] ?: 0, zone, superseded = it.id in superseded).section == TriageSection.Waiting
    }
}

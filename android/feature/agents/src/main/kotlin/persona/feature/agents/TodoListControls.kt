package persona.feature.agents

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import persona.core.designsystem.JAction
import persona.core.designsystem.JChip
import persona.core.designsystem.JIconButton
import persona.core.designsystem.JMenu
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.tasks.TodoListFilter
import persona.core.domain.tasks.TodoListGrouping
import persona.core.domain.tasks.TodoListSort

// The To-dos list's filter and sort (#3740, docs/assistant/todos.md "List filter and sort"), beside List | Calendar as
// on iOS (TodosView.listControls): a chip that clears an active filter, and one button whose menu picks what shows and
// how each section is ordered. Kept on this device, worked out on it: no request.

/** The filter and sort this device keeps for the to-dos list (iOS `todos.filter` / `todos.sort`). */
internal class TodoListPrefs(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("todos_list", Context.MODE_PRIVATE)

    var filter: TodoListFilter
        get() = TodoListFilter.of(prefs.getString("filter", null))
        set(value) = prefs.edit().putString("filter", value.key).apply()

    var sort: TodoListSort
        get() = TodoListSort.of(prefs.getString("sort", null))
        set(value) = prefs.edit().putString("sort", value.key).apply()

    /** Status sections (the default) or one per direction of the hub's tree (#4352). */
    var grouping: TodoListGrouping
        get() = TodoListGrouping.of(prefs.getString("group", null))
        set(value) = prefs.edit().putString("group", value.key).apply()
}

internal val TodoListGrouping.title: String @Composable get() = stringResource(when (this) {
    TodoListGrouping.Status -> R.string.agents_todos_group_status
    TodoListGrouping.Direction -> R.string.agents_todos_group_direction
})

internal val TodoListFilter.title: String @Composable get() = stringResource(when (this) {
    TodoListFilter.All -> R.string.agents_todos_filter_all
    TodoListFilter.NoDue -> R.string.agents_todos_filter_no_due
    TodoListFilter.Today -> R.string.agents_todos_filter_today
    TodoListFilter.Week -> R.string.agents_todos_filter_week
    TodoListFilter.Month -> R.string.agents_todos_filter_month
})

internal val TodoListSort.title: String @Composable get() = stringResource(when (this) {
    TodoListSort.Recent -> R.string.agents_todos_sort_recent
    TodoListSort.Due -> R.string.agents_todos_sort_due
    TodoListSort.Priority -> R.string.agents_todos_sort_priority
})

/** What an empty filtered list says ("Nothing due this week"). */
internal val TodoListFilter.emptyTitle: String @Composable get() = stringResource(when (this) {
    TodoListFilter.NoDue -> R.string.agents_todos_filter_empty_no_due
    TodoListFilter.Today -> R.string.agents_todos_filter_empty_today
    TodoListFilter.Week -> R.string.agents_todos_filter_empty_week
    else -> R.string.agents_todos_filter_empty_month
})

/**
 * The list's controls at the end of the List | Calendar row: "This week ×" while a filter is on (a tap clears it), then
 * the filter button, filled while the filter or the sort is not the default. Its menu: Show (each choice with how many
 * it would show, [counts]) and Sort by; with [onGrouping] (a hub with the direction tree, #4352) Group by first: Status | Pinned.
 */
@Composable
internal fun TodoListControls(
    filter: TodoListFilter,
    sort: TodoListSort,
    counts: Map<TodoListFilter, Int>,
    onFilter: (TodoListFilter) -> Unit,
    onSort: (TodoListSort) -> Unit,
    modifier: Modifier = Modifier,
    grouping: TodoListGrouping = TodoListGrouping.Status,
    onGrouping: ((TodoListGrouping) -> Unit)? = null,
) {
    var open by remember { mutableStateOf(false) }
    val active = filter != TodoListFilter.All || sort != TodoListSort.Recent || grouping != TodoListGrouping.Status
    Row(modifier, verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        if (filter != TodoListFilter.All) {
            val clear = stringResource(R.string.agents_todos_filter_clear, filter.title)
            JChip(filter.title, selected = true, onClick = { onFilter(TodoListFilter.All) }, trailingIcon = PhIcons.X, role = Role.Button,
                modifier = Modifier.semantics { contentDescription = clear }.testTag("todos-filter-clear"))
        }
        Box {
            JIconButton(PhIcons.FunnelSimple, stringResource(R.string.agents_todos_filter_button), { open = true }, selected = active,
                tint = if (active) PersonaTheme.palette.accent else null, modifier = Modifier.testTag("todos-filter"))
            val groupBy = stringResource(R.string.agents_todos_group_by)
            val groupings = if (onGrouping == null) emptyList() else TodoListGrouping.entries.mapIndexed { i, g ->
                JAction(g.title, checked = g == grouping, section = if (i == 0) groupBy else null) { onGrouping(g) }
            }
            val show = stringResource(R.string.agents_todos_filter_show)
            val sortBy = stringResource(R.string.agents_todos_sort_by)
            val filters = TodoListFilter.entries.mapIndexed { i, f ->
                val n = counts[f] ?: 0
                JAction(f.title, checked = f == filter, subtitle = pluralStringResource(R.plurals.agents_todos_filter_count, n, n),
                    section = if (i == 0) show else null) { onFilter(f) }
            }
            val sorts = TodoListSort.entries.mapIndexed { i, s ->
                JAction(s.title, checked = s == sort, section = if (i == 0) sortBy else null) { onSort(s) }
            }
            JMenu(open, { open = false }, groupings + filters + sorts)
        }
    }
}

package persona.feature.agents

import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.data.KotlinAgentListAdapter
import persona.core.domain.sort.AgentGrouping
import persona.core.domain.sort.AgentListResult
import persona.core.domain.sort.AgentListSettings
import persona.core.domain.sort.AgentSession
import persona.core.domain.sort.draws
import persona.core.domain.sort.isCodexTask
import persona.core.domain.sort.isNestedSubtask
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue

/** Pins the pre-adapter scoped projection, including range, forest and history, against the new default boundary. */
class AgentListDefaultEquivalenceTest {
    @Test fun allAndHistoryKeepTheOriginalPreprocessing() {
        val rows = listOf(
            AgentSession(Session(machineId = "m", sessionId = "live", state = "working", updatedAt = "2026-01-02T00:00:00Z")),
            AgentSession(Session(machineId = "m", sessionId = "history", state = "idle", updatedAt = "2026-01-01T00:00:00Z")),
        )
        val machines = listOf(Machine("m"))
        for (range in AgentRange.entries) for (grouping in listOf(AgentGrouping.State, AgentGrouping.Repo)) {
            val settings = AgentListSettings(grouping = grouping)
            val preferences = AgentsPreferencesSnapshot(range = range)
            assertEquals("$range/$grouping", originalScopedAgentList(rows, machines, preferences, settings),
                scopedAgentList(rows, machines, preferences, settings, adapter = KotlinAgentListAdapter()))
        }
    }

    @Test fun showAllDoesNotUnfoldIdleRows() {
        val rows = (1..25).map { AgentSession(Session(machineId = "m", sessionId = "s$it", state = "idle", updatedAt = "2026-01-01T00:00:${it.toString().padStart(2, '0')}Z")) }
        val settings = AgentListSettings(grouping = AgentGrouping.State, folded = setOf("!state/idle"), expandedAll = setOf("all:state/idle"))
        val expected = persona.core.domain.sort.buildFlatAgentList(rows, emptyList(), settings = settings)
        assertEquals(expected, scopedAgentList(rows, emptyList(), AgentsPreferencesSnapshot(), settings, adapter = KotlinAgentListAdapter()))
        assertTrue(expected.groups.single().visibleRows.isNotEmpty())
        assertFalse(settings.expandedIdle.contains("all:state/idle"))
    }

    @Test fun runningForestSearchAndHistoryFiltersMatchTheFlatBoundary() {
        val parent = AgentSession(Session(machineId = "m", sessionId = "parent", state = "working", cwd = "/work/main", repoRoot = "/work/repo", updatedAt = "2026-01-02T00:00:00Z"))
        val child = AgentSession(Session(machineId = "m", sessionId = "child", state = "idle", agent = "codex", parentSessionId = "parent", isSubagent = true, title = "needle", updatedAt = "2026-01-03T00:00:00Z"))
        for (query in listOf("", "needle")) {
            val settings = AgentListSettings(grouping = AgentGrouping.Repo, query = query)
            val prefs = AgentsPreferencesSnapshot()
            val tree = persona.core.domain.sort.codexTaskForest(persona.core.domain.sort.foldRows(listOf(parent, child)))
            val expected = persona.core.domain.sort.buildFlatAgentList(tree.roots, emptyList(), settings = tree.searchSettings(settings))
                .copy(children = tree.children)
            assertEquals(expected, scopedAgentList(listOf(parent, child), emptyList(), prefs, settings, adapter = KotlinAgentListAdapter()))
        }
    }

    private fun originalScopedAgentList(
        rows: List<AgentSession>, machines: List<Machine>, preferences: AgentsPreferencesSnapshot, settings: AgentListSettings,
    ): AgentListResult {
        val allRunning = settings.copy(includeCodexHistory = true)
        val runs: ((AgentSession) -> Boolean)? = when (preferences.range) {
            AgentRange.Running -> { row -> settings.draws(row, preferences.pins) }
            AgentRange.All -> { row -> (row.session.isLive || row.session.isCodexTask || row.session.foldedState != persona.core.contracts.AgentState.Offline) && allRunning.draws(row, preferences.pins) }
            AgentRange.History -> null
        }
        val tree = persona.core.domain.sort.codexTaskForest(persona.core.domain.sort.foldRows(rows, settings.hierarchy), emptySet(), runs)
        val roots = tree.roots
        val result = if (preferences.range == AgentRange.Running) {
            persona.core.domain.sort.buildFlatAgentList(roots, machines, preferences.pins, preferences.mutes, settings)
        } else {
            val history = roots.filter { !it.session.isLive && !(preferences.range == AgentRange.All && it.session.isCodexTask) &&
                !it.session.assistant && (settings.machineFilter.isEmpty() || settings.machineFilter == it.session.machineId) }
            val filtered = history.filter { settings.quick.matches(it) }
            val groups = filtered.groupBy { it.session.machineId to if (it.session.isCodexTask) it.session.cwd else it.session.cwd }
                .map { (project, members) ->
                    val id = "history/${project.first}/${project.second}"
                    val ordered = members.sortedWith(compareByDescending<AgentSession> { it.updatedAt }.thenBy { it.id })
                    val folded = id in settings.folded
                    persona.core.domain.sort.AgentListGroup(id, persona.core.contracts.CoreText.plain(project.second), ordered,
                        if (folded) emptyList() else ordered, folded)
                }.sortedWith(compareByDescending<persona.core.domain.sort.AgentListGroup> { it.rows.first().updatedAt }.thenBy { it.id })
            val running = if (preferences.range == AgentRange.All) {
                val live = roots.filter { it.session.isLive || it.session.isCodexTask || it.session.children != null }
                persona.core.domain.sort.buildFlatAgentList(live, machines, preferences.pins, preferences.mutes,
                    settings.copy(includeCodexHistory = true))
            } else AgentListResult(emptyMap(), emptyList(), emptyList())
            running.copy(groups = running.groups + groups, counts = persona.core.domain.sort.AgentQuickFilter.entries.associateWith { filter ->
                (running.counts[filter] ?: 0) + history.count { !it.isNestedSubtask && filter.matches(it) }
            })
        }
        return result.copy(children = tree.children)
    }
}

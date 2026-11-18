package persona.core.domain.sort

import persona.core.domain.zhAny

import persona.core.contracts.AgentState
import persona.core.contracts.Machine
import persona.core.contracts.Session
import org.junit.Assert.*
import org.junit.Test

/** The "By machine and space" tree these tests were written for (the default grouping is now "By repository"). */
private val MachineSpaceTree = AgentListSettings(grouping = AgentGrouping.MachineSpace)

class AgentMachineTreeTest {
    private fun row(machine: String, space: String, id: String, state: String = "working", date: Int = 1) =
        AgentSession(Session(machineId = machine, sessionId = id, spaceId = space, spaceName = space,
            state = state, title = id, updatedAt = "2026-01-0${date}T00:00:00Z"))

    @Test fun onlineMachinesKeepHubOrderAndEmptyMachinesDisappear() {
        val machines = listOf(Machine("offline"), Machine("second", online = true),
            Machine("first", online = true), Machine("empty", online = true))
        val rows = listOf(row("first", "space", "one"), row("offline", "space", "two"), row("second", "space", "three"))
        val tree = buildAgentList(rows, machines, settings = MachineSpaceTree)
        assertEquals(listOf("second", "first", "offline"), (tree.machines.map { it.machine.id }).zhAny())
        assertEquals(listOf("second/space", "first/space", "offline/space"), (tree.groups.map { it.id }).zhAny())
        assertEquals(listOf(1, 1, 1), tree.machines.map { it.sessionCount })
        assertEquals("unknown", (buildAgentList(listOf(row("unknown", "space", "four")), settings = MachineSpaceTree).machines.single().machine.id).zhAny())
    }

    @Test fun spacesKeepPinsThenActiveThenMutedThenTheirOwnQuietTail() {
        val rows = listOf(row("one", "muted", "muted", "needs_you"), row("one", "active", "active"),
            row("one", "pinned", "pinned", "idle"), row("one", "quiet", "quiet", "idle"),
            row("two", "quiet", "other-quiet", "idle"), row("one", "active", "session-pin", "unread"))
        val pins = AgentPreferences(spaces = listOf("one/pinned"), sessions = listOf("one/session-pin"))
        val mutes = AgentPreferences(spaces = listOf("one/muted"))
        val tree = buildAgentList(rows, listOf(Machine("one", online = true), Machine("two", online = true)), pins, mutes, settings = MachineSpaceTree)
        assertEquals(listOf("pinned", "one/pinned", "one/active", "one/muted", "one/quiet", "two/quiet"), (tree.groups.map { it.id }).zhAny())
        assertEquals(listOf("one/session-pin"), (tree.groups.first().rows.map { it.id }).zhAny())
        assertEquals(listOf("quiet", "quiet"), (tree.groups.filter { it.parent == "quiet" }.map { it.title }).zhAny())
        assertTrue(tree.groups.filter { it.parent == "quiet" }.all { it.folded && it.visibleRows.isEmpty() })
        assertEquals(5, tree.machines.first().sessionCount)
        assertEquals(4, tree.machines.first().spaceCount)
        assertEquals(1, tree.machines.first().stateCounts[AgentState.Unread])
        assertEquals(6, tree.groups.sumOf { it.rows.size })
        val expanded = buildAgentList(rows, pins = pins, mutes = mutes,
            settings = AgentListSettings(grouping = AgentGrouping.MachineSpace, expandedQuiet = setOf("one/quiet")))
        assertEquals(listOf("quiet"), (expanded.groups.single { it.id == "one/quiet" }.visibleRows.map { it.session.sessionId }).zhAny())
        assertTrue(expanded.groups.single { it.id == "two/quiet" }.folded)
    }

    @Test fun machineAndSpaceFoldsHideRowsWithoutChangingCountsAndSearchOpensIdle() {
        val rows = listOf(row("one", "active", "work"), row("one", "active", "idle", "idle"),
            row("one", "quiet", "quiet", "idle"), row("two", "space", "unread", "unread"))
        val folded = buildAgentList(rows, settings = AgentListSettings(grouping = AgentGrouping.MachineSpace, folded = setOf("machine/one", "two/space")))
        assertTrue(folded.machines.single { it.machine.id == "one" }.folded)
        assertTrue(folded.groups.all { it.visibleRows.isEmpty() })
        assertEquals(3, folded.machines.single { it.machine.id == "one" }.sessionCount)
        assertEquals(1, folded.machines.single { it.machine.id == "one" }.stateCounts[AgentState.Working])
        val idle = buildAgentList(rows, settings = AgentListSettings(grouping = AgentGrouping.MachineSpace, expandedIdle = setOf("one/active")))
        assertEquals(2, idle.groups.single { it.id == "one/active" }.visibleRows.size)
        val searched = buildAgentList(rows, settings = AgentListSettings(grouping = AgentGrouping.MachineSpace, query = "quiet"))
        assertEquals(listOf("one/quiet"), (searched.groups.flatMap { it.visibleRows }.map { it.id }).zhAny())
        assertEquals(1, searched.machines.single().sessionCount)
        assertEquals(0, searched.counts[AgentQuickFilter.Working])
    }

    @Test fun quickMachineAndSearchFiltersDriveHeaderCountsWhileKeptRowsRemain() {
        val rows = listOf(row("one", "project", "match-unread", "unread"), row("one", "project", "match-work"),
            row("one", "other", "hidden", "needs_you"), row("two", "project", "match-other", "unread"))
        val settings = AgentListSettings(grouping = AgentGrouping.MachineSpace, machineFilter = "one", query = "match", quick = AgentQuickFilter.Unread)
        val tree = buildAgentList(rows, settings = settings)
        assertEquals(1, tree.counts[AgentQuickFilter.Unread])
        assertEquals(1, tree.counts[AgentQuickFilter.Working])
        assertEquals(0, tree.counts[AgentQuickFilter.NeedsYou])
        assertEquals(1, tree.machines.single().sessionCount)
        assertEquals(1, tree.groups.single().stateCounts?.get(AgentState.Unread))
        val kept = buildAgentList(rows, settings = settings.copy(kept = setOf("one/match-work")))
        assertEquals(2, kept.machines.single().sessionCount)
        assertEquals(1, kept.machines.single().stateCounts[AgentState.Working])
        assertEquals(listOf("one/match-unread"), (kept.unreadShown).zhAny())
        assertTrue(buildAgentList(rows, settings = settings.copy(query = "absent")).machines.isEmpty())
    }

    @Test fun sessionOrderingRemainsStableWithinSpacesAndStateGroupingStillWorks() {
        val rows = listOf(row("one", "space", "work-new", date = 3), row("one", "space", "unread", "unread"),
            row("one", "space", "needs", "needs_you"), row("one", "space", "work-old"))
        val tree = buildAgentList(rows, settings = MachineSpaceTree)
        assertEquals(listOf("needs", "unread", "work-new", "work-old"), (tree.groups.single().visibleRows.map { it.session.sessionId }).zhAny())
        assertEquals(tree, buildAgentList(rows.reversed(), settings = MachineSpaceTree))
        val state = buildAgentList(rows, settings = AgentListSettings(grouping = AgentGrouping.State))
        assertTrue(state.machines.isEmpty())
        assertEquals(listOf("state/needs", "state/unread", "state/working"), (state.groups.map { it.id }).zhAny())
    }

    @Test fun oldAndUnknownPreferenceValuesFallBackToTheTree() {
        // A stored Machine preference now opens By Machine (#9319); Space / MachineSpace / unknown stay By project.
        listOf("Space", "MachineSpace", "unknown", "").forEach {
            assertEquals(AgentGrouping.MachineSpace, AgentGrouping.fromPreference(it))
        }
        assertEquals(AgentGrouping.Machine, AgentGrouping.fromPreference("Machine"))
        assertEquals("a phone that never chose gets 按仓库", AgentGrouping.Repo, AgentGrouping.fromPreference(null))
        assertEquals(AgentGrouping.Repo, AgentGrouping.fromPreference("Repo"))
        assertEquals(AgentGrouping.State, AgentGrouping.fromPreference("State"))
        assertEquals(AgentGrouping.Todo, AgentGrouping.fromPreference("Todo"))
        assertEquals(AgentGrouping.Direction, AgentGrouping.fromPreference("Direction"))
        assertEquals(AgentGrouping.App, AgentGrouping.fromPreference("App"))
        assertEquals(AgentGrouping.Recent, AgentGrouping.fromPreference("Recent"))
        assertEquals(
            listOf(AgentGrouping.Repo, AgentGrouping.MachineSpace, AgentGrouping.State, AgentGrouping.Todo,
                AgentGrouping.Machine, AgentGrouping.App, AgentGrouping.Recent, AgentGrouping.Direction),
            AgentGrouping.selectable,
        )
        assertEquals(AgentGrouping.Repo, AgentListSettings().grouping)
    }

    @Test fun foldAllIncludesPinnedAndQuietSpacesAndCanReopenOneQuietSpace() {
        val rows = listOf(row("one", "active", "pinned", "unread"), row("one", "quiet", "idle", "idle"))
        val pins = AgentPreferences(sessions = listOf("one/pinned"))
        val initial = buildAgentList(rows, pins = pins, settings = MachineSpaceTree)
        val settings = AgentListSettings(grouping = AgentGrouping.MachineSpace, folded = (initial.groups.map { it.id } + initial.machines.map { it.id }).toSet())
        val folded = buildAgentList(rows, pins = pins, settings = settings)
        assertTrue(folded.groups.all { it.folded && it.visibleRows.isEmpty() })
        assertTrue(folded.machines.single().folded)
        val reopened = buildAgentList(rows, pins = pins, settings = settings.copy(
            folded = settings.folded - setOf("machine/one", "one/quiet"), expandedQuiet = setOf("one/quiet")))
        assertEquals(listOf("one/idle"), (reopened.groups.flatMap { it.visibleRows }.map { it.id }).zhAny())
        val pinnedOnly = buildAgentList(rows.take(1), pins = pins, settings = MachineSpaceTree)
        assertEquals(1, pinnedOnly.machines.single().sessionCount)
        assertEquals(1, pinnedOnly.machines.single().spaceCount)
        assertEquals(listOf("pinned"), (pinnedOnly.groups.map { it.id }).zhAny())
    }
}

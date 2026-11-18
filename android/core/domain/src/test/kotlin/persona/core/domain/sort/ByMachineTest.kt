package persona.core.domain.sort

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Machine
import persona.core.contracts.Session

/**
 * By Machine (#9319, iOS `DevGrouping.machine` / `machineSections`): one section per computer with live agents, rows in the
 * list's sort order, idle folded behind "N more idle". Preference round-trip and menu order.
 */
class ByMachineTest {
    private val machines = listOf(
        Machine("mac", "MacBook Pro", online = true),
        Machine("box", "Build server", online = true),
        Machine("empty", "Idle rack", online = true),
        Machine("off", "Offline box", online = false),
    )
    private val byMachine = AgentListSettings(grouping = AgentGrouping.Machine)

    private fun row(machine: String, id: String, state: String = "working", date: Int = 1) =
        AgentSession(Session(
            machineId = machine, sessionId = id, title = id, state = state,
            updatedAt = "2026-01-0${date}T00:00:00Z",
        ))

    private fun list(
        rows: List<AgentSession>,
        hosts: List<Machine> = machines,
        settings: AgentListSettings = byMachine,
        pins: AgentPreferences = AgentPreferences(),
        mutes: AgentPreferences = AgentPreferences(),
    ) = buildAgentList(rows, hosts, pins = pins, mutes = mutes, settings = settings)

    @Test fun sectionsArePerComputerWithLiveAgentsInHubOrder() {
        val rows = listOf(
            row("box", "deploy"),
            row("mac", "build"),
            row("off", "old", "idle"),
            row("mac", "review", "idle"),
        )
        val result = list(rows)
        assertEquals(listOf("machine/mac", "machine/box", "machine/off"), result.groups.map { it.id })
        assertTrue(result.groups.none { it.id == "machine/empty" })
        assertEquals("MacBook Pro", result.groups[0].title.shown)
        assertEquals("Build server", result.groups[1].title.shown)
        assertEquals("Offline box", result.groups[2].title.shown)
        assertTrue(result.machines.isEmpty())
        // Shared agents_list fixtures leave state_counts null for Machine groups (#9319 rework).
        assertTrue(result.groups.all { it.stateCounts == null })
    }

    @Test fun recentlyUpdatedPutsTheComputerWithTheLatestAgentFirst() {
        val rows = listOf(
            row("mac", "old", date = 1),
            row("box", "new", date = 5),
        )
        val result = list(rows, settings = byMachine.copy(sort = AgentSort.Updated))
        assertEquals(listOf("machine/box", "machine/mac"), result.groups.map { it.id })
    }

    @Test fun rowsFollowStateSortAndIdleFold() {
        val rows = listOf(
            row("mac", "needs", "needs_you", date = 1),
            row("mac", "work-new", date = 3),
            row("mac", "work-old", date = 2),
            row("mac", "seen", "idle", date = 4),
        )
        val closed = list(rows).groups.single()
        assertEquals(listOf("needs", "work-new", "work-old"), closed.visibleRows.map { it.session.sessionId })
        assertEquals(setOf("seen"), closed.idleFold!!.rows.map { it.session.sessionId }.toSet())
        assertFalse(closed.idleFold!!.open)
        val opened = list(rows, settings = byMachine.copy(expandedIdle = setOf("machine-mac"))).groups.single()
        assertTrue(opened.idleFold!!.open)
        assertEquals(4, opened.visibleRows.size)
        assertNull(list(rows, settings = byMachine.copy(query = "seen")).groups.single().idleFold)
        assertNull(list(rows, settings = byMachine.copy(quick = AgentQuickFilter.Working)).groups.single().idleFold)
    }

    @Test fun aFoldedComputerHidesItsRows() {
        val rows = listOf(row("mac", "a"), row("box", "b"))
        val folded = list(rows, settings = byMachine.copy(folded = setOf("machine/mac")))
        assertTrue(folded.groups.single { it.id == "machine/mac" }.folded)
        assertTrue(folded.groups.single { it.id == "machine/mac" }.visibleRows.isEmpty())
        assertEquals(1, folded.groups.single { it.id == "machine/box" }.visibleRows.size)
    }

    @Test fun subAgentsHangUnderTheirParentAsInOtherGroupings() {
        // Runtime sub-agents hang under their parent (CodexTaskForest), as in the other groupings.
        val parent = AgentSession(Session(machineId = "mac", sessionId = "parent", title = "parent", state = "working",
            sourceKind = "runtime", agent = "claude", cwd = "/code/app", updatedAt = "2026-01-01T00:00:00Z"))
        val child = AgentSession(Session(machineId = "mac", sessionId = "child", title = "child", state = "working",
            sourceKind = "runtime", agent = "claude", isSubagent = true, parentSessionId = "parent",
            cwd = "/code/app", updatedAt = "2026-01-01T00:00:00Z"))
        val result = list(listOf(parent, child))
        assertEquals(listOf("mac/parent"), result.groups.single().rows.map { it.id })
        assertEquals(listOf("mac/child"), result.children["mac/parent"]!!.map { it.id })
    }

    @Test fun storedMachineOpensByMachineAndMenuOrderMatchesIos() {
        assertEquals(AgentGrouping.Machine, AgentGrouping.fromPreference("Machine"))
        assertEquals(AgentGrouping.MachineSpace, AgentGrouping.fromPreference("Space"))
        assertEquals(AgentGrouping.MachineSpace, AgentGrouping.fromPreference("MachineSpace"))
        assertEquals(AgentGrouping.MachineSpace, AgentGrouping.fromPreference("unknown"))
        assertEquals(AgentGrouping.Repo, AgentGrouping.fromPreference(null))
        val offered = AgentGrouping.selectable
        assertEquals(
            listOf(
                AgentGrouping.Repo, AgentGrouping.MachineSpace, AgentGrouping.State, AgentGrouping.Todo,
                AgentGrouping.Machine, AgentGrouping.App, AgentGrouping.Recent, AgentGrouping.Direction,
            ),
            offered,
        )
        assertTrue(offered.indexOf(AgentGrouping.Machine) > offered.indexOf(AgentGrouping.Todo))
        assertEquals(offered.indexOf(AgentGrouping.Machine) + 1, offered.indexOf(AgentGrouping.App))
    }
}

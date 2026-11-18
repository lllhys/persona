package persona.core.domain.sort

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.JsonCodec
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.AgentWorkspace

class PaseoWorkspacePinsTest {
    private val earlier = "2026-11-15T10:00:00Z"
    private val later = "2026-11-16T10:00:00Z"
    private val machine = Machine("computer", "Computer", online = true, engine = "paseo")
    private fun workspace(id: String, pin: String? = null, main: Boolean = false, project: String = "project") =
        AgentWorkspace(id = "computer/paseo/$id", machineId = machine.id, source = "paseo", sourceId = id,
            title = id, cwd = "/code/$project/$id", projectId = project, projectName = project, projectPath = "/code/$project",
            isMain = main, workspaceKind = "git", branch = "main", pinnedAt = pin, online = true, available = true)
    private fun session(id: String, workspace: AgentWorkspace, status: String = "idle") =
        AgentSession(Session(machineId = workspace.machineId, sessionId = id, title = id, cwd = workspace.cwd,
            workspaceId = workspace.sourceId, sourceKind = "runtime", sourceInstanceId = "paseo", spaceKind = "paseo",
            state = status, status = status, repoRoot = workspace.projectPath))

    @Test fun optionalPinDecodesAndInvalidDatesAreNotPins() {
        assertNull(JsonCodec.decodeFromString<AgentWorkspace>("""{"source":"paseo"}""").pinnedAt)
        assertNull(JsonCodec.decodeFromString<AgentWorkspace>("""{"pinned_at":null}""").nativePinTime)
        assertEquals(java.time.Instant.parse(later), JsonCodec.decodeFromString<AgentWorkspace>("""{"pinned_at":"$later"}""").nativePinTime)
        assertNull(workspace("invalid", "not a date").nativePinTime)
    }

    @Test fun pinsPrecedeMainAndOrderByActualInstantWithStableTies() {
        val main = workspace("main", main = true)
        val old = workspace("old", earlier)
        val newest = workspace("newest", "2026-11-16T11:00:00+01:00")
        val tie = workspace("tie", later)
        val plain = workspace("aaa")
        val all = listOf(plain, tie, old, main, newest)
        val rows = all.map { session("agent-${it.sourceId}", it) }
        val expected = listOf("newest", "tie", "old", "main", "aaa")
        assertEquals(expected, agentRepos(rows, all).single().worktrees.map { it.workspace!!.sourceId })
        assertEquals(expected, agentRepos(rows.reversed(), all.reversed()).single().worktrees.map { it.workspace!!.sourceId })
        val cleared = all.map { it.copy(pinnedAt = null) }
        assertEquals("main", agentRepos(rows, cleared).single().worktrees.first().workspace!!.sourceId)
    }

    @Test fun exactWorkspaceIdentitySeparatesSameFolderAndOtherSources() {
        val pinned = workspace("pinned", later)
        val sibling = workspace("sibling").copy(cwd = pinned.cwd)
        val foreign = session("foreign", pinned).let { it.copy(session = it.session.copy(sourceInstanceId = "other", spaceKind = "other")) }
        val rows = listOf(session("chosen", pinned), session("other", sibling), foreign)
        val repos = agentRepos(rows, listOf(pinned, sibling))
        val trees = repos.flatMap { it.worktrees }
        assertEquals(listOf("chosen"), trees.single { it.pinnedAt != null }.members.map { it.session.sessionId })
        assertEquals(listOf("other"), trees.single { it.workspace?.sourceId == "sibling" }.members.map { it.session.sessionId })
        assertEquals(listOf("foreign"), trees.single { it.workspace == null }.members.map { it.session.sessionId })
        assertEquals(trees.size, trees.map { it.id }.distinct().size)
    }

    @Test fun emptyPinnedWorkspaceAndItsRepositoryRemainVisibleBeforeAnActiveRepository() {
        val empty = workspace("empty", later, project = "z-project")
        val busy = AgentSession(Session(machineId = machine.id, sessionId = "busy", cwd = "/code/a-project", state = "working"))
        val settings = AgentListSettings(workspaces = listOf(empty, workspace("empty-unpinned")))
        val list = buildAgentList(listOf(busy), listOf(machine), settings = settings)
        assertEquals(2, list.machines.single().repoCount)
        val pin = list.groups.single { it.worktree?.pinned == true }
        assertTrue(pin.rows.isEmpty())
        assertFalse(pin.folded)
        assertEquals(pin.under, list.groups.first().id)
        assertEquals(1, list.machines.single().sessionCount)
        assertTrue(list.groups.none { it.id.endsWith("empty-unpinned") })
        assertTrue(buildAgentList(emptyList(), listOf(machine), settings = settings).groups.any { it.worktree?.pinned == true })
        val folded = buildAgentList(emptyList(), listOf(machine), settings = settings.copy(folded = setOf(pin.id)))
        assertTrue(folded.groups.single { it.id == pin.id }.folded)
    }

    @Test fun aFilteredSessionFromAnotherEngineCannotAdmitAPinnedWorkspaceWithTheSameId() {
        val native = workspace("shared-id", later)
        val foreign = session("matching-words", native, "working").let {
            it.copy(session = it.session.copy(sourceInstanceId = "other", spaceKind = "other"))
        }
        for (settings in listOf(AgentListSettings(query = "matching-words"), AgentListSettings(quick = AgentQuickFilter.Working))) {
            val list = buildAgentList(listOf(foreign), listOf(machine), settings = settings.copy(workspaces = listOf(native)))
            assertTrue(list.groups.none { it.worktree?.pinned == true })
            assertTrue(list.groups.none { it.id.startsWith("repo/catalog/") })
            assertEquals(listOf(foreign), list.groups.flatMap { it.visibleRows })
        }
    }

    @Test fun stoppedNativePinnedChatsStayVisibleButExplicitFiltersAndArchivesStillApply() {
        val pinned = workspace("pinned", later)
        val sibling = workspace("sibling")
        val stopped = session("stopped", pinned, "offline")
        val ordinary = session("ordinary", sibling, "offline")
        val settings = AgentListSettings(workspaces = listOf(pinned, sibling))
        val rows = listOf(stopped, ordinary)
        val list = buildAgentList(rows, listOf(machine), settings = settings)
        assertEquals(listOf("stopped"), list.groups.flatMap { it.visibleRows }.map { it.session.sessionId })
        assertTrue(buildAgentList(rows, listOf(machine), settings = settings.copy(quick = AgentQuickFilter.Working)).groups.isEmpty())
        assertTrue(buildAgentList(rows, listOf(machine), settings = settings.copy(query = "missing")).groups.isEmpty())
        assertTrue(buildAgentList(rows, listOf(machine), settings = settings.copy(machineFilter = "elsewhere")).groups.isEmpty())
        val archived = stopped.copy(session = stopped.session.copy(archived = true))
        assertTrue(buildAgentList(listOf(archived), listOf(machine), settings = settings).groups.flatMap { it.rows }.isEmpty())
        val unpinned = buildAgentList(rows, listOf(machine), settings = settings.copy(workspaces = listOf(pinned.copy(pinnedAt = null), sibling)))
        assertTrue(unpinned.groups.isEmpty())
    }

    @Test fun personaPinsAreIndependentAndShownOnlyOnce() {
        val native = workspace("native", later)
        val own = session("own", native)
        val prefs = AgentPreferences(sessions = listOf(own.id), spaces = listOf("computer/another-project"))
        val list = buildAgentList(listOf(own), listOf(machine), pins = prefs, settings = AgentListSettings(workspaces = listOf(native)))
        assertEquals(listOf(own), list.groups.single { it.id == "pinned" }.rows)
        assertEquals(1, list.groups.flatMap { it.rows }.count { it.id == own.id })
        assertTrue(list.groups.any { it.worktree?.pinned == true })
        assertEquals(listOf("computer/another-project"), prefs.spaces)
        assertEquals(listOf(own.id), prefs.sessions)
    }
}

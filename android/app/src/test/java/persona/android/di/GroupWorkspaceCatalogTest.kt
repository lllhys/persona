package persona.android.di

import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.*
import persona.feature.agents.AgentsWiringState

/** Partial catalog rows carry the same hub namespace as complete rows, even when both hubs reuse every source ID. */
class GroupWorkspaceCatalogTest {
    private class Catalog(override val scope: String, private val complete: Boolean) : AgentCreationService {
        val calls = mutableListOf<String>()
        override suspend fun workspaceCatalog(machineId: String): AgentWorkspaceCatalog {
            calls += machineId
            return AgentWorkspaceCatalog(listOf(AgentWorkspace(machineId = machineId, source = "paseo", sourceId = "workspace",
                title = scope, cwd = "/code/project", pinnedAt = "2026-11-16T10:00:00Z", online = false, available = false)), complete)
        }
        override suspend fun launchers(machineId: String) = emptyList<AgentLauncher>()
        override suspend fun directories(machineId: String, path: String) = error("Not used")
        override suspend fun start(request: AgentStartRequest, operationId: String) = error("Not used")
        override suspend fun operation(operationId: String) = error("Not used")
    }

    @Test fun mergedCatalogRoutesToItsHubAndQualifiesPartialRows() = runBlocking {
        val home = GroupNames(null)
        val work = GroupNames("g0a1b2c3d")
        val complete = Catalog("home", complete = true)
        val partial = Catalog("work", complete = false)
        assertSame(complete, complete.inGroup(home))
        val merged = requireNotNull(mergedAgentsState(listOf(home, work), listOf(
            AgentsWiringState(scope = "home", creation = complete.inGroup(home)),
            AgentsWiringState(scope = "work", creation = partial.inGroup(work)),
        ), 0).creation)
        val first = merged.workspaceCatalog("computer")
        val second = merged.workspaceCatalog("computer~g0a1b2c3d")
        assertTrue(first.complete)
        assertFalse(second.complete)
        assertEquals(listOf("computer"), complete.calls)
        assertEquals(listOf("computer"), partial.calls)
        assertEquals("computer", first.workspaces.single().machineId)
        assertEquals("computer~g0a1b2c3d", second.workspaces.single().machineId)
        assertEquals("work", second.workspaces.single().title)
        assertEquals("workspace", second.workspaces.single().sourceId)
        assertNotNull(second.workspaces.single().pinnedAt)
        assertFalse(second.workspaces.single().online!!)
        assertTrue(runCatching { merged.workspaceCatalog("computer~g12345678") }.isFailure)
        assertEquals(1, complete.calls.size)
        assertEquals(1, partial.calls.size)
    }
}

package persona.feature.agents

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Machine
import persona.core.contracts.PaseoAgent
import persona.core.contracts.Session
import persona.core.contracts.api.*

/**
 * New Agent's rule for a launcher that lists its own permission modes (#8261, the node's `permission_modes`: Paseo's
 * Grok, OpenCode or Codex), as on the web and iOS: its own approval (sent as no mode) or Allow everything where it lists
 * that, picked on the form and never remembered; only the efforts its model lists, none while that is not known; a
 * Claude Code launcher keeps its five modes and the choice remembered for them.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class NewAgentProviderModesTest {
    private val grok = AgentLauncher("grok", agentType = PaseoAgent, displayName = "Grok", permissionModes = listOf("default", "bypassPermissions"))
    private val kimi = AgentLauncher("kimi", agentType = PaseoAgent, permissionModes = listOf("default"))
    private val codex = AgentLauncher("my-codex", agentType = "codex", promptOptional = true, permissionModes = listOf("default", "bypassPermissions"))
    private val claude = AgentLauncher("claude")
    private val grokModels = LauncherModels(PaseoAgent, listOf(
        AgentModel("grok-5", "Grok 5", isDefault = true, efforts = listOf("low", "high"), defaultEffort = "high"),
        AgentModel("grok-5-fast", "Grok 5 Fast")))

    private class MemoryStore(var saved: AgentDefaults = AgentDefaults()) : AgentCreationStore {
        var operation: AgentStartOperation? = null
        var recents = emptyList<RecentSetup>()
        override suspend fun lastMachine(scope: String) = "computer"
        override suspend fun defaults(scope: String, machine: String) = saved
        override suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults) { saved = defaults }
        override suspend fun pending(scope: String) = operation
        override suspend fun pending(scope: String, value: AgentStartOperation?) { operation = value }
        override suspend fun recentSetups(scope: String) = recents
        override suspend fun updateRecentSetups(scope: String, change: (List<RecentSetup>) -> List<RecentSetup>) = change(recents).also { recents = it }
    }

    private inner class Service(private val models: Map<String, LauncherModels?> = mapOf("grok" to grokModels)) : AgentCreationService {
        override val scope = "hub/pairing"
        val requests = mutableListOf<AgentStartRequest>()
        override suspend fun launchers(machineId: String) = listOf(claude, grok, kimi, codex)
        override suspend fun directories(machineId: String, path: String) = AgentDirectories(path.ifEmpty { "/code/project" })
        override suspend fun start(request: AgentStartRequest, operationId: String): AgentStartResult {
            requests += request
            return AgentStartResult.Created(Session(machineId = request.machineId, sessionId = "started-${requests.size}", cwd = request.cwd))
        }
        override suspend fun operation(operationId: String) = AgentStartResult.Unknown
        override suspend fun launcherModels(machineId: String, launcher: String, refresh: Boolean) = models[launcher]
    }

    private fun TestScope.form(store: MemoryStore, service: Service): NewAgentViewModel {
        val model = NewAgentViewModel(service, store, MutableStateFlow(listOf(Machine("computer", online = true))))
        advanceUntilIdle()
        return model
    }

    private fun TestScope.start(model: NewAgentViewModel, launcher: String, then: NewAgentViewModel.() -> Unit = {}) {
        model.selection(model.state.value.selection.copy(launcher = launcher))
        advanceUntilIdle()
        model.then()
        model.prompt("Sort the photo albums")
        model.submit()
        advanceUntilIdle()
        model.opened()
        advanceUntilIdle()
    }

    @Test fun aProviderStartsOnItsOwnApprovalOrAllowEverythingAndAnEffortItsModelLists() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        // Claude Code's choice remembered on this computer: Plan, xhigh.
        val store = MemoryStore(AgentDefaults("/code/project", "claude", "plan", "xhigh"))
        val service = Service()
        val model = form(store, service)
        try {
            model.selection(model.state.value.selection.copy(launcher = "grok"))
            advanceUntilIdle()
            val state = model.state.value
            assertEquals(listOf("default", "bypassPermissions"), state.providerModes)
            assertFalse(state.claudeModes)
            assertEquals(listOf("low", "high"), state.providerEfforts)
            assertEquals("Grok", state.modelLauncherTitle)
            // Its own approval: no mode and no effort sent, never Claude Code's remembered Plan or xhigh.
            model.prompt("Sort the photo albums")
            model.submit()
            advanceUntilIdle()
            model.opened()
            advanceUntilIdle()
            assertNull(service.requests.last().permissionMode)
            assertNull(service.requests.last().effort)
            // Allow everything and an effort its model lists go as picked.
            start(model, "grok") { providerMode("bypassPermissions"); providerEffort("high") }
            assertEquals("bypassPermissions", service.requests.last().permissionMode)
            assertEquals("high", service.requests.last().effort)
            assertEquals(PaseoAgent, service.requests.last().agentType)
            // Neither is remembered: the computer keeps Claude Code's, and the recent setup has none.
            assertEquals("plan", store.saved.mode)
            assertEquals("xhigh", store.saved.effort)
            assertEquals(listOf("" to ""), store.recents.filter { it.launcher == "grok" }.map { it.mode to it.effort }.distinct())
        } finally {
            model.viewModelScope.cancel()
            Dispatchers.resetMain()
        }
    }

    @Test fun thePickIsMadeEachTimeTheFormOpensAndOnlyWhatTheLauncherListsGoes() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val store = MemoryStore(AgentDefaults("/code/project", "grok"))
        val service = Service()
        val model = form(store, service)
        try {
            model.providerMode("bypassPermissions")
            model.startIn(null)
            assertEquals("", model.state.value.providerMode)
            // One with its own approval alone (kimi) takes no mode, whatever was picked for another; nor an effort, with no
            // list of its models.
            start(model, "kimi") { providerMode("bypassPermissions"); providerEffort("high") }
            assertEquals(listOf("default"), model.state.value.providerModes)
            assertTrue(model.state.value.providerEfforts.isEmpty())
            assertNull(service.requests.last().permissionMode)
            assertNull(service.requests.last().effort)
            // Paseo's Codex lists both: full-access as Allow everything, with Codex's own effort left alone (no list).
            start(model, "my-codex") { providerMode("bypassPermissions") }
            assertEquals("bypassPermissions", service.requests.last().permissionMode)
            assertEquals("codex", service.requests.last().agentType)
        } finally {
            model.viewModelScope.cancel()
            Dispatchers.resetMain()
        }
    }

    @Test fun anEffortTheNextModelDoesNotListGoesBackToItsOwn() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val service = Service()
        val model = form(MemoryStore(AgentDefaults("/code/project", "grok")), service)
        try {
            advanceUntilIdle()
            model.providerEffort("low")
            assertEquals("low", model.state.value.providerEffort)
            // Grok 5 Fast lists no efforts: the pick goes, and the start sends none.
            model.pickModel("grok-5-fast")
            assertEquals("", model.state.value.providerEffort)
            assertTrue(model.state.value.providerEfforts.isEmpty())
        } finally {
            model.viewModelScope.cancel()
            Dispatchers.resetMain()
        }
    }

    @Test fun claudeCodeKeepsItsFiveModesAndTheChoiceRememberedForThem() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val store = MemoryStore(AgentDefaults("/code/project", "claude", "plan", "high"))
        val service = Service()
        val model = form(store, service)
        try {
            assertNull(model.state.value.providerModes)
            assertTrue(model.state.value.claudeModes)
            start(model, "claude")
            assertEquals("plan", service.requests.last().permissionMode)
            assertEquals("high", service.requests.last().effort)
            assertEquals("plan", store.saved.mode)
        } finally {
            model.viewModelScope.cancel()
            Dispatchers.resetMain()
        }
    }
}

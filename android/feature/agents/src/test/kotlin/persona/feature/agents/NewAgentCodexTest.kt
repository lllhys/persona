package persona.feature.agents

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.*
import kotlinx.serialization.encodeToString
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*

@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class NewAgentCodexTest {
    private val computers = listOf(Machine("computer", online = true))

    @Test fun advertisedCodexStartsSynchronouslyWithoutPromptAndAdoptsTheWaitingRow() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val service = Service(optional = true)
        val adopted = mutableListOf<Session>()
        val model = NewAgentViewModel(service, Store(), MutableStateFlow(computers), sender = object : MessageSender {
            override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) =
                error("An empty Codex start must not send a message")
        }, adopt = { adopted += it })
        try {
            advanceUntilIdle()
            assertFalse(model.state.value.promptRequired)
            assertTrue(startable(model.state.value, computers))
            model.prompt("  \n ")
            model.submit()
            advanceUntilIdle()
            val request = requireNotNull(service.request)
            val wire = JsonCodec.encodeToString(request)
            assertEquals("codex", request.launcher)
            assertNull(request.prompt)
            assertNull(request.async)
            assertFalse(wire.contains("\"prompt\""))
            assertFalse(wire.contains("\"async\""))
            assertEquals(service.waiting, adopted.single())
            assertEquals(service.waiting, model.state.value.created)
        } finally { model.viewModelScope.cancel(); Dispatchers.resetMain() }
    }

    @Test fun anOlderComputerRequiresTheFirstMessageInBothTheFormAndSubmit() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val service = Service(optional = false)
        val model = NewAgentViewModel(service, Store(), MutableStateFlow(computers))
        try {
            advanceUntilIdle()
            assertTrue(model.state.value.promptRequired)
            assertFalse(startable(model.state.value, computers))
            model.submit()
            advanceUntilIdle()
            assertNull(service.request)
            model.prompt("  Review the changes  ")
            assertTrue(startable(model.state.value, computers))
            model.submit()
            advanceUntilIdle()
            assertEquals("Review the changes", service.request?.prompt)
            assertNull(service.request?.async)
        } finally { model.viewModelScope.cancel(); Dispatchers.resetMain() }
    }

    @Test fun followingConfigurationUsesThatLaunchersCapabilityOnThatComputer() {
        val codex = AgentLauncher("codex", agentType = "codex", promptOptional = true)
        val configured = NewAgentState(initialized = true, machineId = "computer", selection = AgentDefaults("/code", ""),
            launchers = listOf(codex), modelConfig = NewAgentModelConfig.Follow(ModelPurposeDetail(
                ModelPurpose("agent.new", effective = ModelBinding("launcher.codex", "")))))
        assertFalse(configured.promptRequired)
        assertTrue(startable(configured, computers))
        assertTrue(configured.copy(launchers = listOf(codex.copy(promptOptional = false))).promptRequired)
        val claude = configured.copy(selection = AgentDefaults("/code", "claude"), launchers = listOf(AgentLauncher("claude")))
        assertFalse(claude.promptRequired)
    }

    private class Service(optional: Boolean) : AgentCreationService {
        override val scope = "hub/pairing"
        private val launcher = JsonCodec.decodeFromString<AgentLauncher>(
            """{"name":"codex","agent":"codex","prompt_optional":$optional}""")
        var request: AgentStartRequest? = null
        val waiting = Session("computer", "waiting", agent = "codex", status = "idle", paneId = "pane", awaitingFirstMessage = true)
        override suspend fun launchers(machineId: String) = listOf(launcher)
        override suspend fun directories(machineId: String, path: String) = AgentDirectories("/code")
        override suspend fun start(request: AgentStartRequest, operationId: String): AgentStartResult {
            this.request = request
            return AgentStartResult.Created(waiting)
        }
        override suspend fun operation(operationId: String) = AgentStartResult.Unknown
    }
    private class Store : AgentCreationStore {
        private var operation: AgentStartOperation? = null
        override suspend fun lastMachine(scope: String) = "computer"
        override suspend fun defaults(scope: String, machine: String) = AgentDefaults()
        override suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults) = Unit
        override suspend fun pending(scope: String) = operation
        override suspend fun pending(scope: String, value: AgentStartOperation?) { operation = value }
    }
}

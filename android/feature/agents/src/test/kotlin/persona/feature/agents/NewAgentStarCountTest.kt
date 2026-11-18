package persona.feature.agents

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.*
import java.io.IOException

/**
 * What the GitHub star card counts (#5761): an Agent the hub took from a start sent now, once; never a resume, and never a
 * start checked again after its answer went missing.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class NewAgentStarCountTest {
    private val machines = MutableStateFlow(listOf(Machine("first", online = true)))

    private fun TestScope.counted(service: Service, act: TestScope.(NewAgentViewModel) -> Unit): Int {
        var launches = 0
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val model = NewAgentViewModel(service, Store(), machines, launched = { launches++ })
        try {
            advanceUntilIdle()
            model.selection(model.state.value.selection.copy(cwd = "/code/project"))
            act(model)
            advanceUntilIdle()
        } finally {
            model.viewModelScope.cancel()
            Dispatchers.resetMain()
        }
        return launches
    }

    @Test fun aStartTheHubTookCountsOnce() = runTest {
        val service = Service()
        assertEquals(1, counted(service) { it.submit() })
        assertEquals(1, service.starts)
    }

    @Test fun aStartCheckedAgainIsNotCounted() = runTest {
        val service = Service().apply { lose = true }
        assertEquals(0, counted(service) { model ->
            model.submit()
            advanceUntilIdle()
            service.lose = false
            model.submit()
        })
        assertEquals(1, service.starts)
        assertEquals(1, service.checks)
    }

    @Test fun aResumeIsNotCounted() = runTest {
        val history = Session(machineId = "first", sessionId = "history", status = "offline", cwd = "/code/project", launcher = "claude")
        assertEquals(0, counted(Service()) { it.resume(history) })
    }

    private class Service : AgentCreationService {
        override val scope = "hub/pairing"
        var starts = 0
        var checks = 0
        var lose = false
        override suspend fun launchers(machineId: String) = listOf(AgentLauncher("claude"))
        override suspend fun directories(machineId: String, path: String) = AgentDirectories(path.ifEmpty { "/code" })
        override suspend fun start(request: AgentStartRequest, operationId: String): AgentStartResult {
            starts++
            if (lose) throw IOException("Lost response")
            return AgentStartResult.Created(Session(machineId = "first", sessionId = "s$starts"))
        }
        override suspend fun operation(operationId: String): AgentStartResult {
            checks++
            return AgentStartResult.Created(Session(machineId = "first", sessionId = "checked"))
        }
    }

    private class Store : AgentCreationStore {
        private var operation: AgentStartOperation? = null
        override suspend fun lastMachine(scope: String) = "first"
        override suspend fun defaults(scope: String, machine: String) = AgentDefaults()
        override suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults) = Unit
        override suspend fun pending(scope: String) = operation
        override suspend fun pending(scope: String, value: AgentStartOperation?) { operation = value }
    }
}

package persona.feature.agents

import androidx.lifecycle.viewModelScope
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test
import java.time.Instant

/**
 * New Agent's "Recent" cards and the memory around them (#2134): a card fills every field and never starts, an offline
 * machine's card does nothing, picking a machine brings back its model, a start is recorded, and a to-do's
 * "Start a new one" starts from the same memory.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class NewAgentRecentTest {
    private val at = Instant.parse("2026-11-09T05:00:00.250Z")
    private val claudeModels = LauncherModels("claude", listOf(
        AgentModel("default", "Default", isDefault = true, efforts = listOf("low", "medium", "high")),
        AgentModel("opus", "Opus", efforts = listOf("low", "medium", "high", "xhigh")),
        AgentModel("sonnet", "Sonnet", efforts = listOf("low", "medium", "high", "xhigh")),
        AgentModel("haiku", "Haiku"),
    ))
    private val codexModels = LauncherModels("codex", listOf(
        AgentModel("gpt-5.5", "GPT-5.5", isDefault = true), AgentModel("gpt-5.5-mini", "GPT-5.5-Mini"),
    ))

    private fun test(body: suspend TestScope.() -> Unit) = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        try { body() } finally { Dispatchers.resetMain() }
    }

    private fun online(vararg ids: String) = MutableStateFlow(ids.map { Machine(it, online = true) })

    @Test fun aCardFillsEveryFieldAndNeverStarts() = test {
        val store = MemoryStore().apply {
            saved["first"] = AgentDefaults("/code/persona", "claude", "plan", "low")
            recents = listOf(
                RecentSetup("second", "/code/notes-app", "work", "sonnet", "claude", "bypassPermissions", "xhigh", "2026-11-09T02:00:00Z"),
                RecentSetup("second", "/code/data-pipeline", "codex", "gpt-5.5-mini", "codex", at = "2026-11-09T01:00:00Z"),
            )
        }
        val service = Service()
        val model = NewAgentViewModel(service, store, online("first", "second"))
        try {
            advanceUntilIdle()
            assertEquals("first", model.state.value.machineId)
            assertEquals(0, model.state.value.focusPrompt)

            model.useRecent(store.recents[0])
            advanceUntilIdle()
            model.state.value.let { state ->
                assertEquals("second", state.machineId)
                assertEquals(AgentDefaults("/code/notes-app", "work", "bypassPermissions", "xhigh"), state.selection)
                assertEquals("sonnet", state.model)
                assertEquals("sonnet", state.sentModel)
                assertEquals(store.recents[0], state.pickedSetup)
                assertEquals(1, state.focusPrompt)
                assertNull(state.pending)
            }

            // Another card on the same machine fills it at once; a Codex card brings no mode or effort of its own.
            model.useRecent(store.recents[1])
            advanceUntilIdle()
            model.state.value.let { state ->
                assertEquals("/code/data-pipeline", state.selection.cwd)
                assertEquals("codex", state.selection.launcher)
                assertEquals("bypassPermissions", state.selection.mode)
                assertEquals("codex", state.agentType)
                assertEquals("gpt-5.5-mini", state.sentModel)
                assertEquals(store.recents[1], state.pickedSetup)
                assertEquals(2, state.focusPrompt)
            }
            assertEquals("Filling the form never starts", 0, service.starts)
            assertNull(store.operation)

            // Changing a field leaves the card.
            model.selection(model.state.value.selection.copy(cwd = "/code/other"))
            assertNull(model.state.value.pickedSetup)
        } finally {
            model.viewModelScope.cancel()
        }
    }

    @Test fun aCardFollowsTheConfigurationOnlyWhereThereIsOne() = test {
        val follow = RecentSetup("first", "/code/persona", "", "", "claude", "plan", "high")
        val gone = RecentSetup("first", "/code/persona", "retired", "opus", "claude", "plan", "high")
        val following = Service().apply {
            model = ModelPurposeDetail(ModelPurpose("agent.new", effective = ModelBinding("launcher.claude", "sonnet")),
                listOf(ModelProvider("launcher.claude", "claude")))
        }
        val withConfig = NewAgentViewModel(following, MemoryStore(), online("first"))
        val older = NewAgentViewModel(Service(), MemoryStore().apply { saved["first"] = AgentDefaults("/code", "work") }, online("first"))
        try {
            advanceUntilIdle()
            withConfig.selection(withConfig.state.value.selection.copy(launcher = "work"))
            withConfig.useRecent(follow)
            advanceUntilIdle()
            assertTrue(withConfig.state.value.following)
            assertEquals("plan", withConfig.state.value.selection.mode)

            // An older hub can't follow, and a launcher the machine no longer has can't run: the machine's stays.
            older.useRecent(follow)
            advanceUntilIdle()
            assertEquals("work", older.state.value.selection.launcher)
            assertEquals("/code/persona", older.state.value.selection.cwd)
            older.useRecent(gone)
            advanceUntilIdle()
            assertEquals("work", older.state.value.selection.launcher)
            assertEquals("", older.state.value.model)
        } finally {
            withConfig.viewModelScope.cancel()
            older.viewModelScope.cancel()
        }
    }

    @Test fun pickingAMachineBringsBackItsModel() = test {
        val store = MemoryStore().apply {
            saved["first"] = AgentDefaults("/code/persona", "claude")
            saved["second"] = AgentDefaults("/code/notes-app", "work")
            recents = listOf(
                RecentSetup("second", "/code/notes-app", "claude", "haiku"),
                RecentSetup("second", "/code/notes-app", "work", "sonnet"),
                RecentSetup("first", "/code/persona", "claude", "opus"),
            )
        }
        val model = NewAgentViewModel(Service(), store, online("first", "second", "third"))
        try {
            advanceUntilIdle()
            assertEquals("opus", model.state.value.sentModel)
            model.selectMachine("second")
            advanceUntilIdle()
            assertEquals("work", model.state.value.selection.launcher)
            assertEquals("The latest setup with the machine's launcher", "sonnet", model.state.value.sentModel)
            model.selectMachine("third")
            advanceUntilIdle()
            assertEquals("", model.state.value.model)
            model.selectMachine("first")
            advanceUntilIdle()
            assertEquals("opus", model.state.value.sentModel)
        } finally {
            model.viewModelScope.cancel()
        }
    }

    @Test fun anOfflineMachinesCardCannotBePicked() = test {
        val card = RecentSetup("second", "/code/data-pipeline", "codex", "gpt-5.5", "codex")
        val store = MemoryStore().apply { recents = listOf(card) }
        val machines = MutableStateFlow(listOf(Machine("first", online = true), Machine("second", online = false)))
        val model = NewAgentViewModel(Service(), store, machines)
        try {
            advanceUntilIdle()
            val before = model.state.value
            assertEquals(listOf(RecentCard(0, card, online = false)), RecentSetups.cards(before.recentSetups, machines.value))
            model.useRecent(card)
            advanceUntilIdle()
            assertEquals(before, model.state.value)
        } finally {
            model.viewModelScope.cancel()
        }
    }

    @Test fun aStartIsRecordedWithItsTypeAndTheModelSent() = test {
        val store = MemoryStore().apply { saved["first"] = AgentDefaults("/code/persona", "claude") }
        val service = Service()
        val machines = online("first")
        val model = NewAgentViewModel(service, store, machines, now = { at })
        try {
            advanceUntilIdle()
            model.selection(model.state.value.selection.copy(mode = "plan", effort = "high"))
            advanceUntilIdle()
            model.pickModel("opus")
            model.submit()
            advanceUntilIdle()
            val claude = RecentSetup("first", "/code/persona", "claude", "opus", "claude", "plan", "high", "2026-11-09T05:00:00Z")
            assertEquals(listOf(claude), store.recents)
            assertEquals(store.recents, model.state.value.recentSetups)
            model.opened()
            advanceUntilIdle()

            model.selection(model.state.value.selection.copy(launcher = "codex"))
            model.prompt("Summarize the logs")
            advanceUntilIdle()
            model.submit()
            advanceUntilIdle()
            val codex = RecentSetup("first", "/code/persona", "codex", "", "codex", "", "", "2026-11-09T05:00:00Z")
            assertEquals(listOf(codex, claude), store.recents)
            model.opened()
            advanceUntilIdle()

            // A tab in an existing space is kept too, with the space's folder (as on iOS, the Mac and the web).
            model.startIn(AgentSpacePreset("first", "w1", "/code/space", "space"))
            advanceUntilIdle()
            model.prompt("Review the space")
            model.submit()
            advanceUntilIdle()
            val space = store.recents.first()
            assertEquals(listOf("first", "/code/space"), listOf(space.machine, space.cwd))
            assertEquals(listOf(space, codex, claude), store.recents)
            model.opened()
            advanceUntilIdle()
            // A resume is not a setup of this form.
            model.resume(Session(machineId = "first", sessionId = "old", cwd = "/code/history", launcher = "claude"))
            advanceUntilIdle()
            assertEquals(4, service.starts)
            assertEquals(listOf(space, codex, claude), store.recents)
        } finally {
            model.viewModelScope.cancel()
        }
    }

    @Test fun removeOneOrClearAll() = test {
        val a = RecentSetup("first", "/code/a")
        val b = RecentSetup("first", "/code/b")
        val store = MemoryStore().apply { recents = listOf(a, b) }
        val model = NewAgentViewModel(Service(), store, online("first"))
        try {
            advanceUntilIdle()
            model.useRecent(b)
            advanceUntilIdle()
            model.removeRecent(b.copy(at = "2026-11-09T00:00:00Z"))
            advanceUntilIdle()
            assertEquals(listOf(a), store.recents)
            assertEquals(listOf(a), model.state.value.recentSetups)
            assertNull("The removed card is no longer picked", model.state.value.pickedSetup)
            model.clearRecent()
            advanceUntilIdle()
            assertTrue(store.recents.isEmpty())
            assertTrue(model.state.value.recentSetups.isEmpty())
        } finally {
            model.viewModelScope.cancel()
        }
    }

    @Test fun theToDoHandOffStartsFromNewAgentsMemory() = test {
        val store = MemoryStore(last = "second").apply {
            saved["second"] = AgentDefaults("/code/notes-app", "", "plan", "high")
            saved["third"] = AgentDefaults("/code/data-pipeline", "codex", "", "")
        }
        val machines = listOf(Machine("first", online = true), Machine("second", online = true), Machine("third", online = true))
        assertEquals("second", handOffMachine(store, "hub", machines)?.id)
        assertEquals(HandOffDefaults("/code/notes-app", "", "plan"), handOffDefaults(store, "hub", "second"))
        assertEquals(HandOffDefaults("/code/data-pipeline", "codex", "default"), handOffDefaults(store, "hub", "third"))
        assertEquals("Nothing remembered: as before", HandOffDefaults("~", "claude", "default"), handOffDefaults(store, "hub", "first"))

        val offline = machines.map { if (it.id == "second") it.copy(online = false) else it }
        assertEquals("first", handOffMachine(store, "hub", offline)?.id)
        assertEquals("second", handOffMachine(store, "hub", listOf(Machine("second", online = false)))?.id)
        assertEquals("first", handOffMachine(null, "", machines)?.id)
        assertEquals(HandOffDefaults(), handOffDefaults(null, "", "second"))
    }

    @Test fun theHandOffKeepsThePickedAgentWhileTheListReSorts() {
        val a = Session(machineId = "m", sessionId = "a")
        val b = Session(machineId = "m", sessionId = "b")
        val c = Session(machineId = "other", sessionId = "c")
        assertEquals("Nothing picked: the first", a.key, handOffTarget(listOf(a, b, c), null))
        assertEquals(b.key, handOffTarget(listOf(a, b, c), b.key))
        assertEquals("Another agent updated and moved up: the pick stays", b.key, handOffTarget(listOf(c, a, b), b.key))
        assertEquals("The picked agent stopped: the first", c.key, handOffTarget(listOf(c, a), b.key))
        assertEquals(null, handOffTarget(emptyList(), b.key))

        // The list is empty for a moment (a refresh, a reconnect): the pick survives and comes back with B.
        val kept = handOffPick(b.key, handOffTarget(emptyList(), b.key))
        assertEquals(b.key, kept)
        assertEquals(b.key, handOffTarget(listOf(a, b), kept))
        assertEquals(c.key, handOffPick(b.key, c.key))
    }

    private class MemoryStore(var last: String = "first") : AgentCreationStore {
        var operation: AgentStartOperation? = null
        val saved = mutableMapOf<String, AgentDefaults>()
        var recents = emptyList<RecentSetup>()
        override suspend fun lastMachine(scope: String) = last
        override suspend fun defaults(scope: String, machine: String) = saved[machine] ?: AgentDefaults()
        override suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults) { saved[machine] = defaults; last = machine }
        override suspend fun pending(scope: String) = operation
        override suspend fun pending(scope: String, value: AgentStartOperation?) { operation = value }
        override suspend fun recentSetups(scope: String) = recents
        override suspend fun updateRecentSetups(scope: String, change: (List<RecentSetup>) -> List<RecentSetup>) =
            change(recents).also { recents = it }
    }

    private inner class Service : AgentCreationService {
        override val scope = "hub/pairing"
        var starts = 0
        var model: ModelPurposeDetail? = null
        override suspend fun launchers(machineId: String) =
            listOf(AgentLauncher("claude"), AgentLauncher("work"), AgentLauncher("codex", agentType = "codex"))
        override suspend fun directories(machineId: String, path: String) = AgentDirectories(path.ifEmpty { "/code" })
        override suspend fun start(request: AgentStartRequest, operationId: String): AgentStartResult {
            starts++
            return AgentStartResult.Created(Session(machineId = request.machineId, sessionId = "s$starts"))
        }
        override suspend fun operation(operationId: String): AgentStartResult = AgentStartResult.Unknown
        override suspend fun newAgentModel(): ModelPurposeDetail? = model
        override suspend fun launcherModels(machineId: String, launcher: String, refresh: Boolean) =
            if (launcher == "codex") codexModels else claudeModels
    }
}

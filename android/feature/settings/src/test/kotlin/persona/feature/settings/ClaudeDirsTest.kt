package persona.feature.settings

import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Machine
import persona.core.contracts.api.AgentLauncher
import persona.core.contracts.api.ClaudeDir
import persona.core.contracts.api.ClaudeDirsState
import persona.core.contracts.api.ClaudeSetting
import persona.core.contracts.api.HerdrState
import persona.core.contracts.api.LauncherQuotaAnswer
import persona.core.contracts.api.MachineManagement

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ClaudeDirsTest {
    private class FakeApi : MachineManagement {
        val reads = mutableListOf<String>()
        val writes = mutableListOf<Pair<String, List<String>>>()
        var readResult: suspend (String) -> ClaudeDirsState = { id -> stateFor(id) }
        var writeResult: suspend (String, List<String>) -> ClaudeDirsState = { id, blocked ->
            stateFor(id).copy(blocked = blocked)
        }

        override suspend fun claudeDirs(machineId: String): ClaudeDirsState {
            reads += machineId
            return readResult(machineId)
        }

        override suspend fun setBlockedClaudeDirs(machineId: String, blocked: List<String>): ClaudeDirsState {
            writes += machineId to blocked
            return writeResult(machineId, blocked)
        }

        override suspend fun claudeSettings(machineId: String) = emptyList<ClaudeSetting>()
        override suspend fun setClaudeSetting(machineId: String, key: String, value: kotlinx.serialization.json.JsonElement) = emptyList<ClaudeSetting>()
        override suspend fun codexSettings(machineId: String) = emptyList<ClaudeSetting>()
        override suspend fun setCodexSetting(machineId: String, key: String, value: kotlinx.serialization.json.JsonElement) = emptyList<ClaudeSetting>()
        override suspend fun launchers(machineId: String) = emptyList<AgentLauncher>()
        override suspend fun launcherQuota(machineId: String, name: String, refresh: Boolean) = LauncherQuotaAnswer()
        override suspend fun reconnect(machineId: String) = Unit
        override suspend fun deploy(machineId: String) = Unit
        override suspend fun herdr(machineId: String): HerdrState = error("unused")
        override suspend fun startHerdr(machineId: String): HerdrState = error("unused")
        override suspend fun setEngine(machineId: String, engine: String) = Machine(machineId)

        companion object {
            fun stateFor(id: String) = ClaudeDirsState(
                dirs = listOf(
                    ClaudeDir("/home/$id/.claude", isDefault = true, source = "default", sessions = 1),
                    ClaudeDir("/home/$id/.automation", blocked = true, blockedBy = "~/.automation", source = "found", sessions = 2),
                ),
                blocked = listOf("~/.automation"),
                home = "/home/$id",
                live = true,
            )
        }
    }

    @Test fun anotherComputersReadingIsDrawnAsThisOneStillReading() {
        val boxState = ClaudeDirsUiState(
            machineId = "box",
            state = FakeApi.stateFor("box"),
            busy = "/home/box/.automation",
            error = ResText.of(R.string.settings_src_saved),
        )
        assertEquals(boxState, boxState.forMachine("box"))
        val picked = boxState.forMachine("studio")
        assertEquals("studio", picked.machineId)
        assertNull(picked.state)
        assertTrue(picked.loading)
        assertNull(picked.busy)
        assertNull(picked.error)
        assertFalse(picked.unsupported)
    }

    @Test fun aReadingOfThePreviousComputerDoesNotArriveOverTheOnePickedNext() = runBlocking {
        val held = CompletableDeferred<ClaudeDirsState>()
        val api = FakeApi().apply { readResult = { if (it == "box") held.await() else FakeApi.stateFor(it) } }
        val model = ClaudeDirsModel(api)
        val slow = launch { model.load("box") }
        while (api.reads.isEmpty()) kotlinx.coroutines.yield()

        model.load("studio")
        assertEquals("studio", model.state.value.machineId)
        assertEquals(FakeApi.stateFor("studio"), model.state.value.state)
        held.complete(FakeApi.stateFor("box"))
        slow.join()
        assertEquals("studio", model.state.value.machineId)
        assertEquals(FakeApi.stateFor("studio"), model.state.value.state)
        assertFalse(model.state.value.loading)
        assertNull(model.state.value.error)
    }

    @Test fun switchThenActBeforeLoadReturnsSendsNothingAndDrawsNoOldRows() = runBlocking {
        val held = CompletableDeferred<ClaudeDirsState>()
        val api = FakeApi().apply { readResult = { if (it == "studio") held.await() else FakeApi.stateFor(it) } }
        val model = ClaudeDirsModel(api)
        model.load("box")
        val boxDirs = FakeApi.stateFor("box")
        assertEquals(boxDirs, model.state.value.state)

        // Owner picked another computer; its read is still out. forMachine must hide the old rows, and a save aimed
        // at the shown computer must no-op (nothing goes to box, and studio is not written either).
        val slow = launch { model.load("studio") }
        while (api.reads.lastOrNull() != "studio") kotlinx.coroutines.yield()

        val drawn = model.state.value.forMachine("studio")
        assertNull(drawn.state)
        assertTrue(drawn.loading)
        assertEquals("studio", drawn.machineId)

        val oldBlocked = ClaudeDirsLogic.toggled(boxDirs, boxDirs.dirs[1])
        assertFalse(model.save("studio", oldBlocked, boxDirs.dirs[1].path))
        assertFalse(model.save("box", oldBlocked, boxDirs.dirs[1].path))
        assertTrue(api.writes.isEmpty())

        held.complete(FakeApi.stateFor("studio"))
        slow.join()
        assertEquals(FakeApi.stateFor("studio"), model.state.value.state)
        assertFalse(model.state.value.loading)
    }

    @Test fun aSaveOnThePreviousComputerFinishingLateDoesNotArriveOverTheOnePickedNext() = runBlocking {
        val held = CompletableDeferred<ClaudeDirsState>()
        val api = FakeApi().apply {
            writeResult = { id, blocked ->
                if (id == "box") held.await() else FakeApi.stateFor(id).copy(blocked = blocked)
            }
        }
        val model = ClaudeDirsModel(api)
        model.load("box")
        val boxDirs = FakeApi.stateFor("box")
        val nextBlocked = ClaudeDirsLogic.toggled(boxDirs, boxDirs.dirs[1])
        val saving = async { model.save("box", nextBlocked, boxDirs.dirs[1].path) }
        while (model.state.value.busy == null) kotlinx.coroutines.yield()

        model.load("studio")
        assertEquals("studio", model.state.value.machineId)
        held.complete(FakeApi.stateFor("box").copy(blocked = nextBlocked))
        assertFalse(saving.await())
        assertEquals("studio", model.state.value.machineId)
        assertEquals(FakeApi.stateFor("studio"), model.state.value.state)
        assertNull(model.state.value.busy)
        assertNull(model.state.value.error)
        assertEquals(listOf("box" to nextBlocked), api.writes)
    }

    @Test fun saveTargetsTheShownComputer() = runBlocking {
        val api = FakeApi()
        val model = ClaudeDirsModel(api)
        model.load("box")
        val boxDirs = FakeApi.stateFor("box")
        val next = ClaudeDirsLogic.toggled(boxDirs, boxDirs.dirs[1])
        assertTrue(model.save("box", next, boxDirs.dirs[1].path))
        assertEquals(listOf("box" to next), api.writes)
        assertEquals(next, model.state.value.state?.blocked)
        assertNull(model.state.value.error)
    }
}

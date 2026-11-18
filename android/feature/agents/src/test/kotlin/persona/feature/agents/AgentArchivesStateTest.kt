package persona.feature.agents

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.AgentArchives

class AgentArchivesStateTest {
    @Test fun bothSourcesArchiveAndRestoreWithoutChangingTheirIdentity() = runTest {
        val api = ArchiveFake()
        val changed = mutableListOf<Pair<SessionKey, Boolean>>()
        val state = AgentArchivesState(api, this) { key, archived -> changed += key to archived }
        for (session in api.source) {
            state.archive(session)
            advanceUntilIdle()
            assertTrue(state.state.value.sessions.any { it.key == session.key && it.archived })
            state.restore(session)
            advanceUntilIdle()
            assertFalse(state.state.value.sessions.any { it.key == session.key })
        }
        assertEquals(api.source.flatMap { listOf(it.key to true, it.key to false) }, changed)
        assertNull(state.state.value.error)
    }

    @Test fun failedWritesKeepTheArchivedRowAndCanBeRetried() = runTest {
        val api = ArchiveFake().apply { rows = source.map { it.copy(archived = true) } }
        val state = AgentArchivesState(api, this) { _, _ -> }
        state.refresh(); advanceUntilIdle()
        api.failWrite = true
        state.restore(api.source.first()); advanceUntilIdle()
        assertEquals(2, state.state.value.sessions.size)
        assertNotNull(state.state.value.error)
        assertNull(state.state.value.busy)
        api.failWrite = false
        state.restore(api.source.first()); advanceUntilIdle()
        assertEquals(listOf("codex"), state.state.value.sessions.map { it.agent })
        assertNull(state.state.value.error)
    }

    @Test fun refreshFailureDoesNotPretendConfirmedRestoreFailed() = runTest {
        val api = ArchiveFake().apply { rows = source.map { it.copy(archived = true) } }
        val state = AgentArchivesState(api, this) { _, _ -> throw IllegalStateException("Refresh unavailable") }
        state.refresh(); advanceUntilIdle()
        state.restore(api.source.first()); advanceUntilIdle()
        assertEquals(1, state.state.value.sessions.size)
        assertEquals(1, api.rows.size)
        assertNotNull(state.state.value.error)
        api.failRead = true
        state.refresh(); advanceUntilIdle()
        assertEquals(1, state.state.value.sessions.size)
    }

    @Test fun repeatedClicksDoNotRaceWritesOrRefresh() = runTest {
        val api = ArchiveFake().apply { gate = CompletableDeferred() }
        val state = AgentArchivesState(api, this) { _, _ -> }
        state.archive(api.source.first())
        state.archive(api.source.first())
        state.refresh()
        assertNotNull(state.state.value.busy)
        api.gate!!.complete(Unit)
        advanceUntilIdle()
        assertEquals(1, api.writes)
        assertEquals(1, api.reads)
    }

    /** #8143: any agent archives, as the manager and the web take them all: a Paseo provider's (Grok's) too, idle only. */
    @Test fun aPaseoProvidersAgentArchivesToo() {
        val grok = Session("computer", "paseo-1", title = "Sort the albums", agent = "paseo", agentLabelHint = "Grok", launcher = "grok",
            sourceKind = "runtime", spaceKind = "paseo", status = "idle", state = "idle", cwd = "/projects/albums")
        assertTrue(grok.canArchiveAgent)
        assertTrue(grok.copy(agent = "dsh", agentLabelHint = "DSH").canArchiveAgent)
        assertTrue(grok.copy(status = "offline", state = "offline").canArchiveAgent)
        assertFalse(grok.copy(status = "working", state = "working").canArchiveAgent)
        assertFalse(grok.copy(pendingInteractions = 1).canArchiveAgent)
        assertFalse(grok.copy(assistant = true).canArchiveAgent)
    }

    @Test fun unsupportedHubNeverWritesAndActiveWorkHasNoArchiveAction() = runTest {
        val api = ArchiveFake().apply { supported = false }
        val state = AgentArchivesState(api, this) { _, _ -> fail("No change expected") }
        state.archive(api.source.first()); advanceUntilIdle()
        assertEquals(0, api.writes)
        for (session in api.source) {
            assertTrue(session.canArchiveAgent)
            listOf(session.copy(assistant = true), session.copy(starting = true), session.copy(status = "working"),
                session.copy(state = "needs_you"), session.copy(backgroundWorking = true), session.copy(archived = true),
                session.copy(awaitsReply = true), session.copy(pendingInteractions = 1), session.copy(runningSubagents = 1),
                session.copy(subagents = listOf(persona.core.contracts.Subagent("child", running = true))))
                .forEach { assertFalse(it.canArchiveAgent) }
        }
    }
}

/** The hub's archive over a Claude and a Codex session, and [extra] ones a test adds (a Paseo provider's, #8143). */
internal class ArchiveFake(extra: List<Session> = emptyList()) : AgentArchives {
    override var supported = true
    val source = listOf(
        Session("computer", "claude-session", title = "Claude history", agent = "claude", status = "offline", state = "offline", cwd = "/projects/app"),
        Session("computer", "codex-session", title = "Codex history", agent = "codex", sourceKind = "codex_history", status = "offline", state = "offline", cwd = "/projects/api"),
    ) + extra
    var rows = emptyList<Session>()
    var failRead = false
    var failWrite = false
    var writes = 0
    var reads = 0
    var gate: CompletableDeferred<Unit>? = null
    override suspend fun list(): List<Session> {
        reads++
        check(!failRead) { "Archive list unavailable" }
        return rows
    }
    override suspend fun archive(key: SessionKey) {
        writes++; gate?.await()
        check(!failWrite) { "Archive unavailable" }
        rows = rows.filterNot { it.key == key } + source.first { it.key == key }.copy(archived = true)
    }
    override suspend fun restore(key: SessionKey) {
        writes++; gate?.await()
        check(!failWrite) { "Restore unavailable" }
        rows = rows.filterNot { it.key == key }
    }
}

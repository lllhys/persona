package persona.core.data

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/** A session the hub just started and a machine the hub just changed show before the next push (docs/protocol/protocol.md). */
@OptIn(ExperimentalCoroutinesApi::class)
class SessionsAdoptTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test fun waitingAndReturnedThreadMetadataStayAvailableOutsideTheFilteredWindow() = runTest {
        val transport = Transport().apply { paged = true }
        val repository = HubSessionsRepository(transport, Connection(), backgroundScope)
        repository.setListFilter(SessionListFilter(state = "working"))
        val waiting = Session("computer", "waiting", agent = "codex", status = "idle", paneId = "pane", awaitingFirstMessage = true)
        val thread = waiting.copy(sessionId = "thread", awaitingFirstMessage = false)
        repository.adopt(waiting)
        repository.adoptThread(waiting.key, thread)
        advanceTimeBy(100); runCurrent()
        assertTrue(repository.sessions.value.isEmpty())
        assertEquals(0, repository.listStatus.value.total)
        assertEquals(listOf(waiting, thread), repository.adoptedSessions.value)
        assertEquals(thread, repository.startedThreads.value[waiting.key])
    }

    @Test fun adoptedSessionsAreListedOnceAndKeptThroughARefreshInFlight() = runTest {
        val directory = temporary.newFolder()
        val transport = Transport()
        val repository = HubSessionsRepository(transport, Connection(), backgroundScope, ListSnapshotStore(directory))
        repository.refresh()
        val started = Session("mac", "new", status = "idle", starting = true)
        // A refresh that began before the start answered must not drop what the start returned.
        val answer = CompletableDeferred<List<Session>>()
        transport.listFetch = { answer.await() }
        val refresh = async { repository.refresh() }
        runCurrent()
        repository.adopt(started)
        repository.adopt(started.copy(title = "again"))
        answer.complete(emptyList())
        refresh.await()
        assertEquals(listOf(started), repository.sessions.value)
        // The push for that machine replaces it as usual.
        transport.listFetch = null
        transport.list = listOf(started.copy(starting = false, status = "working"))
        repository.refresh()
        assertEquals(listOf("working"), repository.sessions.value.map { it.status })
        assertEquals(listOf("working"), ListSnapshotStore(directory).restore<List<Session>>("sessions")?.map { it.status })
    }

    @Test fun aNormalSendAnswerCannotAdoptAnotherSession() = runTest {
        val original = Session("computer", "normal", agent = "codex", status = "idle")
        val transport = Transport().apply { list = listOf(original) }
        val repository = HubSessionsRepository(transport, Connection(), backgroundScope)
        repository.refresh()
        repository.adoptThread(original.key, original.copy(sessionId = "different"))
        assertEquals(listOf(original), repository.sessions.value)
        assertTrue(repository.adoptedSessions.value.isEmpty())
        assertTrue(repository.startedThreads.value.isEmpty())
        repository.adoptThread(SessionKey("computer", "unknown"), original.copy(sessionId = "unknown-answer"))
        assertTrue(repository.adoptedSessions.value.isEmpty())
    }

    @Test fun anAppliedMachineReplacesItsEntryUntilTheNextList() = runTest {
        val transport = Transport().apply { machineList = listOf(Machine("a", engine = "herdr"), Machine("b")) }
        val repository = HubSessionsRepository(transport, Connection(), backgroundScope)
        repository.refresh()
        repository.applyMachine(Machine("a", engine = "orca"))
        assertEquals(listOf("orca", "herdr"), repository.machines.value.map { it.engine })
        repository.applyMachine(Machine("c"))
        assertEquals(listOf("a", "b", "c"), repository.machines.value.map { it.id })
        transport.machineList = listOf(Machine("a", engine = "orca"))
        repository.refresh()
        assertEquals(listOf("a"), repository.machines.first().map { it.id })
    }

    /** Offline, so the repository refreshes only when the test asks it to. */
    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject): Boolean = true
    }

    private class Transport : ChatTransport {
        var paged = false
        override suspend fun sessionPage(filter: SessionListFilter, cursor: String?, since: String?): SessionListPage {
            if (!paged) throw SessionListsUnsupported()
            return SessionListPage(revision = "revision")
        }
        override suspend fun sessionCounts(filter: SessionListFilter) = SessionListCounts()
        var list = emptyList<Session>()
        var machineList = emptyList<Machine>()
        var listFetch: (suspend () -> List<Session>)? = null
        override suspend fun machines() = machineList
        override suspend fun sessions() = listFetch?.invoke() ?: list
        override suspend fun items(session: SessionKey, query: ItemsQuery): ItemsResponse = error("Unexpected items request")
        override suspend fun interactions() = emptyList<Interaction>()
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
        override suspend fun image(session: SessionKey, itemId: String) = byteArrayOf()
        override suspend fun fullItem(session: SessionKey, itemId: String) = Item(itemId)
    }
}

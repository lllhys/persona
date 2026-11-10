package persona.core.data

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.test.*
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.*
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class CompleteSessionsRepositoryTest {
    private val rows = (0 until 8000).map { Session("demo", "session/$it") }
    private fun counts() = SessionListCounts(total = 8000)

    @Test fun completeMembershipRetainsDeepRowsRefreshesNoticesAndCanReturnToPaging() = runTest {
        val connection = Connection()
        val transport = Transport().apply { fetch = { _, _, _ -> SessionListPage(rows.take(50), total = 8000) } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        assertTrue(repository.listStatus.value.paged)
        repository.setListFilter(SessionListFilter(requiresCompleteMembership = true))
        assertFalse(repository.listStatus.value.paged)
        assertEquals(rows, repository.sessions.value)
        assertEquals(8000, repository.listStatus.value.total)
        val requests = transport.requests.size
        transport.legacyFetch = { rows.dropLast(1) }
        connection.events.emit(SessionsListEvent("archive-revision"))
        advanceTimeBy(100); runCurrent()
        assertEquals(rows.dropLast(1), repository.sessions.value)
        assertEquals(requests, transport.requests.size)
        transport.legacyFetch = { rows }
        connection.events.emit(SessionsListEvent("restore-revision"))
        advanceTimeBy(100); runCurrent()
        assertEquals(rows, repository.sessions.value)
        repository.setListFilter(SessionListFilter())
        assertTrue(repository.listStatus.value.paged)
        assertEquals(50, repository.sessions.value.size)
    }

    @Test fun failedCompleteReadKeepsPreviousRecordsAndPropagatesFailure() = runTest {
        val transport = Transport().apply { fetch = { _, _, _ -> SessionListPage(rows.take(50), total = 8000) } }
        val connection = Connection()
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.setListFilter(SessionListFilter(requiresCompleteMembership = true))
        assertEquals(rows, repository.sessions.value)
        transport.legacyFetch = { throw java.io.IOException("Read unavailable") }
        assertThrows(java.io.IOException::class.java) { runBlocking { repository.refresh() } }
        assertEquals(rows, repository.sessions.value)
        assertFalse(repository.listStatus.value.paged)
        assertFalse(repository.listStatus.value.loading)
        assertEquals(0, transport.requests.size)
    }

    @Test fun revisionPushDuringCompleteRefreshKeepsMembershipAndNewRevision() = runTest {
        val connection = Connection()
        val requested = CompletableDeferred<Unit>()
        val response = CompletableDeferred<List<Session>>()
        val transport = Transport().apply { legacyFetch = { requested.complete(Unit); response.await() } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        val refresh = backgroundScope.launch { repository.setListFilter(SessionListFilter(requiresCompleteMembership = true)) }
        requested.await()
        connection.events.emit(RevsEvent("demo", mapOf("session/0" to 9L)))
        runCurrent()
        response.complete(rows)
        refresh.join()
        assertEquals(rows.map { it.key }, repository.sessions.value.map { it.key })
        assertEquals(9L, repository.sessions.value.first().rev)
    }

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override var resumed = false
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }
    private inner class Transport : ChatTransport {
        var fetch: suspend (SessionListFilter, String?, String?) -> SessionListPage = { _, _, _ -> error("Unexpected page") }
        val requests = mutableListOf<Triple<SessionListFilter, String?, String?>>()
        val countFilters = mutableListOf<SessionListFilter>()
        var legacyCalls = 0
        var legacyFetch: (suspend () -> List<Session>)? = null
        override suspend fun sessionPage(filter: SessionListFilter, cursor: String?, since: String?): SessionListPage {
            requests += Triple(filter, cursor, since)
            return fetch(filter, cursor, since)
        }
        override suspend fun sessionCounts(filter: SessionListFilter): SessionListCounts { countFilters += filter; return counts() }
        override suspend fun machines() = listOf(Machine("demo", online = true))
        override suspend fun sessions(): List<Session> { legacyCalls++; return legacyFetch?.invoke() ?: rows }
        override suspend fun items(session: SessionKey, query: ItemsQuery): ItemsResponse = error("Unexpected items")
        override suspend fun interactions() = emptyList<Interaction>()
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
        override suspend fun image(session: SessionKey, itemId: String) = byteArrayOf()
        override suspend fun fullItem(session: SessionKey, itemId: String) = Item(itemId)
    }
}

package persona.core.data

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.test.*
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.*
import org.junit.Test
import org.junit.Rule
import org.junit.rules.TemporaryFolder

@OptIn(ExperimentalCoroutinesApi::class)
class PagedSessionsRepositoryTest {
    @get:Rule val temporary = TemporaryFolder()
    private val rows = (0 until 8000).map { Session("demo", "session/$it", title = "Row $it", updatedAt = "2026-01-01T00:00:00Z") }
    private fun page(rows: List<Session>, revision: String = "r1", cursor: String? = null,
        total: Int = 8000, remove: List<String> = emptyList()) =
        SessionListPage(rows, remove, cursor != null, cursor, revision, total)
    private fun counts(revision: String = "r1") = SessionListCounts(total = 8000, revision = revision)

    @Test fun firstPaintAndPaginationNeverFetchTheFull8000Rows() = runTest {
        val transport = Transport().apply { fetch = { _, cursor, since ->
            if (since != null) page(emptyList())
            else if (cursor == null) page(rows.take(50), cursor = "p2") else page(rows.drop(50).take(50), cursor = "p3")
        } }
        val repository = HubSessionsRepository(transport, Connection(), backgroundScope)
        repository.refresh()
        assertEquals(50, repository.sessions.value.size)
        assertEquals(8000, repository.listStatus.value.total)
        assertTrue(repository.listStatus.value.paged)
        assertTrue(repository.listStatus.value.loaded)
        assertTrue(repository.listStatus.value.hasMore)
        println("8000 synthetic sessions: initial rows=50, initial page requests=${transport.requests.size}, legacy full-list calls=${transport.legacyCalls}")
        repository.loadMore()
        assertEquals(100, repository.sessions.value.size)
        assertEquals(0, transport.legacyCalls)
        assertEquals(3, transport.requests.size)
        assertTrue(repository.verifiedSessions().isEmpty())
    }

    @Test fun visibleGroupCountsNeverDownloadGroupRowsOrReplaceTheActiveCursor() = runTest {
        val transport = Transport().apply { fetch = { _, _, since ->
            page(if (since == null) rows.take(50) else emptyList(), cursor = if (since == null) "p2" else null)
        } }
        val repository = HubSessionsRepository(transport, Connection(), backgroundScope)
        repository.refresh()
        val before = transport.requests.size
        val filter = SessionListFilter(machineId = "demo", project = "/demo/app", group = "space", query = "row")
        assertEquals(8000, repository.listCounts(filter).total)
        assertEquals(filter, transport.countFilters.last())
        assertEquals(before, transport.requests.size)
        assertEquals(50, repository.sessions.value.size)
        assertTrue(repository.listStatus.value.hasMore)
        assertEquals(0, transport.legacyCalls)
    }

    @Test fun fieldOnlyDeltaStreamsAllPagesButIgnoresUnloadedKeysAndAdvancesOnlyAtEnd() = runTest {
        val connection = Connection()
        val last = CompletableDeferred<SessionListPage>()
        val transport = Transport().apply { fetch = { _, cursor, since ->
            when {
                since == null -> page(rows.take(50), cursor = "p2")
                cursor == null -> page(listOf(rows[0].copy(title = "New title", state = "working"), rows[6000]), "r2", "d2")
                else -> last.await()
            }
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        connection.events.emit(SessionsListEvent("r2"))
        advanceTimeBy(100); runCurrent()
        assertEquals("New title", repository.sessions.value.first().title)
        assertFalse(repository.sessions.value.any { it.sessionId == rows[6000].sessionId })
        last.complete(page(emptyList(), "r2", remove = listOf(JsonCodec.encodeToString(listOf("demo", "session/1")))))
        runCurrent()
        assertEquals(49, repository.sessions.value.size)
        connection.events.emit(SessionsListEvent("r3"))
        advanceTimeBy(100); runCurrent()
        assertEquals("r2", transport.requests.last().third)
        assertEquals(0, transport.legacyCalls)
    }

    @Test fun adoptedSessionNewerThanTheWindowJoinsPageOneAndSurvivesLoadingMore() = runTest {
        val connection = Connection()
        val started = Session("demo", "started", title = "Started", updatedAt = "2026-01-02T00:00:00Z")
        val transport = Transport().apply { fetch = { _, cursor, since ->
            when {
                since != null -> page(listOf(started, rows[6000].copy(title = "Older edit")), "r2")
                cursor != null -> page(rows.drop(50).take(50), cursor = "p3")
                else -> page(rows.take(50), cursor = "p2")
            }
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        repository.adopt(started)
        advanceTimeBy(100); runCurrent()
        assertEquals(started, repository.sessions.value.first())
        assertFalse(repository.sessions.value.any { it.key == rows[6000].key })
        repository.loadMore()
        assertEquals(101, repository.sessions.value.size)
        assertEquals(1, repository.sessions.value.count { it.key == started.key })
        assertEquals(0, transport.legacyCalls)
    }

    @Test fun sessionThatStartsNeedingYouJoinsTheFilteredListAndLeavesWhenItStops() = runTest {
        val connection = Connection()
        val waiting = Session("demo", "waiting", state = "needs_you", updatedAt = "2025-12-01T00:00:00Z")
        var delta = page(listOf(waiting), "r2", total = 1)
        val transport = Transport().apply { fetch = { _, _, since -> if (since == null) page(emptyList(), total = 0) else delta } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.setListFilter(SessionListFilter(state = "needs_you"))
        connection.events.emit(SessionsListEvent("r2")); advanceTimeBy(100); runCurrent()
        assertEquals(listOf(waiting), repository.sessions.value)
        delta = page(emptyList(), "r3", total = 0, remove = listOf(JsonCodec.encodeToString(listOf("demo", "waiting"))))
        connection.events.emit(SessionsListEvent("r3")); advanceTimeBy(100); runCurrent()
        assertTrue(repository.sessions.value.isEmpty())
    }

    @Test fun burstOfNewerRowsReloadsPageOneInsteadOfGrowingTheWindow() = runTest {
        val connection = Connection()
        val burst = (0 until 60).map { Session("demo", "burst/$it", updatedAt = "2026-01-02T00:00:00Z") }
        var firstPages = 0
        val transport = Transport().apply { fetch = { _, _, since ->
            when {
                since != null -> page(burst, "r2")
                firstPages++ == 0 -> page(rows.take(50), cursor = "p2")
                else -> page((burst + rows).take(50), "r2", "p2")
            }
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        connection.events.emit(SessionsListEvent("r2")); advanceTimeBy(100); runCurrent()
        assertEquals(2, firstPages)
        assertEquals(burst.take(50), repository.sessions.value)
        assertEquals(0, transport.legacyCalls)
    }

    @Test fun malformedRepeatedDeltaCursorDoesNotLoopOrAdvanceCheckpoint() = runTest {
        val connection = Connection()
        val transport = Transport().apply { fetch = { _, _, since ->
            if (since == null) page(rows.take(50), cursor = "p2") else page(emptyList(), "r2", "repeat")
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        connection.events.emit(SessionsListEvent("r2")); advanceTimeBy(100); runCurrent()
        assertEquals(3, transport.requests.size)
        connection.events.emit(SessionsListEvent("r2")); advanceTimeBy(100); runCurrent()
        assertEquals("r1", transport.requests.last().third)
        assertFalse(repository.listStatus.value.loading)
    }

    @Test fun failedDeltaFinalPageRetriesTheOldCheckpoint() = runTest {
        val connection = Connection()
        val transport = Transport().apply { fetch = { _, cursor, since ->
            when {
                since == null -> page(rows.take(50), cursor = "p2")
                cursor == null -> page(listOf(rows[0].copy(title = "Changed")), "r2", "d2")
                else -> throw java.io.IOException("Interrupted delta")
            }
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        repeat(2) {
            connection.events.emit(SessionsListEvent("r2")); advanceTimeBy(100); runCurrent()
        }
        assertEquals(listOf("r1", "r1"), transport.requests.filter { it.third != null && it.second == null }.map { it.third })
        assertEquals(50, repository.sessions.value.size)
    }

    @Test fun all8000DeltaRowsAreStreamedWithoutGrowingThe50RowWindow() = runTest {
        val connection = Connection()
        var deltaPages = 0
        val transport = Transport().apply { fetch = { _, cursor, since ->
            if (since == null) page(rows.take(50), cursor = "p2") else {
                assertEquals("r1", since)
                val offset = cursor?.toInt() ?: 0
                deltaPages++
                page(rows.drop(offset).take(50).map { it.copy(title = "Changed ${it.sessionId}") }, "r2",
                    cursor = (offset + 50).takeIf { it < 8000 }?.toString())
            }
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        var maxLoaded = 0
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            repository.sessions.collect { maxLoaded = maxOf(maxLoaded, it.size) }
        }
        repeat(20) { connection.events.emit(SessionsListEvent("r2")) }
        advanceTimeBy(100); runCurrent()
        assertEquals(160, deltaPages)
        assertEquals(50, maxLoaded)
        assertEquals(50, repository.sessions.value.size)
        assertTrue(repository.sessions.value.all { it.title.startsWith("Changed") })
        assertEquals(0, transport.legacyCalls)
    }

    @Test fun frozenPageAfterDeltaIsReconciledBeforeExposingItsRows() = runTest {
        val connection = Connection()
        val transport = Transport().apply { fetch = { _, cursor, since ->
            when {
                since != null -> page(listOf(rows[51].copy(title = "Updated while unloaded")), "r2",
                    remove = listOf(JsonCodec.encodeToString(listOf("demo", "session/50"))))
                cursor != null -> page(rows.drop(50).take(50), "r1", "p3")
                else -> page(rows.take(50), "r1", "p2")
            }
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        connection.events.emit(SessionsListEvent("r2")); advanceTimeBy(100); runCurrent()
        val observed = mutableListOf<List<Session>>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { repository.sessions.collect { observed += it } }
        repository.loadMore()
        assertEquals("Updated while unloaded", repository.sessions.value.first { it.key == rows[51].key }.title)
        assertFalse(repository.sessions.value.any { it.key == rows[50].key })
        assertFalse(observed.any { list -> list.any { it.key == rows[51].key && it.title == rows[51].title } })
        assertEquals("r1", transport.requests.last().third)
        repository.loadMore()
        assertEquals("r1", transport.requests.last().third)
    }

    @Test fun filterSwitchCancelsAndDiscardsEvenANonCancellableOldResponse() = runTest {
        val blocked = CompletableDeferred<SessionListPage>()
        val transport = Transport().apply { fetch = { filter, _, _ ->
            if (filter.query.isEmpty()) withContext(NonCancellable) { blocked.await() }
            else page(listOf(rows[7000]), "filtered", total = 1)
        } }
        val repository = HubSessionsRepository(transport, Connection(), backgroundScope)
        val old = async { repository.refresh() }
        runCurrent()
        val switched = async { repository.setListFilter(SessionListFilter(query = "new")) }
        runCurrent()
        blocked.complete(page(rows.take(50)))
        switched.await()
        assertTrue(old.isCancelled)
        assertEquals(listOf(rows[7000]), repository.sessions.value)
        assertEquals("new", repository.listStatus.value.filter.query)
    }

    @Test fun scrollingAll8000RowsRetainsAtMost500AndCanReloadPreviousPages() = runTest {
        val transport = Transport().apply { fetch = { _, cursor, since ->
            if (since != null) page(emptyList()) else {
                val offset = cursor?.toInt() ?: 0
                page(rows.drop(offset).take(50), cursor = (offset + 50).takeIf { it < rows.size }?.toString())
            }
        } }
        val repository = HubSessionsRepository(transport, Connection(), backgroundScope)
        repository.refresh()
        repeat(159) {
            repository.loadMore()
            assertTrue(repository.sessions.value.size <= 500)
        }
        assertEquals(rows.takeLast(500), repository.sessions.value)
        assertFalse(repository.listStatus.value.hasMore)
        assertTrue(repository.listStatus.value.hasPrevious)
        repository.loadPrevious()
        assertEquals(rows.drop(7450).take(500), repository.sessions.value)
        assertTrue(repository.listStatus.value.hasMore)
        repository.loadMore()
        assertEquals(rows.takeLast(500), repository.sessions.value)
        repeat(150) {
            repository.loadPrevious()
            assertTrue(repository.sessions.value.size <= 500)
        }
        // Page zero has no starting token: returning to it safely resets to a fresh first page.
        assertEquals(rows.take(50), repository.sessions.value)
        assertFalse(repository.listStatus.value.hasPrevious)
        assertTrue(repository.listStatus.value.hasMore)
        assertEquals(0, transport.legacyCalls)
    }

    @Test fun previousFrozenPageAlsoReconcilesUnloadedChangesBeforeExposure() = runTest {
        val connection = Connection()
        var changed = false
        val transport = Transport().apply { fetch = { _, cursor, since ->
            if (since != null) {
                if (changed) page(listOf(rows[50].copy(title = "Updated previous page")), "r2",
                    remove = listOf(JsonCodec.encodeToString(listOf("demo", "session/51"))))
                else page(emptyList())
            } else {
                val offset = cursor?.toInt() ?: 0
                page(rows.drop(offset).take(50), cursor = (offset + 50).toString())
            }
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        repeat(11) { repository.loadMore() }
        assertFalse(repository.sessions.value.any { it.key == rows[50].key })
        changed = true
        connection.events.emit(SessionsListEvent("r2")); advanceTimeBy(100); runCurrent()
        val observed = mutableListOf<List<Session>>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { repository.sessions.collect { observed += it } }
        repository.loadPrevious()
        assertEquals("Updated previous page", repository.sessions.value.first().title)
        assertFalse(repository.sessions.value.any { it.key == rows[51].key })
        assertFalse(observed.any { list -> list.any { it.key == rows[50].key && it.title == rows[50].title } })
        assertTrue(repository.sessions.value.size <= 500)
        assertEquals("r1", transport.requests.last().third)
    }

    @Test fun countsKeepSelectedMachineButHaveSeparateCrossMachineMetadata() = runTest {
        val transport = Transport().apply { fetch = { _, _, _ -> page(rows.take(50), total = 123) } }
        val repository = HubSessionsRepository(transport, Connection(), backgroundScope)
        val filter = SessionListFilter(state = "working", machineId = "demo", project = "/demo/app")
        repository.setListFilter(filter)
        assertEquals(listOf(filter.copy(state = null), filter.copy(state = null, machineId = null)), transport.countFilters)
        assertEquals(123, repository.listStatus.value.total)
        assertEquals(8000, repository.listStatus.value.counts.total)
        assertEquals(8000, repository.listStatus.value.machineCounts.total)
    }

    @Test fun expiredPageAndDeltaResetToFirstPage() = runTest {
        val connection = Connection()
        val transport = Transport().apply { fetch = { _, cursor, since ->
            if (cursor != null || since != null) throw SessionListExpired()
            page(rows.take(50), cursor = "p2")
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh(); repository.loadMore()
        assertNull(transport.requests.last().second)
        assertNull(transport.requests.last().third)
        connection.events.emit(SessionsListEvent("restart")); advanceTimeBy(100); runCurrent()
        assertNull(transport.requests.last().third)
        assertEquals(50, repository.sessions.value.size)
        assertEquals(0, transport.legacyCalls)
    }

    @Test fun resumedPagedReconnectFetchesDeltaAndNeverVerifiesAPartialMachine() = runTest {
        val connection = Connection()
        val transport = Transport().apply { fetch = { _, _, since ->
            if (since == null) page(rows.take(50), cursor = "p2") else page(emptyList(), "r2")
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        connection.resumed = true
        connection.state.value = ConnectionState.Online
        runCurrent()
        assertEquals("r1", transport.requests.last().third)
        assertTrue(repository.verifiedSessions().isEmpty())
    }

    @Test fun onlyUnsupportedFallsBackAndLegacyWebSocketStillReplacesMachine() = runTest {
        val connection = Connection()
        val transport = Transport().apply { fetch = { _, _, _ -> throw SessionListsUnsupported() } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        assertEquals(8000, repository.sessions.value.size)
        assertFalse(repository.listStatus.value.paged)
        connection.events.emit(SessionsEvent("demo", listOf(rows[1])))
        runCurrent()
        assertEquals(listOf(rows[1]), repository.sessions.value)
        assertEquals(1, transport.legacyCalls)
        val failure = Transport().apply { fetch = { _, _, _ -> throw java.io.IOException("Unavailable") } }
        val other = HubSessionsRepository(failure, Connection(), backgroundScope)
        assertTrue(runCatching { other.refresh() }.isFailure)
        assertEquals(0, failure.legacyCalls)
    }

    @Test fun firstPageFailureExposesARetryableStatus() = runTest {
        var available = false
        val transport = Transport().apply { fetch = { _, _, _ ->
            if (!available) throw java.io.IOException("HTTP 500")
            page(emptyList(), total = 0)
        } }
        val repository = HubSessionsRepository(transport, Connection(), backgroundScope)
        assertTrue(runCatching { repository.refresh() }.isFailure)
        assertFalse(repository.listStatus.value.loaded)
        assertFalse(repository.listStatus.value.loading)
        assertTrue(repository.listStatus.value.failed)

        available = true
        repository.refresh()
        assertTrue(repository.listStatus.value.loaded)
        assertFalse(repository.listStatus.value.failed)
    }

    @Test fun helloCapabilityChangesReprobeUpgradeAndDowngradeWithoutStickyLegacyMode() = runTest {
        val connection = Connection()
        var supported = false
        val transport = Transport().apply { fetch = { _, _, _ ->
            if (!supported) throw SessionListsUnsupported()
            page(rows.take(50), cursor = "p2")
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        connection.events.emit(Hello(features = emptyList()))
        repository.refresh()
        assertEquals(8000, repository.sessions.value.size)
        supported = true
        connection.events.emit(Hello(features = listOf("sessions_list")))
        advanceTimeBy(100); runCurrent()
        assertTrue(repository.listStatus.value.paged)
        assertEquals(50, repository.sessions.value.size)
        supported = false
        connection.events.emit(Hello(features = emptyList()))
        advanceTimeBy(100); runCurrent()
        assertFalse(repository.listStatus.value.paged)
        assertEquals(8000, repository.sessions.value.size)
        assertEquals(2, transport.legacyCalls)
        connection.events.emit(SessionsEvent("demo", listOf(rows[5]))); runCurrent()
        assertEquals(listOf(rows[5]), repository.sessions.value)
    }

    @Test fun unsupportedDeltaFallsBackAndALaterHelloUpgradeCanReprobe() = runTest {
        val connection = Connection()
        var supported = true
        val transport = Transport().apply { fetch = { _, _, since ->
            if (!supported) throw SessionListsUnsupported()
            if (since == null) page(rows.take(50), cursor = "p2") else page(emptyList())
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        repository.refresh()
        supported = false
        connection.events.emit(SessionsListEvent("older-hub")); advanceTimeBy(100); runCurrent()
        assertFalse(repository.listStatus.value.paged)
        assertEquals(8000, repository.sessions.value.size)
        supported = true
        connection.events.emit(Hello(features = listOf("sessions_list"))); advanceTimeBy(100); runCurrent()
        assertTrue(repository.listStatus.value.paged)
        assertEquals(50, repository.sessions.value.size)
    }

    @Test fun capabilityChangeDiscardsAnOldInFlightLegacyResponse() = runTest {
        val connection = Connection()
        val blocked = CompletableDeferred<List<Session>>()
        var supported = false
        val transport = Transport().apply {
            legacyFetch = { withContext(NonCancellable) { blocked.await() } }
            fetch = { _, _, _ ->
                if (!supported) throw SessionListsUnsupported()
                page(rows.take(50), cursor = "p2")
            }
        }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        val old = async { repository.refresh() }
        runCurrent()
        supported = true
        connection.events.emit(Hello(features = listOf("sessions_list")))
        advanceTimeBy(100); runCurrent()
        blocked.complete(rows)
        runCurrent()
        assertTrue(old.isCancelled)
        assertTrue(repository.listStatus.value.paged)
        assertEquals(50, repository.sessions.value.size)
        assertTrue(repository.verifiedSessions().isEmpty())
    }

    @Test fun capabilityDowngradeDiscardsAnOldInFlightPageResponse() = runTest {
        val connection = Connection()
        val blocked = CompletableDeferred<SessionListPage>()
        var supported = true
        val transport = Transport().apply { fetch = { _, _, _ ->
            if (!supported) throw SessionListsUnsupported()
            withContext(NonCancellable) { blocked.await() }
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope)
        connection.events.emit(Hello(features = listOf("sessions_list")))
        val old = async { repository.refresh() }
        runCurrent()
        supported = false
        connection.events.emit(Hello(features = emptyList()))
        advanceTimeBy(100); runCurrent()
        blocked.complete(page(rows.take(50), cursor = "p2"))
        runCurrent()
        assertTrue(old.isCancelled)
        assertFalse(repository.listStatus.value.paged)
        assertEquals(8000, repository.sessions.value.size)
        assertEquals(1, transport.legacyCalls)
    }

    @Test fun cacheAndLegacyPushAndAdoptionStayBoundedOnSupportedPath() = runTest {
        val store = ListSnapshotStore(temporary.newFolder())
        store.save("sessions", rows)
        assertEquals(50, store.sessionWindow(50)!!.size)
        val connection = Connection()
        val transport = Transport().apply { fetch = { _, _, since ->
            if (since == null) page(rows.take(50), cursor = "p2") else page(emptyList())
        } }
        val repository = HubSessionsRepository(transport, connection, backgroundScope, store)
        repository.sessions.first { it.isNotEmpty() }
        assertEquals(50, repository.sessions.value.size)
        repository.refresh()
        connection.events.emit(SessionsEvent("demo", rows)); runCurrent()
        repository.adopt(rows[6000])
        assertEquals(50, repository.sessions.value.size)
        assertTrue(store.restore<List<Session>>("sessions")!!.size <= 50)
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

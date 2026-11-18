package persona.feature.agents

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.asCoroutineDispatcher
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.JsonObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.domain.sort.SessionRenamer
import java.util.concurrent.Executors

/** The agents list is built where the app asks (#5377): every push sorts and counts all sessions, so not on main. */
@OptIn(ExperimentalCoroutinesApi::class)
class AgentsPageContextTest {
    private val main = Executors.newSingleThreadExecutor { Thread(it, "test-main") }.asCoroutineDispatcher()
    private val list = Executors.newSingleThreadExecutor { Thread(it, "test-list") }.asCoroutineDispatcher()
    private lateinit var model: AgentsViewModel

    @Before fun setMain() { Dispatchers.setMain(main) }
    @After fun tearDown() {
        // Real executor callbacks must finish before replacing the global Main dispatcher.
        if (::model.isInitialized) runBlocking {
            withTimeout(10_000) { model.viewModelScope.coroutineContext[Job]?.cancelAndJoin() }
        }
        Dispatchers.resetMain()
        main.close(); list.close()
    }

    @Test fun thePageIsBuiltOnTheListContext() = runBlocking {
        val built = java.util.Collections.synchronizedList(mutableListOf<String>())
        val repository = object : SessionsRepository {
            override val sessions = MutableStateFlow(listOf(Session("box", "one", state = "working")))
            override val machines = MutableStateFlow(listOf(Machine("box", online = true)))
            override suspend fun refresh() = Unit
        }
        // The renamer's projection runs where the page is built.
        val renamer = object : SessionRenamer {
            override fun project(source: Flow<List<Session>>) = source.map { built += Thread.currentThread().name; it }
            override suspend fun rename(session: Session, title: String?, tabName: String?) = true
        }
        model = modelOn(repository, renamer)

        val page = withTimeout(10_000) { model.page.first { it.sessions.isNotEmpty() } }
        assertEquals(listOf("one"), page.sessions.map { it.sessionId })
        assertTrue(built.toString(), built.isNotEmpty() && built.all { it.startsWith("test-list") })
    }

    private fun modelOn(repository: SessionsRepository, renamer: SessionRenamer? = null) =
        AgentsViewModel(repository, object : InteractionCenter {
            override val pending = MutableStateFlow<List<Interaction>>(emptyList())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
        }, object : HubConnection {
            override val state = MutableStateFlow(ConnectionState.Online)
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }, object : MessageSender {
            override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
        }, AgentsPreferences(object : DataStore<Preferences> {
            override val data = MutableStateFlow<Preferences>(emptyPreferences())
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                transform(data.value).also { data.value = it }
        }, object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }), renamer = renamer, listContext = list)

    /**
     * The page is built off the hub's own status and trails it by up to the throttle: a load that arrives right after a
     * build used to be joined to that earlier build's empty list and called loaded, so the screen's saved reading row
     * was missing from a list that said it was in.
     */
    @Test fun aPageIsLoadedOnlyWithTheListThatLoadedItNotTheBuildBeforeIt() = runBlocking {
        val rows = MutableStateFlow(emptyList<Session>())
        val status = MutableStateFlow(SessionListStatus())
        model = modelOn(object : SessionsRepository {
            override val sessions = rows
            override val machines = MutableStateFlow(listOf(Machine("box", online = true)))
            override val listStatus = status
            override suspend fun refresh() = Unit
        })
        val loadedPages = java.util.Collections.synchronizedList(mutableListOf<AgentsPageState>())
        val collecting = kotlinx.coroutines.CoroutineScope(Dispatchers.Default).launch { model.page.collect { if (it.loaded) loadedPages += it } }
        try {
            // The first build (nothing in yet) is on screen; the load follows inside the throttle's window, rows first as the repository does.
            withTimeout(10_000) { model.page.first { it.machines.isNotEmpty() } }
            rows.value = listOf(Session("box", "one", state = "working"))
            status.value = SessionListStatus(loaded = true, total = 1)
            withTimeout(10_000) { while (loadedPages.isEmpty()) kotlinx.coroutines.delay(10) }
            kotlinx.coroutines.delay(600)
            assertTrue("every page that says loaded has the loaded rows: " + loadedPages.map { it.sessions.size },
                loadedPages.toList().all { it.sessions.map { s -> s.sessionId } == listOf("one") })
        } finally {
            collecting.cancel()
        }
    }
}

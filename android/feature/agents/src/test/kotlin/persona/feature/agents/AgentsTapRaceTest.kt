package persona.feature.agents

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.JsonObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import persona.core.contracts.Interaction
import persona.core.contracts.Machine
import persona.core.contracts.Resolution
import persona.core.contracts.ServerEvent
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.HubPreferences
import persona.core.contracts.api.HubPreferencesSnapshot
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.api.SessionsRepository
import persona.core.domain.sort.AgentGrouping
import persona.core.domain.sort.agentTabs

/**
 * A fold and a grouping change publish the new list and the tap's epoch together, before the
 * phone finishes saving (`ChatsView.swift:2080`, `AgentsMenus.swift:405`). A failed save is put
 * back (`PinsStore.swift:131`, `:139`, `:163`).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AgentsTapRaceTest {
    private lateinit var model: AgentsViewModel
    private val store = GatedStore()
    private val preferences = AgentsPreferences(store, object : HubPreferences {
        override val preferences = MutableStateFlow(HubPreferencesSnapshot())
        override suspend fun refresh() = Unit
        override suspend fun togglePin(path: String, space: Boolean) = Unit
        override suspend fun toggleMute(path: String, space: Boolean) = Unit
        override suspend fun markSeen(session: SessionKey, version: String) = Unit
    })
    private val sessions = (0..1).map { pane ->
        Session("shown", "pane-$pane", state = "working", spaceId = "space", spaceName = "project",
            slotId = "tab", slotIndex = 1, paneId = "space:p$pane", paneIndex = pane)
    }

    @Before fun setMain() { Dispatchers.setMain(StandardTestDispatcher()) }
    @After fun tearDown() {
        if (::model.isInitialized) model.viewModelScope.cancel()
        Dispatchers.resetMain()
    }

    private suspend fun TestScope.start(throttled: Boolean = false) {
        model = AgentsViewModel(object : SessionsRepository {
            override val sessions = MutableStateFlow(this@AgentsTapRaceTest.sessions)
            override val machines = MutableStateFlow(listOf(Machine("shown", online = true)))
            override suspend fun refresh() = Unit
        }, object : InteractionCenter {
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
        }, preferences, listContext = if (throttled) StandardTestDispatcher(testScheduler) else kotlin.coroutines.EmptyCoroutineContext)
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { model.page.collect {} }
        runCurrent()
    }

    @Test fun foldReachesThePageBeforeTheSave() = runTest {
        preferences.grouping(AgentGrouping.MachineSpace)
        start()
        model.machine("shown")
        runCurrent()
        val tab = agentTabs(model.page.value.list.groups.single().rows).values.single()
        store.gate = CompletableDeferred()
        model.foldTab(tab)
        runCurrent()
        assertEquals(setOf(tab.foldKey), model.page.value.preferences.foldedTabs)
        assertEquals(1, model.page.value.tapEpoch)
        assertTrue(preferences.snapshots.first().foldedTabs.isEmpty())
        store.gate?.complete(Unit)
        runCurrent()
        assertEquals(setOf(tab.foldKey), preferences.snapshots.first().foldedTabs)
        assertEquals(setOf(tab.foldKey), model.page.value.preferences.foldedTabs)
        assertEquals(1, model.page.value.tapEpoch)
    }

    @Test fun aFailedFoldIsPutBack() = runTest {
        preferences.grouping(AgentGrouping.MachineSpace)
        start()
        model.machine("shown")
        runCurrent()
        val tab = agentTabs(model.page.value.list.groups.single().rows).values.single()
        store.fail = true
        model.foldTab(tab)
        runCurrent()
        assertFalse(tab.foldKey in model.page.value.preferences.foldedTabs)
        assertTrue(preferences.snapshots.first().foldedTabs.isEmpty())
        assertTrue("rollback did not take its own epoch (${model.page.value.tapEpoch})", model.page.value.tapEpoch >= 2)
    }

    @Test fun aFailedGroupingDoesNotClobberTheNextTap() = runTest {
        start()
        store.gate = CompletableDeferred()
        store.failTimes = 1
        model.grouping(AgentGrouping.State)
        model.grouping(AgentGrouping.Machine)
        runCurrent()
        assertEquals(AgentGrouping.Machine, model.page.value.preferences.grouping)
        store.gate?.complete(Unit)
        runCurrent()
        assertEquals(AgentGrouping.Machine, preferences.snapshots.first().grouping)
        assertEquals(AgentGrouping.Machine, model.page.value.preferences.grouping)
    }

    @Test fun aFailedGroupingDoesNotStickOverALaterSave() = runTest {
        start()
        store.fail = true
        model.grouping(AgentGrouping.State)
        runCurrent()
        assertEquals(AgentGrouping.Repo, model.page.value.preferences.grouping)
        store.fail = false
        preferences.grouping(AgentGrouping.Machine)
        runCurrent()
        assertEquals(AgentGrouping.Machine, model.page.value.preferences.grouping)
    }

    @Test fun aSecondFoldUsesThePendingSet() = runTest {
        preferences.grouping(AgentGrouping.MachineSpace)
        start(throttled = true)
        model.machine("shown")
        runCurrent()
        val tab = agentTabs(model.page.value.list.groups.single().rows).values.single()
        model.foldTab(tab)
        model.foldTab(tab)
        runCurrent()
        assertTrue(model.page.value.preferences.foldedTabs.isEmpty())
        assertTrue(preferences.snapshots.first().foldedTabs.isEmpty())
    }

    @Test fun tappingBackToTheSavedGroupingWhileOneIsPending() = runTest {
        start(throttled = true)
        runCurrent()
        store.gate = CompletableDeferred()
        model.grouping(AgentGrouping.State)
        model.grouping(AgentGrouping.Repo)
        runCurrent()
        assertEquals(AgentGrouping.Repo, model.page.value.preferences.grouping)
        store.gate?.complete(Unit)
        runCurrent()
        assertEquals(AgentGrouping.Repo, preferences.snapshots.first().grouping)
        assertEquals(AgentGrouping.Repo, model.page.value.preferences.grouping)
    }

    @Test fun groupingReachesThePageBeforeTheSave() = runTest {
        start()
        store.gate = CompletableDeferred()
        model.grouping(AgentGrouping.State)
        runCurrent()
        assertEquals(AgentGrouping.State, model.page.value.preferences.grouping)
        assertEquals(1, model.page.value.tapEpoch)
        assertEquals(AgentGrouping.Repo, preferences.snapshots.first().grouping)
        store.gate?.complete(Unit)
        runCurrent()
        assertEquals(AgentGrouping.State, preferences.snapshots.first().grouping)
        assertEquals(AgentGrouping.State, model.page.value.preferences.grouping)
        assertEquals(1, model.page.value.tapEpoch)
    }

    private class GatedStore : DataStore<Preferences> {
        override val data = MutableStateFlow<Preferences>(emptyPreferences())
        var gate: CompletableDeferred<Unit>? = null
        var fail = false
        var failTimes = 0
        override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences {
            val next = transform(data.value)
            gate?.await()
            if (fail || failTimes > 0) {
                if (failTimes > 0) failTimes--
                throw IllegalStateException("disk")
            }
            data.value = next
            return next
        }
    }
}

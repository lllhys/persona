package persona.feature.agents

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
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
import org.junit.Before
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.domain.answer.AnswerAction
import persona.feature.chat.AnswerReceipt

@OptIn(ExperimentalCoroutinesApi::class)
class AgentsAlreadyAnsweredTest {
    private lateinit var model: AgentsViewModel
    private val request = Interaction("request", machineId = "m", sessionId = "s", kind = "permission", toolName = "Bash")
    private val pending = MutableStateFlow(listOf(request))
    private var outcome = ResolveOutcome()
    private val preferences = AgentsPreferences(object : DataStore<Preferences> {
        override val data = MutableStateFlow<Preferences>(emptyPreferences())
        override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
            transform(data.value).also { data.value = it }
    }, object : HubPreferences {
        override val preferences = MutableStateFlow(HubPreferencesSnapshot())
        override suspend fun refresh() = Unit
        override suspend fun togglePin(path: String, space: Boolean) = Unit
        override suspend fun toggleMute(path: String, space: Boolean) = Unit
        override suspend fun markSeen(session: SessionKey, version: String) = Unit
    })

    @Before fun setMain() { Dispatchers.setMain(StandardTestDispatcher()) }
    @After fun tearDown() {
        if (::model.isInitialized) model.viewModelScope.cancel()
        Dispatchers.resetMain()
    }

    private fun TestScope.start() {
        model = AgentsViewModel(object : SessionsRepository {
            override val sessions = MutableStateFlow(listOf(Session("m", "s", state = "needs_input")))
            override val machines = MutableStateFlow(listOf(Machine("m", online = true)))
            override suspend fun refresh() = Unit
        }, object : InteractionCenter {
            override val pending = this@AgentsAlreadyAnsweredTest.pending
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) { resolveOutcome(id, resolution) }
            override suspend fun resolveOutcome(id: String, resolution: Resolution): ResolveOutcome = outcome
        }, object : HubConnection {
            override val state = MutableStateFlow(ConnectionState.Online)
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }, object : MessageSender {
            override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
        }, preferences)
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { model.page.collect {} }
        runCurrent()
    }

    private fun TestScope.assertEffectiveReceipt(tap: AnswerAction, effective: Resolution) {
        outcome = ResolveOutcome(already = true, status = "resolved", resolution = effective)
        start()

        model.answer(request, tap)
        runCurrent()

        assertEquals(RequestDelivery.Sent, model.delivery.value[request.id])
        assertEquals(AgentText.receipt(AnswerReceipt(request, effective)), model.notice.value)
    }

    @Test fun allowAfterDenyShowsTheAnswerThatActuallyTookEffect() = runTest {
        assertEffectiveReceipt(AnswerAction.Allow, Resolution(allow = false))
    }

    @Test fun denyAfterAllowShowsTheAnswerThatActuallyTookEffect() = runTest {
        assertEffectiveReceipt(AnswerAction.Deny, Resolution(allow = true))
    }
}

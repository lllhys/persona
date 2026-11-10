package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator

/**
 * Opening the Agents tab (#5377): the sessions and the local preferences arrive asynchronously, and until the first
 * page is built the list must not claim there are no computers or sessions; the first built page has them.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class AgentsFirstPageTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    /** Local preferences that have not been read yet: null until [stored] gets a value. */
    private val stored = MutableStateFlow<Preferences?>(null)

    @Test fun noEmptyStateBeforeTheFirstPageAndThenTheSessions() {
        val rows = MutableStateFlow(listOf(Session("box", "s1", title = "Example task", state = "working")))
        val model = AgentsViewModel(object : SessionsRepository {
            override val sessions = rows
            override val machines = MutableStateFlow(listOf(Machine("box", "Example computer", online = true)))
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
        }, AgentsPreferences(object : DataStore<Preferences> {
            override val data = stored.filterNotNull()
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                transform(stored.value ?: emptyPreferences()).also { stored.value = it }
        }, object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }))
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) { AgentsScreen(model, Nowhere, AgentsMetadata()) }
        }
        rule.waitForIdle()
        // The placeholder page: nothing built yet, so neither empty state shows.
        assertFalse(model.page.value.loaded)
        rule.onNodeWithContentDescription("Loading").assertIsDisplayed()
        listOf("No computers yet", "Start your first coding session").forEach {
            assertTrue("no \"$it\" before the first page",
                rule.onAllNodesWithText(it, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isEmpty())
        }

        rule.runOnIdle { stored.value = emptyPreferences() }
        rule.waitUntil(5_000) { model.page.value.loaded }
        val first = model.page.value
        assertEquals(listOf("s1"), first.sessions.map { it.sessionId })
        rule.waitForIdle()
        fun shown(text: String) = rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        assertTrue(shown("Example computer"))
        assertFalse(shown("No computers yet"))
        rule.onNodeWithContentDescription("Loading").assertDoesNotExist()

        rule.runOnIdle { rows.value = emptyList() }
        rule.waitForIdle()
        assertTrue("a loaded empty page keeps its empty state", shown("Start your first coding session"))
        rule.onNodeWithContentDescription("Loading").assertDoesNotExist()
    }

    private object Nowhere : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
}

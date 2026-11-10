package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.domain.sort.canApprove
import persona.core.designsystem.LocalManagerRoster
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator

/**
 * The Agents page lists the current assistant's agents and computers (docs/assistant/managers.md "Clients"): the hub's
 * lists are every manager's, each row with its own; no stamp is the default's. Switching lists the other one's, and
 * the cards that go with them.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class AgentsCurrentAssistantTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    @After fun reset() = AssistantManagerChoice.reset()

    private val roster = ManagerRoster(listOf(
        HubManager("assistant", "Assistant", online = true, isDefault = true, builtin = true),
        HubManager("m-2", "Work", online = true),
    ), "assistant")

    private val requests = MutableStateFlow(listOf(
        Interaction("c1", "box", "s1", kind = "question"), // its session's manager: the default's
        Interaction("c2", "laptop", "s2", kind = "question", manager = "m-2"),
        Interaction("c3", "old", "s4", kind = "question", manager = "m-gone"), // of a manager the roster no longer lists
        Interaction("c4", "box", "gone", kind = "question"), // on no listed session, no stamp: the default's
        Interaction("c5", "far~g0123abcd", "s3", kind = "question", manager = "assistant"), // another group's: everyone's
        // Work's, on a session whose source does not let the phone answer
        Interaction("c6", "laptop", "s5", kind = "permission", manager = "m-2", sessionTitle = "Work source task"),
    ))

    private fun model(): AgentsViewModel {
        val stored = MutableStateFlow<Preferences>(emptyPreferences())
        return AgentsViewModel(object : SessionsRepository {
            override val sessions = MutableStateFlow(listOf(
                Session("box", "s1", title = "Example task", state = "working"),
                Session("laptop", "s2", title = "Work task", state = "working", manager = "m-2"),
                // another group's computers are no manager's of this hub: on every assistant's list
                Session("far~g0123abcd", "s3", title = "Elsewhere task", state = "working", manager = "assistant"),
                // a manager the roster no longer lists (deleted): the default's
                Session("old", "s4", title = "Old task", state = "working", manager = "m-gone"),
                Session("laptop", "s5", title = "Work source task", state = "working", manager = "m-2", sourceKind = "example-source"),
            ))
            override val machines = MutableStateFlow(listOf(
                Machine("box", "Example computer", online = true),
                Machine("laptop", "Work computer", online = true, manager = "m-2"),
                Machine("far~g0123abcd", "Elsewhere computer", online = true, manager = "assistant"),
                Machine("old", "Old computer", online = true, manager = "m-gone"),
            ))
            override suspend fun refresh() = Unit
        }, object : InteractionCenter {
            override val pending = requests
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
            override val data = stored
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                transform(stored.value).also { stored.value = it }
        }, object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }))
    }

    private fun current() = CurrentAssistant(roster, AssistantManagerChoice.current.value)

    @Test fun thePageListsTheCurrentAssistantsAgentsAndCards() {
        val model = model()
        AssistantManagerChoice.choose("hub", null, "assistant")
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                CompositionLocalProvider(LocalManagerRoster provides roster) { AgentsScreen(model, Nowhere, AgentsMetadata()) }
            }
        }
        fun ids() = model.page.value.sessions.map { it.sessionId }.sorted()
        fun cards() = shownRequests(requests.value, null, current(), model.page.value.sessions).map { it.id }.sorted()
        fun shown(text: String) = rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        rule.waitUntil(5_000) { model.page.value.loaded && ids() == listOf("s1", "s3", "s4") }
        assertEquals(listOf("box", "far~g0123abcd", "old"), model.page.value.machines.map { it.id }.sorted())
        // Its cards: its session's, the removed manager's, the unstamped one on no listed session, another group's.
        assertEquals(listOf("c1", "c3", "c4", "c5"), cards())
        rule.waitForIdle()
        // the list draws its computers (their sessions fold under them)
        assertTrue(shown("Example computer"))
        assertTrue(shown("Old computer"))
        assertFalse(shown("Work computer"))

        rule.runOnIdle { AssistantManagerChoice.pick("m-2") }
        rule.waitUntil(5_000) { ids() == listOf("s2", "s3", "s5") }
        assertEquals(listOf("far~g0123abcd", "laptop"), model.page.value.machines.map { it.id }.sorted())
        assertEquals(listOf("c2", "c5", "c6"), cards())
        rule.waitForIdle()
        assertTrue(shown("Work computer"))
        assertFalse(shown("Example computer"))
        assertFalse(shown("Old computer"))

        // Back on the default: an unstamped row is its, and so is one of a manager the roster no longer lists.
        rule.runOnIdle { AssistantManagerChoice.pick(null) }
        rule.waitUntil(5_000) { ids() == listOf("s1", "s3", "s4") }
        assertEquals(listOf("c1", "c3", "c4", "c5"), cards())
    }

    /** A computer picked on one assistant is let go on another that has no such computer, and the shell hears it (#3374 review). */
    @Test fun switchingLetsGoOfAnotherAssistantsComputer() {
        val model = model()
        var selected = "unset"
        AssistantManagerChoice.choose("hub", null, "assistant")
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                CompositionLocalProvider(LocalManagerRoster provides roster) {
                    AgentsScreen(model, Nowhere, AgentsMetadata(), onMachineSelected = { selected = it })
                }
            }
        }
        fun ids() = model.page.value.sessions.map { it.sessionId }.sorted()
        fun rows() = model.page.value.list.groups.flatMap { it.rows }.map { it.session.sessionId }.sorted()
        // A switch changes the assistant and then the filter: the second rebuild of the list is a task posted to the
        // main looper, which waitUntil alone does not run here.
        fun until(condition: () -> Boolean) = rule.waitUntil(5_000) {
            org.robolectric.Shadows.shadowOf(android.os.Looper.getMainLooper()).idle(); condition()
        }
        rule.runOnIdle { AssistantManagerChoice.pick("m-2") }
        until { model.page.value.loaded && ids() == listOf("s2", "s3", "s5") }
        rule.runOnIdle { model.machine("laptop") }
        until { model.page.value.settings.machineFilter == "laptop" && rows().isNotEmpty() && rows().all { it in setOf("s2", "s5") } }
        rule.waitForIdle()
        assertEquals("laptop", selected)
        // Switching to the default: its list, not an empty one filtered to a computer it does not have.
        rule.runOnIdle { AssistantManagerChoice.pick(null) }
        until { ids() == listOf("s1", "s3", "s4") && model.page.value.settings.machineFilter.isEmpty() && "s1" in rows() && "s4" in rows() }
        rule.waitForIdle()
        assertEquals("", selected)
        // A computer both assistants list (another group's) stays picked across a switch.
        rule.runOnIdle { model.machine("far~g0123abcd") }
        until { rows() == listOf("s3") }
        rule.runOnIdle { AssistantManagerChoice.pick("m-2") }
        until { ids() == listOf("s2", "s3", "s5") }
        assertEquals("far~g0123abcd", model.page.value.settings.machineFilter)
        assertEquals(listOf("s3"), rows())
    }

    /**
     * A notification opens another assistant's card over this assistant's page: its sheet reads that card's session and
     * computer from every manager's lists (the page lists only its own), so the computer shows and a session that cannot
     * be approved from the phone says so (#3374 review). The sheet itself needs the app's Hilt graph: its inputs here.
     */
    @Test fun aNotificationsCardOfAnotherAssistantKnowsItsSessionAndComputer() {
        val model = model()
        AssistantManagerChoice.choose("hub", null, "assistant")
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                CompositionLocalProvider(LocalManagerRoster provides roster) { AgentsScreen(model, Nowhere, AgentsMetadata()) }
            }
        }
        rule.waitUntil(5_000) { model.page.value.loaded && model.page.value.sessions.map { it.sessionId }.sorted() == listOf("s1", "s3", "s4") }
        val card = requests.value.first { it.id == "c6" }
        val page = model.page.value
        // The default's page lists neither Work's session nor its computer ...
        assertEquals(null, page.sessions.firstOrNull { it.key == card.sessionKey })
        assertEquals(null, page.machines.firstOrNull { it.id == card.machineId })
        // ... and the sheet finds both.
        val (session, machine) = cardContext(card, page.sessions, page.machines, model.everySession.value, model.everyMachine.value)
        assertEquals("s5", session?.sessionId)
        assertFalse(session!!.canApprove)
        assertEquals("Work computer", machine?.name)
        // One of the page's own still reads the page's.
        val own = requests.value.first { it.id == "c1" }
        assertEquals(page.sessions.first { it.sessionId == "s1" }, cardContext(own, page.sessions, page.machines, model.everySession.value,
            model.everyMachine.value).first)
    }

    private object Nowhere : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
}

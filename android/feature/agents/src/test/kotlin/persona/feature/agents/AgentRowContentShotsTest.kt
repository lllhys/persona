package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onRoot
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.AppStyle
import persona.core.designsystem.HomeChrome
import persona.core.designsystem.LocalHomeChrome
import persona.core.designsystem.PersonaTheme
import persona.core.domain.sort.AgentGrouping
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File

/**
 * The Agents list's rows (#4338 A9) on two computers, in By Repository and By Status: on Glass iOS's Avatar row, the
 * agent's mark in a well with its state on the corner and the third line (where it was opened, where it runs, the
 * model, the to-do, the bell); Brief and Board as they were. Every style in light and dark, the sessions as the hub's
 * JSON. PNGs in `build/row-content-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h1700dp-xhdpi")
@org.robolectric.annotation.GraphicsMode(org.robolectric.annotation.GraphicsMode.Mode.NATIVE)
class AgentRowContentShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val sessions = listOf(
        """"machine_id":"mac","session_id":"waiting","title":"Start later","state":"idle","status":"idle","agent":"codex",
           "awaiting_first_message":true,"cwd":"/code/app","pane_id":"waiting-pane","space_name":"app","slot_name":"codex"""",
        """"machine_id":"mac","session_id":"needs","title":"Approve the migration","state":"needs_you","cwd":"/code/app","pane_id":"w1:p1",
           "space_id":"s1","space_name":"app","slot_id":"t1","slot_index":1,"model":"claude-opus-4-6","last_text":"Run the database migration on staging?"""",
        """"machine_id":"mac","session_id":"working","title":"Fix the login bug","state":"working","cwd":"/code/app","pane_id":"w1:p2",
           "space_id":"s1","space_name":"app","slot_id":"t2","slot_name":"login","model":"claude-sonnet-4-5",
           "todo":{"id":"t1","title":"Ship 1.2"},"last_text":"Updating the retry logic"""",
        """"machine_id":"mac","session_id":"background","title":"Watch the CI run","state":"working","background_waiting":true,
           "background_working":true,"cwd":"/code/app/.worktrees/ci","pane_id":"w2:p1","space_id":"s2","space_name":"ci","slot_id":"t3",
           "slot_index":1,"agent":"codex","model":"gpt-5-codex","last_text":"Waiting for the test job"""",
        """"machine_id":"mac","session_id":"muted","title":"Side experiment","state":"working","cwd":"/code/notes","pane_id":"w3:p1",
           "space_id":"s3","space_name":"notes","slot_id":"t4","slot_index":1,"model":"claude-haiku-4-5","last_text":"Trying another layout"""",
        """"machine_id":"box","session_id":"unread","title":"Summarize the logs","state":"unread","cwd":"/srv/api","pane_id":"botmux:topic",
           "space_id":"s5","space_name":"api","slot_id":"t5","slot_index":2,"model":"claude-opus-4-6","last_text":"Found three timeouts in the gateway"""",
        """"machine_id":"box","session_id":"awaiting","title":"Pick a release name","state":"idle","awaits_reply":true,"cwd":"/srv/api",
           "pane_id":"w5:p1","space_id":"s5","space_name":"api","slot_id":"t6","slot_index":3,"todo":{"id":"t2","title":"Release 1.2"},
           "last_text":"Which name do you prefer?"""",
        """"machine_id":"box","session_id":"seen","title":"Update the docs","state":"idle","cwd":"/srv/api","pane_id":"w5:p2",
           "space_id":"s5","space_name":"api","slot_id":"t7","slot_name":"docs","agent":"pi","model":"pi-default","last_text":"The docs are updated"""",
    ).map { JsonCodec.decodeFromString(Session.serializer(), """{$it,"updated_at":"2026-01-01T00:00:00Z"}""") }

    @Test fun glassLight() = shoot(AppStyle.Glass, dark = false)
    @Test fun glassDark() = shoot(AppStyle.Glass, dark = true)
    @Test fun briefLight() = shoot(AppStyle.Brief, dark = false)
    @Test fun briefDark() = shoot(AppStyle.Brief, dark = true)
    @Test fun boardLight() = shoot(AppStyle.Board, dark = false)
    @Test fun boardDark() = shoot(AppStyle.Board, dark = true)

    /** TalkBack still hears each row's state, and on Glass its third line: the place, the model, the to-do and the bell. */
    @Test fun talkBackHearsTheStateAndTheThirdLine() {
        val model = open(AppStyle.Glass, dark = false)
        rule.runOnIdle { model.grouping(AgentGrouping.State) }
        rule.waitForIdle()
        fun row(title: String) = rule.onNode(hasText(title) and SemanticsMatcher.keyIsDefined(SemanticsActions.OnLongClick)).fetchSemanticsNode().config
        fun said(title: String) = row(title).let { it.getOrNull(SemanticsProperties.ContentDescription).orEmpty() + it.getOrNull(SemanticsProperties.Text).orEmpty().map { t -> t.text } }
        // Under its state's section the row writes no state word: it is told by name.
        assertEquals("Working", row("Fix the login bug").getOrNull(SemanticsProperties.StateDescription))
        assertEquals("Needs you", row("Approve the migration").getOrNull(SemanticsProperties.StateDescription))
        val working = said("Fix the login bug")
        assertTrue(working.toString(), "To-do: Ship 1.2" in working)
        assertTrue(working.toString(), "Sonnet 4.5" in working)
        assertTrue(working.toString(), "app › login · MacBook" in working)
        assertTrue(working.toString(), "Terminal" in working)
        assertTrue("Notifications muted" in said("Side experiment"))
        // Elsewhere the word leads line 2 and is read with it; a row under its space's card names the tab alone.
        rule.runOnIdle { model.grouping(AgentGrouping.Repo) }
        rule.waitForIdle()
        val needs = said("Approve the migration")
        assertTrue(needs.toString(), "Needs you  Run the database migration on staging?" in needs)
        assertTrue(needs.toString(), "#1" in needs)
    }

    private fun open(style: AppStyle, dark: Boolean): AgentsViewModel {
        val model = AgentsViewModel(object : SessionsRepository {
            override val sessions = MutableStateFlow(this@AgentRowContentShotsTest.sessions)
            override val machines = MutableStateFlow(listOf(Machine("mac", "MacBook", online = true), Machine("box", "Workstation", online = true, icon = "desktop")))
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
            override val data = MutableStateFlow<Preferences>(emptyPreferences())
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences) =
                transform(data.value).also { data.value = it }
        }, object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot(mutes = HubPreferenceSet(sessions = listOf("mac/muted"))))
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }))
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalHomeChrome provides HomeChrome(emptyList(), "Agents", onSelect = {}, leading = {})) {
                    AgentsScreen(model, Nowhere, AgentsMetadata())
                }
            }
        }
        rule.waitUntil(5_000) { model.page.value.loaded }
        return model
    }

    private fun shoot(style: AppStyle, dark: Boolean) {
        val model = open(style, dark)
        val out = File(System.getProperty("persona.shots") ?: "build/row-content-shots").also { it.mkdirs() }
        fun save(name: String) = rule.onRoot().captureToImage().asAndroidBitmap().let { bitmap ->
            out.resolve("$name-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream().use {
                bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it)
            }
        }
        rule.runOnIdle { model.grouping(AgentGrouping.Repo) }
        rule.waitForIdle()
        val idleGroup = model.page.value.list.groups.first { group -> group.idleFold?.rows?.any { it.session.sessionId == "waiting" } == true }
        rule.runOnIdle { model.showIdle(idleGroup) }
        rule.waitForIdle()
        rule.onNode(hasText("Waiting for its first message", substring = true)).assertExists()
        save("by-repository")
        rule.runOnIdle { model.grouping(AgentGrouping.State) }
        rule.waitForIdle()
        save("by-status")
    }

    private object Nowhere : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
}

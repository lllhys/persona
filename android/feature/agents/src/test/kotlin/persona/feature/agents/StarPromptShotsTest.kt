package persona.feature.agents

import android.content.Context
import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
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
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.AppStyle
import persona.core.designsystem.HomeChrome
import persona.core.designsystem.LocalHomeChrome
import persona.core.designsystem.LocalStarPrompt
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.StarPromptStore
import persona.core.domain.PERSONA_REPO_URL
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File
import java.time.Instant

/**
 * The GitHub star prompts on the Agents list (#5761, docs/design/DESIGN_SYSTEM.md "GitHub star prompts"): the pill under
 * the first-run empty state and the card in the notice slot, when each shows and what its buttons do, and both pictured
 * in every style, light and dark (`build/star-prompt-shots/`, or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class StarPromptShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val context: Context get() = rule.activity
    private val now = Instant.parse("2026-11-05T09:30:00Z")
    private fun ago(minutes: Long) = now.minusSeconds(minutes * 60).toString()
    private val computer = Machine("mac", "MacBook Pro", online = true, engine = "herdr")
    private val quiet = listOf(
        Session("mac", "s1", title = "Refactor API client", cwd = "/code/app", state = "working", spaceId = "w1", spaceName = "app",
            slotName = "1", lastText = "Updating the retry logic", updatedAt = ago(1)),
        Session("mac", "s2", title = "Weekly notes", cwd = "/code/notes", state = "unread", spaceId = "w2", spaceName = "notes",
            slotName = "1", lastText = "Summary is ready", updatedAt = ago(40)),
        Session("mac", "s3", title = "Log cleanup", cwd = "/code/app", state = "idle", spaceId = "w1", spaceName = "app",
            slotName = "2", updatedAt = ago(60 * 26)),
    )
    private val waiting = Session("mac", "s4", title = "Fix login page", cwd = "/code/app", state = "needs_you", spaceId = "w1",
        spaceName = "app", slotName = "3", lastText = "Allow running the tests?", updatedAt = ago(2))

    @After fun clear() {
        context.getSharedPreferences(StarPromptStore.NAME, Context.MODE_PRIVATE).edit().clear().commit()
    }

    @Test fun thePillShowsOnTheFirstRunAndItsCrossHidesIt() {
        val store = StarPromptStore(context)
        show(model(emptyList()), store)
        rule.onNodeWithText("Start your first coding session", substring = true).assertIsDisplayed()
        rule.onNodeWithTag("agents.starPill").assertIsDisplayed()
        rule.onNodeWithTag("agents.starCard").assertDoesNotExist()
        rule.onNodeWithContentDescription("Hide").performClick()
        rule.waitForIdle()
        assertTrue(store.state.value.pillHidden)
        rule.onNodeWithTag("agents.starPill").assertDoesNotExist()
    }

    @Test fun thePillStarsAndOpensTheProjectInTheBrowser() {
        val store = StarPromptStore(context)
        show(model(emptyList()), store)
        rule.onNodeWithText("Star on GitHub").performClick()
        rule.waitForIdle()
        assertTrue(store.state.value.done)
        val opened = shadowOf(rule.activity).nextStartedActivity
        assertEquals(PERSONA_REPO_URL, opened.dataString)
        rule.onNodeWithTag("agents.starPill").assertDoesNotExist()
    }

    @Test fun noPillWithoutAComputerOrOnceSessionsExist() {
        val store = StarPromptStore(context)
        val machines = MutableStateFlow(emptyList<Machine>())
        val repository = Repository(MutableStateFlow(emptyList()), machines)
        show(model(repository), store)
        rule.onNodeWithText("No computers yet", substring = true).assertIsDisplayed()
        rule.onNodeWithTag("agents.starPill").assertDoesNotExist()
        rule.runOnIdle { machines.value = listOf(computer) }
        rule.waitForIdle()
        rule.onNodeWithTag("agents.starPill").assertIsDisplayed()
        rule.runOnIdle { repository.sessions.value = quiet }
        rule.waitForIdle()
        rule.onNodeWithTag("agents.starPill").assertDoesNotExist()
    }

    @Test fun theCardWaitsForTheThresholdAndForEveryWaitingSession() {
        val store = StarPromptStore(context)
        repeat(19) { store.launched() }
        val repository = Repository(MutableStateFlow(quiet), MutableStateFlow(listOf(computer)))
        show(model(repository), store)
        rule.onNodeWithText("Refactor API client").assertIsDisplayed()
        rule.onNodeWithTag("agents.starCard").assertDoesNotExist()
        rule.runOnIdle { store.launched() }
        rule.waitForIdle()
        rule.onNodeWithTag("agents.starCard").assertIsDisplayed()
        // A session that needs the user: the card never pushes it down.
        rule.runOnIdle { repository.sessions.value = quiet + waiting }
        rule.waitForIdle()
        rule.onNodeWithText("Terminal needs you").assertExists()
        rule.onNodeWithTag("agents.starCard").assertDoesNotExist()
        rule.runOnIdle { repository.sessions.value = quiet }
        rule.waitForIdle()
        rule.onNodeWithTag("agents.starCard").assertIsDisplayed()
        // ✕ is Later: the threshold doubles and the card waits three days.
        rule.onNodeWithContentDescription("Later").performClick()
        rule.waitForIdle()
        assertEquals(40, store.state.value.threshold)
        rule.onNodeWithTag("agents.starCard").assertDoesNotExist()
    }

    @Test fun theCardWaitsWhileSearching() {
        val store = StarPromptStore(context)
        repeat(20) { store.launched() }
        val model = model(Repository(MutableStateFlow(quiet), MutableStateFlow(listOf(computer))))
        show(model, store)
        rule.onNodeWithTag("agents.starCard").assertIsDisplayed()
        rule.runOnIdle { model.query("notes") }
        rule.waitForIdle()
        rule.onNodeWithTag("agents.starCard").assertDoesNotExist()
    }

    @Test fun pictures() = pictures("en", AppStyle.entries, listOf(false, true))

    @Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
    @Test fun picturesInChinese() = pictures("zh", listOf(AppStyle.Glass), listOf(false))

    @Config(sdk = [35], qualifiers = "ja-w400dp-h900dp-xhdpi")
    @Test fun picturesInJapanese() = pictures("ja", listOf(AppStyle.Glass), listOf(false))

    private fun pictures(language: String, styles: List<AppStyle>, darks: List<Boolean>) {
        val out = File(System.getProperty("persona.shots") ?: "build/star-prompt-shots").also { it.mkdirs() }
        val store = StarPromptStore(context)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var card by mutableStateOf(false)
        val empty = model(emptyList())
        val list = model(quiet)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalStarPrompt provides store,
                    LocalHomeChrome provides HomeChrome(emptyList(), "Agents", onSelect = {}, leading = {})) {
                    Box(Modifier.testTag("shot").width(400.dp).height(900.dp).background(PersonaTheme.palette.background)) {
                        if (card) AgentsScreen(list, Nowhere, AgentsMetadata()) else AgentsScreen(empty, Nowhere, AgentsMetadata())
                    }
                }
            }
        }
        for (shot in listOf("pill", "card")) {
            rule.runOnIdle {
                card = shot == "card"
                if (card) repeat(20 - store.state.value.launches) { store.launched() }
            }
            rule.waitUntil(5_000) { (if (card) list else empty).page.value.loaded }
            for (s in styles) for (d in darks) {
                rule.runOnIdle { style = s; dark = d }
                rule.waitForIdle()
                rule.onNodeWithTag(if (card) "agents.starCard" else "agents.starPill").assertIsDisplayed()
                val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                val prefix = if (language == "en") "star" else "star-$language"
                File(out, "$prefix-$shot-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                    .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            }
        }
    }

    private fun show(model: AgentsViewModel, store: StarPromptStore) {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalStarPrompt provides store,
                    LocalHomeChrome provides HomeChrome(emptyList(), "Agents", onSelect = {}, leading = {})) {
                    AgentsScreen(model, Nowhere, AgentsMetadata())
                }
            }
        }
        rule.waitUntil(5_000) { model.page.value.loaded }
        rule.waitForIdle()
    }

    private class Repository(override val sessions: MutableStateFlow<List<Session>>, override val machines: MutableStateFlow<List<Machine>>) : SessionsRepository {
        override suspend fun refresh() = Unit
    }

    private fun model(sessions: List<Session>) = model(Repository(MutableStateFlow(sessions), MutableStateFlow(listOf(computer))))

    private fun model(repository: SessionsRepository) = AgentsViewModel(repository, object : InteractionCenter {
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
    }))

    private object Nowhere : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
}

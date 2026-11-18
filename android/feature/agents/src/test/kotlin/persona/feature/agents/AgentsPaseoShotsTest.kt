package persona.feature.agents

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File
import java.time.Instant

/**
 * A computer whose engine is Paseo in the Agents list (#8143): each provider's agent by its own name and mark (Grok's
 * letter, TRAE's mark, a row the bridge left unlabelled by its launcher, Claude Code's), never "Paseo"; its card says
 * the provider asks, and one naming only the engine (an older node's) says only what it wants. Every style, light and
 * dark. PNGs in `build/agents-paseo-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1400dp-xhdpi")
class AgentsPaseoShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/agents-paseo-shots").also { it.mkdirs() }

    private val now = Instant.now()
    private fun ago(minutes: Long) = now.minusSeconds(minutes * 60).toString()
    private val machines = listOf(Machine("studio", "Studio", online = true, engine = "paseo"))
    private val caps = SessionCapabilities(read = true, send = true, interrupt = true, approve = true, close = true, rename = true, modes = false, model = true)
    private fun paseo(id: String, title: String, label: String, launcher: String, model: String, state: String, minutes: Long, words: String) =
        Session("studio", id, title = title, cwd = "/code/albums", state = state, agent = PaseoAgent, agentLabelHint = label, launcher = launcher,
            model = model, sourceKind = "runtime", sourceInstanceId = "paseo", spaceKind = "paseo", spaceId = "albums", spaceName = "albums",
            lastText = words, updatedAt = ago(minutes), capabilities = caps)
    private val sessions = listOf(
        paseo("paseo-grok", "Sort the photo albums", "Grok", "grok", "grok-5", "needs_you", 1, "May I list the folder?"),
        paseo("paseo-trae", "Tidy the release notes", "TRAE CLI", "traecli", "trae-pro", "working", 3, "Rewriting the summary"),
        paseo("paseo-kimi", "Check the build", "Paseo", "kimi", "kimi-k3", "needs_you", 6, "Which branch?"),
        Session("studio", "claude-1", title = "Fix the login page", cwd = "/code/albums", state = "unread", agent = "claude", launcher = "my-claude",
            model = "claude-sonnet-4-5", sourceKind = "runtime", sourceInstanceId = "paseo", spaceKind = "paseo", spaceId = "albums", spaceName = "albums",
            lastText = "Done", updatedAt = ago(8), capabilities = caps.copy(modes = true)),
    )
    private val cards = listOf(
        Interaction("ix-grok", "studio", "paseo-grok", sessionTitle = "Sort the photo albums", kind = "permission", toolName = "Bash",
            toolInput = JsonObject(mapOf("command" to JsonPrimitive("ls ~/Pictures"))), agent = PaseoAgent, agentLabelHint = "Grok",
            createdAt = ago(1), expiresAt = now.plusSeconds(1_200).toString()),
        // An older node labels the card with the engine's name: it can't say who asks.
        Interaction("ix-kimi", "studio", "paseo-kimi", sessionTitle = "Check the build", kind = "question", agent = PaseoAgent, agentLabelHint = "Paseo",
            questions = listOf(Question(question = "Which branch?", options = listOf(QuestionOption("main"), QuestionOption("release")))),
            createdAt = ago(6), expiresAt = now.plusSeconds(1_200).toString()),
    )

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override val lastHello: Hello get() = Hello(serverVersion = "1.0.0", protocol = 1)
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }
    private class Interactions(cards: List<Interaction>) : InteractionCenter {
        override val pending = MutableStateFlow(cards)
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }
    private class Sender : MessageSender {
        override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
    }
    private class Repository(sessions: List<Session>, machines: List<Machine>) : SessionsRepository {
        override val sessions = MutableStateFlow(sessions)
        override val machines = MutableStateFlow(machines)
        override suspend fun refresh() = Unit
    }
    private object Nowhere : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }

    private fun preferences(): AgentsPreferences {
        val store = object : DataStore<Preferences> {
            override val data = MutableStateFlow<Preferences>(emptyPreferences())
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                transform(data.value).also { data.value = it }
        }
        val hub = object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }
        return AgentsPreferences(store, hub)
    }

    @Test fun eachProviderByItsOwnNameAndMark() {
        val model = AgentsViewModel(Repository(sessions, machines), Interactions(cards), Connection(), Sender(), preferences())
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(1400.dp).background(PersonaTheme.palette.background)) {
                    AgentsScreen(model, Nowhere, AgentsMetadata())
                }
            }
        }
        rule.waitUntil(5_000) { model.page.value.loaded }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            rule.runOnIdle { style = s; dark = d }
            rule.waitForIdle()
            val look = "${s.name.lowercase()}-${if (d) "dark" else "light"}"
            val list = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "agents-paseo-$look.png").outputStream().use { assertTrue(list.compress(Bitmap.CompressFormat.PNG, 100, it)) }
            // The cards: the provider asks; one that can't name it says only what it wants.
            for (title in listOf("Grok is waiting for your approval", "Has a question")) {
                assertTrue("$title in $look", rule.onAllNodesWithText(title, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
            }
            // The rows' marks, by their providers' names: Grok's letter, TRAE's mark, the unlabelled one's launcher, Claude Code.
            for (said in listOf("Grok", "TRAE CLI", "kimi", "Claude Code")) {
                assertTrue("$said's mark in $look", rule.onAllNodesWithContentDescription(said, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
            }
            // The engine's name is nowhere on the page.
            rule.onAllNodesWithText("paseo", substring = true, ignoreCase = true, useUnmergedTree = true).assertCountEquals(0)
            rule.onAllNodesWithContentDescription("paseo", substring = true, ignoreCase = true, useUnmergedTree = true).assertCountEquals(0)
        }
    }
}

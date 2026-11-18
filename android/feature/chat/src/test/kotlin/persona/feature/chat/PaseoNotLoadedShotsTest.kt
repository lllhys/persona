package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Interaction
import persona.core.contracts.Item
import persona.core.contracts.Machine
import persona.core.contracts.Resolution
import persona.core.contracts.Session
import persona.core.contracts.SessionCapabilities
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatRepository
import persona.core.contracts.api.ChatSnapshot
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.api.SessionsRepository
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File
import java.time.Instant

/**
 * Opening a closed Grok agent's chat on Paseo whose first read is refused as not_loaded (#8321): while the watch loads
 * the agent, the real chat page shows its usual loading state (Glass's skeleton rows, Brief and Board's spinner), never
 * the refusal, "Couldn’t sync messages" or an empty chat; then the history the watch's backlog brings. The snapshots are
 * the ones `HubChatRepository` publishes then (core:data `ChatNotLoadedOpenTest`). Every style, light and dark. PNGs in
 * `build/paseo-not-loaded-shots/` (or `-Dpersona.shots=<dir>`). Native JVM rendering: no system bars, no device.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h760dp-xhdpi")
class PaseoNotLoadedShotsTest {
    @get:Rule val rule = createComposeRule()

    private val key = SessionKey("computer-a", "paseo-agent-1")
    private val start = Instant.parse("2026-11-12T01:00:00Z")
    private val history = listOf(
        Item("u1", 1, role = "user", kind = "text", text = "Sort the holiday photos by place", ts = start.toString()),
        Item("a2", 2, role = "assistant", kind = "text", text = "Done: 212 photos in 9 albums, one per town.", ts = start.plusSeconds(60).toString()),
    )
    // Closed: Paseo let the agent go after an hour idle (the bridge lists it offline).
    private val grok = Session(key.machineId, key.sessionId, title = "Sort the holiday photos", cwd = "/home/me/photos",
        status = "offline", agent = "paseo", agentLabelHint = "Grok", launcher = "grok", sourceKind = "runtime", spaceKind = "paseo",
        capabilities = SessionCapabilities(read = true, send = true, interrupt = true, approve = true, close = true, rename = true))
    private val refusal = "This agent isn’t running in Paseo. Its conversation loads while its chat is open."
    private val never = listOf(refusal, "Couldn’t sync messages", "No messages yet")

    private fun texts(): List<String> = rule.onAllNodes(SemanticsMatcher("any node") { true }, useUnmergedTree = true).fetchSemanticsNodes().flatMap { node ->
        node.config.getOrNull(SemanticsProperties.Text).orEmpty().map { it.text } +
            node.config.getOrNull(SemanticsProperties.ContentDescription).orEmpty()
    }

    /** Frames by hand (the spinner never settles), real time until [ready] holds for what the page shows. */
    private fun until(what: String, ready: (List<String>) -> Boolean) {
        val deadline = System.currentTimeMillis() + 30_000
        while (true) {
            Snapshot.sendApplyNotifications()
            rule.mainClock.advanceTimeByFrame()
            if (ready(texts())) break
            check(System.currentTimeMillis() < deadline) { "$what never showed; shown: ${texts()}" }
            Thread.sleep(20)
        }
        repeat(10) { rule.mainClock.advanceTimeByFrame() }
    }

    private fun shoot(name: String) {
        val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val output = File(System.getProperty("persona.shots") ?: "build/paseo-not-loaded-shots").also { it.mkdirs() }
        File(output, "$name.png").outputStream().use { assertTrue(image.compress(Bitmap.CompressFormat.PNG, 100, it)) }
    }

    private fun scene(style: AppStyle, dark: Boolean) {
        // What the repository publishes while the watch loads the agent, then once its backlog came.
        val snapshot = MutableStateFlow(ChatSnapshot(loading = true, verified = false))
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow(listOf(Machine(key.machineId, "Computer A", online = true)))
            override val sessions = MutableStateFlow(listOf(grok))
            override suspend fun refresh() = Unit
        }
        val chats = object : ChatRepository {
            override fun observe(session: SessionKey) = snapshot
            override suspend fun loadOlder(session: SessionKey) = Unit
            override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int): List<Item> = emptyList()
        }
        val sender = object : MessageSender {
            override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
        }
        val interactions = object : InteractionCenter {
            override val pending = MutableStateFlow(emptyList<Interaction>())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
        }
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("shot")) {
                    ChatScreen(key, chats, sender, interactions, sessions, onBack = {})
                }
            }
        }
        val look = "${style.name.lowercase()}-${if (dark) "dark" else "light"}"
        until("the loading state") { shown -> "Loading" in shown && shown.any { it.startsWith("Sort the holiday photos") } }
        assertEquals("$look while loading", emptyList<String>(), texts().filter { shown -> never.any { shown.contains(it) } })
        shoot("paseo-not-loaded-loading-$look")
        rule.runOnIdle { snapshot.value = ChatSnapshot(items = history, loading = false) }
        until("the history") { shown -> shown.any { it.startsWith("Done: 212 photos") } }
        assertTrue("$look: no loading left", "Loading" !in texts())
        assertEquals("$look with the history", emptyList<String>(), texts().filter { shown -> never.any { shown.contains(it) } })
        shoot("paseo-not-loaded-history-$look")
    }

    @Test fun glass() = scene(AppStyle.Glass, false)
    @Test fun glassDark() = scene(AppStyle.Glass, true)
    @Test fun brief() = scene(AppStyle.Brief, false)
    @Test fun briefDark() = scene(AppStyle.Brief, true)
    @Test fun board() = scene(AppStyle.Board, false)
    @Test fun boardDark() = scene(AppStyle.Board, true)
}

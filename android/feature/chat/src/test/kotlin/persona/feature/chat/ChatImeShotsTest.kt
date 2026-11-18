package persona.feature.chat

import android.graphics.Bitmap
import android.view.View
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.imePadding
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.core.graphics.Insets
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
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
 * The chat pages with the keyboard down and up (#8334): an Agent chat and the assistant conversation embedded under a
 * page's imePadding, in every style, light and dark. Following the keyboard moved from composition to layout, so these
 * must not change; compare them with the same test on the base commit. PNGs in build/chat-ime-shots/.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class ChatImeShotsTest {
    @get:Rule val rule = createComposeRule()

    private val key = SessionKey("computer-a", "importer")
    private val start = Instant.parse("2026-11-12T01:00:00Z")
    private fun at(seq: Int) = start.plusSeconds(seq * 60L).toString()
    private val exchange = listOf(
        Item("u1", 1, role = "user", kind = "text", text = "Make the importer retry once when the hub restarts", ts = at(1)),
        Item("a2", 2, role = "assistant", kind = "text", ts = at(2),
            text = "Done. The importer now retries once after a restart, and a test covers the restart path.\n\n- `Importer.kt`: one retry with a 2 s backoff\n- `ImporterTest.kt`: a new restart case"),
        Item("u3", 3, role = "user", kind = "text", text = "Does it also log the retry?", ts = at(3)),
        Item("a4", 4, role = "assistant", kind = "text", ts = at(4),
            text = "Yes, one info line per retry, with the hub id. Nothing is logged when the first try succeeds."),
        Item("u5", 5, role = "user", kind = "text", text = "Great, ship it", ts = at(5)),
        Item("a6", 6, role = "assistant", kind = "text", ts = at(6),
            text = "Pushed to the branch and opened the PR. CI is running the unit tests now."),
    )
    private val agent = Session(key.machineId, key.sessionId, title = "Fix the importer retry", agent = "claude", status = "idle",
        cwd = "/home/me/code/app")

    private fun texts(): List<String> = rule.onAllNodes(SemanticsMatcher("any node") { true }, useUnmergedTree = true).fetchSemanticsNodes().flatMap { node ->
        node.config.getOrNull(SemanticsProperties.Text).orEmpty().map { it.text } +
            node.config.getOrNull(SemanticsProperties.ContentDescription).orEmpty()
    }

    private fun scene(assistant: Boolean, keyboard: Boolean, style: AppStyle, dark: Boolean) {
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow(listOf(Machine(key.machineId, "Computer A", online = true)))
            override val sessions = MutableStateFlow(listOf(agent))
            override suspend fun refresh() = Unit
        }
        val chats = object : ChatRepository {
            override fun observe(session: SessionKey) = MutableStateFlow(ChatSnapshot(items = exchange, loading = false))
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
        lateinit var view: View
        rule.mainClock.autoAdvance = false
        rule.setContent {
            view = LocalView.current
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("shot")) {
                    if (assistant) Box(Modifier.fillMaxSize().imePadding()) {
                        ChatScreen(key, chats, sender, interactions, sessions, onBack = {},
                            renderStyle = ChatRenderStyle.Assistant, embedded = true)
                    } else ChatScreen(key, chats, sender, interactions, sessions, onBack = {})
                }
            }
        }
        val deadline = System.currentTimeMillis() + 30_000
        while (true) {
            Snapshot.sendApplyNotifications()
            rule.mainClock.advanceTimeByFrame()
            if (texts().any { it.startsWith("Pushed to the branch") }) break
            check(System.currentTimeMillis() < deadline) { "never showed; shown: ${texts()}" }
            Thread.sleep(20)
        }
        if (keyboard) {
            // A phone keyboard's height (300 dp at xhdpi), reached in steps as the slide does.
            for (bottom in listOf(150, 300, 450, 600)) {
                rule.runOnUiThread {
                    ViewCompat.dispatchApplyWindowInsets(view, WindowInsetsCompat.Builder()
                        .setInsets(WindowInsetsCompat.Type.ime(), Insets.of(0, 0, 0, bottom))
                        .setVisible(WindowInsetsCompat.Type.ime(), true).build())
                }
                Snapshot.sendApplyNotifications()
                rule.mainClock.advanceTimeByFrame()
            }
        }
        repeat(10) { rule.mainClock.advanceTimeByFrame() }
        val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val output = File(System.getProperty("persona.shots") ?: "build/chat-ime-shots").also { it.mkdirs() }
        val name = "${if (assistant) "assistant" else "agent"}-${if (keyboard) "keyboard" else "nokeyboard"}-" +
            "${style.name.lowercase()}-${if (dark) "dark" else "light"}.png"
        File(output, name).outputStream().use { assertTrue(image.compress(Bitmap.CompressFormat.PNG, 100, it)) }
    }

    @Test fun agentKeyboardDownGlassLight() = scene(false, false, AppStyle.Glass, false)
    @Test fun agentKeyboardDownGlassDark() = scene(false, false, AppStyle.Glass, true)
    @Test fun agentKeyboardDownBriefLight() = scene(false, false, AppStyle.Brief, false)
    @Test fun agentKeyboardDownBriefDark() = scene(false, false, AppStyle.Brief, true)
    @Test fun agentKeyboardDownBoardLight() = scene(false, false, AppStyle.Board, false)
    @Test fun agentKeyboardDownBoardDark() = scene(false, false, AppStyle.Board, true)
    @Test fun agentKeyboardUpGlassLight() = scene(false, true, AppStyle.Glass, false)
    @Test fun agentKeyboardUpGlassDark() = scene(false, true, AppStyle.Glass, true)
    @Test fun agentKeyboardUpBriefLight() = scene(false, true, AppStyle.Brief, false)
    @Test fun agentKeyboardUpBriefDark() = scene(false, true, AppStyle.Brief, true)
    @Test fun agentKeyboardUpBoardLight() = scene(false, true, AppStyle.Board, false)
    @Test fun agentKeyboardUpBoardDark() = scene(false, true, AppStyle.Board, true)
    @Test fun assistantKeyboardDownGlassLight() = scene(true, false, AppStyle.Glass, false)
    @Test fun assistantKeyboardDownGlassDark() = scene(true, false, AppStyle.Glass, true)
    @Test fun assistantKeyboardDownBriefLight() = scene(true, false, AppStyle.Brief, false)
    @Test fun assistantKeyboardDownBriefDark() = scene(true, false, AppStyle.Brief, true)
    @Test fun assistantKeyboardDownBoardLight() = scene(true, false, AppStyle.Board, false)
    @Test fun assistantKeyboardDownBoardDark() = scene(true, false, AppStyle.Board, true)
    @Test fun assistantKeyboardUpGlassLight() = scene(true, true, AppStyle.Glass, false)
    @Test fun assistantKeyboardUpGlassDark() = scene(true, true, AppStyle.Glass, true)
    @Test fun assistantKeyboardUpBriefLight() = scene(true, true, AppStyle.Brief, false)
    @Test fun assistantKeyboardUpBriefDark() = scene(true, true, AppStyle.Brief, true)
    @Test fun assistantKeyboardUpBoardLight() = scene(true, true, AppStyle.Board, false)
    @Test fun assistantKeyboardUpBoardDark() = scene(true, true, AppStyle.Board, true)
}

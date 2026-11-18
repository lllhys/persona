package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.Density
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
import persona.core.contracts.api.DeliveryState
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.api.SessionsRepository
import persona.core.designsystem.AppStyle
import persona.core.designsystem.AppStyleStore
import persona.core.designsystem.LayoutDensity
import persona.core.designsystem.PersonaTheme
import java.io.File
import java.time.Instant

/** Chat page, Standard vs Compact density (#5564). PNGs in build/chat-density-shots/. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class ChatDensityShotsTest {
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

    private fun scene(density: LayoutDensity, style: AppStyle, dark: Boolean) {
        val context = androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>()
        AppStyleStore(context).setDensity(density)
        val snapshot = ChatSnapshot(items = exchange, loading = false)
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow(listOf(Machine(key.machineId, "Computer A", online = true)))
            override val sessions = MutableStateFlow(listOf(agent))
            override suspend fun refresh() = Unit
        }
        val chats = object : ChatRepository {
            override fun observe(session: SessionKey) = MutableStateFlow(snapshot)
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
        val deadline = System.currentTimeMillis() + 30_000
        while (true) {
            Snapshot.sendApplyNotifications()
            rule.mainClock.advanceTimeByFrame()
            if (texts().any { it.startsWith("Pushed to the branch") }) break
            check(System.currentTimeMillis() < deadline) { "never showed; shown: ${texts()}" }
            Thread.sleep(20)
        }
        repeat(10) { rule.mainClock.advanceTimeByFrame() }
        val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val output = File(System.getProperty("persona.shots") ?: "build/chat-density-shots").also { it.mkdirs() }
        File(output, "chat-${density.key}-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream()
            .use { assertTrue(image.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        AppStyleStore(context).setDensity(LayoutDensity.Standard)
    }

    @Test fun standardGlass() = scene(LayoutDensity.Standard, AppStyle.Glass, false)
    @Test fun standardBrief() = scene(LayoutDensity.Standard, AppStyle.Brief, false)
    @Test fun standardBoard() = scene(LayoutDensity.Standard, AppStyle.Board, false)
    @Test fun compactGlass() = scene(LayoutDensity.Compact, AppStyle.Glass, false)
    @Test fun compactBrief() = scene(LayoutDensity.Compact, AppStyle.Brief, false)
    @Test fun compactBoard() = scene(LayoutDensity.Compact, AppStyle.Board, false)
    @Test fun standardGlassDark() = scene(LayoutDensity.Standard, AppStyle.Glass, true)
    @Test fun compactGlassDark() = scene(LayoutDensity.Compact, AppStyle.Glass, true)
}

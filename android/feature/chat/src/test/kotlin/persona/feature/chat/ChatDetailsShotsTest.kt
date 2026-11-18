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
import persona.core.designsystem.PersonaTheme
import java.io.File
import java.time.Instant

/**
 * The real chat page in four states of docs/plans/android-ios-alignment/chats.md C9, every style, light and dark: a
 * message whose turn has not begun ("ready"), the first page loading ("loading"), a load error ("error") and a recap at the
 * end ("recap"), and the Glass two at the largest text size. The same file draws `main` for the comparison: it uses nothing new. PNGs in `build/chat-details-shots/`
 * (or `-Dpersona.shots=<dir>`). Native JVM rendering: no system bars, no glass refraction, no device.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h760dp-xhdpi")
class ChatDetailsShotsTest {
    @get:Rule val rule = createComposeRule()

    private val key = SessionKey("computer-a", "importer")
    // A fixed hour, so two runs (main and this branch) draw the same times.
    private val start = Instant.parse("2026-11-12T01:00:00Z")
    private fun at(seq: Int) = start.plusSeconds(seq * 60L).toString()
    private val exchange = listOf(
        Item("u1", 1, role = "user", kind = "text", text = "Make the importer retry once when the hub restarts", ts = at(1)),
        Item("a2", 2, role = "assistant", kind = "text", ts = at(2),
            text = "Done. The importer now retries once after a restart, and a test covers the restart path."),
    )
    private val recap = Item("r3", 3, role = "system", kind = "recap", ts = at(3),
        text = "**Where things are:** the importer retries once after a hub restart. The new test passes; the release notes still need a line.")
    private val agent = Session(key.machineId, key.sessionId, title = "Fix the importer retry", agent = "claude", status = "idle",
        cwd = "/home/me/code/app")
    private val suggestion = "Review the release notes before sending"

    private fun texts(): List<String> = rule.onAllNodes(SemanticsMatcher("any node") { true }, useUnmergedTree = true).fetchSemanticsNodes().flatMap { node ->
        node.config.getOrNull(SemanticsProperties.Text).orEmpty().map { it.text } +
            node.config.getOrNull(SemanticsProperties.ContentDescription).orEmpty()
    }

    private fun scene(name: String, style: AppStyle, dark: Boolean, large: Boolean = false) {
        val snapshot = when (name) {
            "loading" -> ChatSnapshot(loading = true)
            "error" -> ChatSnapshot(items = exchange, loading = false, error = "Couldn’t sync messages")
            "recap" -> ChatSnapshot(items = exchange + recap, loading = false)
            else -> ChatSnapshot(items = exchange, loading = false)
        }
        val outbox = if (name == "ready") listOf(OutboxMessage("m1", key, "Also add a line to the release notes", DeliveryState.Sent))
            else emptyList()
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow(listOf(Machine(key.machineId, "Computer A", online = true)))
            override val sessions = MutableStateFlow(listOf(agent.copy(
                suggestion = suggestion.takeIf { name == "safe-suggestion" || name == "risky-suggestion" },
                suggestionRisky = name == "risky-suggestion",
            )))
            override suspend fun refresh() = Unit
        }
        val chats = object : ChatRepository {
            override fun observe(session: SessionKey) = MutableStateFlow(snapshot)
            override suspend fun loadOlder(session: SessionKey) = Unit
            override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int): List<Item> = emptyList()
        }
        val sender = object : MessageSender {
            override val outbox = MutableStateFlow(outbox)
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
        }
        val interactions = object : InteractionCenter {
            override val pending = MutableStateFlow(emptyList<Interaction>())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
        }
        androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>()
            .getSharedPreferences("closed_recaps", android.content.Context.MODE_PRIVATE).edit().clear().commit()
        rule.mainClock.autoAdvance = false // the spinner and the typing dots never settle
        rule.setContent {
            // The largest text: the density's font scale, as the system's largest accessibility size gives it.
            val density = Density(LocalDensity.current.density, if (large) 2f else 1f)
            CompositionLocalProvider(LocalDensity provides density) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("shot")) {
                        ChatScreen(key, chats, sender, interactions, sessions, onBack = {})
                    }
                }
            }
        }
        // Lines are built off the main thread: frames by hand, real time until the scene's words show.
        val ready: (List<String>) -> Boolean = when (name) {
            "loading" -> { shown -> shown.any { it.startsWith("Message") } }
            "ready" -> { shown -> "Also add a line to the release notes" in shown }
            "recap" -> { shown -> shown.any { it.startsWith("Recap while you were away") || it.startsWith("While you were away · ") } }
            "safe-suggestion", "risky-suggestion" -> { shown -> suggestion in shown }
            else -> { shown -> shown.any { it.startsWith("Done. The importer") } }
        }
        val deadline = System.currentTimeMillis() + 30_000
        while (true) {
            Snapshot.sendApplyNotifications()
            rule.mainClock.advanceTimeByFrame()
            if (ready(texts())) break
            check(System.currentTimeMillis() < deadline) { "$name never showed; shown: ${texts()}" }
            Thread.sleep(20)
        }
        repeat(10) { rule.mainClock.advanceTimeByFrame() }
        val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val output = File(System.getProperty("persona.shots") ?: "build/chat-details-shots").also { it.mkdirs() }
        try {
            File(output, "$name-${style.name.lowercase()}-${if (dark) "dark" else "light"}${if (large) "-large" else ""}.png").outputStream()
                .use { assertTrue(image.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        } finally {
            image.recycle()
        }
    }

    @Test fun readyGlass() = scene("ready", AppStyle.Glass, false)
    @Test fun readyGlassDark() = scene("ready", AppStyle.Glass, true)
    @Test fun readyBrief() = scene("ready", AppStyle.Brief, false)
    @Test fun readyBriefDark() = scene("ready", AppStyle.Brief, true)
    @Test fun readyBoard() = scene("ready", AppStyle.Board, false)
    @Test fun readyBoardDark() = scene("ready", AppStyle.Board, true)
    @Test fun loadingGlass() = scene("loading", AppStyle.Glass, false)
    @Test fun loadingGlassDark() = scene("loading", AppStyle.Glass, true)
    @Test fun loadingBrief() = scene("loading", AppStyle.Brief, false)
    @Test fun loadingBriefDark() = scene("loading", AppStyle.Brief, true)
    @Test fun loadingBoard() = scene("loading", AppStyle.Board, false)
    @Test fun loadingBoardDark() = scene("loading", AppStyle.Board, true)
    @Test fun errorGlass() = scene("error", AppStyle.Glass, false)
    @Test fun errorGlassDark() = scene("error", AppStyle.Glass, true)
    @Test fun errorBrief() = scene("error", AppStyle.Brief, false)
    @Test fun errorBriefDark() = scene("error", AppStyle.Brief, true)
    @Test fun errorBoard() = scene("error", AppStyle.Board, false)
    @Test fun errorBoardDark() = scene("error", AppStyle.Board, true)
    @Test fun recapGlass() = scene("recap", AppStyle.Glass, false)
    @Test fun recapGlassDark() = scene("recap", AppStyle.Glass, true)
    @Test fun recapBrief() = scene("recap", AppStyle.Brief, false)
    @Test fun recapBriefDark() = scene("recap", AppStyle.Brief, true)
    @Test fun recapBoard() = scene("recap", AppStyle.Board, false)
    @Test fun recapBoardDark() = scene("recap", AppStyle.Board, true)
    @Test fun readyGlassLarge() = scene("ready", AppStyle.Glass, false, large = true)
    @Test fun recapGlassLarge() = scene("recap", AppStyle.Glass, false, large = true)

    // Semantics tests own the action difference. These six scenes only prove risk does not change the chip's drawing:
    // one risky chip per style, light and dark, Chinese, the largest text, and one safe comparison.
    @Test fun safeSuggestionGlass() = scene("safe-suggestion", AppStyle.Glass, false)
    @Test fun riskySuggestionGlass() = scene("risky-suggestion", AppStyle.Glass, false)
    @Test fun riskySuggestionBriefDark() = scene("risky-suggestion", AppStyle.Brief, true)
    @Test fun riskySuggestionBoard() = scene("risky-suggestion", AppStyle.Board, false)

    @Config(sdk = [35], qualifiers = "zh-w400dp-h760dp-xhdpi")
    @Test fun riskySuggestionZhGlassDark() = scene("risky-suggestion", AppStyle.Glass, true)

    @Test fun riskySuggestionGlassLarge() = scene("risky-suggestion", AppStyle.Glass, false, large = true)
}

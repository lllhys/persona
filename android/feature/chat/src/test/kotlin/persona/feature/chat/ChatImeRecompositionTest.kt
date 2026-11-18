package persona.feature.chat

import android.view.View
import androidx.compose.runtime.ExperimentalComposeRuntimeApi
import androidx.compose.runtime.RecomposeScope
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.imePadding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Composition
import androidx.compose.runtime.currentComposer
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.runtime.tooling.CompositionObserver
import androidx.compose.runtime.tooling.ObservableComposition
import androidx.compose.runtime.tooling.setObserver
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.test.junit4.createComposeRule
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
import java.time.Instant

/**
 * The keyboard sliding up moves the IME inset on every frame of its animation. The chat page must follow it with layout,
 * not by composing the page again on each of those frames: counted here as the recompose scopes that run per inset step.
 */
@OptIn(ExperimentalComposeRuntimeApi::class)
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp")
class ChatImeRecompositionTest {
    @get:Rule val rule = createComposeRule()

    private val key = SessionKey("macbook", "weather")
    private val start = Instant.parse("2026-11-01T10:00:00Z")
    private val items = (1..40).map { n ->
        Item("m$n", n, role = if (n % 2 == 1) "user" else "assistant", kind = "text",
            text = "Message $n: " + "a few words that fill the line ".repeat(1 + n % 4), ts = start.plusSeconds(n * 30L).toString())
    }
    private val idle = Session(machineId = key.machineId, sessionId = key.sessionId, title = "Weather widget", status = "idle")
    private val sessions = object : SessionsRepository {
        override val machines = MutableStateFlow(emptyList<Machine>())
        override val sessions = MutableStateFlow(listOf(idle))
        override suspend fun refresh() = Unit
    }
    private val chats = object : ChatRepository {
        override fun observe(session: SessionKey) = MutableStateFlow(ChatSnapshot(items = items, loading = false))
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int): List<Item> = emptyList()
    }
    private val sender = object : MessageSender {
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
    }
    private val interactions = object : InteractionCenter {
        override val pending = MutableStateFlow(emptyList<Interaction>())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }

    /**
     * Counts the recompose scopes that run, in the page's composition and its subcompositions, by the file and line of the
     * code that ran each one (the first frame outside the Compose runtime).
     */
    private class ScopeCounter : CompositionObserver {
        val callers = HashMap<String, Int>()
        val total get() = callers.values.sum()
        override fun onScopeEnter(scope: RecomposeScope) {
            val caller = Throwable().stackTrace.firstOrNull {
                !it.className.startsWith("androidx.compose.runtime") && !it.className.startsWith(ChatImeRecompositionTest::class.java.name)
            }
            val where = "${caller?.fileName}:${caller?.lineNumber}"
            callers[where] = (callers[where] ?: 0) + 1
        }
        override fun onBeginComposition(composition: ObservableComposition) = Unit
        override fun onReadInScope(scope: RecomposeScope, value: Any) = Unit
        override fun onScopeExit(scope: RecomposeScope) = Unit
        override fun onEndComposition(composition: ObservableComposition) = Unit
        override fun onScopeInvalidated(scope: RecomposeScope, value: Any?) = Unit
        override fun onScopeDisposed(scope: RecomposeScope) = Unit
    }

    /** One frame of the keyboard's slide: the IME inset at [bottom] pixels, as the window's insets dispatch it. */
    private fun ime(view: View, bottom: Int) {
        rule.runOnUiThread {
            val insets = WindowInsetsCompat.Builder()
                .setInsets(WindowInsetsCompat.Type.ime(), Insets.of(0, 0, 0, bottom))
                .setVisible(WindowInsetsCompat.Type.ime(), bottom > 0)
                .build()
            ViewCompat.dispatchApplyWindowInsets(view, insets)
        }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        rule.waitForIdle()
    }

    @Test fun theKeyboardSlidingUpDoesNotComposeThePageAgainEachFrame() =
        slide { ChatScreen(key, chats, sender, interactions, sessions, onBack = {}) }

    /** Persona's page embeds the conversation under its own page, whose imePadding moves it (AssistantScreen). */
    @Test fun theEmbeddedAssistantConversationFollowsTheKeyboardTheSameWay() = slide {
        Box(Modifier.fillMaxSize().imePadding()) {
            ChatScreen(key, chats, sender, interactions, sessions, onBack = {}, renderStyle = ChatRenderStyle.Assistant, embedded = true)
        }
    }

    private fun slide(page: @Composable () -> Unit) {
        lateinit var view: View
        lateinit var composition: Composition
        rule.setContent {
            view = LocalView.current
            composition = currentComposer.composition
            page()
        }
        rule.waitForIdle()
        // The first steps settle what the first dispatch starts (the keyboard's visibility, the composer's room).
        ime(view, 0)
        ime(view, 1)
        val still = ScopeCounter()
        val stillHandle = checkNotNull(rule.runOnUiThread { composition.setObserver(still) }) { "this composition cannot be observed" }
        repeat(5) { ime(view, 1) }
        stillHandle.dispose()
        // Observing the root composition observes its subcompositions too (lazy items, SubcomposeLayout slots).
        val sliding = ScopeCounter()
        val handle = checkNotNull(rule.runOnUiThread { composition.setObserver(sliding) })
        val steps = 20
        for (step in 1..steps) ime(view, 40 * step)
        handle.dispose()
        val perStep = sliding.total.toDouble() / steps
        val report = "per IME step: $perStep scopes; by caller: " +
            sliding.callers.entries.sortedByDescending { it.value }.take(12).joinToString { "${it.key}=${it.value}" }
        println("ChatImeRecompositionTest: unchanged inset ${still.total} scopes over 5 steps; $report")
        // Before #8334 the page's BoxWithConstraints, ChatLinkHost's and the footer's IME read ran 61 scopes a step: the
        // footer's every row, the link host and the page body. What is left (23) is the shared page frame's slots (Page.kt,
        // which subcomposes them on each measure) and the pending cards' room.
        assertTrue(report, perStep < 30)
        // An inset that does not move composes next to nothing (a late timer at most): the count above is the slide's.
        assertTrue("an unchanged inset composed ${still.total} scopes", still.total <= 5)
    }
}

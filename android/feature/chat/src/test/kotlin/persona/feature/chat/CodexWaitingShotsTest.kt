package persona.feature.chat

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File

/** Production chat, including a send response whose thread is outside the current session-list window. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class CodexWaitingShotsTest {
    @get:Rule val rule = createComposeRule()
    private val waiting = Session("computer", "waiting", title = "Project", agent = "codex", status = "idle",
        paneId = "pane", awaitingFirstMessage = true)
    private val thread = waiting.copy(sessionId = "thread", title = "Review the changes", awaitingFirstMessage = false)
    private val rows = object : SessionsRepository {
        override val machines = MutableStateFlow(listOf(Machine("computer", "Studio", online = true)))
        override val sessions = MutableStateFlow(listOf(waiting))
        override val adoptedSessions = MutableStateFlow(emptyList<Session>())
        override val startedThreads = MutableStateFlow(emptyMap<SessionKey, Session>())
        override suspend fun refresh() = Unit
    }
    private val sender = object : MessageSender {
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
    }
    private val chats = object : ChatRepository {
        override fun observe(session: SessionKey) = MutableStateFlow(ChatSnapshot(loading = false))
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
    }
    private val requests = object : InteractionCenter {
        override val pending = MutableStateFlow(emptyList<Interaction>())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }

    @Test fun emptyChatInEveryStyleLightAndDarkThenFollowsAReturnedThreadOutsideTheWindow() {
        rows.sessions.value = emptyList()
        rows.adoptedSessions.value = listOf(waiting)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var key by mutableStateOf(waiting.key)
        val followed = mutableListOf<SessionKey>()
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.fillMaxSize()) {
                    ChatScreen(key, chats, sender, requests, rows, onBack = {}, onTerminal = {},
                        chrome = ChatChromeBindings(onFollowSession = { followed += it; key = it }))
                }
            }
        }
        val note = "Codex is open on Studio. Send its first message below, or type it in its terminal."
        rule.waitUntil(10_000) { rule.onAllNodes(androidx.compose.ui.test.hasText(note)).fetchSemanticsNodes().isNotEmpty() }
        val out = File(System.getProperty("persona.shots") ?: "build/codex-waiting-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            rule.runOnIdle { style = s; dark = d }
            rule.waitForIdle()
            rule.onNodeWithText(note).assertExists()
            rule.onNodeWithText("First message to Codex").assertExists()
            rule.onRoot().captureToImage().asAndroidBitmap().let { bitmap ->
                out.resolve("chat-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                    bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it)
                }
            }
        }
        rule.runOnIdle {
            rows.sessions.value = emptyList()
            rows.adoptedSessions.value = listOf(waiting, thread)
            rows.startedThreads.value = mapOf(waiting.key to thread)
        }
        rule.waitUntil(10_000) { key == thread.key }
        rule.waitForIdle()
        assertEquals(listOf(thread.key), followed)
        rule.onNodeWithText(note).assertDoesNotExist()
        rule.onNodeWithText("Review the changes", substring = true, useUnmergedTree = true).assertExists()
    }

    @Test fun aMessageTypedInTheTerminalFollowsThePaneWhenTheWaitingRowDisappears() {
        val followed = mutableListOf<SessionKey>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                ChatScreen(waiting.key, chats, sender, requests, rows, onBack = {},
                    chrome = ChatChromeBindings(onFollowSession = { followed += it }))
            }
        }
        rule.waitForIdle()
        rule.runOnIdle { rows.sessions.value = listOf(thread) }
        rule.waitUntil(10_000) { followed.isNotEmpty() }
        assertEquals(listOf(thread.key), followed)
    }

    @Test fun anAbsentTerminalThreadDoesNotFetchOnListHintsOrReconnect() {
        rows.sessions.value = emptyList()
        rows.adoptedSessions.value = listOf(waiting)
        val followed = mutableListOf<SessionKey>()
        var lookups = 0
        val actions = object : ChatActions {
            override suspend fun isMuted(session: Session) = false
            override suspend fun execute(session: Session, action: ChatAction) = ChatActionResult.Done
            override suspend fun verify(session: Session, action: ChatAction) = ChatActionResult.Done
            override suspend fun completeRow(session: Session): Session? {
                lookups++
                return null
            }
        }
        val connection = object : HubConnection {
            override val state = MutableStateFlow(ConnectionState.Online)
            override val events = MutableSharedFlow<ServerEvent>(extraBufferCapacity = 10)
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                ChatScreen(waiting.key, chats, sender, requests, rows, onBack = {},
                    chrome = ChatChromeBindings(actions = actions, connection = connection, onFollowSession = { followed += it }))
            }
        }
        rule.waitForIdle()
        repeat(3) { revision ->
            rule.runOnIdle {
                connection.events.tryEmit(SessionsListEvent("revision-$revision"))
                connection.state.value = ConnectionState.WaitingForNetwork
            }
            rule.waitForIdle()
            rule.runOnIdle { connection.state.value = ConnectionState.Online }
            rule.mainClock.advanceTimeBy(60_000)
            rule.waitForIdle()
        }
        assertTrue(followed.isEmpty())
        assertEquals(0, lookups)
        assertTrue(rows.sessions.value.isEmpty())
        rule.onNodeWithText("First message to Codex").assertExists()
    }

    @Test fun aNormalConversationNeverFollowsAReturnedThread() {
        val normal = waiting.copy(awaitingFirstMessage = false)
        rows.sessions.value = listOf(normal, thread)
        rows.startedThreads.value = mapOf(normal.key to thread)
        val followed = mutableListOf<SessionKey>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                ChatScreen(normal.key, chats, sender, requests, rows, onBack = {},
                    chrome = ChatChromeBindings(onFollowSession = { followed += it }))
            }
        }
        rule.waitForIdle()
        rule.runOnIdle { rows.sessions.value = listOf(thread) }
        rule.waitForIdle()
        assertTrue(followed.isEmpty())
    }

    @Test fun reusingAPaneNeverFollowsAnOlderStartsReceipt() {
        val old = thread.copy(sessionId = "older")
        rows.sessions.value = listOf(waiting, old.copy(status = "offline", state = "offline"))
        rows.adoptedSessions.value = listOf(old, waiting)
        rows.startedThreads.value = mapOf(SessionKey("computer", "older-waiting") to old)
        val followed = mutableListOf<SessionKey>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                ChatScreen(waiting.key, chats, sender, requests, rows, onBack = {},
                    chrome = ChatChromeBindings(onFollowSession = { followed += it }))
            }
        }
        rule.waitForIdle()
        assertTrue(followed.isEmpty())
        rule.onNodeWithText("First message to Codex").assertExists()
    }
}

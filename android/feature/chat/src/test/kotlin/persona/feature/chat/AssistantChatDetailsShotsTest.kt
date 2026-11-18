package persona.feature.chat

import android.content.res.Configuration
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.serialization.json.JsonObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.domain.tasks.TasksStore
import java.io.File

/**
 * Persona's conversation as iOS shows it (#4339 C18), on the real chat page: In progress folds the shelved tasks into one
 * "Shelved N" line that opens the board (Glass; Brief and Board keep the tray as it was), answering the last card offers
 * the next one another chat waits on you with, and a message waiting for its turn says "Persona is getting ready…" under
 * an "Ask Persona…" composer. PNGs in `build/assistant-chat-details-shots/`, light and dark.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi", application = QuestionTestApplication::class)
class AssistantChatDetailsShotsTest {
    @get:Rule val rule = createComposeRule()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    @After fun close() { scope.cancel() }

    private val main = SessionKey("computer-a", "main")
    private val running = Session("computer-a", "working", title = "Update the web interface", state = "working", status = "working",
        activity = "Checking the routes")
    private val todos = listOf(
        Todo("task-a", running.title, origin = "main", agents = listOf(TodoAgent(running.machineId, running.sessionId))),
        Todo("task-old", "Tidy the release notes", origin = "main", createdAt = "2026-10-01T09:00:00Z", updatedAt = "2026-10-01T09:00:00Z"),
        Todo("task-older", "Compare the backup plans", origin = "main", createdAt = "2026-09-20T09:00:00Z", updatedAt = "2026-09-20T09:00:00Z"),
    )
    private val permission = Interaction("permission", machineId = running.machineId, sessionId = running.sessionId, kind = "permission",
        toolName = "Bash", summary = "Run the integration tests")
    private val elsewhere = WaitingRequest(SessionKey("computer-b", "weather"), "plan-1", "weather-app", "Plan ready")

    private val transcript = listOf(
        Item("user", 1, role = "user", kind = "text", text = "What is left on the release?", ts = "2026-11-01T09:00:00Z"),
        Item("answer", 2, role = "assistant", kind = "text", text = "The web interface is being updated; it needs your go-ahead to run the tests.", ts = "2026-11-01T09:00:01Z"),
    )

    private inner class Fixture(val items: List<Item> = transcript) {
        val requests = MutableStateFlow(listOf(permission))
        val opened = mutableListOf<Pair<SessionKey, String>>()
        val sessionRows = MutableStateFlow(listOf(Session(main.machineId, main.sessionId, title = "Persona", assistant = true,
            status = "done", state = "idle"), running))
        val sessions = object : SessionsRepository {
            override val sessions = sessionRows
            override val machines = MutableStateFlow(listOf(Machine("computer-a", "Computer A", online = true)))
            override suspend fun refresh() = Unit
        }
        val interactions = object : InteractionCenter {
            override val pending = requests
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) { requests.value = requests.value.filterNot { it.id == id } }
        }
        val connection = object : HubConnection {
            override val state = MutableStateFlow(ConnectionState.Online)
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }
        val api = object : TasksApi {
            override suspend fun todos() = todos
            override suspend fun patchTodo(id: String, title: String?, notes: String?, done: Boolean?): Todo = error("not used")
            override suspend fun acceptTask(todoId: String, askedAt: String) = Unit
            override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList())
            override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("not used")
            override suspend fun dismissPlan(id: String): TaskPlan = error("not used")
            override suspend fun retryTask(todoId: String): Todo = error("not used")
            override suspend fun removeTask(todoId: String) = Unit
            override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("not used")
            override suspend fun today(conversation: String): FeedItem = error("not used")
            override suspend fun mainConversation() = "main"
            override suspend fun inboxReply(itemId: String, text: String, card: String?) = error("not used")
        }
        val tasks = TasksStore(api, connection, scope).also { it.reloadTodos() }
        val chats = object : ChatRepository {
            override fun observe(session: SessionKey) = flowOf(ChatSnapshot(loading = false, items = items))
            override suspend fun loadOlder(session: SessionKey) = Unit
            override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
        }
        val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        val sender = object : MessageSender {
            override val outbox = this@Fixture.outbox
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
        }
    }

    private fun settle() {
        Snapshot.sendApplyNotifications()
        repeat(5) { rule.mainClock.advanceTimeByFrame() }
        rule.mainClock.advanceTimeBy(300)
        rule.waitForIdle()
    }
    private fun shot(name: String, dialog: Boolean = false) {
        val image = (if (dialog) rule.onNode(isDialog()) else rule.onNodeWithTag("assistant-chat-shot")).captureToImage().asAndroidBitmap()
        val output = File(System.getProperty("persona.shots") ?: "build/assistant-chat-details-shots").also { it.mkdirs() }
        File(output, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun shelvedNextAndGettingReadyAsOnIos() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var round by mutableIntStateOf(0)
        var fixture = Fixture()
        rule.mainClock.autoAdvance = false
        rule.setContent {
            val configuration = Configuration(LocalConfiguration.current).apply {
                uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                    if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
            }
            CompositionLocalProvider(LocalConfiguration provides configuration) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    key(round) {
                        val f = fixture
                        Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("assistant-chat-shot")) {
                            ChatScreen(main, f.chats, f.sender, f.interactions, f.sessions, {},
                                chrome = ChatChromeBindings(tasks = f.tasks, waiting = listOf(elsewhere),
                                    onOpenRequest = { chat, request -> f.opened += chat to request }),
                                renderStyle = ChatRenderStyle.Assistant, embedded = true, showTopBar = false)
                        }
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            val name = "${s.name.lowercase()}-${if (d) "dark" else "light"}"
            fixture = Fixture(); style = s; dark = d; round++
            settle()
            rule.waitUntil(10_000) { rule.onAllNodesWithTag("assistant-tray-bar").fetchSemanticsNodes().isNotEmpty() }
            rule.onNodeWithText("Ask Persona…").assertExists()

            // In progress: the running task, and on Glass the two shelved ones as one quiet line.
            rule.onNodeWithTag("assistant-entry-running").performClick(); settle()
            shot("$name-1-in-progress")
            rule.onAllNodesWithTag("assistant-tray-shelved").assertCountEquals(if (s == AppStyle.Glass) 1 else 0)
            rule.onNodeWithTag("assistant-entry-running").performClick(); settle()

            // Needs you: approving the last card here offers the next one waiting elsewhere, as an Agent's chat does.
            rule.onNodeWithTag("assistant-entry-needs").performClick(); settle()
            rule.onNodeWithTag("assistant-decision-permission").performClick(); settle()
            rule.waitUntil(5_000) { fixture.requests.value.isEmpty() }
            settle()
            shot("$name-3-next")
            rule.onNodeWithTag("chat-next-waiting").performClick(); settle()
            assertEquals(listOf(elsewhere.session to elsewhere.requestId), fixture.opened)

            // A message waiting for its turn: Persona is getting ready.
            fixture.outbox.value = listOf(OutboxMessage("m1", main, "And the release notes?", DeliveryState.Sent)); settle()
            rule.onNodeWithText("Persona is getting ready…").assertExists()
            shot("$name-4-getting-ready")

            // The shelved line opens the board, which lists them.
            if (s != AppStyle.Glass) continue
            rule.onNodeWithTag("assistant-entry-running").performClick(); settle()
            rule.onNodeWithText("Shelved").assertExists()
            rule.onNodeWithTag("assistant-tray-shelved").performClick(); settle()
            rule.mainClock.advanceTimeBy(1_000); rule.waitForIdle() // the sheet slides up
            rule.onNodeWithText("Tidy the release notes").assertExists()
            // iOS's board has no Shelved section either (`TaskBoardSheet`): a shelved task sits under its own state, with the
            // others of it, as Android's always has; "Shelved" is the To-dos page's section.
            rule.onNodeWithText("Compare the backup plans").assertExists()
            rule.onAllNodes(hasText("Shelved") and hasAnyAncestor(isDialog())).assertCountEquals(0)
            shot("$name-5-board", dialog = true)
        }
    }

    // iOS's DayLine over a first message the transcript does not have yet (#1911): Persona's conversation says its day from the
    // start (Glass), so the line does not appear above it later; the day it was written. Brief and Board keep the page.
    @Test fun aFirstMessageNotInTheTranscriptYetHasItsDayOnGlass() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        val fixture = Fixture(items = emptyList()).apply {
            requests.value = emptyList()
            outbox.value = listOf(OutboxMessage("first", main, "Plan a weekend in the mountains", DeliveryState.Sent, mainTarget = true,
                createdAt = java.time.LocalDate.now().atTime(8, 30).atZone(java.time.ZoneId.systemDefault()).toInstant().toEpochMilli()))
        }
        rule.mainClock.autoAdvance = false
        rule.setContent {
            val configuration = Configuration(LocalConfiguration.current).apply {
                uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                    if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
            }
            CompositionLocalProvider(LocalConfiguration provides configuration) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("assistant-chat-shot")) {
                        ChatScreen(main, fixture.chats, fixture.sender, fixture.interactions, fixture.sessions, {},
                            chrome = ChatChromeBindings(tasks = fixture.tasks),
                            renderStyle = ChatRenderStyle.Assistant, embedded = true, showTopBar = false)
                    }
                }
            }
        }
        settle()
        rule.waitUntil(10_000) { rule.onAllNodesWithTag("assistant-tray-bar").fetchSemanticsNodes().isNotEmpty() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d; settle()
            val name = "${s.name.lowercase()}-${if (d) "dark" else "light"}"
            rule.onNodeWithText("Plan a weekend in the mountains").assertExists()
            rule.onNodeWithText("Persona is getting ready…").assertExists()
            if (s == AppStyle.Glass) rule.onNodeWithTag("chat-pending-day").assertExists().assert(hasText("Today · ", substring = true))
            else rule.onAllNodesWithTag("chat-pending-day").assertCountEquals(0)
            shot("$name-6-first-message")
        }
    }
}

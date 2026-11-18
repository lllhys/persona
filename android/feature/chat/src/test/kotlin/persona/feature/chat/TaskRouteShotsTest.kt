package persona.feature.chat

import android.content.res.Configuration
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.focus.FocusManager
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.serialization.json.JsonObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.ChatRepository
import persona.core.contracts.api.ChatSnapshot
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.Interaction
import persona.core.contracts.Item
import persona.core.contracts.Machine
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.Resolution
import persona.core.contracts.ServerEvent
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.TodoAgent
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.SessionsRepository
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.domain.tasks.TasksStore
import java.io.File
import java.io.IOException

/**
 * Where a message goes in Persona's conversation (#4339 C15), on the real page: the route chip above the composer while
 * the next message goes to a task, the In progress tray with a route set, and the @ list with the conversation's tasks
 * first. Every style, light and dark (`build/task-route-shots/`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h760dp-xhdpi")
class TaskRouteShotsTest {
    @get:Rule val rule = createComposeRule()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    @After fun close() { scope.cancel() }

    private val main = SessionKey("computer-a", "main")
    private val web = Session("computer-a", "web", title = "Update the web interface", cwd = "/work/web", state = "working", status = "working",
        activity = "Checking the routes", updatedAt = "2026-11-12T09:30:00Z")
    private val release = Session("computer-b", "release", title = "Prepare the release", cwd = "/work/app", state = "needs_you", status = "blocked",
        updatedAt = "2026-11-12T09:20:00Z")
    private val crash = Session("computer-a", "crash", title = "Explore the crash logs", cwd = "/work/app", state = "idle", status = "idle",
        updatedAt = "2026-11-12T08:00:00Z")
    private val todos = listOf(
        Todo("task-web", web.title, origin = "main", createdAt = "2026-11-12T09:00:00Z", agents = listOf(TodoAgent(web.machineId, web.sessionId, cwd = web.cwd))),
        Todo("task-release", release.title, origin = "main", createdAt = "2026-11-12T09:10:00Z",
            agents = listOf(TodoAgent(release.machineId, release.sessionId, cwd = release.cwd))),
        Todo("task-old", "Rotate the signing keys", origin = "main", createdAt = "2026-11-11T09:00:00Z", doneAt = "2026-11-11T10:00:00Z"),
    )

    private val routed = mutableListOf<Triple<String, String, String>>()
    private var routeHandler: suspend (String, String, String) -> FeedItem = { _, _, _ -> error("not used") }
    private val api = object : TasksApi {
        override suspend fun todos() = todos
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList())
        override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("not used")
        override suspend fun dismissPlan(id: String): TaskPlan = error("not used")
        override suspend fun retryTask(todoId: String): Todo = error("not used")
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem {
            routed += Triple(conversation, todoId, text)
            return routeHandler(conversation, todoId, text)
        }
        override suspend fun today(conversation: String): FeedItem = error("not used")
        override suspend fun mainConversation() = "main"
        override suspend fun inboxReply(itemId: String, text: String, card: String?) = Unit
    }
    private val connection = object : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }
    private val sessions = object : SessionsRepository {
        override val sessions = MutableStateFlow(listOf(Session(main.machineId, main.sessionId, title = "Persona", assistant = true,
            status = "done", state = "idle"), web, release, crash))
        override val machines = MutableStateFlow(listOf(Machine("computer-a", "Computer A", online = true), Machine("computer-b", "Computer B", online = true)))
        override suspend fun refresh() = Unit
    }
    private val interactions = object : InteractionCenter {
        override val pending = MutableStateFlow(emptyList<Interaction>())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }
    private val chats = object : ChatRepository {
        override fun observe(session: SessionKey) = flowOf(ChatSnapshot(loading = false, items = listOf(
            Item("user", 1, role = "user", kind = "text", text = "How are the release and the web update going?", ts = "2026-11-12T09:40:00Z"),
            Item("answer", 2, role = "assistant", kind = "text", text = "The web update is checking its routes; the release waits for you.", ts = "2026-11-12T09:40:05Z"),
        )))
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
    }
    private val ordinarySends = mutableListOf<String>()
    private val sender = object : MessageSender {
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
            ordinarySends += text
        }
    }

    private fun settle() {
        Snapshot.sendApplyNotifications()
        repeat(5) { rule.mainClock.advanceTimeByFrame() }
        rule.mainClock.advanceTimeBy(300)
        rule.waitForIdle()
    }

    private fun shot(name: String) {
        val image = rule.onNodeWithTag("task-route-shot").captureToImage().asAndroidBitmap()
        val output = File("build/task-route-shots").also { it.mkdirs() }
        File(output, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun aDisconnectedRoutedSendKeepsTheDraftAndRouteForRetry() {
        val tasks = TasksStore(api, connection, scope).also { it.reloadTodos() }
        tasks.setRoute("main", "task-release")
        val text = "Keep this draft until the task accepts it"
        val composer = ChatComposerState().apply { value = TextFieldValue(text, TextRange(4)) }
        val editing = composer.value
        val observed = mutableListOf<ChatComposerDraft>()
        val disconnected = CompletableDeferred<FeedItem>()
        routeHandler = { _, _, _ -> disconnected.await() }
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                ChatScreen(main, chats, sender, interactions, sessions, {},
                    chrome = ChatChromeBindings(tasks = tasks, onOpenSession = {}),
                    renderStyle = ChatRenderStyle.Assistant, composerState = composer,
                    onDraftChanged = { observed += it }, embedded = true, showTopBar = false)
            }
        }
        settle()

        rule.onNodeWithContentDescription("Send").performClick()
        rule.waitUntil(10_000) { routed.size == 1 }
        // Launching the request is not acceptance: neither editing state nor its route is consumed while it is pending.
        rule.runOnIdle {
            assertEquals(editing, composer.value)
            assertEquals(mapOf("main" to "task-release"), tasks.routes.value)
            assertFalse(observed.any { it.text.isEmpty() })
        }

        disconnected.completeExceptionally(IOException("Disconnected"))
        settle()
        // The ordinary Send button is the retry: the same draft and route remain, with no outbox fallback or empty draft.
        rule.runOnIdle {
            assertEquals(editing, composer.value)
            assertEquals(mapOf("main" to "task-release"), tasks.routes.value)
            assertEquals(text, observed.last().text)
            assertTrue(ordinarySends.isEmpty())
        }

        routeHandler = { conversation, _, _ ->
            FeedItem("routed", conversation, "2026-11-12T09:41:00Z", "routed", "example:1", JsonObject(emptyMap()))
        }
        rule.onNodeWithContentDescription("Send").performClick()
        rule.waitUntil(10_000) { routed.size == 2 && composer.value.text.isEmpty() }
        settle()
        assertEquals(listOf(Triple("main", "task-release", text), Triple("main", "task-release", text)), routed)
        assertEquals(emptyMap<String, String>(), tasks.routes.value)
        assertEquals("", observed.last().text)
        assertTrue(ordinarySends.isEmpty())
    }

    @Test fun theRouteChipAndTheTaskListInEveryStyle() {
        val tasks = TasksStore(api, connection, scope).also { it.reloadTodos() }
        val composer = ChatComposerState()
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var focus: FocusManager? = null
        rule.mainClock.autoAdvance = false
        rule.setContent {
            focus = LocalFocusManager.current
            val configuration = Configuration(LocalConfiguration.current).apply {
                uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                    if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
            }
            CompositionLocalProvider(LocalConfiguration provides configuration) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Box(Modifier.width(400.dp).height(760.dp).background(PersonaTheme.palette.background).testTag("task-route-shot")) {
                        ChatScreen(main, chats, sender, interactions, sessions, {},
                            chrome = ChatChromeBindings(tasks = tasks, onOpenSession = {}),
                            renderStyle = ChatRenderStyle.Assistant, composerState = composer, embedded = true, showTopBar = false)
                    }
                }
            }
        }
        settle()
        rule.waitUntil(10_000) { rule.onAllNodesWithTag("assistant-tray-bar").fetchSemanticsNodes().isNotEmpty() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            val suffix = "${s.name.lowercase()}-${if (d) "dark" else "light"}"
            // Each pass starts with the composer at rest: the last pass's pick put the cursor back in it.
            settle(); rule.runOnIdle { focus?.clearFocus(force = true) }
            // A task's Message set the route: the chip says so above the composer, the tray closed.
            tasks.setRoute("main", "task-release"); composer.value = TextFieldValue(); settle()
            shot("route-$suffix")
            rule.onNodeWithText("Message “Prepare the release”").assertIsDisplayed()
            // In progress open: the route is not a row of the tray any more, the chip stays under the bar.
            rule.onNodeWithTag("assistant-entry-running").performClick(); settle()
            shot("route-tray-$suffix")
            rule.onNodeWithTag("assistant-entry-running").performClick(); settle()
            // × hands the next message back to Persona.
            rule.onNodeWithText("Message “Prepare the release”").assertIsDisplayed()
            rule.onNodeWithTag("assistant-route-chip").assertIsDisplayed()
            rule.onNodeWithContentDescription("Hand to Persona").performClick(); settle()
            assertEquals(emptyMap<String, String>(), tasks.routes.value)
            // @ lists the open tasks first, then files and sessions; picking one routes the next message to it.
            composer.value = TextFieldValue("Ask @", TextRange(5)); settle()
            shot("mention-$suffix")
            rule.onNodeWithText("Message a task").assertIsDisplayed()
            rule.onNodeWithText("Mention a file or session").assertIsDisplayed()
            rule.onNodeWithText("Rotate the signing keys").assertDoesNotExist()
            rule.onAllNodesWithText(web.title)[0].performClick(); settle()
            assertEquals(mapOf("main" to "task-web"), tasks.routes.value)
            assertEquals("Ask ", composer.value.text)
            shot("picked-$suffix")
            tasks.setRoute("main", null)
        }
    }
}

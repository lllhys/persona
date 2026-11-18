package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import coil3.ImageLoader
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.domain.tasks.*
import java.io.File

/**
 * #4339 C13: the hub's task records in Persona's conversation, as iOS draws them (Glass): the receipt, the review line,
 * the report and Persona's hand-off as exchanges folded to three lines, the user's own word to the task, the notice line,
 * and a message card with its drafted reply (Send and Edit, Later and Don't handle).
 * Brief and Board keep them behind the trays. Native JVM captures of the real renderers, with synthetic data.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1600dp-xhdpi")
class TaskRecordsShotsTest {
    @get:Rule val rule = createComposeRule()
    private val session = SessionKey("computer", "main")
    private fun at(n: Int) = "2026-11-12T10:00:${n.toString().padStart(2, '0')}Z"
    private val plan = TaskPlan("plan", "main", listOf(PlanTask("Fix the login crash", todoId = "task")), "started", at(1))
    private val todo = Todo("task", "Fix the login crash", origin = "main", planId = plan.id, createdAt = at(1), doneAt = at(9),
        agents = listOf(TodoAgent("computer", "agent", title = "Login agent")))
    private val handOff = listOf("Fix the crash on the sign-in page when the password is empty.",
        "Start from the stack trace in the issue, write a test that reproduces it first.",
        "Keep the change to the sign-in form; do not touch the session store.",
        "Open a pull request when the tests pass.").joinToString("\n")
    private fun item(id: String, n: Int, kind: String, ref: String? = "task", body: JsonObjectBuilder.() -> Unit = {}) =
        FeedItem(id, "main", at(n), kind, ref, buildJsonObject(body))
    private val transcript = listOf(
        Item("ask", 1, ts = at(0), role = "user", kind = "text", text = "The login page crashes, can you fix it?"),
        Item("answer", 2, ts = at(1), role = "assistant", kind = "text", text = "I handed it to an agent on your computer."),
    )
    private val feed = listOf(
        item("plan-feed", 1, "plan", "plan"),
        item("handoff", 2, "routed") { put("from", "assistant"); put("text", handOff) },
        item("asked", 3, "routed") { put("text", "Use the staging key for the test.") },
        item("report", 4, "report") { put("kind", "question"); put("text", "Which key should the test use?") },
        item("review", 5, "review") { put("verdict", "rework"); put("round", 1); put("note", "The test is missing") },
        item("receipt", 6, "receipt") {
            put("title", "Fix the login crash"); put("summary", "Fixed the empty password crash, **tests pass**.")
            put("pr", "https://example.com/acme/app/pull/42")
        },
        item("notice", 7, "notice", null) { put("text", "Your computer is connected again; the morning brief runs as planned.") },
        item("message", 8, "message", "inbox-1") {
            put("todo_id", "task"); put("source", "mail"); put("sender", "Example sender"); put("chat", "Login bug")
            put("text", "Is the login crash fixed yet?"); put("draft", "Yes, the fix is merged and ships today.")
        },
    )
    private class Api(val todo: Todo, val plan: TaskPlan) : TasksApi {
        override suspend fun todos() = listOf(todo)
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(plans = mapOf(plan.id to plan))
        override suspend fun startPlan(id: String, remember: Boolean) = plan
        override suspend fun dismissPlan(id: String) = plan.copy(state = "dismissed")
        override suspend fun retryTask(todoId: String) = todo
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("unused")
        override suspend fun today(conversation: String): FeedItem = error("unused")
        override suspend fun mainConversation(): String? = null
        override suspend fun inboxReply(itemId: String, text: String, card: String?) = Unit
    }
    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }

    private fun show(style: AppStyle, dark: MutableState<Boolean>) {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val store = TasksStore(Api(todo, plan), Connection(), scope)
        store.reloadTodos(); store.load("main")
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = dark.value, reduceMotion = true, style = style) {
                val context = LocalContext.current
                val loader = remember { ImageLoader.Builder(context).build() }
                DisposableEffect(loader) { onDispose { loader.shutdown(); scope.cancel() } }
                val todos by store.todos.collectAsState()
                val host = AssistantTaskHost(store, "main", emptyList(), { it }, {}, { throw it }, {})
                val original = ChatTimeline().lines(transcript, false, emptySet()).lines
                val lines = AssistantTaskCards.lines(AssistantConversation.project(original, transcript).foreground, feed,
                    mapOf(plan.id to plan), todos, inline = style == AppStyle.Glass)
                // the Assistant home destination, where Persona's words sit in their box
                CompositionLocalProvider(LocalAssistantTaskHost provides host, LocalHomeChrome provides HomeChrome(
                    listOf(NavItem("assistant", "Assistant", persona.core.designsystem.icons.PhIcons.House)), "assistant", onSelect = {}, leading = {})) {
                    Column(Modifier.fillMaxSize().testTag("records-shot").background(PersonaTheme.palette.background)
                        .padding(Tokens.Space.l), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        lines.forEach { line -> key(line.contentKey) {
                            ChatLineContent(line, session, loader, null, onToggle = {}, onFold = {}, folding = emptySet(),
                                foldErrors = emptyMap(), onFullText = {}, style = ChatRenderStyle.Assistant)
                        } }
                    }
                }
            }
        }
    }

    private fun settle() {
        Snapshot.sendApplyNotifications()
        repeat(4) { rule.mainClock.advanceTimeByFrame() }
        rule.mainClock.advanceTimeBy(500)
    }

    private fun capture(name: String): Int {
        val directory = File(System.getProperty("persona.shots") ?: "build/task-records-shots").also { it.mkdirs() }
        val bitmap = rule.onNodeWithTag("records-shot").captureToImage().asAndroidBitmap()
        File(directory, "$name.png").outputStream().use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        return bitmap.getPixel(2, 2)
    }

    @Test fun glassDrawsTheRecordsAsIOSLightAndDark() {
        val dark = mutableStateOf(false)
        show(AppStyle.Glass, dark)
        val colors = mutableListOf<Int>()
        for (night in listOf(false, true)) {
            dark.value = night; settle()
            val mode = if (night) "dark" else "light"
            rule.onNodeWithText("Persona → @Fix the login crash").assertIsDisplayed()
            rule.onNodeWithText("Me → @Fix the login crash").assertIsDisplayed()
            rule.onNodeWithText("Fix the login crash → @Persona").assertDoesNotExist()
            rule.onNodeWithText("Asked Persona: Which key should the test use?").assertDoesNotExist()
            rule.onNodeWithText("Rework round 1", substring = true).assertDoesNotExist()
            rule.onNodeWithText("Fix the login crash is done").assertDoesNotExist()
            rule.onNodeWithText("PR #42").assertDoesNotExist()
            rule.onNodeWithText("Your computer is connected again; the morning brief runs as planned.").assertIsDisplayed()
            // the hand-off is four lines: three show, with Expand; the short report has no button
            rule.onAllNodesWithTag("exchange-toggle", useUnmergedTree = true).assertCountEquals(1)
            rule.onNodeWithText("Expand").assertIsDisplayed()
            colors += capture("records-glass-$mode")
            rule.onNodeWithText("Expand").performClick(); settle()
            rule.onNodeWithText("Collapse").assertIsDisplayed()
            capture("records-glass-open-$mode")
            rule.onNodeWithText("Collapse").performClick(); settle()
            rule.onNodeWithText("Expand").assertIsDisplayed()
            // the message card: its reply reads until Edit opens it for typing, and Done closes it again
            rule.onNodeWithText("Is the login crash fixed yet?").assertIsDisplayed()
            rule.onNodeWithText("Reply drafted from the task's progress").assertIsDisplayed()
            rule.onAllNodes(hasSetTextAction()).assertCountEquals(0)
            rule.onNodeWithTag("assistant-draft-edit-message").assertTextEquals("Edit").performClick(); settle()
            rule.onAllNodes(hasSetTextAction()).assertCountEquals(1)
            capture("records-glass-edit-$mode")
            rule.onNodeWithTag("assistant-draft-edit-message").assertTextEquals("Done").performClick(); settle()
            rule.onAllNodes(hasSetTextAction()).assertCountEquals(0)
        }
        assertNotEquals("dark capture must differ from light", colors[0], colors[1])
        // Later puts the card away from the conversation (it waits in Needs You)
        rule.onNodeWithTag("assistant-draft-later-message").performClick(); settle()
        rule.onNodeWithText("Is the login crash fixed yet?").assertDoesNotExist()
    }

    private fun keepsTheRecordsBehindTheTrays(style: AppStyle) {
        show(style, mutableStateOf(false)); settle()
        rule.onNodeWithText("Your computer is connected again; the morning brief runs as planned.").assertDoesNotExist()
        rule.onNodeWithText("Fix the login crash is done").assertDoesNotExist()
        rule.onNodeWithTag("exchange-header", useUnmergedTree = true).assertDoesNotExist()
        rule.onNodeWithText("Is the login crash fixed yet?").assertDoesNotExist()
        // the user's own word keeps its bubble and label
        rule.onNodeWithText("Use the staging key for the test.").assertIsDisplayed()
        capture("records-${style.name.lowercase()}-light")
    }

    @Test fun briefKeepsTheRecordsBehindTheTrays() = keepsTheRecordsBehindTheTrays(AppStyle.Brief)
    @Test fun boardKeepsTheRecordsBehindTheTrays() = keepsTheRecordsBehindTheTrays(AppStyle.Board)
}

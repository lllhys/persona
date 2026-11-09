package persona.feature.chat

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsNode
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.isRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import coil3.ImageLoader
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.BackgroundTask
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.Item
import persona.core.contracts.LoopInfo
import persona.core.contracts.Interaction
import persona.core.contracts.Machine
import persona.core.contracts.PlanTask
import persona.core.contracts.ServerEvent
import persona.core.contracts.Session
import persona.core.contracts.SessionInfo
import persona.core.contracts.SessionKey
import persona.core.contracts.SessionTodo
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.api.ChatAction
import persona.core.contracts.api.ChatActionResult
import persona.core.contracts.api.ChatActions
import persona.core.contracts.api.ChatRepository
import persona.core.contracts.api.ChatSnapshot
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.DeliveryState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.*
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.ToolCall
import persona.core.domain.lines.WorkItem
import persona.core.domain.tasks.AssistantExtra
import persona.core.domain.tasks.TasksStore
import persona.feature.chat.media.ChatImageViewer
import java.io.ByteArrayOutputStream
import java.io.File
import java.time.Instant

/**
 * The chat's part-a pages in each of the app's languages (docs/protocol/i18n.md): work lines with tool calls, the assistant's
 * task, plan and "Today" cards, "Session status", "Session info", "Search in session" and the image viewer's chrome, Glass light and dark (the work
 * page in every style). The demo data is English, so the English pages must show no Chinese at all. PNGs in
 * `build/i18n-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ChatLanguageShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2000dp-xhdpi")
    @Test fun workZh() = shots("zh", "work", "正在运行 ./gradlew test")

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2000dp-xhdpi")
    @Test fun statusZh() = shots("zh", "status", "会话状态")

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2000dp-xhdpi")
    @Test fun statusZhDark() = shots("zh", "status", "会话状态", dark = true)

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2000dp-xhdpi")
    @Test fun infoZh() = shots("zh", "info", "会话信息")

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2000dp-xhdpi")
    @Test fun infoZhDark() = shots("zh", "info", "会话信息", dark = true)

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2000dp-xhdpi")
    @Test fun searchZh() = shots("zh", "search", "needle")

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2000dp-xhdpi")
    @Test fun searchZhDark() = shots("zh", "search", "needle", dark = true)

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2000dp-xhdpi")
    @Test fun viewerZh() = shots("zh", "viewer", "保存")

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2000dp-xhdpi")
    @Test fun viewerZhDark() = shots("zh", "viewer", "保存", dark = true)

    @Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
    @Test fun workEn() = shots("en", "work", "Running ./gradlew test")

    @Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
    @Test fun statusEn() = shots("en", "status", "Session status")

    @Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
    @Test fun statusEnDark() = shots("en", "status", "Session status", dark = true)

    @Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
    @Test fun infoEn() = shots("en", "info", "Session info")

    @Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
    @Test fun infoEnDark() = shots("en", "info", "Session info", dark = true)

    @Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
    @Test fun searchEn() = shots("en", "search", "needle")

    @Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
    @Test fun searchEnDark() = shots("en", "search", "needle", dark = true)

    @Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
    @Test fun viewerEn() = shots("en", "viewer", "Save")

    @Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
    @Test fun viewerEnDark() = shots("en", "viewer", "Save", dark = true)

    @Config(sdk = [35], qualifiers = "ja-w400dp-h2000dp-xhdpi")
    @Test fun workJa() = shots("ja", "work", "./gradlew testを実行中")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h2000dp-xhdpi")
    @Test fun statusJa() = shots("ja", "status", "セッションの状態")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h2000dp-xhdpi")
    @Test fun statusJaDark() = shots("ja", "status", "セッションの状態", dark = true)

    @Config(sdk = [35], qualifiers = "ja-w400dp-h2000dp-xhdpi")
    @Test fun infoJa() = shots("ja", "info", "セッション情報")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h2000dp-xhdpi")
    @Test fun infoJaDark() = shots("ja", "info", "セッション情報", dark = true)

    @Config(sdk = [35], qualifiers = "ja-w400dp-h2000dp-xhdpi")
    @Test fun searchJa() = shots("ja", "search", "needle")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h2000dp-xhdpi")
    @Test fun searchJaDark() = shots("ja", "search", "needle", dark = true)

    @Config(sdk = [35], qualifiers = "ja-w400dp-h2000dp-xhdpi")
    @Test fun viewerJa() = shots("ja", "viewer", "保存")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h2000dp-xhdpi")
    @Test fun viewerJaDark() = shots("ja", "viewer", "保存", dark = true)

    private companion object {
        /**
         * This part's Chinese strings (values-zh/strings_a.xml) as patterns, placeholders matching anything: no text on an
         * English page may be one of them. Chinese that other modules still draw (the design system, core/domain, the
         * rest of feature/chat) is printed for them instead; they move it to resources themselves (#2453).
         */
        val OURS: List<Regex> by lazy {
            val xml = File("src/main/res/values-zh/strings_a.xml").readText()
            Regex(">([^<]*)</(?:string|item)>").findAll(xml).map { it.groupValues[1].trim('"').replace("\\'", "'") }
                // Three Chinese characters at least outside the placeholders: shorter ones ("Close", "%1$d s") are words other
                // modules draw too.
                .filter { value -> value.replace(Regex("%\\d\\$[sd]"), "").count { Character.UnicodeScript.of(it.code) == Character.UnicodeScript.HAN } >= 3 }
                .map { value -> Regex(value.split(Regex("%\\d\\$[sd]")).joinToString(".*") { Regex.escape(it) }) }
                .toList()
        }
    }

    // ── demo data (English, so leftover Chinese in the English pages is the page's own) ──

    private fun use(id: String, name: String, input: JsonObject) =
        Item(id, kind = "tool_use", role = "assistant", toolName = name, toolInput = input).asLineItem()
    private fun done(id: String) = Item("$id-r", kind = "tool_result", role = "user", text = "ok").asLineItem()
    private fun tool(id: String, name: String, finished: Boolean, vararg fields: Pair<String, String>) =
        WorkItem.Tool(ToolCall(use(id, name, buildJsonObject { fields.forEach { (k, v) -> put(k, v) } }),
            if (finished) done(id) else null, emptyList()))

    private val start = Instant.now().minusSeconds(83)
    private val steps = listOf(
        tool("a", "Bash", true, "command" to "ls android/feature/chat"),
        tool("b", "Read", true, "file_path" to "/w/ChatContent.kt"),
        tool("c", "Grep", true, "pattern" to "workSummary"),
        tool("d", "Edit", true, "file_path" to "/w/ContentPresentation.kt", "old_string" to "a\nb", "new_string" to "a\nc\nd"),
    )
    private val live = ChatLine.Work("w-live", steps + tool("e", "Bash", false, "command" to "./gradlew test"),
        live = true, start = start, end = null, open = false)
    private val finished = ChatLine.Work("w-done", steps, live = false, start = start, end = start.plusSeconds(83), open = false)
    private val todoPlan = WorkItem.Tool(ToolCall(use("p1", "TodoWrite", buildJsonObject {
        put("todos", buildJsonArray {
            listOf("Read the work line" to "completed", "Preview running steps" to "in_progress", "Write tests" to "pending").forEach { (c, s) ->
                add(buildJsonObject { put("content", c); put("status", s) })
            }
        })
    }), done("p1"), emptyList()))

    private val now = Instant.now()
    private val lead = Session(machineId = "mac", sessionId = "lead", title = "Fix the login bug", cwd = "/w/shop", status = "working",
        agent = "claude")
    private val todos = listOf(
        Todo("t1", "Fix the login bug", createdAt = now.minusSeconds(1500).toString(), origin = "main",
            agents = listOf(persona.core.contracts.TodoAgent("mac", "lead", cwd = "/w/shop"))),
        Todo("t2", "Update the docs", createdAt = now.minusSeconds(4000).toString(), origin = "main", startError = "launcher not found"),
    )
    private val plan = TaskPlan("plan1", "main", listOf(
        PlanTask("Write release notes", machine = "mac", cwd = "/w/shop", permission = "acceptEdits", why = "The release goes out today"),
        PlanTask("Check the crash report", machine = "mac", cwd = "/w/app", readonly = true),
    ), state = "open")
    private fun feed(id: String, kind: String, ref: String? = null, body: JsonObject? = null) = FeedItem(id, "main", now.toString(), kind, ref, body)
    private val feedItems = listOf(
        feed("f1", "plan", "plan1"),
        feed("f2", "task", "t1"),
        feed("f3", "task", "t2"),
        feed("f4", "receipt", "t3", buildJsonObject { put("title", "Ship the beta"); put("summary", "Uploaded build **42** and sent it for review.") }),
        feed("f5", "today", null, buildJsonObject {
            put("done", buildJsonArray { add(buildJsonObject { put("id", "t9"); put("title", "Ship the beta") }) })
            put("waiting", buildJsonArray { add(buildJsonObject { put("id", "t1"); put("title", "Fix the login bug") }) })
            put("working", 2)
            put("meetings", buildJsonArray { add(buildJsonObject { put("start", now.plusSeconds(3600).toString()); put("title", "Design review") }) })
            put("at_me", buildJsonArray { add(buildJsonObject { put("name", "Mobile team"); put("preview", "Can you check the build?") }) })
        }),
    )

    private class Api(val todos: List<Todo>, val plan: TaskPlan) : TasksApi {
        override suspend fun todos() = todos
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList(), mapOf(plan.id to plan))
        override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("unused")
        override suspend fun dismissPlan(id: String): TaskPlan = error("unused")
        override suspend fun retryTask(todoId: String): Todo = error("unused")
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("unused")
        override suspend fun today(conversation: String): FeedItem = error("unused")
        override suspend fun mainConversation(): String? = "main"
        override suspend fun inboxReply(itemId: String, text: String, card: String?) = Unit
    }

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject): Boolean = true
    }

    private val session = Session(machineId = "mac", sessionId = "s1", title = "Fix the login bug", cwd = "/w/shop", status = "offline",
        launcher = "claude", model = "opus", updatedAt = "2026-11-05 09:30", todo = SessionTodo("t1", "Fix the login bug"),
        queued = listOf("Also run the UI tests"), loop = LoopInfo(intervalSeconds = 1800, nextAt = now.plusSeconds(600).toString(),
            prompt = "Check the CI and report back"),
        background = listOf(BackgroundTask("b1", "shell", "Run the snapshot tests", "xcodebuild test")))
    private val machine = Machine("mac", "Work laptop", online = false)

    private val actions = object : ChatActions {
        override suspend fun isMuted(session: Session) = false
        override suspend fun execute(session: Session, action: ChatAction) = ChatActionResult.Done
        override suspend fun verify(session: Session, action: ChatAction) = ChatActionResult.Done
        override suspend fun info(session: Session) = SessionInfo(sessionId = "s1", cwd = "/w/shop", model = "claude-opus-5",
            version = "2.1.320", permissionMode = "acceptEdits", effort = "high", contextTokens = 48_200, outputTokens = 12_800,
            turns = 6, compactions = 2, gitBranch = "main", startedAt = "2026-11-01T02:00:00Z")
    }

    private val searchItems = listOf(
        Item("i1", 1, kind = "text", role = "assistant", text = "The login needle is in AuthService.kt"),
        Item("i2", 2, kind = "thinking", role = "assistant", text = "Look for the needle in the token refresh"),
        Item("i3", 3, kind = "tool_use", role = "assistant", toolName = "Grep", toolInput = buildJsonObject { put("pattern", "needle") }),
        Item("i4", 4, kind = "tool_result", role = "user", text = "AuthService.kt:42 needle()"),
    )
    private val repository = object : ChatRepository {
        override fun observe(session: SessionKey) = MutableStateFlow(ChatSnapshot(items = searchItems, loading = false, hasOlder = true))
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
        override suspend fun loadOlder(session: SessionKey) = Unit
    }

    private fun picture(): ByteArray {
        val bitmap = Bitmap.createBitmap(600, 400, Bitmap.Config.ARGB_8888)
        Canvas(bitmap).apply { drawColor(Color.rgb(70, 110, 160)); drawCircle(300f, 200f, 120f, Paint().apply { color = Color.rgb(240, 200, 90) }) }
        return ByteArrayOutputStream().also { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }.toByteArray()
    }

    // ── shots ──

    private fun texts(node: SemanticsNode): List<String> =
        node.config.getOrNull(SemanticsProperties.Text).orEmpty().map { it.text } +
            node.config.getOrNull(SemanticsProperties.ContentDescription).orEmpty() +
            node.children.flatMap(::texts)

    private fun shots(language: String, scene: String, expected: String, dark: Boolean = false) {
        val store = TasksStore(Api(todos, plan), Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.reloadTodos()
        store.load("main")
        var style by mutableStateOf(AppStyle.Glass)
        var darkNow by mutableStateOf(dark)
        rule.mainClock.autoAdvance = false // the work clock and the spinners never settle
        rule.setContent {
            PersonaTheme(darkTheme = darkNow, reduceMotion = true, style = style) {
                val context = LocalContext.current
                val resources = LocalResources.current
                val loader = remember { ImageLoader.Builder(context).build() }
                val scope = rememberCoroutineScope()
                when (scene) {
                    "work" -> Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l),
                        verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        // the lead's card: the task card and the "Today" card's "Needs you" row say what it asks (#3964, #4752)
                        val host = AssistantTaskHost(store, "main", listOf(lead), { "Work laptop" }, {}, {}, {},
                            pending = listOf(Interaction("c1", machineId = "mac", sessionId = "lead", kind = "permission", toolName = "Bash", summary = "./gradlew test")))
                        for (line in listOf(live, finished)) ChatLineContent(line, SessionKey("mac", "s1"), loader,
                            recordedDuration = 83, onToggle = {}, onFold = {}, folding = emptySet(), foldErrors = emptyMap(), onFullText = {})
                        ChatLineContent(ChatLine.Step("w-open", todoPlan, true), SessionKey("mac", "s1"), loader, recordedDuration = null,
                            onToggle = {}, onFold = {}, folding = emptySet(), foldErrors = emptyMap(), onFullText = {},
                            style = ChatRenderStyle(compactToolSummary = true))
                        feedItems.forEach { AssistantExtraLine(AssistantExtra.Feed(it), host) }
                        AssistantTaskBar(host)
                    }
                    "status" -> {
                        val recovery = remember { ChatStatusRecovery(resources, null, scope) {} }
                        ChatStatusSheet(StatusLineState(ended = true, machineOffline = true), session,
                            listOf(OutboxMessage("m1", session.key, "Please also check the iPad layout", DeliveryState.NeedsConfirmation)),
                            emptySet(), onAction = { _, _ -> }, onDismiss = {}, replacement = null, onOpenReplacement = null,
                            onConnectionDetails = {}, terminal = {}, recordedDuration = 125, notice = null, recovery = recovery,
                            machine = machine, draftSaveError = "", readOnly = false, onOpenSession = {})
                    }
                    "info" -> {
                        val chrome = remember { ChatChromeState(resources, actions, scope, {}, {}) }
                        ChatSessionInfoSheet(session, machine, chrome, onOpenTodo = {}, onDismiss = {})
                    }
                    "search" -> {
                        val search = remember { ChatSearchState().apply { query = "needle" } }
                        ChatSearchScreen(search, searchItems, 3, hasOlder = true, repository, session.key, onClose = {})
                    }
                    "viewer" -> {
                        val bytes = remember { picture() }
                        ChatImageViewer(listOf(ViewerImage("p", "Photo", 600, 400) { bytes }), 0, onDismiss = {}, onEdited = {})
                    }
                }
            }
        }
        run {
            val name = scene
            // A sheet or dialog captured again after a theme change keeps its first frame: one look per test for those.
            val variants = if (name == "work") AppStyle.entries.map { it to false } + (AppStyle.Glass to true) else listOf(AppStyle.Glass to dark)
            for ((s, d) in variants) {
                style = s; darkNow = d
                rule.mainClock.advanceTimeBy(1_500)
                // Search works out its hits on Dispatchers.Default, in real time: under load 1.5 s of the frame clock can
                // pass before they arrive (#5424), so give the scene up to 10 s of real time to show what it should.
                fun shownNow() = rule.onAllNodes(isRoot()).fetchSemanticsNodes().flatMap(::texts)
                val until = System.nanoTime() + 10_000_000_000L
                while (shownNow().none { it.contains(expected) } && System.nanoTime() < until) {
                    Thread.sleep(20)
                    rule.mainClock.advanceTimeBy(100)
                }
                val shown = shownNow()
                assertTrue("$name: no 「$expected」 in $shown", shown.any { it.contains(expected) })
                assertFalse("$name: raw key in $shown", shown.any { it.contains("chat_") })
                if (language == "en") {
                    val chinese = shown.filter { text -> text.any { Character.UnicodeScript.of(it.code) == Character.UnicodeScript.HAN } }
                    val mine = chinese.filter { text -> OURS.any { it.matches(text) } }
                    assertTrue("$name: this part's Chinese left in English: $mine", mine.isEmpty())
                    if (chinese.isNotEmpty()) println("$name: Chinese other modules still draw: $chinese")
                }
                val node = if (name == "work") rule.onNodeWithTag("shot") else rule.onNode(isDialog())
                val bitmap = node.captureToImage().asAndroidBitmap()
                File(out(), "chat-$name-$language-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                    .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            }
        }
    }
}

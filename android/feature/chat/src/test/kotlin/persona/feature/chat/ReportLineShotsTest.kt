package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.Machine
import persona.core.contracts.ServerEvent
import persona.core.contracts.Session
import persona.core.contracts.Supervision
import persona.core.contracts.TaskPlan
import persona.core.contracts.TaskProgress
import persona.core.contracts.Todo
import persona.core.contracts.TodoAgent
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.domain.tasks.TasksStore
import java.io.File
import java.time.Instant

/**
 * A supervised task's report in the user's language (#3181): the Progress row shows the hub's paraphrase (`progress.line`), the
 * task's details the report as the agent wrote it, Markdown rendered. "before" is the same task without a line (an older hub, no
 * model): the row shows the agent's words. Every style, light and dark; a sheet keeps its first frame after a theme change, so
 * each look is a test of its own. PNGs in `build/report-line-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1400dp-xhdpi")
class ReportLineShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/report-line-shots").also { it.mkdirs() }

    private val now = Instant.now()
    private val worker = Session(machineId = "mac", sessionId = "worker", title = "Migrate the orders table", cwd = "/w/shop", state = "working",
        agent = "claude", lastText = "Copying the rows")
    private val report = TaskProgress("progress", 2, 4, "Copied 41 of 42 tables to the new schema; `orders` still needs a backfill before the cutover.",
        line = "已搬完 41 张表，orders 还要补数据",
        detail = "Copied **41 of 42** tables to the new schema.\n\n- `orders` still needs a backfill before the cutover\n" +
            "- Row counts match on every copied table\n\nNext: run the backfill in batches of 10k rows.",
        at = now.minusSeconds(180).toString())
    private fun todos(line: Boolean) = listOf(
        Todo("t1", "迁移订单表", createdAt = now.minusSeconds(1500).toString(), origin = "main", agents = listOf(TodoAgent("mac", "worker", cwd = "/w/shop")),
            supervision = Supervision(supervisor = "main", progress = if (line) report else report.copy(line = ""))),
    )

    private class Api(val todos: List<Todo>) : TasksApi {
        override suspend fun todos() = todos
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList())
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

    private fun shot(scene: String, style: AppStyle, dark: Boolean) {
        val line = scene != "before"
        val store = TasksStore(Api(todos(line)), Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.reloadTodos()
        val machines = listOf(Machine("mac", "工作笔记本", online = true))
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val sheet = remember { mutableStateOf<TaskSheet?>(if (scene == "detail") TaskSheet.Task("t1") else null) }
                val host = AssistantTaskHost(store, "main", listOf(worker), { id -> machines.first { it.id == id }.name }, {}, {}, {},
                    sheet = sheet, machines = machines)
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(vertical = Tokens.Space.l)) {
                    AssistantTrays(host, remember { AssistantTrayState().apply { expanded = AssistantTray.Running } },
                        emptyList(), emptyList(), emptyList(), persona.core.designsystem.AssistantTrayTokens.maximumHeight,
                        busy = false, enabled = true, onApprove = {}, onReview = {}, onSend = {}, onSchedule = {})
                }
                AssistantTaskSheets(host)
            }
        }
        rule.mainClock.advanceTimeBy(1_500)
        val expected = when (scene) {
            "before" -> "Copied 41 of 42 tables"
            "detail" -> "最新汇报"
            else -> "已搬完 41 张表，orders 还要补数据"
        }
        assertTrue("$scene: no 「$expected」", rule.onAllNodesWithText(expected, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        if (scene == "after") assertTrue("the row still shows the agent's words",
            rule.onAllNodesWithText("Copied 41 of 42 tables", substring = true, useUnmergedTree = true).fetchSemanticsNodes().isEmpty())
        if (scene == "detail") assertTrue("the details do not show the original report",
            rule.onAllNodesWithText("Row counts match on every copied table", substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        val node = if (scene == "detail") rule.onNode(isDialog()) else rule.onNodeWithTag("shot")
        val bitmap = node.captureToImage().asAndroidBitmap()
        File(out(), "report-line-$scene-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream()
            .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    /** The row prefers the line and falls back to the text; the hub's JSON without the new fields still reads. */
    @Test fun rowPrefersTheLine() {
        assertEquals("已搬完 41 张表，orders 还要补数据", report.rowText)
        assertEquals(report.text, report.copy(line = " ").rowText)
        assertEquals(report.detail, report.reportText)
        assertEquals(report.text, report.copy(detail = "").reportText)
        val old = Json { ignoreUnknownKeys = true }.decodeFromString(TaskProgress.serializer(), """{"kind":"progress","text":"Copying","at":"2026-11-10T08:00:00Z"}""")
        assertEquals("Copying", old.rowText)
        assertEquals("Copying", old.reportText)
        val store = TasksStore(Api(todos(true)), Connection(), CoroutineScope(Dispatchers.Unconfined))
        val host = AssistantTaskHost(store, "main", listOf(worker), { it }, {}, {}, {})
        assertEquals("已搬完 41 张表，orders 还要补数据", trayProgress(todos(true), emptyList(), host).single().text)
        assertEquals(report.text, trayProgress(todos(false), emptyList(), host).single().text)
    }

    @Test fun beforeGlass() = shot("before", AppStyle.Glass, false)
    @Test fun beforeGlassDark() = shot("before", AppStyle.Glass, true)
    @Test fun beforeBrief() = shot("before", AppStyle.Brief, false)
    @Test fun beforeBriefDark() = shot("before", AppStyle.Brief, true)
    @Test fun beforeBoard() = shot("before", AppStyle.Board, false)
    @Test fun beforeBoardDark() = shot("before", AppStyle.Board, true)

    @Test fun afterGlass() = shot("after", AppStyle.Glass, false)
    @Test fun afterGlassDark() = shot("after", AppStyle.Glass, true)
    @Test fun afterBrief() = shot("after", AppStyle.Brief, false)
    @Test fun afterBriefDark() = shot("after", AppStyle.Brief, true)
    @Test fun afterBoard() = shot("after", AppStyle.Board, false)
    @Test fun afterBoardDark() = shot("after", AppStyle.Board, true)

    @Test fun detailGlass() = shot("detail", AppStyle.Glass, false)
    @Test fun detailGlassDark() = shot("detail", AppStyle.Glass, true)
    @Test fun detailBrief() = shot("detail", AppStyle.Brief, false)
    @Test fun detailBriefDark() = shot("detail", AppStyle.Brief, true)
    @Test fun detailBoard() = shot("detail", AppStyle.Board, false)
    @Test fun detailBoardDark() = shot("detail", AppStyle.Board, true)
}

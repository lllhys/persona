package persona.feature.chat

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.res.stringResource
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.Interaction
import persona.core.contracts.Machine
import persona.core.contracts.ServerEvent
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.Supervision
import persona.core.contracts.TaskPlan
import persona.core.contracts.TaskProgress
import persona.core.contracts.Todo
import persona.core.contracts.TodoAgent
import persona.core.contracts.TodoAsk
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.domain.tasks.TasksStore
import java.io.File
import java.time.Instant

/**
 * A task's and an agent's detail as the page a wide window pushes into the chat's column (#9347, iOS `detailPresentation`):
 * the Fold's inner screen, 673 × 951 dp, every style, light and dark. A phone keeps the sheet, which
 * `TaskSheetsShotsTest` draws at 400 dp. PNGs in `build/i18n-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w673dp-h951dp-mdpi")
class TaskDetailPageShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    private val now = Instant.now()
    private val lead = Session(machineId = "mac", sessionId = "lead", title = "修登录页的崩溃", cwd = "/w/shop", state = "needs_you", agent = "claude",
        lastText = "要跑一下集成测试吗？")
    private val helper = Session(machineId = "mac", sessionId = "helper", title = "补单元测试", cwd = "/w/shop", state = "working", agent = "codex",
        lastText = "在写 AuthService 的测试")
    private val own = Session(machineId = "box", sessionId = "own", title = "夜间构建", cwd = "/srv/ci", state = "working", agent = "claude",
        lastText = "第 3 步，共 5 步")
    private val todos = listOf(
        Todo("t1", "修登录页的崩溃", createdAt = now.minusSeconds(1500).toString(), origin = "main",
            agents = listOf(TodoAgent("mac", "lead", cwd = "/w/shop"), TodoAgent("mac", "helper", cwd = "/w/shop")),
            ask = TodoAsk(outcome = "accept", verdict = "可以交付了：登录页不再崩溃，回归也过了。",
                recommend = "合并到 main 并发布一个修复版。", askedAt = now.minusSeconds(120).toString()),
            supervision = Supervision(supervisor = "main",
                progress = TaskProgress(step = 4, steps = 4, detail = "修好了崩溃，补了回归测试，`./gradlew test` 全绿。",
                    at = now.minusSeconds(300).toString()))),
        Todo("t2", "更新发布说明", createdAt = now.minusSeconds(400).toString(), origin = "main"),
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

    private val machines = listOf(Machine("mac", "工作笔记本", online = true), Machine("box", "构建服务器", online = true))

    // The lead agent waits on a permission: what it asks is the task's line (#3964).
    private val bash = Interaction("p1", machineId = "mac", sessionId = "lead", kind = "permission", toolName = "Bash", summary = "swift test")

    private fun host(sheet: androidx.compose.runtime.MutableState<TaskSheet?>) =
        AssistantTaskHost(TasksStore(Api(todos), Connection(), CoroutineScope(Dispatchers.Unconfined)).apply { reloadTodos() },
            "main", listOf(lead, helper, own), { id -> machines.first { it.id == id }.name }, {}, {}, {},
            sheet = sheet, machines = machines, pending = listOf(bash))

    /** The page as the chat draws it on a wide window, over it (the chat's own background here). */
    private fun page(open: TaskSheet, style: AppStyle, dark: Boolean, expected: String) {
        val dismissed = androidx.compose.runtime.mutableStateOf(false)
        val sheet = androidx.compose.runtime.mutableStateOf<TaskSheet?>(open)
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val taskHost = remember { host(sheet) }
                Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("shot")) {
                    AssistantTaskPage(open, taskHost, onDismiss = { dismissed.value = true; sheet.value = null })
                }
            }
        }
        rule.mainClock.advanceTimeBy(1_500)
        assertTrue("${open::class.simpleName}: no 「$expected」",
            rule.onAllNodesWithText(expected, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        // A page, not the sheet: nothing rises from the bottom.
        assertTrue("${open::class.simpleName}: rose as a sheet", rule.onAllNodes(isDialog()).fetchSemanticsNodes().isEmpty())
        rule.onNodeWithContentDescription(rule.activity.getString(persona.core.designsystem.R.string.designsystem_back)).assertExists()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out(), "task-page-${open.file()}-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream()
            .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        assertFalse("the page closed by itself", dismissed.value)
    }

    private fun TaskSheet.file(): String = when (this) {
        is TaskSheet.Task -> "task"
        is TaskSheet.Agent -> "agent"
        TaskSheet.Board -> "board"
    }

    @Test fun taskGlass() = page(TaskSheet.Task("t1"), AppStyle.Glass, false, "打开对话")
    @Test fun taskGlassDark() = page(TaskSheet.Task("t1"), AppStyle.Glass, true, "打开对话")
    @Test fun taskBrief() = page(TaskSheet.Task("t1"), AppStyle.Brief, false, "打开对话")
    @Test fun taskBriefDark() = page(TaskSheet.Task("t1"), AppStyle.Brief, true, "打开对话")
    @Test fun taskBoard() = page(TaskSheet.Task("t1"), AppStyle.Board, false, "打开对话")
    @Test fun taskBoardDark() = page(TaskSheet.Task("t1"), AppStyle.Board, true, "打开对话")

    @Test fun agentGlass() = page(TaskSheet.Agent(SessionKey("box", "own")), AppStyle.Glass, false, "不是从对话开的")
    @Test fun agentGlassDark() = page(TaskSheet.Agent(SessionKey("box", "own")), AppStyle.Glass, true, "不是从对话开的")
    @Test fun agentBrief() = page(TaskSheet.Agent(SessionKey("box", "own")), AppStyle.Brief, false, "不是从对话开的")
    @Test fun agentBriefDark() = page(TaskSheet.Agent(SessionKey("box", "own")), AppStyle.Brief, true, "不是从对话开的")
    @Test fun agentBoard() = page(TaskSheet.Agent(SessionKey("box", "own")), AppStyle.Board, false, "不是从对话开的")
    @Test fun agentBoardDark() = page(TaskSheet.Agent(SessionKey("box", "own")), AppStyle.Board, true, "不是从对话开的")

    /** The bar's Back goes back to the chat, as the sheet's ✕ closes it (iOS `closes: !pushed`). */
    @Test fun theBarsBackGoesBack() {
        val sheet = androidx.compose.runtime.mutableStateOf<TaskSheet?>(TaskSheet.Task("t1"))
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                val taskHost = remember { host(sheet) }
                AssistantTaskPage(TaskSheet.Task("t1"), taskHost, onDismiss = { sheet.value = null })
            }
        }
        rule.waitForIdle()
        rule.onNodeWithContentDescription(rule.activity.getString(persona.core.designsystem.R.string.designsystem_back)).performClick()
        rule.waitForIdle()
        assertTrue("Back left the detail open", sheet.value == null)
    }

    /** The system's Back closes the page, as it closes any page of the chat's stack. */
    @Test fun systemBackClosesThePage() {
        val sheet = androidx.compose.runtime.mutableStateOf<TaskSheet?>(TaskSheet.Agent(SessionKey("box", "own")))
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                val taskHost = remember { host(sheet) }
                AssistantTaskPage(TaskSheet.Agent(SessionKey("box", "own")), taskHost, onDismiss = { sheet.value = null })
            }
        }
        rule.waitForIdle()
        rule.runOnUiThread { rule.activity.onBackPressedDispatcher.onBackPressed() }
        rule.waitForIdle()
        assertTrue("Back left the detail open", sheet.value == null)
    }

    /** A task the hub no longer lists says so, under the page's own bar. */
    @Test fun aTaskThatIsGoneSaysSo() {
        val sheet = androidx.compose.runtime.mutableStateOf<TaskSheet?>(TaskSheet.Task("deleted"))
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                val taskHost = remember { host(sheet) }
                AssistantTaskPage(TaskSheet.Task("deleted"), taskHost, onDismiss = {})
            }
        }
        rule.waitForIdle()
        assertTrue(rule.onAllNodesWithText("可能被删了", substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        assertTrue(rule.onAllNodesWithContentDescription(rule.activity.getString(persona.core.designsystem.R.string.designsystem_back))
            .fetchSemanticsNodes().isNotEmpty())
    }
}

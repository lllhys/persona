package persona.feature.agents

import android.graphics.Bitmap
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.test.performTouchInput
import androidx.compose.foundation.layout.padding
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.Hello
import persona.core.contracts.ServerEvent
import persona.core.contracts.TaskPlan
import persona.core.contracts.TodoDue
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.AppFeedback
import persona.core.designsystem.CalendarTokens
import persona.core.designsystem.FeedbackAction
import persona.core.designsystem.LocalAppFeedback
import persona.core.designsystem.Tokens
import persona.core.domain.tasks.TasksStore
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.click
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.contracts.Todo
import persona.core.contracts.TodoAgent
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File
import java.time.Instant
import java.util.TimeZone
import persona.core.designsystem.FeedbackNotice
import persona.core.designsystem.JToast
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/**
 * The to-dos' month (#2046) on a phone: List | Calendar, November 2026 from Monday with today (the 5th) circled, a
 * day's to-dos in their state colors (overdue red, done faded and struck through), "+3" on a day of five, the days of
 * October and December faded, "2 without a due date" (whose "See list" goes back to the list); and a day's to-dos in the
 * sheet a tap brings up, one of which opens. Every style, light and dark; the list with the switch above it.
 *
 * Moving one (#2045): the month before, 体检预约 (09:00 on the 6th) carried to the 11th with where it would land, and
 * after it is let go there with the shell's "Moved to … Undo" (drawn here as Root.kt draws it, kept up for the shot);
 * and TalkBack's "Change Due Date…" sheet.
 *
 * Day and week (#4200): the 12th as a day, from the month's "Show Day", with its week's strip, its All Day row and
 * 季度复盘 and 准备评审材料 side by side at 14:00 and 14:15; this week (Nov 2 – 8) with today's red line at 17:30;
 * 体检预约 carried in the week to 11:00 on Wednesday, with where it would land; and a tap at 17:10 on the 12th
 * bringing up a new to-do due at 17:00.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h860dp-xhdpi")
class TodoCalendarShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    private val zone = TimeZone.getDefault()
    @Before fun inShanghai() = TimeZone.setDefault(TimeZone.getTimeZone("Asia/Shanghai"))
    @After fun back() = TimeZone.setDefault(zone)

    // 17:30 on Thursday, November 5 2026, in the test's zone
    private val now = Instant.parse("2026-11-05T09:30:00Z")
    private val sessions = listOf(
        Session("mac", "s1", title = "改合同", state = "needs_you", updatedAt = "2026-11-05T09:00:00Z"),
        Session("mac", "s2", title = "修登录页", state = "working", updatedAt = "2026-11-05T09:20:00Z"),
    )
    private fun at(local: String) = java.time.LocalDateTime.parse(local).atZone(java.time.ZoneId.of("Asia/Shanghai")).toInstant().toString()
    private val todos = listOf(
        Todo("t1", title = "交周报", dueDate = "2026-11-03", doneAt = at("2026-11-03T17:00")),
        Todo("t2", title = "续域名", dueDate = "2026-11-04", priority = "high"),
        Todo("t3", title = "发登录修复", dueAt = at("2026-11-05T10:00"), agents = listOf(TodoAgent("mac", "s2"))),
        Todo("t4", title = "回复合同邮件", dueDate = "2026-11-05", agents = listOf(TodoAgent("mac", "s1"))),
        Todo("t5", title = "体检预约", dueAt = at("2026-11-06T09:00"), remindAt = at("2026-11-06T08:00")),
        Todo("t6", title = "评审设计稿", dueDate = "2026-11-12"),
        Todo("t7", title = "季度复盘", dueAt = at("2026-11-12T14:00"), priority = "high"),
        Todo("t8", title = "写发布说明", dueAt = at("2026-11-12T16:00"), agents = listOf(TodoAgent("mac", "s2"))),
        Todo("t9", title = "订机票", dueAt = at("2026-11-12T18:00"), remindAt = at("2026-11-12T12:00")),
        Todo("t10", title = "买礼物", dueAt = at("2026-11-12T20:00"), doneAt = at("2026-11-04T20:00")),
        Todo("t11", title = "交房租", dueDate = "2026-11-18"),
        Todo("t12", title = "整理崩溃报告", dueAt = at("2026-11-20T11:00"), doneAt = at("2026-11-02T11:00")),
        Todo("t13", title = "牙医复诊", dueAt = at("2026-11-26T15:30")),
        Todo("t14", title = "年度总结", dueDate = "2026-12-01"),
        Todo("t15", title = "交水电费", dueDate = "2026-10-30", doneAt = at("2026-10-29T10:00")),
        Todo("t16", title = "轮换 API Key"),
        Todo("t17", title = "读那篇论文"),
    )

    private class Api(var rows: List<Todo>) : TasksApi {
        val sent = mutableListOf<Pair<String, TodoDue>>()
        override suspend fun todos() = rows
        override suspend fun setTodoDue(id: String, due: TodoDue): Todo =
            rows.first { it.id == id }.withDue(due).also { moved -> sent += id to due; rows = rows.map { if (it.id == id) moved else it } }
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList(), emptyMap())
        override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("unused")
        override suspend fun dismissPlan(id: String): TaskPlan = error("unused")
        override suspend fun retryTask(todoId: String): Todo = error("unused")
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("unused")
        override suspend fun today(conversation: String): FeedItem = error("unused")
        override suspend fun mainConversation(): String? = null
        override suspend fun inboxReply(itemId: String, text: String, card: String?) = Unit
    }

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override val lastHello: Hello get() = Hello(serverVersion = "1.0.0", protocol = 1)
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }

    /** The shell's notices, to the toast the test draws where Root.kt draws it. */
    private class Shell : AppFeedback {
        val shown = MutableSharedFlow<Pair<String, FeedbackAction?>>(extraBufferCapacity = 4)
        override fun failure(error: Throwable, operation: Boolean) = shown.tryEmit((error.message ?: "") to null)
        override fun notice(message: String) = shown.tryEmit(message to null)
        override fun notice(message: String, action: FeedbackAction) = shown.tryEmit(message to action)
    }

    /** Moving 体检预约 (#2045): "before" (the month), "drag" (carried to the 11th), "moved" (let go there), "reschedule" (TalkBack's sheet). */
    private fun move(scene: String, style: AppStyle, dark: Boolean) {
        val api = Api(todos)
        val store = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.reloadTodos()
        val shell = Shell()
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                // The shell's toast, drawn where Root.kt draws it but kept up for the shot (the host times it out).
                var notice by remember { mutableStateOf<FeedbackNotice?>(null) }
                LaunchedEffect(shell) { shell.shown.collect { (message, action) -> notice = FeedbackNotice(message, action) } }
                CompositionLocalProvider(LocalAppFeedback provides shell) {
                    val shown by store.todos.collectAsState()
                    Box(Modifier.testTag("shot").width(400.dp).height(860.dp).background(PersonaTheme.palette.background)) {
                        TodosScreen(shown, sessions, store, onBack = {}, onOpenTodo = {}, now = now)
                        notice?.let { JToast(it, dismiss = {}, modifier = Modifier.align(Alignment.TopCenter).padding(top = 12.dp)) }
                    }
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("日历").performClick()
        rule.waitForIdle()
        fun has(text: String) = rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        fun save(name: String, node: androidx.compose.ui.test.SemanticsNodeInteraction = rule.onNodeWithTag("shot")) {
            val bitmap = node.captureToImage().asAndroidBitmap()
            File(out(), "todo-calendar-$name-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        val from = rule.onNodeWithTag("todo-calendar-day-2026-11-06")
        when (scene) {
            "before" -> save("before")
            "reschedule" -> {
                val action = from.fetchSemanticsNode().config[SemanticsActions.CustomActions].single()
                assertEquals("改截止时间：体检预约…", action.label)
                rule.runOnIdle { action.action() }
                rule.waitForIdle()
                assertTrue("reschedule: no time", has("09:00"))
                save("reschedule", rule.onNode(isDialog()))
            }
            else -> {
                val start = from.fetchSemanticsNode().boundsInRoot
                val end = rule.onNodeWithTag("todo-calendar-day-2026-11-11").fetchSemanticsNode().boundsInRoot
                from.performTouchInput {
                    val chip = Offset(centerX, (Tokens.Space.xs + CalendarTokens.dayMark + Tokens.Space.xxs + 6.dp).toPx())
                    down(chip)
                    move(delayMillis = viewConfiguration.longPressTimeoutMillis + 100)
                    // a little off the middle of the 11th, as a finger would be
                    val target = Offset(end.center.x - start.left + 10.dp.toPx(), end.center.y - start.top + 8.dp.toPx())
                    for (i in 1..6) moveTo(chip + (target - chip) * (i / 6f), delayMillis = 30)
                }
                rule.waitForIdle()
                assertTrue("drag: no landing", has("11月11日周三 09:00"))
                rule.onNodeWithTag("todo-calendar-dragging").assertExists()
                if (scene == "drag") save("drag")
                from.performTouchInput { up() }
                rule.waitForIdle()
                assertEquals(listOf("t5" to TodoDue.At(Instant.parse("2026-11-11T01:00:00Z"))), api.sent)
                assertTrue("moved: no notice", has("已移到 11月11日周三 09:00"))
                if (scene == "moved") save("moved")
            }
        }
    }

    /** Day and week (#4200): "dayview", "week", "weekdrag" (体检预约 carried to 11:00 on the 4th), "new" (a tap on the axis). */
    private fun axis(scene: String, style: AppStyle, dark: Boolean, suffix: String = "") {
        val api = Api(todos + Todo("t18", title = "准备评审材料", dueAt = at("2026-11-12T14:15")))
        val store = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.reloadTodos()
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val shown by store.todos.collectAsState()
                Box(Modifier.testTag("shot").width(400.dp).height(860.dp).background(PersonaTheme.palette.background)) {
                    TodosScreen(shown, sessions, store, onBack = {}, onOpenTodo = {}, now = now)
                }
            }
        }
        rule.waitForIdle()
        fun has(text: String) = rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        fun mode(name: String) {
            rule.onNode(hasText(name) and hasAnyAncestor(hasTestTag("todo-calendar-mode"))).performClick()
            rule.waitForIdle()
        }
        fun save(node: androidx.compose.ui.test.SemanticsNodeInteraction = rule.onNodeWithTag("shot")) {
            val bitmap = node.captureToImage().asAndroidBitmap()
            File(out(), "todo-calendar-$scene-${style.name.lowercase()}-${if (dark) "dark" else "light"}$suffix.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        fun hours(h: Float) = with(rule.density) { (CalendarTokens.hour * h).toPx() }
        rule.onNodeWithText("日历").performClick()
        rule.waitForIdle()
        mode("月")
        if (scene == "dayview" || scene == "new") {
            rule.onNodeWithTag("todo-calendar-day-2026-11-12").performClick()
            rule.waitForIdle()
            rule.onNodeWithTag("todo-calendar-show-day").performClick()
            rule.waitForIdle()
            assertTrue("$scene: not the 12th", has("11月12日周四"))
            for (t in listOf("评审设计稿", "季度复盘", "准备评审材料", "写发布说明")) assertTrue("$scene: no $t", has(t))
        } else {
            mode("周")
            assertTrue("$scene: no now line", rule.onAllNodesWithTag("todo-calendar-now").fetchSemanticsNodes().isNotEmpty())
        }
        when (scene) {
            "dayview", "week" -> save()
            "weekdrag" -> {
                val block = rule.onNodeWithTag("todo-calendar-entry-t5")
                val start = block.fetchSemanticsNode().boundsInRoot
                val from = rule.onNodeWithTag("todo-calendar-day-header-2026-11-06").fetchSemanticsNode().boundsInRoot
                val to = rule.onNodeWithTag("todo-calendar-day-header-2026-11-04").fetchSemanticsNode().boundsInRoot
                block.performTouchInput {
                    down(center)
                    move(delayMillis = viewConfiguration.longPressTimeoutMillis + 100)
                    val by = Offset(to.center.x - from.center.x, hours(2f))
                    for (i in 1..6) moveTo(center + by * (i / 6f), delayMillis = 30)
                }
                rule.waitForIdle()
                assertTrue("weekdrag: no landing", has("11月4日周三 11:00"))
                rule.onNodeWithTag("todo-calendar-drop-at").assertExists()
                assertTrue(start.width > 0f)
                save()
                block.performTouchInput { up() }
                rule.waitForIdle()
                assertEquals(listOf("t5" to TodoDue.At(Instant.parse("2026-11-04T03:00:00Z"))), api.sent)
            }
            "new" -> {
                val day = rule.onNodeWithTag("todo-calendar-axis-2026-11-12").fetchSemanticsNode().positionInRoot
                rule.onRoot().performTouchInput { click(day + Offset(hours(0.5f), hours(17f + 10 / 60f))) }
                rule.waitForIdle()
                rule.onNode(hasSetTextAction() and hasAnyAncestor(hasTestTag("todo-calendar-new"))).performTextInput("准备周会")
                rule.waitForIdle()
                assertTrue("new: no time", has("17:00"))
                save(rule.onNode(isDialog()))
            }
        }
    }

    private fun shot(scene: String, style: AppStyle, dark: Boolean) {
        var opened: String? = null
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(860.dp).background(PersonaTheme.palette.background)) {
                    TodosScreen(todos, sessions, null, onBack = {}, onOpenTodo = { opened = it }, now = now)
                }
            }
        }
        rule.waitForIdle()
        fun has(text: String) = rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        if (scene != "list") {
            rule.onNodeWithText("日历").performClick()
            rule.waitForIdle()
            assertTrue("$scene: no month title", has("2026年11月"))
            assertTrue("$scene: no undated bar", has("2 条没有截止"))
        }
        val node = if (scene == "day") {
            rule.onNodeWithTag("todo-calendar-day-2026-11-12").performClick()
            rule.waitForIdle()
            // the whole day, in the month's order: all day first, then by time, done last at its time
            assertTrue("day: no sheet", has("11月12日"))
            for (t in listOf("评审设计稿", "季度复盘", "写发布说明", "订机票", "买礼物")) assertTrue("day: no $t", has(t))
            rule.onNode(isDialog())
        } else rule.onNodeWithTag("shot")
        val bitmap = node.captureToImage().asAndroidBitmap()
        File(out(), "todo-calendar-$scene-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream()
            .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        if (scene == "day") {
            rule.onNodeWithText("订机票").performClick()
            rule.waitForIdle()
            assertEquals("t9", opened)
        }
        if (scene == "month") {
            // "See list" goes back to the list
            rule.onNodeWithText("看列表").performClick()
            rule.waitForIdle()
            assertTrue("month: See list did not show the list", rule.onAllNodesWithTag("todos-filter").fetchSemanticsNodes().isNotEmpty())
        }
    }

    @Test fun monthGlass() = shot("month", AppStyle.Glass, false)
    @Test fun monthGlassDark() = shot("month", AppStyle.Glass, true)
    @Test fun monthBrief() = shot("month", AppStyle.Brief, false)
    @Test fun monthBriefDark() = shot("month", AppStyle.Brief, true)
    @Test fun monthBoard() = shot("month", AppStyle.Board, false)
    @Test fun monthBoardDark() = shot("month", AppStyle.Board, true)

    @Test fun dayGlass() = shot("day", AppStyle.Glass, false)
    @Test fun dayGlassDark() = shot("day", AppStyle.Glass, true)
    @Test fun dayBrief() = shot("day", AppStyle.Brief, false)
    @Test fun dayBriefDark() = shot("day", AppStyle.Brief, true)
    @Test fun dayBoard() = shot("day", AppStyle.Board, false)
    @Test fun dayBoardDark() = shot("day", AppStyle.Board, true)

    // Moving one (#2045)
    @Test fun beforeGlass() = move("before", AppStyle.Glass, false)
    @Test fun beforeGlassDark() = move("before", AppStyle.Glass, true)
    @Test fun beforeBrief() = move("before", AppStyle.Brief, false)
    @Test fun beforeBriefDark() = move("before", AppStyle.Brief, true)
    @Test fun beforeBoard() = move("before", AppStyle.Board, false)
    @Test fun beforeBoardDark() = move("before", AppStyle.Board, true)
    @Test fun dragGlass() = move("drag", AppStyle.Glass, false)
    @Test fun dragGlassDark() = move("drag", AppStyle.Glass, true)
    @Test fun dragBrief() = move("drag", AppStyle.Brief, false)
    @Test fun dragBriefDark() = move("drag", AppStyle.Brief, true)
    @Test fun dragBoard() = move("drag", AppStyle.Board, false)
    @Test fun dragBoardDark() = move("drag", AppStyle.Board, true)
    @Test fun movedGlass() = move("moved", AppStyle.Glass, false)
    @Test fun movedGlassDark() = move("moved", AppStyle.Glass, true)
    @Test fun movedBrief() = move("moved", AppStyle.Brief, false)
    @Test fun movedBriefDark() = move("moved", AppStyle.Brief, true)
    @Test fun movedBoard() = move("moved", AppStyle.Board, false)
    @Test fun movedBoardDark() = move("moved", AppStyle.Board, true)
    @Test fun rescheduleGlass() = move("reschedule", AppStyle.Glass, false)
    @Test fun rescheduleGlassDark() = move("reschedule", AppStyle.Glass, true)
    @Test fun rescheduleBrief() = move("reschedule", AppStyle.Brief, false)
    @Test fun rescheduleBriefDark() = move("reschedule", AppStyle.Brief, true)
    @Test fun rescheduleBoard() = move("reschedule", AppStyle.Board, false)
    @Test fun rescheduleBoardDark() = move("reschedule", AppStyle.Board, true)

    // Day and week (#4200)
    @Test fun dayViewGlass() = axis("dayview", AppStyle.Glass, false)
    @Test fun dayViewGlassDark() = axis("dayview", AppStyle.Glass, true)
    @Test fun dayViewBrief() = axis("dayview", AppStyle.Brief, false)
    @Test fun dayViewBriefDark() = axis("dayview", AppStyle.Brief, true)
    @Test fun dayViewBoard() = axis("dayview", AppStyle.Board, false)
    @Test fun dayViewBoardDark() = axis("dayview", AppStyle.Board, true)
    @Test fun weekGlass() = axis("week", AppStyle.Glass, false)
    @Test fun weekGlassDark() = axis("week", AppStyle.Glass, true)
    @Test fun weekBrief() = axis("week", AppStyle.Brief, false)
    @Test fun weekBriefDark() = axis("week", AppStyle.Brief, true)
    @Test fun weekBoard() = axis("week", AppStyle.Board, false)
    @Test fun weekBoardDark() = axis("week", AppStyle.Board, true)

    /** The largest text: the hours, the blocks' titles and the week's day names grow; the blocks keep their height and cut their titles short. */
    @Config(fontScale = 2f)
    @Test fun dayViewGlassLargeText() = axis("dayview", AppStyle.Glass, false, suffix = "-large")
    @Config(fontScale = 2f)
    @Test fun weekGlassLargeText() = axis("week", AppStyle.Glass, false, suffix = "-large")
    @Test fun weekDragGlass() = axis("weekdrag", AppStyle.Glass, false)
    @Test fun weekDragGlassDark() = axis("weekdrag", AppStyle.Glass, true)
    @Test fun weekDragBrief() = axis("weekdrag", AppStyle.Brief, false)
    @Test fun weekDragBriefDark() = axis("weekdrag", AppStyle.Brief, true)
    @Test fun weekDragBoard() = axis("weekdrag", AppStyle.Board, false)
    @Test fun weekDragBoardDark() = axis("weekdrag", AppStyle.Board, true)
    @Test fun newGlass() = axis("new", AppStyle.Glass, false)
    @Test fun newGlassDark() = axis("new", AppStyle.Glass, true)
    @Test fun newBrief() = axis("new", AppStyle.Brief, false)
    @Test fun newBriefDark() = axis("new", AppStyle.Brief, true)
    @Test fun newBoard() = axis("new", AppStyle.Board, false)
    @Test fun newBoardDark() = axis("new", AppStyle.Board, true)

    // The list with List | Calendar above it
    @Test fun listGlass() = shot("list", AppStyle.Glass, false)
    @Test fun listGlassDark() = shot("list", AppStyle.Glass, true)
}

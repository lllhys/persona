package persona.feature.agents

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsNodeInteraction
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.assertTextEquals
import androidx.compose.ui.test.click
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeLeft
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.Hello
import persona.core.contracts.ServerEvent
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.TodoDue
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.AppFeedback
import persona.core.designsystem.CalendarTokens
import persona.core.designsystem.FeedbackAction
import persona.core.designsystem.LocalAppFeedback
import persona.core.designsystem.PersonaTheme
import persona.core.domain.tasks.CalendarMode
import persona.core.domain.tasks.TasksStore
import java.time.Instant
import java.time.LocalDate
import java.util.TimeZone

/**
 * The calendar's day and week (#4200): Day | Week | Month and the pages they turn; a to-do's block dragged to another
 * quarter hour, another day's column or a day's All Day row, with Undo, nothing sent when let go in place and a failed
 * save put back; TalkBack's "Change Due Date…" on a block; a tap on the empty axis making a to-do due there.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh-w400dp-h860dp-xhdpi")
class TodoCalendarAxisTest {
    @get:Rule val rule = createComposeRule()

    private val zone = TimeZone.getDefault()
    @Before fun inShanghai() = TimeZone.setDefault(TimeZone.getTimeZone("Asia/Shanghai"))
    @After fun back() = TimeZone.setDefault(zone)

    // 17:30 on Thursday, November 5 2026, in the test's zone; weeks start on Monday (Nov 2)
    private val now = Instant.parse("2026-11-05T09:30:00Z")
    // 09:00 on Nov 6; all day on Nov 6
    private val checkup = Todo("t5", title = "体检预约", dueAt = "2026-11-06T01:00:00Z")
    private val review = Todo("t6", title = "评审设计稿", dueDate = "2026-11-06")

    private class Api(var rows: List<Todo>) : TasksApi {
        val sent = mutableListOf<Pair<String, TodoDue>>()
        val created = mutableListOf<Pair<String, TodoDue?>>()
        var failure: Throwable? = null
        override suspend fun todos() = rows
        override suspend fun setTodoDue(id: String, due: TodoDue): Todo {
            sent += id to due
            failure?.let { throw it }
            return rows.first { it.id == id }.withDue(due).also { moved -> rows = rows.map { if (it.id == id) moved else it } }
        }
        override suspend fun createTodo(title: String, notes: String, due: TodoDue?): Todo {
            created += title to due
            failure?.let { throw it }
            return Todo("n${created.size}", title = title).let { if (due != null) it.withDue(due) else it }.also { rows = rows + it }
        }
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

    private class Notes : AppFeedback {
        val failures = mutableListOf<Throwable>()
        val notices = mutableListOf<Pair<String, FeedbackAction?>>()
        override fun failure(error: Throwable, operation: Boolean) = true.also { failures += error }
        override fun notice(message: String) = true.also { notices += message to null }
        override fun notice(message: String, action: FeedbackAction) = true.also { notices += message to action }
    }

    private val notes = Notes()
    private val opened = mutableListOf<String>()
    private lateinit var store: TasksStore

    /** The calendar in [mode], on [day] when given (through the day view's strip). */
    private fun open(api: Api, mode: String, day: String? = null) {
        store = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.reloadTodos()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalAppFeedback provides notes) {
                    val todos by store.todos.collectAsState()
                    Box(Modifier.width(400.dp).height(860.dp)) {
                        TodosScreen(todos, emptyList(), store, onBack = {}, onOpenTodo = { opened += it }, now = now)
                    }
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("日历").performClick()
        rule.waitForIdle()
        if (day != null) {
            mode("日")
            rule.onNodeWithTag("todo-calendar-strip-$day").performClick()
            rule.waitForIdle()
        }
        mode(mode)
    }

    private fun mode(name: String) {
        rule.onNode(hasText(name) and hasAnyAncestor(hasTestTag("todo-calendar-mode"))).performClick()
        rule.waitForIdle()
    }

    private fun title() = rule.onNodeWithTag("todo-calendar-title")
    private fun block(id: String) = rule.onNodeWithTag("todo-calendar-entry-$id")
    private fun spoken(node: SemanticsNodeInteraction) =
        node.fetchSemanticsNode().config.getOrElse(SemanticsProperties.ContentDescription) { emptyList() }.joinToString()

    /** Long-presses [id]'s block or chip at its middle, carries it by [by] in [steps] (or keeps it there) and lets go. */
    private fun drag(id: String, by: Offset?, steps: Int = 4) {
        block(id).performTouchInput {
            down(center)
            move(delayMillis = viewConfiguration.longPressTimeoutMillis + 100)
            if (by != null) for (i in 1..steps) moveTo(center + by * (i / steps.toFloat()), delayMillis = 30)
            up()
        }
        rule.waitForIdle()
    }

    /** From [id]'s middle to the middle of the part tagged [tag]. */
    private fun toward(id: String, tag: String): Offset {
        val from = block(id).fetchSemanticsNode().boundsInRoot
        val to = rule.onNodeWithTag(tag).fetchSemanticsNode().boundsInRoot
        return Offset(to.center.x - from.center.x, to.center.y - from.center.y)
    }

    /** Where the part tagged [tag] starts, scrolled out of sight or not: on the axis, midnight. */
    private fun midnight(tag: String) = rule.onNodeWithTag(tag).fetchSemanticsNode().positionInRoot

    private fun hours(h: Float) = with(rule.density) { (CalendarTokens.hour * h).toPx() }

    @Test fun dayWeekAndMonthTurnTheirOwnPages() {
        open(Api(listOf(checkup, review)), "日")
        title().assertTextEquals("11月5日周四")
        rule.onNodeWithContentDescription("后一天").performClick()
        rule.waitForIdle()
        title().assertTextEquals("11月6日周五")
        // the week's strip: the day shown is picked, a tap shows another
        rule.onNodeWithTag("todo-calendar-strip-2026-11-06").assertIsSelected()
        assertTrue(spoken(rule.onNodeWithTag("todo-calendar-strip-2026-11-06")).contains("2 条待办"))
        rule.onNodeWithTag("todo-calendar-strip-2026-11-08").performClick()
        rule.waitForIdle()
        title().assertTextEquals("11月8日周日")
        // a swipe turns the day
        rule.onNodeWithTag("todo-calendar-axis").performTouchInput { swipeLeft(startX = centerX + 150f, endX = centerX - 150f) }
        rule.waitForIdle()
        title().assertTextEquals("11月9日周一")

        mode("周")
        // Nov 9 – 15, the week from Monday
        rule.onNodeWithTag("todo-calendar-day-header-2026-11-09").assertExists()
        rule.onNodeWithTag("todo-calendar-day-header-2026-11-15").assertExists()
        rule.onNodeWithContentDescription("上一周").performClick()
        rule.waitForIdle()
        rule.onNodeWithTag("todo-calendar-day-header-2026-11-02").assertExists()
        // a day of the week shows it on its own
        rule.onNodeWithTag("todo-calendar-day-header-2026-11-04").performClick()
        rule.waitForIdle()
        title().assertTextEquals("11月4日周三")
        // the choice is kept on this device
        assertEquals(CalendarMode.Day, TodoCalendarPrefs(ApplicationProvider.getApplicationContext()).mode)

        mode("月")
        title().assertTextEquals("2026年11月")
        // the month's day still lists its to-dos, and shows the day from there
        rule.onNodeWithTag("todo-calendar-day-2026-11-06").performClick()
        rule.waitForIdle()
        rule.onNodeWithTag("todo-calendar-show-day").performClick()
        rule.waitForIdle()
        title().assertTextEquals("11月6日周五")
        rule.onNodeWithTag("todo-calendar-entry-t5").assertExists()
        // Today
        rule.onNodeWithText("今天").performClick()
        rule.waitForIdle()
        title().assertTextEquals("11月5日周四")
    }

    @Test fun aBlockSnapsToTheQuarterHourAndUndoPutsItBack() {
        val api = Api(listOf(checkup, review))
        open(api, "日", day = "2026-11-06")
        // down an hour and 20 minutes: its top at 10:20, the nearest quarter 10:15
        drag("t5", Offset(0f, hours(80 / 60f)))
        assertEquals(listOf("t5" to TodoDue.At(Instant.parse("2026-11-06T02:15:00Z"))), api.sent)
        assertTrue(spoken(block("t5")).contains("10:15"))
        val (message, undo) = notes.notices.single()
        assertEquals("已移到 11月6日周五 10:15", message)
        undo!!.run()
        rule.waitForIdle()
        assertEquals("t5" to TodoDue.At(Instant.parse("2026-11-06T01:00:00Z")), api.sent.last())
        assertTrue(spoken(block("t5")).contains("09:00"))
        assertEquals(1, notes.notices.size)
    }

    @Test fun aBlockDroppedOnTheAllDayRowIsDueThatDayAllDay() {
        val api = Api(listOf(checkup, review))
        open(api, "日", day = "2026-11-06")
        drag("t5", toward("t5", "todo-calendar-allday-2026-11-06"))
        assertEquals(listOf("t5" to TodoDue.Day(LocalDate.of(2026, 11, 6))), api.sent)
        assertEquals("已移到 11月6日周五", notes.notices.single().first)
        // and back onto the axis from there: an all-day one dropped at 13:00
        val row = rule.onNodeWithTag("todo-calendar-allday-2026-11-06").fetchSemanticsNode().boundsInRoot
        val from = block("t5").fetchSemanticsNode().boundsInRoot
        // the axis' top is midnight, scrolled away above; a chip is carried by its middle and its top edge lands
        val top = midnight("todo-calendar-axis").y + hours(13f) + (from.center.y - from.top)
        // in one go: passing the axis' top edge on the way would scroll it
        drag("t5", Offset(0f, top - from.center.y), steps = 1)
        assertTrue(row.bottom < top)
        assertEquals("t5" to TodoDue.At(Instant.parse("2026-11-06T05:00:00Z")), api.sent.last())
    }

    @Test fun inPlaceNothingIsSentAndNothingOpens() {
        val api = Api(listOf(checkup, review))
        open(api, "日", day = "2026-11-06")
        drag("t5", null)
        drag("t5", Offset(0f, hours(5 / 60f)))
        drag("t6", null)
        assertTrue(api.sent.isEmpty())
        assertTrue(notes.notices.isEmpty())
        assertTrue(opened.isEmpty())
        // a tap opens it
        block("t5").performClick()
        assertEquals(listOf("t5"), opened)
    }

    @Test fun inTheWeekABlockMovesToAnotherDaysColumn() {
        val api = Api(listOf(checkup, review))
        open(api, "周")
        val from = rule.onNodeWithTag("todo-calendar-day-header-2026-11-06").fetchSemanticsNode().boundsInRoot
        val to = rule.onNodeWithTag("todo-calendar-day-header-2026-11-04").fetchSemanticsNode().boundsInRoot
        drag("t5", Offset(to.center.x - from.center.x, 0f))
        // the same time, two days earlier
        assertEquals(listOf("t5" to TodoDue.At(Instant.parse("2026-11-04T01:00:00Z"))), api.sent)
        // and an all-day one onto another day's All Day row
        drag("t6", toward("t6", "todo-calendar-allday-2026-11-08"))
        assertEquals("t6" to TodoDue.Day(LocalDate.of(2026, 11, 8)), api.sent.last())
    }

    @Test fun aFailedSavePutsItBackAndSaysWhy() {
        val api = Api(listOf(checkup, review)).apply { failure = IllegalStateException("offline") }
        open(api, "日", day = "2026-11-06")
        drag("t5", Offset(0f, hours(2f)))
        assertEquals(1, api.sent.size)
        assertTrue(spoken(block("t5")).contains("09:00"))
        assertEquals("offline", notes.failures.single().message)
        assertTrue(notes.notices.isEmpty())
    }

    @Test fun talkBackChangesABlocksDueDate() {
        val api = Api(listOf(checkup, review))
        open(api, "日", day = "2026-11-06")
        assertEquals("体检预约, 未开始, 09:00", spoken(block("t5")))
        for (id in listOf("t5", "t6")) {
            val actions = block(id).fetchSemanticsNode().config[SemanticsActions.CustomActions]
            assertEquals(listOf("改截止时间…"), actions.map { it.label })
        }
        val actions = block("t5").fetchSemanticsNode().config[SemanticsActions.CustomActions]
        rule.runOnIdle { actions.single().action() }
        rule.waitForIdle()
        rule.onNode(hasText("全天") and hasAnyAncestor(hasTestTag("todo-calendar-reschedule"))).performClick()
        rule.onNodeWithTag("todo-calendar-reschedule-save").performClick()
        rule.waitForIdle()
        assertEquals(listOf("t5" to TodoDue.Day(LocalDate.of(2026, 11, 6))), api.sent)
    }

    private fun typeTitle(text: String) {
        rule.onNode(hasSetTextAction() and hasAnyAncestor(hasTestTag("todo-calendar-new"))).performTextInput(text)
        rule.waitForIdle()
    }

    @Test fun aTapOnTheAxisMakesOneDueAtThatHalfHour() {
        val api = Api(listOf(checkup, review))
        open(api, "日", day = "2026-11-06")
        // 14:40 snaps down to 14:30
        val day = midnight("todo-calendar-axis-2026-11-06")
        rule.onRoot().performTouchInput { click(day + Offset(hours(0.5f), hours(14f + 40 / 60f))) }
        rule.waitForIdle()
        rule.onNodeWithTag("todo-calendar-new").assertExists()
        assertTrue(rule.onAllNodesWithText("14:30").fetchSemanticsNodes().isNotEmpty())
        typeTitle("交周报")
        rule.onNodeWithTag("todo-calendar-new-add").performClick()
        rule.waitForIdle()
        assertEquals(listOf("交周报" to TodoDue.At(Instant.parse("2026-11-06T06:30:00Z"))), api.created)
        rule.onNodeWithTag("todo-calendar-new").assertDoesNotExist()
        block("n1").assertExists()
    }

    @Test fun aTapOnTheAllDayRowMakesOneDueThatDayAndAFailureKeepsTheTitle() {
        val api = Api(listOf(checkup)).apply { failure = IllegalStateException("offline") }
        open(api, "日", day = "2026-11-06")
        rule.onNodeWithTag("todo-calendar-allday-2026-11-06").performClick()
        rule.waitForIdle()
        typeTitle("买花")
        rule.onNodeWithTag("todo-calendar-new-add").performClick()
        rule.waitForIdle()
        assertEquals(listOf("买花" to TodoDue.Day(LocalDate.of(2026, 11, 6))), api.created)
        // still there, with what was typed, and why it failed said
        rule.onNodeWithTag("todo-calendar-new").assertExists()
        assertTrue(rule.onAllNodesWithText("买花").fetchSemanticsNodes().isNotEmpty())
        assertEquals("offline", notes.failures.single().message)
        api.failure = null
        rule.onNodeWithTag("todo-calendar-new-add").performClick()
        rule.waitForIdle()
        rule.onNodeWithTag("todo-calendar-new").assertDoesNotExist()
        assertEquals(2, api.created.size)
    }

    @Test fun thePagesPlusMakesOneDueOnTheDayShownAllDay() {
        val api = Api(listOf(checkup, review))
        open(api, "日", day = "2026-11-08")
        rule.onNodeWithContentDescription("新建待办").performClick()
        rule.waitForIdle()
        typeTitle("整理照片")
        rule.onNodeWithTag("todo-calendar-new-add").performClick()
        rule.waitForIdle()
        assertEquals(listOf("整理照片" to TodoDue.Day(LocalDate.of(2026, 11, 8))), api.created)
        assertTrue(rule.onAllNodes(isRoot()).fetchSemanticsNodes().isNotEmpty())
    }
}

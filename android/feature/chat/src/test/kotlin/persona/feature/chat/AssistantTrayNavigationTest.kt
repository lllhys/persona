package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.*
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
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

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1000dp-xhdpi")
class AssistantTrayNavigationTest {
    @get:Rule val rule = createComposeRule()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    @After fun close() { scope.cancel() }

    private val agent = Session("computer", "agent", title = "Check the release", state = "needs_you", status = "blocked")
    private val permission = Interaction("permission", machineId = agent.machineId, sessionId = agent.sessionId,
        kind = "permission", toolName = "Bash", summary = "Run the release tests")
    private val draft = AssistantInboxDraft("draft", "Reply to the discussion", InboxSuggestion(version = 1, kind = "reply", text = "The tests passed."))
    private val plan = TaskPlan("plan", "main", listOf(PlanTask("Prepare the next release", prompt = "Check the release")), state = "open")

    private inner class Fixture(
        val todos: List<Todo> = emptyList(),
        val sessions: List<Session> = emptyList(),
        val requests: List<Interaction> = emptyList(),
        val drafts: List<AssistantInboxDraft> = emptyList(),
        val plans: List<TaskPlan> = emptyList(),
    ) {
        val reviews = mutableListOf<Interaction>()
        val approvals = mutableListOf<Pair<Interaction, Resolution>>()
        val sends = mutableListOf<TrayDraft>()
        val openedPlans = mutableListOf<TaskPlan>()
        val api = object : TasksApi {
            override suspend fun todos() = todos
            override suspend fun todoDecisions(todoId: String) = emptyList<TodoDecision>()
            override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList(), plans.associateBy { it.id })
            override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("Navigation must not start a plan")
            override suspend fun dismissPlan(id: String): TaskPlan = error("Navigation must not dismiss a plan")
            override suspend fun retryTask(todoId: String): Todo = error("Navigation must not retry a task")
            override suspend fun removeTask(todoId: String): Unit = error("Navigation must not remove a task")
            override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("Navigation must not send a message")
            override suspend fun today(conversation: String): FeedItem = error("Not requested")
            override suspend fun mainConversation() = "main"
            override suspend fun inboxReply(itemId: String, text: String, card: String?): Unit = error("Navigation must not send a draft")
        }
        val connection = object : HubConnection {
            override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }
        val tasks = TasksStore(api, connection, scope).also {
            it.reloadTodos()
            it.loadTodayIfMain("other")
            it.load("main")
        }
        val host = AssistantTaskHost(tasks, "main", sessions, { "Computer" }, {}, { throw it }, {})
        val tray = AssistantTrayState().apply { expanded = AssistantTray.NeedsYou }

        @Composable fun Content(enabled: Boolean = true) {
            Column(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("tray-navigation-shot")) {
                AssistantTrays(host, tray, requests, drafts, emptyList(), 600.dp, busy = false, enabled = enabled,
                    onApprove = { approvals += it }, onReview = { reviews += it }, onSend = { sends += it }, onSchedule = {},
                    onPlan = { openedPlans += it })
                AssistantTaskSheets(host)
            }
        }
    }

    private fun show(fixture: Fixture, enabled: Boolean = true) {
        rule.mainClock.autoAdvance = false
        rule.setContent { PersonaTheme(reduceMotion = true) { fixture.Content(enabled) } }
        settle()
    }

    private fun settle() {
        Snapshot.sendApplyNotifications()
        repeat(5) { rule.mainClock.advanceTimeByFrame() }
        rule.mainClock.advanceTimeBy(300)
        rule.waitForIdle()
    }

    @Test fun pendingTaskTitleOpensItsRealDetailSheet() {
        val todo = Todo("task", "Review the release result", origin = "main", supervision = Supervision(state = "pending_review"))
        val fixture = Fixture(todos = listOf(todo))
        show(fixture)
        rule.onNodeWithText(todo.title).performClick()
        settle()
        rule.runOnIdle { assertEquals(TaskSheet.Task(todo.id), fixture.host.sheet.value) }
        rule.onAllNodesWithText(todo.title).assertCountEquals(2)
        assertTrue(fixture.approvals.isEmpty())
        assertTrue(fixture.sends.isEmpty())
    }

    @Test fun taskTitleReviewsPermissionWhileOnlyItsButtonApproves() {
        val todo = Todo("task", agent.title, origin = "main", agents = listOf(TodoAgent(agent.machineId, agent.sessionId)))
        val fixture = Fixture(todos = listOf(todo), sessions = listOf(agent), requests = listOf(permission))
        show(fixture)
        rule.onNodeWithText(agent.title).performClick()
        rule.runOnIdle {
            assertEquals(listOf(permission), fixture.reviews)
            assertTrue(fixture.approvals.isEmpty())
        }
        rule.onNodeWithTag("assistant-decision-permission").performTouchInput { click() }
        rule.runOnIdle {
            assertEquals(1, fixture.approvals.size)
            assertEquals(permission, fixture.approvals.single().first)
            assertEquals(1, fixture.reviews.size)
        }
    }

    @Test fun conversationsOwnRequestTitleReviewsInsteadOfApproving() {
        val request = permission.copy(sessionId = "main", sessionTitle = "Review command access")
        val fixture = Fixture(sessions = listOf(agent.copy(sessionId = "main")), requests = listOf(request))
        show(fixture)
        rule.onNodeWithText("Review command access").performClick()
        rule.runOnIdle {
            assertEquals(listOf(request), fixture.reviews)
            assertTrue(fixture.approvals.isEmpty())
        }
    }

    @Test fun planTitleOpensReviewWithoutStartingThePlan() {
        val fixture = Fixture(plans = listOf(plan))
        show(fixture)
        rule.onNodeWithText(plan.tasks.single().title).performClick()
        rule.runOnIdle { assertEquals(listOf(plan), fixture.openedPlans) }
    }

    @Test fun draftTitleDoesNotSendButItsButtonStillDoes() {
        val fixture = Fixture(drafts = listOf(draft))
        show(fixture)
        rule.onNodeWithText(draft.title).performTouchInput { click() }
        rule.runOnIdle { assertTrue(fixture.sends.isEmpty()) }
        rule.onNodeWithTag("assistant-draft-draft").performClick()
        rule.runOnIdle { assertEquals(listOf(TrayDraft.Inbox(draft)), fixture.sends) }
    }

    @Test fun disabledTitlesAndButtonsDoNotNavigateOrApprove() {
        val fixture = Fixture(sessions = listOf(agent), requests = listOf(permission), plans = listOf(plan))
        show(fixture, enabled = false)
        rule.onNodeWithText(agent.title).performTouchInput { click() }
        rule.onNodeWithText(plan.tasks.single().title).performTouchInput { click() }
        rule.onNodeWithTag("assistant-decision-permission").assertIsNotEnabled().performTouchInput { click() }
        rule.runOnIdle {
            assertTrue(fixture.reviews.isEmpty())
            assertTrue(fixture.approvals.isEmpty())
            assertTrue(fixture.openedPlans.isEmpty())
        }
    }

    @Test fun navigationRowsRenderInEveryStyleAndAppearance() {
        val todo = Todo("task", "Review the release result", origin = "main", supervision = Supervision(state = "pending_review"))
        val fixture = Fixture(todos = listOf(todo), sessions = listOf(agent), requests = listOf(permission), drafts = listOf(draft))
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.mainClock.autoAdvance = false
        rule.setContent { PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) { fixture.Content() } }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            settle()
            rule.onNodeWithText(todo.title).assertIsDisplayed()
            rule.onNodeWithText(agent.title).assertIsDisplayed()
            rule.onNodeWithText(draft.title).assertIsDisplayed()
            val bitmap = rule.onNodeWithTag("tray-navigation-shot").captureToImage().asAndroidBitmap()
            val output = File("build/assistant-tray-navigation-shots").also { it.mkdirs() }
            File(output, "${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
            }
        }
    }
}

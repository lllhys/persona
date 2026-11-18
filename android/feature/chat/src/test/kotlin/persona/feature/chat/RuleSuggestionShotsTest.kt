package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
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
import persona.core.contracts.AskOption
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.Interaction
import persona.core.contracts.PlanTask
import persona.core.contracts.Question
import persona.core.contracts.QuestionOption
import persona.core.contracts.RuleScope
import persona.core.contracts.RuleSuggestion
import persona.core.contracts.RuleSuggestionMade
import persona.core.contracts.ServerEvent
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.TodoAsk
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubCodedFailure
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.RequestManager
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.domain.tasks.TasksStore
import persona.core.domain.tasks.todoRuleCard

/**
 * The rule a card suggests after the same answer three times (#4929) and the mark of a decision held for the next
 * briefing (#4930), under the three cards that carry them: an open plan, a to-do's decision and a question a supervisor
 * passed up. Every style, light and dark; native JVM captures in `build/reports/tests/rule-suggestion-shots/` (CI uploads the reports folder) (or
 * `-Dpersona.shots=<dir>`), not device screenshots.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h2400dp-xhdpi", application = QuestionTestApplication::class)
class RuleSuggestionShotsTest {
    @get:Rule val rule = createComposeRule()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    @After fun close() = scope.cancel()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/reports/tests/rule-suggestion-shots").also { it.mkdirs() }

    private val where = RuleScope("example-node", "Example node", "/code/example")
    private val start = RuleSuggestion("rs_start", "plan_start",
        "You started tasks like this 3 times. Always start tasks that change files in /code/example on Example node directly?",
        where, RuleSuggestion.TASK_TRUST, count = 3)
    private val merge = RuleSuggestion("rs_merge", "standing_authorization",
        "You approved this 3 times. Always allow merging in /code/example on Example node once checks pass and there are no conflicts?",
        where.copy(action = "merge", conditions = listOf("ci_green", "no_conflicts"), quote = "Merge the PR now? → Yes"),
        RuleSuggestion.STANDING_AUTHORIZATION, count = 3)
    private val delegate = RuleSuggestion("rs_delegate", "delegate",
        "You took the recommendation 3 times. Let Persona answer agents' own-call questions for you?",
        creates = "", setting = "agents.delegate", count = 3)

    private val askedAt = "2026-11-10T08:00:00Z"
    /** The decision's own round: what is answered on it is not the to-do's next decision's. */
    private val todoCard = todoRuleCard("t1", askedAt)
    private fun plan(suggestion: RuleSuggestion? = start) = TaskPlan("p1", "main", listOf(
        PlanTask("Fix the importer", "Fix the importer's date parsing", "example-node", "/code/example", why = "The nightly import failed twice.")),
        "open", suggestion = suggestion)
    private fun decision(suggestion: RuleSuggestion? = merge, forBriefing: Boolean = true) = Todo("t1", "Merge the importer fix", origin = "main",
        manager = "work", ask = TodoAsk("decide", "Needs your decision: merge the importer fix?", "Merge", askedAt,
            question = "Merge the PR now?", options = listOf(AskOption("Merge", "The fix is on main today"), AskOption("Wait", "It stays open until you say so")),
            default = "never", forBriefing = forBriefing, suggestion = suggestion))
    private fun question(suggestion: RuleSuggestion? = delegate) = Interaction("q1", machineId = "example-node", sessionId = "agent1",
        kind = "question", questions = listOf(Question("Rerun the flaky test?", options = listOf(QuestionOption("Rerun"), QuestionOption("Skip")))),
        suggestion = suggestion)

    private class Refused(override val status: Int?) : RuntimeException("refused"), HubCodedFailure {
        override val code: String? = null
    }

    private class Api(var rows: List<Todo>) : TasksApi {
        var status: Int? = null
        /** What Always answers, when not the three kinds' own. */
        var made: RuleSuggestionMade? = null
        val calls = mutableListOf<Triple<String, String, String?>>()
        override suspend fun todos() = rows
        override suspend fun ruleAlways(suggestionId: String): RuleSuggestionMade {
            calls += Triple("always", suggestionId, currentCoroutineContext()[RequestManager]?.id)
            status?.let { throw Refused(it) }
            made?.let { return it }
            // answered: the hub words the decision again without its line
            rows = rows.map { todo -> todo.copy(ask = todo.ask?.takeIf { it.suggestion?.id != suggestionId } ?: todo.ask?.copy(suggestion = null)) }
            return when (suggestionId) {
                "rs_start" -> RuleSuggestionMade("plan_start", RuleSuggestion.TASK_TRUST)
                "rs_merge" -> RuleSuggestionMade("standing_authorization", RuleSuggestion.STANDING_AUTHORIZATION)
                else -> RuleSuggestionMade("delegate", "", "agents.delegate")
            }
        }
        override suspend fun ruleNotNow(suggestionId: String) {
            calls += Triple("not-now", suggestionId, currentCoroutineContext()[RequestManager]?.id)
            status?.let { throw Refused(it) }
        }
        override suspend fun feed(conversation: String, since: String?) = FeedResponse()
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
        override fun send(message: JsonObject) = true
    }

    private val errors = mutableListOf<Throwable>()
    private var style by mutableStateOf(AppStyle.Glass)
    private var dark by mutableStateOf(false)
    private var shownPlan by mutableStateOf(plan())
    private var shownQuestion by mutableStateOf(question())

    /** The three cards as the conversation stacks them, each with its line under it. */
    private fun show(api: Api): TasksStore {
        val store = TasksStore(api, Connection(), scope)
        store.reloadTodos()
        val host = AssistantTaskHost(store, "main", emptyList(), { it }, {}, { errors += it }, {}, manager = "owner")
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                val todos by store.todos.collectAsStateWithLifecycle()
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l),
                    verticalArrangement = Arrangement.spacedBy(Tokens.Space.l)) {
                    PlanCard(shownPlan, host)
                    todos.forEach { TaskDecisionCard(it, host) }
                    InteractionPanel(listOf(shownQuestion), null, 600.dp, null, emptySet(), { _, _ -> }, null, {},
                        suggestion = { card -> RuleSuggestionLine("card:${card.id}", card.suggestion, store, { errors += it }, card.manager) })
                }
            }
        }
        rule.waitForIdle()
        return store
    }

    private fun capture(name: String): Bitmap {
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out(), "$name.png").outputStream().use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        return bitmap
    }

    private fun everyLook(prefix: String, check: () -> Unit) {
        for (s in AppStyle.entries) {
            var light: Bitmap? = null
            for (d in listOf(false, true)) {
                style = s; dark = d
                Snapshot.sendApplyNotifications()
                rule.waitForIdle()
                check()
                val bitmap = capture("$prefix-${s.name.lowercase()}-${if (d) "dark" else "light"}")
                if (!d) light = bitmap.copy(Bitmap.Config.ARGB_8888, false)
                else assertFalse("Light and dark must differ for $s", light!!.sameAs(bitmap))
            }
        }
    }

    @Test fun everyCardSaysWhatWasNoticedWhereItAppliesAndWhatAlwaysCreates() {
        show(Api(listOf(decision())))
        everyLook("rule") {
            for (suggestion in listOf(start, merge, delegate)) rule.onNodeWithText(suggestion.text, useUnmergedTree = true).assertIsDisplayed()
            // the scope of the plan's and the decision's rule; a delegate suggestion has none
            rule.onAllNodesWithText("Applies to Example node · /code/example", useUnmergedTree = true).assertCountEquals(2)
            rule.onNodeWithText("Always remembers this kind of task: Persona starts ones like it directly, without a plan card.",
                useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("Always creates a standing authorization: merge.", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("Only when: checks pass, no conflicts", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("Kept as your words: “Merge the PR now? → Yes”", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("agents.delegate is turned on in the assistant's configuration file on the hub's computer", substring = true, useUnmergedTree = true).assertIsDisplayed()
            rule.onAllNodesWithText("Always", useUnmergedTree = true).assertCountEquals(3)
            rule.onAllNodesWithText("Not now", useUnmergedTree = true).assertCountEquals(3)
            // the decision was held for the next briefing, and its card says so
            rule.onNodeWithTag("decision-for-briefing", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("Comes with the next briefing", useUnmergedTree = true).assertIsDisplayed()
        }
        assertTrue(errors.isEmpty())
    }

    @Test fun alwaysSaysWhatWasMadeAndWhereItIsListedAndStaysWithItsCard() {
        val api = Api(listOf(decision()))
        show(api)
        rule.onNodeWithTag("rule-always-plan:p1").performClick()
        rule.onNodeWithTag("rule-always-$todoCard").performClick()
        rule.onNodeWithTag("rule-always-card:q1").performClick()
        rule.waitForIdle()
        // the plan's goes to its conversation's assistant, the decision's to its to-do's, the card's to its own (none here)
        assertEquals(listOf(Triple("always", "rs_start", "owner"), Triple("always", "rs_merge", "work"),
            Triple("always", "rs_delegate", null as String?)), api.calls)
        // the hub sends the plan and the card again without the line, and read the decision without it already
        shownPlan = plan(suggestion = null); shownQuestion = question(suggestion = null)
        Snapshot.sendApplyNotifications()
        rule.waitForIdle()
        everyLook("rule-made") {
            rule.onAllNodesWithText("Always", useUnmergedTree = true).assertCountEquals(0)
            rule.onAllNodesWithText("Not now", useUnmergedTree = true).assertCountEquals(0)
            rule.onNodeWithTag("rule-made-plan:p1", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("under Remembered auto-start rules in Persona's settings in the web app", substring = true, useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithTag("rule-made-$todoCard", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("in the assistant's profile under Standing Authorizations", substring = true, useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithTag("rule-made-card:q1", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("No rule was stored. To let Persona answer such questions first, turn on agents.delegate in the assistant's configuration file on the hub's computer.",
                useUnmergedTree = true).assertIsDisplayed()
        }
        assertTrue(errors.isEmpty())
    }

    @Test fun notNowAndAStaleSuggestionLeaveNothingAndAnotherRefusalLeavesTheLine() {
        val api = Api(listOf(decision()))
        show(api)
        rule.onNodeWithTag("rule-not-now-plan:p1").performClick()
        rule.waitForIdle()
        rule.onAllNodesWithTag("rule-suggestion-plan:p1", useUnmergedTree = true).assertCountEquals(0)
        rule.onAllNodesWithTag("rule-made-plan:p1", useUnmergedTree = true).assertCountEquals(0)
        // 404: the answers do not earn it any more, or it was answered on another device
        api.status = 404
        rule.onNodeWithTag("rule-always-$todoCard").performClick()
        rule.waitForIdle()
        rule.onAllNodesWithTag("rule-suggestion-$todoCard", useUnmergedTree = true).assertCountEquals(0)
        rule.onAllNodesWithTag("rule-made-$todoCard", useUnmergedTree = true).assertCountEquals(0)
        assertTrue(errors.isEmpty())
        // the decision itself is still there to answer
        rule.onNodeWithTag("assistant-task-decision-t1").assertIsDisplayed()
        // 409: the standing authorizations changed meanwhile; said, and the line stays to answer again
        api.status = 409
        rule.onNodeWithTag("rule-always-card:q1").performClick()
        rule.waitForIdle()
        assertEquals(listOf(409), errors.map { (it as Refused).status })
        rule.onNodeWithTag("rule-suggestion-card:q1", useUnmergedTree = true).assertIsDisplayed()
        capture("rule-answered-glass-light")
    }

    @Test fun whatWasMadeOnOneDecisionIsNotDrawnUnderTheToDosNextOne() {
        val api = Api(listOf(decision()))
        val store = show(api)
        rule.onNodeWithTag("rule-always-$todoCard").performClick()
        rule.waitForIdle()
        rule.onNodeWithTag("rule-made-$todoCard", useUnmergedTree = true).assertIsDisplayed()
        // the to-do raises another decision, which suggests nothing: no standing authorization is claimed under it
        val next = "2026-11-11T09:00:00Z"
        api.rows = listOf(decision(suggestion = null).let { it.copy(ask = it.ask!!.copy(askedAt = next, question = "Deploy to production?")) })
        store.reloadTodos()
        rule.waitForIdle()
        rule.onNodeWithTag("assistant-task-decision-t1").assertIsDisplayed()
        rule.onAllNodesWithText("Standing authorization made.", substring = true, useUnmergedTree = true).assertCountEquals(0)
        rule.onAllNodesWithTag("rule-made-${todoRuleCard("t1", next)}", useUnmergedTree = true).assertCountEquals(0)
        // and one that earns the same rule again, under the same id, is there to answer
        api.rows = listOf(decision().let { it.copy(ask = it.ask!!.copy(askedAt = next)) })
        store.reloadTodos()
        rule.waitForIdle()
        rule.onNodeWithTag("rule-suggestion-${todoRuleCard("t1", next)}", useUnmergedTree = true).assertIsDisplayed()
    }

    @Test fun aRuleThisBuildDoesNotKnowKeepsTheHubsWordsAndIsNeverCalledNothing() {
        val unknown = RuleSuggestion("rs_new", "new_kind", "You did this 3 times. Always do it?", where, creates = "some_new_rule", count = 3)
        // a delegate suggestion that names no setting says only that nothing is stored
        shownPlan = plan(suggestion = unknown); shownQuestion = question(suggestion = delegate.copy(setting = null))
        val api = Api(listOf(decision(suggestion = null, forBriefing = false))).apply { made = RuleSuggestionMade("new_kind", "some_new_rule") }
        show(api)
        rule.onNodeWithText(unknown.text, useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("Applies to Example node · /code/example", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("Always stores no rule and puts this line away.", useUnmergedTree = true).assertIsDisplayed()
        // one line of what Always creates: the delegate's. The unknown rule has none of ours
        rule.onAllNodesWithTag("rule-creates", useUnmergedTree = true).assertCountEquals(1)
        rule.onAllNodesWithText("turned on", substring = true, useUnmergedTree = true).assertCountEquals(0)
        rule.onNodeWithTag("rule-always-plan:p1").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("Rule made.", useUnmergedTree = true).assertIsDisplayed()
        rule.onAllNodesWithText("No rule was stored", substring = true, useUnmergedTree = true).assertCountEquals(0)
        api.made = RuleSuggestionMade("delegate", "")
        rule.onNodeWithTag("rule-always-card:q1").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("No rule was stored.", useUnmergedTree = true).assertIsDisplayed()
        assertTrue(errors.isEmpty())
        capture("rule-unknown-glass-light")
    }

    @Test fun aHubWithoutTheFieldsDrawsTheCardsAsBefore() {
        shownPlan = plan(suggestion = null); shownQuestion = question(suggestion = null)
        show(Api(listOf(decision(suggestion = null, forBriefing = false))))
        rule.onNodeWithTag("assistant-task-decision-t1").assertIsDisplayed()
        rule.onNodeWithText("Fix the importer", useUnmergedTree = true).assertIsDisplayed()
        rule.onAllNodesWithText("Always", useUnmergedTree = true).assertCountEquals(0)
        rule.onAllNodesWithText("Not now", useUnmergedTree = true).assertCountEquals(0)
        rule.onAllNodesWithTag("decision-for-briefing", useUnmergedTree = true).assertCountEquals(0)
        for (card in listOf("plan:p1", todoCard, "card:q1")) {
            rule.onAllNodesWithTag("rule-suggestion-$card", useUnmergedTree = true).assertCountEquals(0)
            rule.onAllNodesWithTag("rule-made-$card", useUnmergedTree = true).assertCountEquals(0)
        }
        capture("rule-none-glass-light")
    }
}

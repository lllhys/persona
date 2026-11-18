package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
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
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.isHeading
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.currentCoroutineContext
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
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
import persona.core.contracts.BriefSetupPage
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.JsonCodec
import persona.core.contracts.ServerEvent
import persona.core.contracts.SessionKey
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.TodoAsk
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.HubCodedFailure
import persona.core.contracts.api.RequestManager
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.domain.tasks.AssistantExtra
import persona.core.domain.tasks.TasksStore

/** The real assistant feed renders a hub brief, with native JVM captures rather than an emulator. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h2800dp-xhdpi")
class BriefCardShotsTest {
    @get:Rule val rule = createComposeRule()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    @After fun close() = scope.cancel()

    private fun brief() = JsonCodec.decodeFromString<FeedItem>("""{"id":"brief1","conversation":"main","kind":"brief","ref":"routine1","body":{
        "title":"Morning briefing","hours":12,
        "waiting":[{"id":"wait1","title":"Approve the release?","from":"Release agent","waited_s":600,"machine_id":"box","session_id":"agent1"}],
        "done":[{"id":"done1","title":"Ship the fix","at":"2026-11-10T08:00:00Z"}],"done_total":1,
        "failed":[{"kind":"routine","id":"failed1","title":"Check the gateway","at":"2026-11-10T07:00:00Z","why":"The test gateway was offline"},
            {"kind":"routine","id":"failed2","title":"Night patrol","at":"2026-11-10T06:00:00Z","skipped":true}],"failed_total":2,
        "accept":[{"id":"accept1","title":"Verify the fix","detail":"Changed the parser","verdict":"done","asked_at":"2026-11-10T08:30:00Z","waited_s":120},
            {"id":"decision1","title":"Choose the rollout","verdict":"decide","outcome":"decide","recommend":"Canary first","asked_at":"2026-11-10T08:31:00Z","waited_s":180},
            {"id":"decision2","title":"Close the experiment","verdict":"decide","outcome":"decide","recommend":"Keep it","asked_at":"2026-11-10T08:32:00Z","waited_s":240}],
        "later":[{"id":"later1","title":"Document the migration?","waited_s":300,"machine_id":"box","session_id":"agent2"}],"later_total":1,
        "progress":[{"id":"progress1","title":"Run the tests","stage":"verify","stage_kind":"code"},
            {"id":"progress2","title":"Run the migration","stage":"verify","stage_kind":"general"},
            {"id":"progress3","title":"Run the future stage","stage":"future-stage","stage_kind":"code"}],"progress_total":3}}""")

    private class TestFailure(override val status: Int?) : RuntimeException("test failure"), HubCodedFailure {
        override val code: String? = null
    }

    private class Api(val item: FeedItem) : TasksApi {
        var acceptStatus: Int? = null
        var laterStatus: Int? = null
        val accepted = mutableListOf<Pair<String, String>>()
        val completed = mutableListOf<String>()
        val handled = mutableListOf<String>()
        val managers = mutableListOf<String?>()
        private suspend fun noteManager() { managers += currentCoroutineContext()[RequestManager]?.id }
        override suspend fun todos() = listOf(
            Todo("accept1", title = "Verify the fix",
                ask = TodoAsk(verdict = "done", askedAt = "2026-11-10T08:30:00Z")),
            Todo("decision1", title = "Choose the rollout",
                ask = TodoAsk(outcome = "decide", askedAt = "2026-11-10T08:31:00Z")),
            Todo("decision2", title = "Close the experiment",
                ask = TodoAsk(outcome = "decide", askedAt = "2026-11-10T08:32:00Z")),
            Todo("done1", title = "Ship the fix"),
            Todo("progress1", title = "Run the tests"),
            Todo("progress2", title = "Run the migration"),
        )
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(listOf(item))
        override suspend fun acceptTask(todoId: String, askedAt: String) {
            noteManager()
            acceptStatus?.let { throw TestFailure(it) }
            accepted += todoId to askedAt
        }
        override suspend fun markLaterHandled(questionId: String) {
            noteManager()
            laterStatus?.let { throw TestFailure(it) }
            handled += questionId
        }
        override suspend fun patchTodo(id: String, title: String?, notes: String?, done: Boolean?): Todo {
            noteManager()
            if (done == true) completed += id
            return Todo(id, title = "Choose the rollout", doneAt = if (done == true) "2026-11-10T09:00:00Z" else null)
        }
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

    private fun host(api: Api, onOpen: (SessionKey) -> Unit = {}): AssistantTaskHost {
        val store = TasksStore(api, Connection(), scope)
        store.reloadTodos()
        store.load("main")
        return AssistantTaskHost(store, "main", emptyList(), { it }, onOpen, { throw it }, {}, manager = "brief-owner")
    }

    @Test fun theRealFeedShowsEveryBriefSectionInEveryStyle() {
        val host = host(Api(brief()))
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                val feeds by host.tasks.feeds.collectAsStateWithLifecycle()
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    feeds["main"].orEmpty().forEach { AssistantExtraLine(AssistantExtra.Feed(it), host) }
                }
            }
        }
        val light = mutableMapOf<AppStyle, Bitmap>()
        val output = File("build/brief-card-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            Snapshot.sendApplyNotifications()
            rule.waitForIdle()
            rule.onNodeWithText("Morning briefing").assertIsDisplayed()
            rule.onNodeWithText("Approve the release?").assertIsDisplayed()
            rule.onNodeWithText("Ship the fix").assertIsDisplayed()
            rule.onNodeWithText("Check the gateway").assertIsDisplayed()
            rule.onNodeWithText("Run failed", substring = true).assertIsDisplayed()
            rule.onNodeWithText("Skipped", substring = true).assertIsDisplayed()
            rule.onNodeWithText("Night patrol").assertIsDisplayed()
            rule.onNodeWithText("Verify the fix").assertIsDisplayed()
            rule.onNodeWithText("Document the migration?").assertIsDisplayed()
            rule.onNodeWithText("Run the tests").assertIsDisplayed()
            rule.onNodeWithContentDescription("Verified, In progress", substring = true).assertIsDisplayed()
            rule.onNodeWithText("Run the migration").assertIsDisplayed()
            rule.onNodeWithText("verify").assertDoesNotExist()
            rule.onNodeWithText("Run the future stage").assertIsDisplayed()
            rule.onNodeWithText("future-stage").assertIsDisplayed()
            rule.onAllNodes(isHeading()).assertCountEquals(5)
            // every other briefing, and an older hub: no recommended setup
            rule.onAllNodesWithTag("brief-setup").assertCountEquals(0)
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            if (!d) light[s] = bitmap.copy(Bitmap.Config.ARGB_8888, false)
            else assertFalse("Light and dark must differ for $s", light.getValue(s).sameAs(bitmap))
            File(output, "brief-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    /** A week's first briefing: three mechanisms this app has a page for, one a newer hub names. */
    private fun setupBrief(): FeedItem {
        val body = (brief().body as JsonObject) + ("setup" to JsonCodec.parseToJsonElement("""[
            {"id":"triage_decide","setting":"agents.triage.decide","count":4,"text":"Card for decisions would have asked you about 4 stops instead of leaving them idle. It costs one triage call per stop. Turn it on in Session triage."},
            {"id":"standing_authorizations","count":3,"text":"A standing authorization would have answered 3 merge approvals. Add one in Persona's settings in the web app."},
            {"id":"supervisor_tier","setting":"assistant.supervise","count":12,"text":"The background supervisor's 12 reviews of the last 30 days held on its model; its provider has a lower tier."},
            {"id":"future_mechanism","count":1,"text":"Something a newer hub recommends."}]"""))
        return brief().copy(body = JsonObject(body))
    }

    /**
     * The first briefing of a week (#5085): the hub's recommended-setup lines as they came, each with Open settings only
     * when its mechanism has a page in this app (session triage, standing authorizations, the supervisor's model);
     * opening one calls nothing on the hub.
     */
    @Test fun theWeeksRecommendedSetupOpensOnlyThePagesThisAppHas() {
        val api = Api(setupBrief())
        val opened = mutableListOf<BriefSetupPage>()
        val store = TasksStore(api, Connection(), scope)
        store.reloadTodos()
        store.load("main")
        val host = AssistantTaskHost(store, "main", emptyList(), { it }, {}, { throw it }, {}, manager = "brief-owner", onOpenSetup = opened::add)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                val feeds by host.tasks.feeds.collectAsStateWithLifecycle()
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    feeds["main"].orEmpty().forEach { AssistantExtraLine(AssistantExtra.Feed(it), host) }
                }
            }
        }
        // under the reports folder, which CI uploads: the only pictures of this section from a machine without an emulator
        val output = File("build/reports/tests/brief-setup-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            Snapshot.sendApplyNotifications()
            rule.waitForIdle()
            rule.onNodeWithText("Recommended setup").assertIsDisplayed()
            rule.onNodeWithText("Nothing was changed", substring = true).assertIsDisplayed()
            rule.onAllNodes(isHeading()).assertCountEquals(6)
            for (id in listOf("triage_decide", "standing_authorizations", "supervisor_tier", "future_mechanism")) {
                rule.onNodeWithTag("brief-setup-$id").assertIsDisplayed()
            }
            rule.onNodeWithText("Something a newer hub recommends.").assertIsDisplayed()
            rule.onAllNodesWithText("Open settings").assertCountEquals(3)
            setupButtonsSitUnderTheirLines()
            rule.onAllNodesWithTag("brief-setup-open-future_mechanism").assertCountEquals(0)
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(output, "brief-setup-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        rule.onNodeWithTag("brief-setup-open-triage_decide").performClick()
        rule.onNodeWithTag("brief-setup-open-standing_authorizations").performClick()
        rule.onNodeWithTag("brief-setup-open-supervisor_tier").performClick()
        assertEquals(listOf(BriefSetupPage.SessionTriage, BriefSetupPage.StandingAuthorizations, BriefSetupPage.SupervisorModel), opened)
        // read-only: nothing was accepted, completed, handled or otherwise sent
        assertTrue(api.accepted.isEmpty() && api.completed.isEmpty() && api.handled.isEmpty() && api.managers.isEmpty())
    }

    /** A setup line keeps the card's width with its button under it, as the card's other rows: never a narrow column beside it. */
    private fun setupButtonsSitUnderTheirLines() {
        for (id in listOf("triage_decide", "standing_authorizations", "supervisor_tier")) {
            val text = rule.onNodeWithTag("brief-setup-$id").getUnclippedBoundsInRoot()
            val button = rule.onNodeWithTag("brief-setup-open-$id").assertIsDisplayed().getUnclippedBoundsInRoot()
            assertTrue("Open settings must sit under the line of $id", button.top >= text.bottom)
            assertTrue("Open settings must not take width from the line of $id", button.left < text.right)
        }
    }

    @Test
    @Config(sdk = [35], qualifiers = "en-w400dp-h4800dp-xhdpi", fontScale = 2f)
    fun largestTextKeepsASetupLineAtTheCardsWidthInEveryStyle() {
        val store = TasksStore(Api(setupBrief()), Connection(), scope)
        store.reloadTodos()
        store.load("main")
        val host = AssistantTaskHost(store, "main", emptyList(), { it }, {}, { throw it }, {}, manager = "brief-owner", onOpenSetup = {})
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                val feeds by host.tasks.feeds.collectAsStateWithLifecycle()
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    feeds["main"].orEmpty().forEach { AssistantExtraLine(AssistantExtra.Feed(it), host) }
                }
            }
        }
        // under the reports folder, which CI uploads: the only pictures of this section from a machine without an emulator
        val output = File("build/reports/tests/brief-setup-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            Snapshot.sendApplyNotifications()
            rule.waitForIdle()
            setupButtonsSitUnderTheirLines()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(output, "brief-setup-large-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    @Test
    @Config(sdk = [35], qualifiers = "en-w400dp-h2400dp-xhdpi", fontScale = 2f)
    fun largestTextKeepsPrimaryActionsFirstInEveryStyle() {
        val host = host(Api(brief()))
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                val feeds by host.tasks.feeds.collectAsStateWithLifecycle()
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    feeds["main"].orEmpty().forEach { AssistantExtraLine(AssistantExtra.Feed(it), host) }
                }
            }
        }
        val output = File("build/brief-card-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            Snapshot.sendApplyNotifications()
            rule.waitForIdle()
            val primary = rule.onNodeWithTag("brief-take-accept1").assertIsDisplayed().getUnclippedBoundsInRoot()
            val open = rule.onNodeWithTag("brief-open-accept1").assertIsDisplayed().getUnclippedBoundsInRoot()
            assertTrue("The primary action must precede Open in $s dark=$d",
                primary.top < open.top || primary.top == open.top && primary.left < open.left)
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(output, "brief-large-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    @Test fun briefActionsUseTheDisplayedRound() {
        val api = Api(brief())
        val host = host(api)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                val feeds by host.tasks.feeds.collectAsStateWithLifecycle()
                feeds["main"].orEmpty().forEach { AssistantExtraLine(AssistantExtra.Feed(it), host) }
            }
        }
        rule.onNodeWithTag("brief-take-accept1").performClick()
        rule.onNodeWithTag("brief-take-decision1").performClick()
        rule.onNodeWithTag("brief-finish-decision2").performClick()
        rule.onNodeWithTag("brief-handled-later1").performClick()
        rule.waitForIdle()
        assertEquals(listOf("accept1" to "2026-11-10T08:30:00Z", "decision1" to "2026-11-10T08:31:00Z"), api.accepted)
        assertEquals(listOf("decision2"), api.completed)
        assertEquals(listOf("later1"), api.handled)
        assertEquals(List(4) { "brief-owner" }, api.managers)
    }

    @Test fun briefRowsOpenTheirOwnChatOrTask() {
        val opened = mutableListOf<SessionKey>()
        val host = host(Api(brief()), onOpen = opened::add)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                val feeds by host.tasks.feeds.collectAsStateWithLifecycle()
                feeds["main"].orEmpty().forEach { AssistantExtraLine(AssistantExtra.Feed(it), host) }
            }
        }
        rule.onNodeWithTag("brief-open-wait1").performClick()
        rule.onNodeWithTag("brief-open-later1").performClick()
        rule.onNodeWithTag("brief-open-accept1").performClick()
        assertEquals(listOf(SessionKey("box", "agent1"), SessionKey("box", "agent2")), opened)
        assertEquals(TaskSheet.Task("accept1"), host.sheet.value)
    }

    @Test fun briefOutcomesAreSharedAndTerminal() {
        val api = Api(brief()).apply { acceptStatus = 409; laterStatus = 404 }
        val host = host(api)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                val feeds by host.tasks.feeds.collectAsStateWithLifecycle()
                feeds["main"].orEmpty().forEach { item ->
                    repeat(2) { AssistantExtraLine(AssistantExtra.Feed(item), host) }
                }
            }
        }
        rule.onAllNodesWithTag("brief-take-accept1").assertCountEquals(2).onFirst().performClick()
        rule.onAllNodesWithText("Already handled").assertCountEquals(2)
        rule.onAllNodesWithTag("brief-take-accept1").assertCountEquals(0)
        rule.onAllNodesWithTag("brief-handled-later1").assertCountEquals(2).onFirst().performClick()
        rule.onAllNodesWithText("The hub is too old for this button").assertCountEquals(2)
        rule.onAllNodesWithTag("brief-handled-later1").assertCountEquals(0)
        assertEquals(emptyList<Pair<String, String>>(), api.accepted)
        assertEquals(emptyList<String>(), api.handled)
    }
}

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
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertTextEquals
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.JsonObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.JsonCodec
import persona.core.contracts.ServerEvent
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.domain.tasks.AssistantExtra
import persona.core.domain.tasks.TasksStore

/** Actual assistant feed rendering, with native JVM images; not emulator or device screenshots. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
class ReportCardShotsTest {
    @get:Rule val rule = createComposeRule()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    @After fun close() = scope.cancel()

    private val body = "Verified the fictional change.\nSee https://example.com/results and /tmp/example-check.log.\nThe original report stays intact."
    private fun report(evidence: String = """["https://example.com/metadata-only","/tmp/example-evidence.png","https://example.com/results"]""") =
        JsonCodec.decodeFromString<FeedItem>("""{"id":"report1","conversation":"main","kind":"report","ref":"task1","body":{
            "kind":"progress","text":${JsonCodec.encodeToString(String.serializer(), body)},"evidence":$evidence}}""")

    private class Api(var item: FeedItem) : TasksApi {
        override suspend fun todos() = listOf(Todo("task1", title = "Fictional verification"))
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(listOf(item))
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

    private fun host(api: Api): AssistantTaskHost {
        val store = TasksStore(api, Connection(), scope)
        store.reloadTodos()
        store.load("main")
        return AssistantTaskHost(store, "main", emptyList(), { it }, {}, { throw it }, {})
    }

    private fun assertOriginalBodyWithoutEvidence() {
        rule.onNodeWithTag("report-card").assertIsDisplayed()
        val node = rule.onNodeWithTag("report-body").assertTextEquals(body).fetchSemanticsNode()
        val annotated = node.config[SemanticsProperties.Text].single()
        assertEquals(body, annotated.text)
        assertEquals(listOf("https://example.com/results"), annotated.getLinkAnnotations(0, annotated.length)
            .mapNotNull { (it.item as? LinkAnnotation.Url)?.url })
        rule.onNodeWithText("https://example.com/metadata-only", substring = true).assertDoesNotExist()
        rule.onNodeWithText("/tmp/example-evidence.png", substring = true).assertDoesNotExist()
    }

    @Test fun theRealFeedShowsTheFullBodyAndInlineLinkInEveryStyle() {
        val host = host(Api(report()))
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
        val output = File("build/report-card-shots").also { it.mkdirs() }
        val light = mutableMapOf<AppStyle, Bitmap>()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            Snapshot.sendApplyNotifications()
            rule.waitForIdle()
            assertOriginalBodyWithoutEvidence()
            rule.onNodeWithText("Fictional verification").assertIsDisplayed()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            if (!d) light[s] = bitmap.copy(Bitmap.Config.ARGB_8888, false)
            else assertFalse("Light and dark must differ for $s", light.getValue(s).sameAs(bitmap))
            File(output, "report-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        rule.onNodeWithText("Fictional verification").performClick()
        rule.runOnIdle { assertEquals(TaskSheet.Task("task1"), host.sheet.value) }
    }

    @Test fun evidenceShapeDoesNotChangeTheBodyAndMissingTasksUseTheExistingTitle() {
        val api = Api(report().copy(ref = "missing-task"))
        val host = host(api)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                val feeds by host.tasks.feeds.collectAsStateWithLifecycle()
                feeds["main"].orEmpty().forEach { AssistantExtraLine(AssistantExtra.Feed(it), host) }
            }
        }
        for (evidence in listOf("[]", "null", """{"unexpected":"https://example.com/metadata-only"}""")) {
            rule.runOnIdle {
                api.item = report(evidence).copy(ref = "missing-task")
                host.tasks.load("main", whole = true)
            }
            rule.waitForIdle()
            assertOriginalBodyWithoutEvidence()
            rule.onNodeWithText("Task").assertIsDisplayed()
        }
    }
}

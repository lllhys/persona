package persona.feature.chat

import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.assertHasClickAction
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.Interaction
import persona.core.contracts.Item
import persona.core.contracts.Machine
import persona.core.contracts.Resolution
import persona.core.contracts.ServerEvent
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.api.ChatRepository
import persona.core.contracts.api.ChatSnapshot
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.api.SessionsRepository
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.domain.tasks.TasksStore
import java.time.Instant

/**
 * When the pill while the chat's model operates a Mac shows (#4339 C9 and C18, iOS `ComputerActivityPill`): on Glass,
 * over an Agent chat's composer and under the entries in Persona's conversation, while the call runs; not for a call that
 * ended a while ago, and not in Brief or Board. A tap opens the work line that holds the call.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp")
class ChatComputerPillTest {
    @get:Rule val rule = createComposeRule()

    private val mac = Machine("macbook", "MacBook", online = true)
    private val hub = Machine("hub", "Hub", online = true)
    private val agent = SessionKey(mac.id, "calc")
    private val persona = SessionKey(hub.id, "main")
    private val opening = "Opening Calculator and typing it in."

    private fun steps() = buildJsonArray {
        listOf("All Clear", "6", "Multiply", "7", "Equals").forEach { name ->
            add(buildJsonObject { put("click", buildJsonObject { put("name", name) }) })
        }
    }

    private fun use(seq: Int, name: String, input: JsonObject, at: Instant? = null) = Item("u$seq", seq, ts = at?.toString(),
        role = "assistant", kind = "tool_use", toolUseId = "t$seq", toolName = name, toolInput = input)

    private fun result(seq: Int, of: Int, at: Instant? = null) =
        Item("r$seq", seq, ts = at?.toString(), role = "user", kind = "tool_result", toolUseId = "t$of", text = "ok")

    private fun said(seq: Int, role: String, text: String) = Item("m$seq", seq, role = role, kind = "text", text = text)

    private fun agentItems(ended: Instant? = null) = listOfNotNull(
        said(1, "user", "Work out 6 × 7 on the Mac's Calculator."), said(2, "assistant", opening),
        use(3, "Bash", buildJsonObject { put("command", "open -a Calculator") }), result(4, 3),
        use(5, "mcp__cua-driver__run_steps", buildJsonObject { put("steps", steps()) }),
        ended?.let { result(6, 5, it) },
    )

    private fun chats(items: List<Item>) = object : ChatRepository {
        override fun observe(session: SessionKey) = MutableStateFlow(ChatSnapshot(items = items, loading = false))
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int): List<Item> = emptyList()
    }

    private fun sessions(vararg rows: Session) = object : SessionsRepository {
        override val machines = MutableStateFlow(listOf(hub, mac))
        override val sessions = MutableStateFlow(rows.toList())
        override suspend fun refresh() = Unit
    }

    private val sender = object : MessageSender {
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
    }
    private val interactions = object : InteractionCenter {
        override val pending = MutableStateFlow(emptyList<Interaction>())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }

    private fun agentChat(style: AppStyle, items: List<Item>) {
        val row = Session(machineId = agent.machineId, sessionId = agent.sessionId, title = "Work out 6 × 7", status = "working",
            state = "working", agent = "claude", paneId = "p1", cwd = "/Users/me/calc")
        rule.setContent {
            PersonaTheme(reduceMotion = true, style = style) {
                ChatScreen(agent, chats(items), sender, interactions, sessions(row), onBack = {})
            }
        }
        rule.waitUntil(20_000) { rule.onAllNodesWithText(opening, substring = true).fetchSemanticsNodes().isNotEmpty() }
        rule.waitForIdle()
    }

    private fun pills() = rule.onAllNodesWithTag("computer-activity").fetchSemanticsNodes()

    @Test fun theWorkLineHoldingTheCallIsWhereItGoes() {
        val lines = ChatTimeline().lines(agentItems(), live = true, open = emptySet()).lines
        val line = computerWorkLine(lines, "t5")
        assertTrue(line != null && line.steps.any { it is persona.core.domain.lines.WorkItem.Tool && it.call.use.item.toolName == "Bash" })
        assertNull(computerWorkLine(lines, "t9"))
    }

    @Test fun anAgentOperatingAMacOnGlassShowsItAndATapOpensItsWorkLine() {
        agentChat(AppStyle.Glass, agentItems())
        val pill = rule.onNodeWithTag("computer-activity")
        val node = pill.fetchSemanticsNode()
        assertEquals(listOf("Controlling MacBook, Click AC → 6 → × → 7 → ="), node.config.getOrNull(SemanticsProperties.ContentDescription))
        assertEquals("Show the steps", node.config.getOrNull(SemanticsActions.OnClick)?.label)
        pill.assertHasClickAction()
        assertTrue("the work line is folded", rule.onAllNodesWithText("open -a Calculator", substring = true).fetchSemanticsNodes().isEmpty())
        pill.performClick()
        // Expanding the line projects its contents on Dispatchers.Default, outside Compose's idle tracking.
        rule.waitUntil(20_000) { rule.onAllNodesWithText("open -a Calculator", substring = true).fetchSemanticsNodes().isNotEmpty() }
        assertTrue("the tap opened it", rule.onAllNodesWithText("open -a Calculator", substring = true).fetchSemanticsNodes().isNotEmpty())
    }

    @Test fun aCallThatEndedAWhileAgoShowsNothing() {
        agentChat(AppStyle.Glass, agentItems(ended = Instant.now().minusSeconds(3600)))
        assertTrue(pills().isEmpty())
    }

    @Test fun aCallThatJustEndedStaysForItsMoment() {
        agentChat(AppStyle.Glass, agentItems(ended = Instant.now()))
        assertEquals(listOf("Just controlled MacBook, Click AC → 6 → × → 7 → ="),
            rule.onNodeWithTag("computer-activity").fetchSemanticsNode().config.getOrNull(SemanticsProperties.ContentDescription))
    }

    @Test fun briefAndBoardDrawNoPill() {
        agentChat(AppStyle.Brief, agentItems())
        assertTrue(pills().isEmpty())
    }

    @Test fun boardDrawsNoPill() {
        agentChat(AppStyle.Board, agentItems())
        assertTrue(pills().isEmpty())
    }

    @Test fun personasConversationShowsItUnderTheEntries() {
        val api = object : TasksApi {
            override suspend fun todos() = emptyList<Todo>()
            override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList())
            override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("not used")
            override suspend fun dismissPlan(id: String): TaskPlan = error("not used")
            override suspend fun retryTask(todoId: String): Todo = error("not used")
            override suspend fun removeTask(todoId: String) = Unit
            override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("not used")
            override suspend fun today(conversation: String): FeedItem = error("not used")
            override suspend fun mainConversation() = "main"
            override suspend fun inboxReply(itemId: String, text: String, card: String?) = Unit
        }
        val connection = object : HubConnection {
            override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }
        val store = TasksStore(api, connection, CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)).also { it.reloadTodos() }
        val items = listOf(
            said(1, "user", "Work out 6 × 7 on the MacBook."), said(2, "assistant", opening),
            use(3, "mcp__persona__computer_run", buildJsonObject { put("machine", "MacBook"); put("steps", steps()) }),
        )
        val row = Session(persona.machineId, persona.sessionId, title = "Persona", assistant = true, status = "working", state = "working")
        rule.setContent {
            PersonaTheme(reduceMotion = true, style = AppStyle.Glass) {
                ChatScreen(persona, chats(items), sender, interactions, sessions(row), onBack = {},
                    chrome = ChatChromeBindings(tasks = store), renderStyle = ChatRenderStyle.Assistant, showTopBar = false)
            }
        }
        rule.waitUntil(20_000) { rule.onAllNodesWithText(opening, substring = true).fetchSemanticsNodes().isNotEmpty() }
        rule.waitForIdle()
        val pill = rule.onNodeWithTag("computer-activity").fetchSemanticsNode()
        assertEquals(listOf("Controlling MacBook, Click AC → 6 → × → 7 → ="), pill.config.getOrNull(SemanticsProperties.ContentDescription))
        // A line of its own under the entries, above the composer.
        val entries = rule.onNodeWithTag("assistant-tray-bar").fetchSemanticsNode().boundsInRoot
        assertTrue(pill.boundsInRoot.top >= entries.bottom)
        assertEquals(entries.left, pill.boundsInRoot.left, 0.5f)
    }
}

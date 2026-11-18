package persona.feature.chat

import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
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
import persona.core.contracts.TodoAgent
import persona.core.contracts.api.ChatRepository
import persona.core.contracts.api.ChatSnapshot
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.api.SessionDirectory
import persona.core.contracts.api.SessionFileEntry
import persona.core.contracts.api.SessionFiles
import persona.core.contracts.api.SessionsRepository
import persona.core.contracts.api.TasksApi
import persona.core.domain.tasks.TasksStore

/**
 * An Agent's chat and Persona's conversation on the real page, over a fake computer that lists their folders: what the
 * @ list's folder entries and the + menu's Mention look like and do (#4339 C16). Written against APIs `main` has too, so
 * the same shots draw its before.
 */
internal class MentionChatHarness(scope: CoroutineScope) {
    val cwd = "/Users/me/code/weather-app"
    val personaCwd = "/Users/me/Documents/assistant"
    val agent = SessionKey("macbook", "forecast")
    val persona = SessionKey("macbook", "main")

    private val tree = mapOf(
        cwd to listOf(SessionFileEntry("app", dir = true), SessionFileEntry("docs", dir = true), SessionFileEntry("scripts", dir = true),
            SessionFileEntry(".github", dir = true), SessionFileEntry("README.md"), SessionFileEntry("build.gradle.kts"), SessionFileEntry(".gitignore")),
        "$cwd/app" to listOf(SessionFileEntry("src", dir = true), SessionFileEntry("build.gradle.kts"), SessionFileEntry("proguard-rules.pro")),
        personaCwd to listOf(SessionFileEntry("Notes", dir = true), SessionFileEntry("Reports", dir = true), SessionFileEntry("todo.md")),
    )

    /** The folders the list asked this computer for, in order. */
    val asked = mutableListOf<String>()
    val computer = object : SessionFiles {
        override suspend fun directory(machineId: String, cwd: String, path: String): SessionDirectory {
            asked += path
            return SessionDirectory(path, tree[path].orEmpty())
        }
        override suspend fun file(machineId: String, cwd: String, path: String) = throw UnsupportedOperationException()
        override suspend fun status(machineId: String, cwd: String) = throw UnsupportedOperationException()
        override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean) = throw UnsupportedOperationException()
        override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int) = throw UnsupportedOperationException()
        override suspend fun show(machineId: String, cwd: String, hash: String) = throw UnsupportedOperationException()
    }

    private val agentSession = Session("macbook", "forecast", title = "Fix the forecast screen", cwd = cwd, agent = "claude", paneId = "p1",
        status = "idle", state = "idle", updatedAt = "2026-11-12T09:50:00Z")
    private val web = Session("macbook", "web", title = "Update the web interface", cwd = "/Users/me/code/web", state = "working", status = "working",
        updatedAt = "2026-11-12T09:30:00Z")
    private val photos = Session("studio", "photos", title = "Photo sync", cwd = "/Users/me/code/photos", agent = "codex", state = "idle", status = "idle",
        updatedAt = "2026-11-12T09:00:00Z")
    private val site = Session("macbook", "site", title = "Release notes for 2.4", cwd = "/Users/me/code/site", state = "idle", status = "idle",
        updatedAt = "2026-11-11T17:20:00Z")

    val sessionRows = MutableStateFlow(listOf(agentSession, web, photos, site,
        Session("macbook", "main", title = "Persona", cwd = personaCwd, assistant = true, status = "done", state = "idle")))
    val sessions = object : SessionsRepository {
        override val sessions = sessionRows
        override val machines = MutableStateFlow(listOf(Machine("macbook", "MacBook", online = true), Machine("studio", "Studio", online = true)))
        override suspend fun refresh() = Unit
    }

    private fun read(n: Int, path: String) = listOf(
        Item("r$n", n * 2, role = "assistant", kind = "tool_use", toolUseId = "r$n", toolName = "Read", ts = "2026-11-12T09:4${n}:00Z",
            toolInput = buildJsonObject { put("file_path", path) }),
        Item("r$n-out", n * 2 + 1, role = "user", kind = "tool_result", toolUseId = "r$n", text = "ok", ts = "2026-11-12T09:4${n}:01Z"),
    )

    val chats = object : ChatRepository {
        override fun observe(session: SessionKey) = MutableStateFlow(ChatSnapshot(loading = false, items = buildList {
            add(Item("u1", 1, role = "user", kind = "text", text = "The forecast screen shows yesterday's numbers.", ts = "2026-11-12T09:40:00Z"))
            if (session == agent) { addAll(read(1, "$cwd/app/src/main/Forecast.kt")); addAll(read(2, "/tmp/forecast.log")) }
            add(Item("a9", 20, role = "assistant", kind = "text", ts = "2026-11-12T09:49:00Z",
                text = if (session == agent) "Found it: the cache is keyed by city, not by day. I'm fixing that now." else "The web update is checking its routes."))
        }))
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
    }
    val sent = mutableListOf<Pair<SessionKey, String>>()
    val sender = object : MessageSender {
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
            sent += session to text
        }
    }
    val interactions = object : InteractionCenter {
        override val pending = MutableStateFlow(emptyList<Interaction>())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }

    private val api = object : TasksApi {
        override suspend fun todos() = listOf(
            Todo("task-web", web.title, origin = "main", createdAt = "2026-11-12T09:00:00Z", agents = listOf(TodoAgent(web.machineId, web.sessionId, cwd = web.cwd))),
            Todo("task-photos", "Sync HEIC photos", origin = "main", createdAt = "2026-11-12T09:10:00Z"),
        )
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
    private val connection = object : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }
    val tasks = TasksStore(api, connection, scope).also { it.reloadTodos() }

    /** [key]'s page (the agent's, or Persona's conversation with its tasks) with the composer holding [composer]'s draft. */
    @Composable
    fun Page(key: SessionKey, composer: ChatComposerState) {
        val rows by sessions.sessions.collectAsState()
        val assistant = rows.firstOrNull { it.key == key }?.assistant == true
        if (assistant) ChatScreen(key, chats, sender, interactions, sessions, {},
            chrome = ChatChromeBindings(tasks = tasks, onOpenSession = {}, files = computer),
            renderStyle = ChatRenderStyle.Assistant, composerState = composer, embedded = true, showTopBar = false)
        else ChatScreen(key, chats, sender, interactions, sessions, {}, chrome = ChatChromeBindings(onOpenSession = {}, files = computer),
            composerState = composer)
    }
}

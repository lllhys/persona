package persona.feature.chat

import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalResources
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.*
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.LocalHubFeatures
import persona.core.domain.lines.ChildSessionCards
import persona.core.domain.lines.ChildSessionDetail
import persona.core.domain.lines.ChildSessionDetailReader
import persona.core.domain.lines.LineItem
import persona.core.domain.lines.SubagentStatus
import persona.core.domain.lines.ToolCall
import persona.core.domain.sort.childSessions
import persona.core.domain.sort.SideFold
import persona.core.domain.sort.SideRollup
import persona.core.domain.sort.isFoldedSide
import persona.core.domain.sort.sideChildren
import persona.core.domain.sort.sideHome
import persona.core.domain.sort.sideRollup
import persona.core.domain.sort.sideSessions
import java.time.Duration

internal data class ChatChildren(val rows: Map<String, Session>, val repository: ChatRepository,
    /** The side sessions folded under this chat's session (#7944), in their panel's order; empty in a side's own chat. */
    val sides: List<Session> = emptyList(), val sideRollup: SideRollup = SideRollup())
internal val LocalChatChildren = staticCompositionLocalOf<ChatChildren?> { null }

data class ChildSessionHome(val key: SessionKey, val childSessionId: String? = null,
    val rows: List<Session> = emptyList())

/** [side]: the route opens a side session as itself (`ChatRoute.side`, #7944), not on its parent's page. */
internal fun childSessionHome(key: SessionKey, row: Session?, parentRow: Session? = null, side: Boolean = false): ChildSessionHome {
    if (side && row?.key == key && row.isFoldedSide) return ChildSessionHome(key)
    val parent = row?.takeIf { it.key == key }?.foldedUnder?.takeIf { it.isNotBlank() && it != key.sessionId }
        ?.let { SessionKey(key.machineId, it) }?.takeIf { it == parentRow?.key }
    return if (parent == null) ChildSessionHome(key) else ChildSessionHome(parent, key.sessionId)
}

/** Exact lookups are page metadata, never additions to the root list's membership. */
internal suspend fun resolveChildSessionHome(key: SessionKey, rows: List<Session>, connected: Boolean = true, side: Boolean = false,
    lookup: suspend (SessionKey) -> Session?): ChildSessionHome {
    suspend fun row(target: SessionKey): Session? = rows.firstOrNull { it.key == target } ?: if (!connected) null else try {
        lookup(target)?.takeIf { it.key == target }
    } catch (cancelled: CancellationException) { throw cancelled }
    catch (_: Exception) { null }
    val child = row(key)
    val parentKey = child?.foldedUnder?.takeIf { it.isNotBlank() && it != key.sessionId }?.let { SessionKey(key.machineId, it) }
    val parent = parentKey?.let { row(it) }
    val home = childSessionHome(key, child, parent, side)
    return home.copy(rows = listOfNotNull(child, parent).filter { found -> rows.none { it.key == found.key } })
}

/** Keep cached pages mounted while looking up metadata; only a confirmed child and parent change the page key. */
@Composable
fun rememberChildSessionHome(key: SessionKey, rows: List<Session>, actions: ChatActions?, features: Collection<String>,
    connected: Boolean, side: Boolean = false): ChildSessionHome {
    val listed = rows.firstOrNull { it.key == key }
    val parent = listed?.foldedUnder?.let { id -> rows.firstOrNull { it.machineId == key.machineId && it.sessionId == id } }
    val local = childSessionHome(key, listed, parent, side)
    val missing = listed == null || !listed.foldedUnder.isNullOrBlank() && parent == null
    val needsLookup = missing && connected && actions != null && "sessions_lookup" in features
    var lookedUp by remember(key, actions, listed?.foldedUnder, side) { mutableStateOf<ChildSessionHome?>(null) }
    LaunchedEffect(key, listed?.foldedUnder, needsLookup, actions, features, side) {
        if (needsLookup) {
            lookedUp = resolveChildSessionHome(key, rows, connected, side) { target ->
                actions?.completeRow(Session(target.machineId, target.sessionId))
            }
        }
    }
    return if (missing) lookedUp ?: local else local
}

/**
 * The side that a link landing on [home] names (#7944): the side itself, or the side a row folded through one belongs
 * to; null when the link named no side, or the hub does not fold sides ([SideFold.FEATURE]).
 */
fun ChildSessionHome.shownSide(rows: List<Session>, features: Collection<String>): SessionKey? {
    val child = childSessionId ?: return null
    if (SideFold.FEATURE !in features) return null
    val row = (rows + this.rows).firstOrNull { it.machineId == key.machineId && it.sessionId == child } ?: return null
    return row.sideHome?.let { SessionKey(key.machineId, it) }
}

/**
 * Bounded hubs keep children outside the root window; a failed refresh keeps the last confirmed cards. With side folding
 * (#7944) a parent's sides are its Side sessions, not cards, and a side's own chat shows the rows folded through it,
 * which the hub lists under the side's topmost parent.
 */
@Composable
internal fun rememberChatChildren(key: SessionKey, parent: Session?, rows: List<Session>, chrome: ChatChromeBindings,
    repository: ChatRepository): ChatChildren {
    val features = chrome.connection?.lastHello?.features ?: LocalHubFeatures.current
    val sideFold = SideFold.FEATURE in features
    val top = parent?.takeIf { sideFold && it.isFoldedSide }?.foldedUnder?.let { SessionKey(key.machineId, it) }
    val home = top ?: key
    val homeRow = if (top == null) parent else (rows + chrome.lookupRows).firstOrNull { it.key == top }
    val list = remember(home, chrome.actions, chrome.connection) { ChildSessionList(home) }
    val held by list.state.collectAsStateWithLifecycle()
    val local = remember(rows, home) { childSessions(rows, home) }
    val folded = remember(held, local, chrome.lookupRows, home) {
        childSessionRows(home, local, held, chrome.lookupRows)
    }
    val children = remember(folded, key, top, sideFold) {
        if (top != null) sideChildren(folded, key) else childSessions(folded, key, sides = sideFold)
    }
    val sides = remember(folded, key, top, sideFold) { if (sideFold && top == null) sideSessions(folded, key) else emptyList() }
    val rollup = remember(parent, sides, folded) { if (sideFold && top == null) sideRollup(parent, sides, folded) else SideRollup() }
    val hasChildren = (homeRow?.children?.total ?: 0) > 0 || folded.isNotEmpty()
    val latestParent by rememberUpdatedState(homeRow)
    val latestLocal by rememberUpdatedState(local)
    // Retain invalidations while covered/backgrounded without doing HTTP work there.
    val invalidation = produceState<String?>(null, chrome.connection) {
        value = null
        chrome.connection?.events?.filterIsInstance<SessionsListEvent>()?.collect { value = it.revision }
    }
    val connected = chrome.connection?.state?.collectAsStateWithLifecycle()?.value == ConnectionState.Online
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(home, homeRow?.key, hasChildren, features, chrome.actions, chrome.connection, connected, chrome.covered, lifecycle) {
        val parentRow = homeRow
        if (parentRow == null || !hasChildren || "sessions_hierarchy" !in features || chrome.covered || !connected) return@LaunchedEffect
        lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            val actions = chrome.actions ?: return@repeatOnLifecycle
            val events = snapshotFlow { invalidation.value }.filterNotNull()
            list.watch(events, { needsChildSessionRead(latestParent, childSessionRows(home, latestLocal, list.state.value, emptyList())) }) {
                actions.childPage(parentRow, it)
            }
        }
    }
    return ChatChildren(children.associateBy { it.sessionId }, repository, sides, rollup)
}

/** The existing task card, with its child's own metadata and a transcript read only while open. */
@Composable
internal fun ChildSessionCard(sessionId: String, assistant: Boolean) {
    val children = LocalChatChildren.current ?: return
    val row = children.rows[sessionId] ?: return
    val callId = ChildSessionCards.call(sessionId)
    val folds = LocalSubagentFolds.current
    val opened = folds?.isOpen(callId) == true
    val source = remember(row.key, opened, children.repository) {
        if (opened) children.repository.observe(row.key) else flowOf(ChatSnapshot())
    }
    val snapshot by source.collectAsStateWithLifecycle(ChatSnapshot())
    val detailReader = remember(row.key, opened, children.repository) {
        ChildSessionDetailReader { from, to -> children.repository.loadRange(row.key, from, to) }
    }
    var detail by remember(row.key, opened) { mutableStateOf(ChildSessionDetail.of(snapshot.items)) }
    LaunchedEffect(detailReader, snapshot.items, snapshot.hasOlder, opened) {
        detail = ChildSessionDetail.of(snapshot.items)
        if (opened) detail = detailReader.read(snapshot)
    }
    val result = detail.result
    val prompt = detail.prompt
    val lastTool = detail.lastTool
    val title = persona.core.domain.lines.Subagents.compact(row.title, 96)
        .ifEmpty { LocalResources.current.getString(R.string.chat_subagent_untitled) }
    val status = ChildSessionCards.status(row)
    val agent = Subagent(sessionId, sessionId = sessionId, description = title, toolUseId = callId,
        running = status == SubagentStatus.Running, startedAt = row.startedAt, updatedAt = row.updatedAt,
        lastText = row.lastText, model = row.model, lastTool = lastTool?.let { WorkflowTool(name = it) })
    val use = Item(callId, role = "assistant", kind = "tool_use", toolName = "Agent", toolUseId = callId,
        toolInput = buildJsonObject { put("description", title); put("prompt", prompt.orEmpty()) })
    val words = result?.let { it.copy(kind = "tool_result") } ?: row.lastText?.let {
        Item("$callId-result", role = "user", kind = "tool_result", text = it)
    }
    val call = ToolCall(LineItem(use), words?.let(::LineItem), emptyList())
    val res = LocalResources.current
    val start = parseTime(row.startedAt)
    val end = parseTime(row.updatedAt)
    val took = if (status != SubagentStatus.Running && start != null && end != null && end >= start)
        workDuration(res, Duration.between(start, end).seconds.coerceAtLeast(1)) else null
    val effort = row.effort?.takeIf { it in setOf("minimal", "low", "medium", "high", "xhigh", "max", "ultra") }
        ?.let { SessionModes.effortLabel(it, res) }
    val model = listOfNotNull(persona.core.domain.modelShortName(row.model).ifEmpty { null }, effort).joinToString(" · ").ifEmpty { null }
    val meta = SubagentCardText.Meta(if (assistant) null else subagentHarness(row, res), model, null, null, took)
    val actions = remember(row.key, children.repository) {
        ChatRowActions(row.key, fullText = { children.repository.fullText(row.key, it.id) }, openPath = null,
            photos = null, pictures = { emptyList() }, openImages = { _, _ -> }, menu = null, retryLineId = null, onRetry = {})
    }
    CompositionLocalProvider(LocalChatSubagents provides ChatSubagents(emptyMap(), null, mapOf(callId to agent)),
        LocalChatRowActions provides actions) {
        SubagentCard(call, live = status == SubagentStatus.Running, assistant = assistant, statusOverride = status, metaOverride = meta)
    }
}

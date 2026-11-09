package persona.feature.chat

import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalResources
import persona.core.chatlist.*
import persona.core.domain.lines.ChatLine
import kotlinx.coroutines.delay

internal val LocalChatSearchItem = staticCompositionLocalOf<String?> { null }

@Stable
internal class ChatSearchState {
    var visible by mutableStateOf(false)
    var query by mutableStateOf("")
    var origin by mutableStateOf<ChatReadingPosition?>(null)
    var selected by mutableStateOf<ChatSearchHit?>(null)
    var pending by mutableStateOf(false)
    var returning by mutableStateOf(false)
    var highlight by mutableStateOf<String?>(null)
    var notice by mutableStateOf<String?>(null)

    fun open(list: ChatListState, freeze: () -> Unit) {
        if (origin == null) origin = list.readingPosition()
        list.holdPosition()
        freeze()
        visible = true
    }

    fun choose(hit: ChatSearchHit) {
        selected = hit
        pending = true
        visible = false
        notice = null
    }

    fun close(list: ChatListState) {
        visible = false
        if (selected == null) returnToOrigin(list)
    }

    fun returnToOrigin(list: ChatListState) {
        list.setExpanded(origin?.expanded ?: list.position.expanded)
        selected = null
        highlight = null
        pending = false
        returning = true
    }
}

/** Reveal background-wake drawers in the same lazy list instead of introducing a second scroller. */
internal fun searchVisibleLines(lines: List<ChatLine>, itemId: String?): List<ChatLine> = lines.flatMap { line ->
    if (line is ChatLine.Wakes && itemId != null && searchTarget(line.drawer, itemId) != null)
        listOf(line) + searchVisibleLines(line.drawer, itemId)
    else listOf(line)
}.distinctBy { it.id }

@Composable
internal fun ChatSearchNavigation(search: ChatSearchState, list: ChatListState, lines: List<ChatLine>) {
    val resources = LocalResources.current
    LaunchedEffect(search.pending, search.selected, lines) {
        val hit = search.selected ?: return@LaunchedEffect
        if (!search.pending) return@LaunchedEffect
        val target = searchTarget(lines, hit.itemId)
        if (target == null) {
            search.pending = false
            search.notice = resources.getString(R.string.chat_search_unlocatable)
            return@LaunchedEffect
        }
        if (!list.position.expanded.containsAll(target.expand)) {
            list.setExpanded(list.position.expanded + target.expand)
            return@LaunchedEffect
        }
        if (lines.none { it.contentKey == target.rowId }) return@LaunchedEffect
        withFrameNanos { }
        if (list.scrollToEntry(ChatListEntry(target.rowId), alignTop = true)) {
            search.pending = false
            search.highlight = target.rowId
        }
    }
    LaunchedEffect(search.returning, lines) {
        if (!search.returning) return@LaunchedEffect
        val origin = search.origin ?: return@LaunchedEffect
        val rowId = origin.rowId
        withFrameNanos { }
        if (rowId == null) list.returnToLatest()
        else if (rowId.startsWith("outbox-") || lines.any { it.contentKey == rowId }) {
            if (!list.scrollToEntry(ChatListEntry(rowId, offsetPx = origin.offsetPx))) return@LaunchedEffect
        } else {
            // Expansion changes are projected asynchronously before the saved key becomes available.
            if (lines.any { it is ChatLine.Step && it.groupId !in origin.expanded }) return@LaunchedEffect
            search.notice = resources.getString(R.string.chat_search_origin_changed)
        }
        search.origin = null
        search.returning = false
    }
    LaunchedEffect(search.highlight) {
        if (search.highlight != null) {
            delay(3_000)
            search.highlight = null
        }
    }
}

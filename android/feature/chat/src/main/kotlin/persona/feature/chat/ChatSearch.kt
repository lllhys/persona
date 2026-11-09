package persona.feature.chat

import android.content.res.Resources
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatRepository
import persona.core.domain.injectedOf
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.WorkItem
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.first
import kotlinx.serialization.json.*

internal data class ChatSearchHit(val itemId: String, val category: String, val snippet: String, val ranges: List<IntRange>)
internal data class ChatSearchResults(val hits: List<ChatSearchHit> = emptyList(), val truncated: Boolean = false)

private fun JsonElement.strings(): Sequence<String> = when (this) {
    is JsonObject -> values.asSequence().flatMap { it.strings() }
    is JsonArray -> asSequence().flatMap { it.strings() }
    is JsonPrimitive -> if (isString) sequenceOf(content) else emptySequence()
}

private fun Item.searchFields(res: Resources): List<Pair<String, String>> = buildList {
    val event = res.getString(R.string.chat_search_category_event)
    val toolInputLabel = res.getString(R.string.chat_search_category_tool_input)
    // Persona's message for the model: what the chat shows of it (its line, a scheduled run's ask), not its words
    injectedOf(this@searchFields)?.let { n ->
        add(event to listOfNotNull(n.line, n.detail).joinToString("\n"))
        return@buildList
    }
    val category = when (kind) {
        "text" -> res.getString(R.string.chat_search_category_text)
        "thinking" -> res.getString(R.string.chat_search_category_thinking)
        "tool_use" -> toolInputLabel
        "tool_result" -> res.getString(R.string.chat_search_category_tool_output)
        "notice", "recap" -> event
        else -> return@buildList
    }
    text?.takeIf(String::isNotBlank)?.let { add(category to it) }
    if (kind == "tool_use") {
        toolName?.let { add(res.getString(R.string.chat_search_category_tool_name) to it) }
        toolInput?.strings()?.joinToString("\n")?.takeIf(String::isNotBlank)?.let { add(toolInputLabel to it) }
    }
    task?.let { add(event to listOfNotNull(it.title, it.detail, it.status).joinToString("\n")) }
}

/** Literal matching preserves UTF-16 offsets used by Compose text spans; [res] names each hit's category. */
internal suspend fun searchChat(res: Resources, items: List<Item>, query: String): ChatSearchResults {
    val needle = query.trim()
    if (needle.isEmpty()) return ChatSearchResults()
    val hits = mutableListOf<ChatSearchHit>()
    for (item in items.distinctBy { it.id }.sortedWith(chatItemOrder).asReversed()) {
        currentCoroutineContext().ensureActive()
        for ((category, text) in item.searchFields(res)) {
            val first = text.indexOf(needle, ignoreCase = true)
            if (first < 0) continue
            if (hits.size == 100) return ChatSearchResults(hits, truncated = true)
            var start = (first - 45).coerceAtLeast(0)
            var end = (first + needle.length + 110).coerceAtMost(text.length)
            if (start > 0 && text[start].isLowSurrogate()) start--
            if (end < text.length && text[end].isLowSurrogate()) end++
            val snippet = (if (start > 0) "…" else "") + text.substring(start, end) + (if (end < text.length) "…" else "")
            val ranges = mutableListOf<IntRange>()
            var at = snippet.indexOf(needle, ignoreCase = true)
            while (at >= 0) {
                ranges += at until at + needle.length
                at = snippet.indexOf(needle, at + needle.length, ignoreCase = true)
            }
            hits += ChatSearchHit(item.id, category, snippet, ranges)
        }
    }
    return ChatSearchResults(hits)
}

internal data class ChatSearchTarget(val rowId: String, val expand: Set<String> = emptySet())

private fun WorkItem.containsItem(id: String): Boolean = when (this) {
    is WorkItem.Tool -> call.use.id == id || call.result?.id == id
    is WorkItem.Thought -> item.id == id
    is WorkItem.Narration -> item.id == id
    is WorkItem.Folded -> item.id == id
}

internal fun searchTarget(lines: List<ChatLine>, itemId: String): ChatSearchTarget? {
    // Prefer visible rows over the same items retained in work summaries.
    lines.forEach { line ->
        if (line is ChatLine.Row && line.row.id == itemId) return ChatSearchTarget(line.contentKey)
        if (line is ChatLine.Step && line.step.containsItem(itemId)) return ChatSearchTarget(line.id)
        if (line is ChatLine.ApiError && line.item.id == itemId) return ChatSearchTarget(line.id)
        if (line is ChatLine.Recaps && line.items.any { it.id == itemId }) return ChatSearchTarget(line.id)
    }
    lines.forEach { line ->
        when (line) {
            is ChatLine.Work -> line.steps.firstOrNull { it.containsItem(itemId) }?.let {
                return ChatSearchTarget("${line.id}/${it.id}", setOf(line.id))
            }
            else -> Unit
        }
    }
    return null
}

/** Only the existing continuous history cursor is used; no disjoint search window is merged. */
internal suspend fun loadEarlierForSearch(res: Resources, repository: ChatRepository, key: SessionKey, onPage: (Int) -> Unit): String? {
    repeat(20) { page ->
        val before = repository.observe(key).first()
        if (!before.hasOlder) return null
        if (before.loading || before.loadingOlder || !before.verified) return res.getString(R.string.chat_search_updating)
        repository.loadOlder(key)
        val after = repository.observe(key).first { !it.loadingOlder }
        if (after.error != null) return res.getString(R.string.chat_search_older_failed)
        if (!after.verified || (after.items.minOfOrNull { it.seq } ?: Int.MAX_VALUE) >=
            (before.items.minOfOrNull { it.seq } ?: Int.MAX_VALUE)) return res.getString(R.string.chat_search_older_none)
        onPage(page + 1)
        if (!after.hasOlder) return null
    }
    return res.getString(R.string.chat_search_older_limit, 20)
}

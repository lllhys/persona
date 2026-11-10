package persona.core.data

import persona.core.contracts.*

/** Ephemeral rows never enter transcript coverage or the disk cache. Callers hold the entry lock. */
internal class ChatRealtimeState(private val key: SessionKey) {
    private data class Draft(var item: Item, val after: Item?, var finalId: String? = null)
    private val drafts = mutableListOf<Draft>()
    private var active: Draft? = null
    private var nextId = 0L
    private val aliases = mutableMapOf<String, String>()
    private val tools = LinkedHashMap<String, ToolCallEvent>()
    private var screen: Item? = null
    private var screenAfter: String? = null
    private var projectedTranscript: List<Item>? = null
    private var projectedItems: List<Item>? = null

    private fun invalidate() { projectedTranscript = null; projectedItems = null }

    fun delta(event: DeltaEvent, items: List<Item>) {
        invalidate()
        if (event.turnFinished) {
            finish()
            return
        }
        val draft = active ?: Draft(Item("realtime-delta:${key.path}:${++nextId}#0",
            nextSeq(items), role = "assistant", kind = "text"), items.lastOrNull()).also {
            drafts += it
            active = it
        }
        draft.item = draft.item.copy(text = event.text)
    }

    fun finish() { active = null; screen = null; invalidate() }

    fun screen(event: ScreenEvent, items: List<Item>) {
        invalidate()
        screenAfter = items.lastOrNull()?.id
        screen = event.text.takeIf { it.isNotEmpty() }?.let {
            Item("realtime-screen:${key.path}#0", screen?.seq ?: nextSeq(items),
                ts = event.ts.takeIf { parseTime(it) != null }, role = "system", kind = "notice", text = it)
        }
    }

    /**
     * Records a call's name and arguments; true when [transcript] already has its row, so the session must be redrawn.
     * Calls have no session identity and may precede their transcript row: then the row picks them up when it arrives.
     */
    fun tool(event: ToolCallEvent, transcript: List<Item>): Boolean {
        tools[event.callId] = event
        if (tools.size > 128) tools.remove(tools.keys.first())
        // Without its row nothing shown changes; the row arrives in a new transcript list, which misses the cache.
        return transcript.any { it.kind == "tool_use" && it.toolUseId == event.callId }.also { if (it) invalidate() }
    }

    fun reset() {
        drafts.clear(); active = null; aliases.clear(); tools.clear(); screen = null; screenAfter = null
        invalidate()
    }

    fun canonicalId(id: String): String = aliases.entries.firstOrNull { it.value == id }?.key ?: id
    fun displayId(id: String): String = aliases[id] ?: id
    fun streamingItemIds(): Set<String> = setOfNotNull(active?.item?.id)

    fun items(transcript: List<Item>): List<Item> {
        if (projectedTranscript === transcript) projectedItems?.let { return it }
        val claimed = aliases.keys.toMutableSet()
        val pending = mutableListOf<Item>()
        val overlays = mutableMapOf<String, String>()
        val retained = mutableListOf<Draft>()
        for (draft in drafts) {
            val nextUser = transcript.firstOrNull { item ->
                item.role == "user" && item.queued != true &&
                    (draft.after == null || order.compare(item, draft.after) > 0)
            }
            val final = draft.finalId?.let { id -> transcript.firstOrNull { it.id == id } } ?: transcript.firstOrNull { item ->
                // A reply the hub cut is matched by its start.
                val text = MessageEcho.uncut(item)
                item.id !in claimed && item.role == "assistant" && item.kind == "text" &&
                    (draft.after == null || order.compare(item, draft.after) > 0) &&
                    (nextUser == null || order.compare(item, nextUser) < 0) &&
                    text.isNotEmpty() && (text == draft.item.text ||
                    text.startsWith(draft.item.text.orEmpty()) || draft.item.text.orEmpty().startsWith(text))
            }
            if (final != null) {
                draft.finalId = final.id
                aliases[final.id] = draft.item.id
                claimed += final.id
                // A shorter transcript must not erase the draft while the final record catches up.
                if (draft.item.text.orEmpty().startsWith(final.text.orEmpty()) &&
                    draft.item.text.orEmpty().length > final.text.orEmpty().length) {
                    overlays[final.id] = draft.item.text.orEmpty()
                    retained += draft
                } else if (draft === active) retained += draft
            } else if (nextUser == null) {
                pending += draft.item.copy(seq = nextSeq(transcript))
                retained += draft
            }
            if (nextUser != null && draft === active) active = null
        }
        drafts.clear(); drafts += retained
        if (screenAfter != transcript.lastOrNull()?.id) screen = null
        val result = (transcript.map { item ->
            val tool = item.toolUseId?.let(tools::get).takeIf { item.kind == "tool_use" }
            val mapped = if (tool == null) item else item.copy(toolName = tool.name, toolInput = tool.args)
            overlays[item.id]?.let { mapped.copy(text = it) } ?: mapped
        } + pending + listOfNotNull(screen?.copy(seq = nextSeq(transcript))))
            .sortedWith(order).map { item ->
                val id = displayId(item.id)
                if (id == item.id) item else item.copy(id = id)
            }
        projectedTranscript = transcript
        projectedItems = result
        return result
    }

    private fun nextSeq(items: List<Item>): Int =
        (items.maxOfOrNull { maxOf(it.seq, it.fold?.throughSeq ?: it.seq) } ?: 0).let {
            if (it == Int.MAX_VALUE) it else it + 1
        }

    companion object {
        private val order = compareBy<Item> { it.seq }
            .thenBy { it.id.substringAfterLast('#').toIntOrNull() ?: 0 }
    }
}

package persona.core.domain.lines

/**
 * [chatLines] for one chat as it changes, with the same result: the turns before the first changed one keep the blocks
 * and lines of the previous call. While the agent works a chat changes at its end, and each update drew every turn again.
 *
 * The chat is cut in parts before a user message that is not queued: it always starts a turn, and no fold before it
 * (compactions, rows, a reopened turn) reaches past it. A tool's result and images are found anywhere in the chat, so
 * parts are only cut where no tool id has items on both sides. What spans the whole chat runs on all of it every time:
 * the live turn, the work line without a turn, API error cards, recaps, open work lines, the turns and the task index.
 * A part's lines are drawn again when the live turn moves into or out of it or open work lines change. Not
 * thread-safe: one owner calls it.
 */
class ChatLinesMemo {
    /** Whole turns between two cuts, with the blocks and lines [chatLines] makes of them. */
    private class Part(val items: List<LineItem>, val blocks: List<Block>) {
        val turnIds = blocks.mapNotNullTo(HashSet()) { (it as? Block.Turn)?.turn?.id }
        var lines: List<ChatLine> = emptyList()
        /** The live turn [lines] were drawn for: any id but one of [turnIds] draws the same. */
        var liveId: String? = null
    }

    private var parts: List<Part> = emptyList()
    /** [hasCompactions] of the chat the parts are from: whether each part folds depends on the whole chat. */
    private var compacting = false
    private var openWork: Set<String> = emptySet()
    /** The part each tool id of the kept parts is in; each is in one only. */
    private val owners = HashMap<String, Int>()
    /** Old notices may carry only task_id; the launch receipt provides their tool binding across turn cuts. */
    private var taskCalls: Map<String, String> = emptyMap()

    private val LineItem.toolId: String?
        get() = if (kind == "tool_use" || kind == "tool_result" || kind == "image") item.toolUseId ?: taskCalls[id]
            else if (task?.kind == "agent") taskCalls[id] else null

    /** How many leading items the last call kept the parts of, for tests. */
    internal var reused = 0
        private set

    fun lines(items: List<LineItem>, options: LineOptions = LineOptions()): ChatSnapshot {
        val source = lineSource(items, options)
        taskCalls = TaskIndex.build(source).calls
        val compacting = hasCompactions(source)
        var kept = if (compacting == this.compacting) unchanged(source) else 0
        // A tool id in the changed items that a kept part has too: that part is not cut from them.
        var start = parts.subList(0, kept).sumOf { it.items.size }
        for (i in start until source.size) {
            val owner = source[i].toolId?.let(owners::get) ?: continue
            if (owner < kept) kept = owner
        }
        owners.entries.removeAll { it.value >= kept }
        start = parts.subList(0, kept).sumOf { it.items.size }
        val added = split(source.subList(start, source.size)).map { part ->
            Part(part, buildBlocks(buildLineRows(foldCompactions(part, compacting))))
        }
        for ((k, part) in added.withIndex()) part.items.forEach { item -> item.toolId?.let { owners[it] = kept + k } }
        val redraw = options.openWork != openWork
        parts = parts.subList(0, kept) + added
        this.compacting = compacting
        openWork = options.openWork
        reused = start

        val blocks = parts.flatMap { it.blocks }
        val live = liveTurnId(blocks, options)
        val builder = LineBuilder(live, options)
        for ((k, part) in parts.withIndex()) {
            val drawn = k < kept && !redraw &&
                (part.liveId == live || (part.liveId !in part.turnIds && live !in part.turnIds))
            if (!drawn) part.lines = part.blocks.flatMap(builder::lines)
            part.liveId = live
        }
        return chatSnapshot(source, blocks, parts.flatMap { it.lines }, live, options)
    }

    /** How many leading parts are still [source]'s, each followed by a cut or the end. */
    private fun unchanged(source: List<LineItem>): Int {
        var at = 0
        for ((k, part) in parts.withIndex()) {
            val end = at + part.items.size
            if (end > source.size || (end < source.size && !source[end].isCut)) return k
            for (i in part.items.indices) if (part.items[i] != source[at + i]) return k
            at = end
        }
        return parts.size
    }

    /** [items] cut before each cut item, except where a tool id has items on both sides. */
    private fun split(items: List<LineItem>): List<List<LineItem>> {
        val starts = items.indices.filter { it == 0 || items[it].isCut }
        // The last piece each tool id is in, then the last piece reached from each piece by an id first seen in it.
        val last = HashMap<String, Int>()
        var piece = -1
        for ((i, item) in items.withIndex()) {
            if (piece + 1 < starts.size && starts[piece + 1] == i) piece++
            item.toolId?.let { last[it] = piece }
        }
        val reach = IntArray(starts.size) { it }
        piece = -1
        val seen = HashSet<String>()
        for ((i, item) in items.withIndex()) {
            if (piece + 1 < starts.size && starts[piece + 1] == i) piece++
            val id = item.toolId ?: continue
            if (seen.add(id)) reach[piece] = maxOf(reach[piece], last.getValue(id))
        }
        val out = ArrayList<List<LineItem>>()
        var from = 0
        var furthest = -1
        for (p in starts.indices) {
            furthest = maxOf(furthest, reach[p])
            if (furthest > p) continue
            val end = starts.getOrElse(p + 1) { items.size }
            out += items.subList(from, end).toList()
            from = end
        }
        return out
    }
}

/** A user message that is not queued: it starts a turn, see [ChatLinesMemo]. */
private val LineItem.isCut get() = kind == "text" && item.role == "user" && item.queued != true

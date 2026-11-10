package persona.feature.chat

import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.WorkItem
import persona.core.contracts.Item

internal data class TaskRecordTarget(val rowId: String? = null, val expandGroup: String? = null, val expandFold: Item? = null)

/** Resolve again after expansion: merged turns acquire different work-group keys. */
internal fun taskRecordTarget(lines: List<ChatLine>, itemId: String, sourceSeq: Int? = null): TaskRecordTarget? =
    recordTarget(lines) { it.containsTaskRecord(itemId, sourceSeq) } ?: lines.firstOrNull { it.holds(itemId) }?.let { TaskRecordTarget(rowId = it.contentKey) }

/**
 * The call with this tool_use_id (a background task's notice: "Jump to the step that started it"), opening the work line it is in first.
 * Not loaded: the hub's fold nearest before the notice [before] opens and it looks again (an earlier fold the next time).
 */
internal fun callRecordTarget(lines: List<ChatLine>, toolUseId: String, before: Item? = null): TaskRecordTarget? {
    recordTarget(lines) { it is WorkItem.Tool && it.call.use.item.toolUseId == toolUseId }?.let { return it }
    if (before == null) return null
    val fold = lines.flatMap { line -> when (line) {
        is ChatLine.Step -> listOf(line.step)
        is ChatLine.Work -> line.steps
        else -> emptyList()
    } }.filterIsInstance<WorkItem.Folded>().lastOrNull { chatItemOrder.compare(it.item.item, before) < 0 } ?: return null
    return recordTarget(lines) { it === fold }
}

private fun recordTarget(lines: List<ChatLine>, matches: (WorkItem) -> Boolean): TaskRecordTarget? {
    lines.filterIsInstance<ChatLine.Step>().firstOrNull { matches(it.step) }?.let {
        val folded = it.step as? WorkItem.Folded
        if (folded != null) return TaskRecordTarget(expandFold = folded.item.item)
        return TaskRecordTarget(rowId = it.id)
    }
    return lines.filterIsInstance<ChatLine.Work>().firstOrNull { it.steps.any(matches) }?.let { TaskRecordTarget(expandGroup = it.id) }
}

private fun WorkItem.containsTaskRecord(id: String, sourceSeq: Int?): Boolean = when (this) {
    is WorkItem.Tool -> call.use.id == id || call.results.any { it.id == id }
    is WorkItem.Thought -> item.id == id
    is WorkItem.Narration -> item.id == id
    is WorkItem.Folded -> if (sourceSeq == null) item.item.fold?.tasks.orEmpty().any { it.sourceItemId == id }
        else item.fold?.let { fold ->
            val source = Item(id, seq = sourceSeq)
            chatItemOrder.compare(source, item.item) >= 0 &&
                chatItemOrder.compare(source, item.item.copy(id = fold.throughId, seq = fold.throughSeq)) <= 0
        } == true
}

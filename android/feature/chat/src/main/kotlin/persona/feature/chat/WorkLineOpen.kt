package persona.feature.chat

import persona.core.contracts.Item
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.LiveWorkId
import persona.core.domain.lines.WorkItem

/** Explicit choices use the chat's persisted expanded set, during and after a turn. */
internal data class WorkOpenState(val open: Set<String> = emptySet())

/** The running work lines among [lines], those in a wake drawer too. */
internal fun liveWorkIds(lines: List<ChatLine>): Set<String> = buildSet {
    fun visit(list: List<ChatLine>) {
        for (line in list) when (line) {
            is ChatLine.Work -> if (line.live) add(line.id)
            is ChatLine.Wakes -> visit(line.drawer)
            else -> {}
        }
    }
    visit(lines)
}

/** Running and completed lines have identical toggles; the completion edge folds the running summary. */
internal fun toggleWork(state: WorkOpenState, id: String): Pair<WorkOpenState, Boolean> {
    val opening = id !in state.open
    return state.copy(open = if (opening) state.open + id else state.open - id) to opening
}

/** Fold a turn on completion; transfer an opened placeholder without forgetting already historical choices. */
internal fun settleWorkOpen(open: Set<String>, live: Set<String>, previousLive: Set<String> = emptySet()): Set<String> {
    val kept = open - (previousLive - live)
    return if (LiveWorkId in open && live.isNotEmpty() && LiveWorkId !in live) kept - LiveWorkId + live else kept
}

/** Load folds only in explicitly expanded lines, including after completion or history restoration. */
internal fun openFoldsToFetch(lines: List<ChatLine>, folding: Set<String>, failed: Set<String>): List<Item> =
    // Wake drawers are rendered as notices, not their contents; don't fetch their invisible folds.
    lines.filterIsInstance<ChatLine.Work>().filter { it.open }.flatMap { line ->
        line.steps.mapNotNull { step ->
            (step as? WorkItem.Folded)?.item?.item?.takeIf { it.id !in folding && it.id !in failed }
        }
    }

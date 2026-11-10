package persona.core.domain.lines

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import java.time.Duration
import java.time.Instant
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

data class TaskBadge(val text: CoreText, val failed: Boolean)
data class TaskNoteText(val label: CoreText, val subject: String, val outcome: CoreText?, val failed: Boolean, val callId: String?)

data class TaskIndex(
    val notes: Map<String, List<TaskNote>> = emptyMap(),
    val calls: Map<String, String> = emptyMap(),
    val started: Set<String> = emptySet(),
    val descriptions: Map<String, String> = emptyMap(),
    val startedAt: Map<String, Instant> = emptyMap(),
    val progress: List<TaskProgressItem> = emptyList(),
) {
    fun note(item: LineItem): TaskNoteText {
        val note = requireNotNull(item.task)
        val call = calls[item.id]
        val label = when (note.kind) {
            "shell" -> Key.TaskShell
            "monitor" -> if (note.status == "event") Key.TaskMonitorEvent else Key.TaskMonitorEnded
            "agent" -> Key.TaskSubagent
            else -> Key.TaskBackground
        }.let(CoreText::of)
        val subject = (if (note.kind == "agent" && item.kind == "tool_result")
            item.text?.lineSequence()?.map(String::trim)?.firstOrNull(String::isNotEmpty) else null)
            ?: (if (note.kind == "monitor" && note.status == "event")
                note.detail?.lineSequence()?.map { it.trim() }?.firstOrNull { it.isNotEmpty() } else null)
            ?: descriptions[call]?.takeIf { it.isNotEmpty() } ?: note.title
        val outcome = buildList {
            when (note.status) {
                "completed", "ended" -> add(CoreText.plain("✓"))
                "failed" -> add(note.exitCode?.let { CoreText.of(Key.TaskExitCode, it) } ?: CoreText.plain("✗"))
                "killed", "stopped" -> add(CoreText.of(Key.TaskStoppedState))
                "waiting" -> add(CoreText.of(Key.TaskWaitingState))
                else -> note.exitCode?.takeIf { it != 0 }?.let { add(CoreText.of(Key.TaskExitCode, it)) }
            }
            val start = startedAt[call]
            val end = item.time
            if (note.status != "event" && start != null && end != null && end > start) {
                val s = Duration.between(start, end).seconds
                add(when {
                    s < 60 -> CoreText.of(Key.DurationSeconds, s)
                    s < 3600 -> CoreText.of(Key.DurationMinutes, s / 60)
                    s % 3600 / 60 == 0L -> CoreText.of(Key.DurationHours, s / 3600)
                    else -> CoreText.of(Key.DurationHoursMinutes, s / 3600, s % 3600 / 60)
                })
            }
        }.takeIf { it.isNotEmpty() }?.let { CoreText.joined(" · ", it) }
        return TaskNoteText(label, subject, outcome, note.failed, call)
    }

    fun badge(callId: String?, concise: Boolean = false): TaskBadge? {
        if (callId == null || (callId !in started && callId !in notes)) return null
        val notes = notes[callId].orEmpty()
        val events = notes.count { it.status == "event" }
        val last = notes.lastOrNull { it.status != "event" }
            ?: return TaskBadge(if (concise) CoreText.of(Key.TaskWorking)
                else if (events > 0) CoreText.of(Key.BadgeEvents, events) else CoreText.of(Key.BadgeRunning), false)
        if (!last.terminal && last.status != "waiting") {
            return TaskBadge(if (concise) CoreText.of(Key.TaskWorking)
                else if (events > 0) CoreText.of(Key.BadgeEvents, events) else CoreText.of(Key.BadgeRunning), last.failed)
        }
        val state = when (last.status) {
            "failed" -> Key.TaskFailedState
            "killed", "stopped" -> Key.TaskStoppedState
            "waiting" -> Key.TaskWaitingState
            "ended" -> Key.TaskEndedState
            else -> Key.TaskCompletedState
        }.let(CoreText::of)
        return TaskBadge(if (concise) state else if (events > 0) CoreText.of(Key.BadgeStateEvents, events, state) else CoreText.of(Key.BadgeState, state), last.failed)
    }

    companion object {
        /** A subagent's id only after "agentId:", as the node reads it: Claude Code 2.1.319's receipt opens with "including
         *  the agentId below", and "below" taken for the id left its notice without its call (#6381). */
        private val launched = Regex("(?:(?:running in background with ID|Monitor started \\(task|moved to the background \\(ID):?|agentId:)\\s*([A-Za-z0-9_-]+)")
        /** What every [launched] match contains, one of them word for word: most results (files, shell output) have
         *  none, and looking for these is much cheaper than running the pattern over them. */
        private val launchMarks = listOf("running in background with ID", "Monitor started (task", "moved to the background (ID", "agentId:")

        fun isLaunchReceipt(text: String): Boolean {
            val start = text.trimStart()
            // Synchronous Agent results can end in agentId too. Only the receipt's explicit opening hides it.
            return start.startsWith("Async agent launched successfully.") ||
                start.startsWith("Command running in background with ID:") ||
                start.startsWith("Monitor started (task ") ||
                (start.startsWith("Command ") && start.substringBefore('\n').contains("moved to the background (ID:"))
        }

        fun build(items: List<LineItem>): TaskIndex {
            val notes = mutableMapOf<String, MutableList<TaskNote>>()
            val calls = mutableMapOf<String, String>()
            val started = mutableSetOf<String>()
            val descriptions = mutableMapOf<String, String>()
            val startedAt = mutableMapOf<String, Instant>()
            val byTask = mutableMapOf<String, String>()
            val uses = items.filter { it.kind == "tool_use" }.mapNotNull { use ->
                use.item.toolUseId?.let { it to use }
            }.toMap()
            fun describe(call: String) {
                val use = uses[call] ?: return
                val description = (use.item.toolInput as? JsonObject)?.get("description") as? JsonPrimitive
                if (description?.isString == true) descriptions[call] = description.content
                else descriptions.remove(call)
                use.time?.let { startedAt[call] = it } ?: startedAt.remove(call)
            }
            for (it in items.sortedWith(itemOrder)) {
                if (it.kind == "tool_result") {
                    val call = it.item.toolUseId ?: it.task?.toolUseId
                    val text = it.text.orEmpty()
                    val match = if (launchMarks.any { mark -> mark in text }) launched.find(text) else null
                    if (call != null && match != null) {
                        byTask[match.groupValues[1]] = call
                        started += call
                        describe(call)
                    }
                }
                // Metadata belongs to the task, not to one particular transcript kind. In particular, a launch
                // receipt and SendMessage handbacks are tool_results, while older hubs send terminal notices.
                val note = it.task ?: continue
                val call = it.item.toolUseId ?: note.toolUseId ?: byTask[note.taskId] ?: continue
                note.taskId?.let { taskId -> byTask[taskId] = call }
                notes.getOrPut(call) { mutableListOf() } += note
                calls[it.id] = call
                if (note.kind == "agent" && !note.terminal) started += call
                if (call !in descriptions) describe(call)
            }
            return TaskIndex(notes.mapValues { it.value.toList() }, calls.toMap(), started.toSet(), descriptions.toMap(), startedAt.toMap(),
                rebuildTaskProgress(items))
        }
    }
}

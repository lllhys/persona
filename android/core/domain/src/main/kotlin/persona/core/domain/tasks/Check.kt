package persona.core.domain.tasks

import persona.core.contracts.Todo
import persona.core.contracts.parseTime

// Why a stopped task is still open (#3180, docs/assistant/todos.md "Automatic completion"): the hub keeps its latest completion
// check on the to-do. Web `stoppedReason` (lib/todos.ts).

/**
 * The reason its current completion check gives for the open to-do not being done ("" when the judge gave none); null once
 * done, without a check, for a check that says done or unsure, or for one from before its latest reopen. Whether the task
 * works or waits for the user now is the caller's to weigh.
 */
val Todo.notDoneReason: String?
    get() {
        val c = check ?: return null
        if (isDone || c.verdict.isBlank() || c.verdict == "done" || c.verdict == "unsure") return null
        val reopened = parseTime(reopenedAt)
        if (reopened != null && parseTime(c.at)?.isBefore(reopened) != false) return null
        return c.reason?.trim().orEmpty()
    }

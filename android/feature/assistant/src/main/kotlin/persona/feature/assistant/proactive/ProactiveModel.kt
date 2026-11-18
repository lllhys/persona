package persona.feature.assistant.proactive

import androidx.annotation.StringRes
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime
import persona.core.contracts.api.AssistantManagement
import persona.core.contracts.api.Run
import persona.core.contracts.api.ScheduledTask
import persona.feature.assistant.R
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.time.Instant
import java.time.ZoneId
import java.time.temporal.ChronoUnit

/**
 * What "Proactive" needs from the hub (docs/protocol/protocol.md, the assistant's tasks and runs). A seam of its own, so the page
 * and its tests do not follow every change of the wider [AssistantManagement].
 */
internal interface ProactiveApi {
    suspend fun tasks(): List<ScheduledTask>
    /** POST for a new one (no id), PUT for an existing one; the hub keeps its own fields. */
    suspend fun saveTask(task: ScheduledTask): ScheduledTask
    suspend fun deleteTask(id: String)
    suspend fun runTask(id: String): SessionKey
    suspend fun freshTask(id: String): ScheduledTask
    /** Newest first, every task; one task's history is filtered by `ref_id`. */
    suspend fun runs(): List<Run>
    /** Where assistant conversations run, for a task's last run (tasks do not say which machine). */
    suspend fun assistantMachine(): String
}

internal fun AssistantManagement.proactive(): ProactiveApi = object : ProactiveApi {
    val api = this@proactive
    override suspend fun tasks() = api.tasks()
    override suspend fun saveTask(task: ScheduledTask) = api.saveTask(task)
    override suspend fun deleteTask(id: String) = api.deleteTask(id)
    override suspend fun runTask(id: String) = api.runTask(id)
    override suspend fun freshTask(id: String) = api.freshTask(id)
    override suspend fun runs() = api.runs()
    override suspend fun assistantMachine() = api.notes().machineId
}


internal val Workdays = listOf(1, 2, 3, 4, 5)
private val TimePattern = Regex("^([01]\\d|2[0-3]):[0-5]\\d$")
private val ShortHour = Regex("^\\d:[0-5]\\d$")

/** "HH:MM" as the editor writes it. */
internal fun validTime(time: String) = TimePattern.matches(time)

/** The hub also takes "9:00" (Persona writes those); the editor shows and writes "09:00". */
internal fun normalizeTime(time: String) = time.trim().let { if (ShortHour.matches(it)) "0$it" else it }

/** Days 1 = Monday … 7 = Sunday in order; all seven are written as none, which the hub reads as every day. */
internal fun normalizeDays(days: List<Int>): List<Int> =
    days.filter { it in 1..7 }.distinct().sorted().takeUnless { it.size == 7 }.orEmpty()

/** How a task repeats (the hub's `repeat`). */
internal enum class TaskRepeat(val wire: String) {
    Once(ScheduledTask.ONCE), Daily(ScheduledTask.DAILY), Interval(ScheduledTask.INTERVAL);
    companion object { fun of(wire: String?) = entries.firstOrNull { it.wire == wire } ?: Once }
}

/** The intervals the editor offers, in minutes (iOS AssistantScheduleText.intervals); the hub takes 5 minutes to 7 days. */
internal val TaskIntervals = listOf(5, 10, 15, 20, 30, 45, 60, 90, 120, 180, 240, 360, 480, 720, 1440)
internal const val DefaultInterval = 30
internal val IntervalRange = 5..7 * 24 * 60

/** Claude Code's permission modes a task can run with; "" is the assistant's default (iOS SessionModes). */
internal const val BypassMode = "bypassPermissions"
internal val TaskPermissionModes = listOf("", "acceptEdits", "plan", "auto", BypassMode)

/**
 * The fields a person edits (iOS AssistantTaskEditor). Everything else on a task belongs to the hub and is never
 * changed here. [continues] is "Continue the original chat": runs go into the conversation the task came from. [push] is "Push when done"
 * ("" every run, [ScheduledTask.PUSH_ASKED] only what a run sends); such a task always runs in its own conversation.
 */
internal data class TaskDraft(
    val title: String = "",
    val prompt: String = "",
    val repeat: TaskRepeat = TaskRepeat.Daily,
    val at: Instant? = null,
    val time: String = "09:00",
    val weekdays: List<Int> = Workdays,
    val everyMinutes: Int = DefaultInterval,
    val enabled: Boolean = true,
    val continues: Boolean = false,
    val permissionMode: String = "",
    val push: String = "",
    val inbox: String = "",
) {
    val pushesOnlyAsked: Boolean get() = push == ScheduledTask.PUSH_ASKED
    /** "Push when done" set to [value]: only its alerts leaves "Continue the original chat" (the hub runs such a task in its own conversation). */
    fun pushing(value: String) = copy(push = value, continues = continues && value != ScheduledTask.PUSH_ASKED)

    /** A one-off may go without a title (its prompt names it); a repeating one needs one. */
    val titled: Boolean get() = repeat == TaskRepeat.Once || title.isNotBlank()
    val timed: Boolean get() = when (repeat) {
        TaskRepeat.Once -> at != null
        TaskRepeat.Daily -> validTime(normalizeTime(time))
        TaskRepeat.Interval -> everyMinutes in IntervalRange
    }
    val valid: Boolean get() = titled && prompt.isNotBlank() && timed
    fun normalized() = copy(title = title.trim(), prompt = prompt.trim(), time = normalizeTime(time), weekdays = normalizeDays(weekdays))
    /** Only what the rule uses counts: another rule's leftover fields do not make a change. */
    private fun key() = normalized().let {
        it.copy(
            at = it.at.takeIf { _ -> repeat == TaskRepeat.Once },
            time = it.time.takeIf { repeat == TaskRepeat.Daily } ?: "",
            weekdays = it.weekdays.takeIf { repeat == TaskRepeat.Daily }.orEmpty(),
            everyMinutes = it.everyMinutes.takeIf { repeat == TaskRepeat.Interval } ?: 0,
        )
    }
    /** Same schedule and words, ignoring surrounding spaces, "9:00" against "09:00" and how "every day" is spelled. */
    fun sameAs(other: TaskDraft) = key() == other.key()

    /** Switching to "Once" picks the next full hour an hour from now; to "Every", half an hour (iOS does the same). */
    fun repeating(next: TaskRepeat, now: Instant, zone: ZoneId): TaskDraft = copy(
        repeat = next,
        at = if (next == TaskRepeat.Once && (at == null || at.isBefore(now))) nextHour(now, zone) else at,
        everyMinutes = if (next == TaskRepeat.Interval && everyMinutes !in IntervalRange) DefaultInterval else everyMinutes,
    )
}

/** The start of the hour an hour from [now]. */
internal fun nextHour(now: Instant, zone: ZoneId): Instant =
    now.plusSeconds(3600).atZone(zone).truncatedTo(ChronoUnit.HOURS).toInstant()

internal val ScheduledTask.repeat: TaskRepeat get() = TaskRepeat.of(repeats)
/** The conversation it came from, which its runs continue ("Continue the original chat"). */
internal val ScheduledTask.origin: SessionKey? get() {
    val session = sessionId?.takeIf { it.isNotBlank() } ?: return null
    val machine = machineId?.takeIf { it.isNotBlank() } ?: return null
    return SessionKey(machine, session)
}
/** The task's own conversation, which the next run continues; null = the next run starts one. */
internal val ScheduledTask.ownConversation: SessionKey? get() {
    val session = runSessionId?.takeIf { it.isNotBlank() } ?: return null
    val machine = runMachineId?.takeIf { it.isNotBlank() } ?: return null
    return SessionKey(machine, session)
}

internal fun ScheduledTask.draft() = TaskDraft(
    title = title, prompt = prompt, repeat = repeat, at = parseTime(at),
    time = normalizeTime(time?.takeIf { it.isNotBlank() } ?: "09:00"), weekdays = weekdays,
    everyMinutes = everyMinutes.takeIf { it > 0 } ?: DefaultInterval, enabled = enabled,
    continues = !sessionId.isNullOrBlank(), permissionMode = permissionMode.orEmpty(), push = push.orEmpty(), inbox = inbox.orEmpty(),
)

/**
 * [draft] written onto the task; turning "Continue the original chat" off clears the conversation (the hub only lets it be cleared). A
 * task that never had "Push when done" keeps it unset while it pushes every run, so a hub without it is never sent one.
 */
internal fun ScheduledTask.applying(draft: TaskDraft): ScheduledTask = draft.normalized().let {
    copy(
        title = it.title, prompt = it.prompt, repeats = it.repeat.wire,
        at = when (it.repeat) { TaskRepeat.Once -> it.at?.toString(); TaskRepeat.Interval -> at; TaskRepeat.Daily -> null },
        time = it.time.takeIf { _ -> it.repeat == TaskRepeat.Daily }, weekdays = if (it.repeat == TaskRepeat.Daily) it.weekdays else emptyList(),
        everyMinutes = if (it.repeat == TaskRepeat.Interval) it.everyMinutes else 0, enabled = it.enabled,
        sessionId = sessionId.takeIf { _ -> it.continues && !it.pushesOnlyAsked }, permissionMode = it.permissionMode.ifBlank { null },
        push = it.push.takeIf { p -> p.isNotEmpty() || push != null },
        inbox = it.inbox.takeIf { p -> p.isNotEmpty() || inbox != null },
    )
}

/** Only what the person changed between [from] and [to]; fields changed on the hub meanwhile (by Persona) stay. */
internal fun ScheduledTask.patched(from: TaskDraft, to: TaskDraft): ScheduledTask {
    val before = from.normalized()
    val after = to.normalized()
    val mine = draft().normalized()
    fun <T> pick(get: (TaskDraft) -> T): T = if (get(before) != get(after)) get(after) else get(mine)
    val ruleChanged = before.repeat != after.repeat
    val patched = applying(TaskDraft(
        title = pick { it.title }, prompt = pick { it.prompt },
        repeat = pick { it.repeat },
        at = if (ruleChanged) after.at else pick { it.at },
        time = if (ruleChanged) after.time else pick { it.time },
        weekdays = if (ruleChanged) after.weekdays else pick { it.weekdays },
        everyMinutes = if (ruleChanged) after.everyMinutes else pick { it.everyMinutes },
        enabled = pick { it.enabled }, continues = pick { it.continues }, permissionMode = pick { it.permissionMode }, push = pick { it.push },
        inbox = pick { it.inbox },
    ))
    // Unlike the other task fields, the hub's contract is to send inbox only when this edit changed it.
    return patched.copy(inbox = patched.inbox.takeIf { before.inbox != after.inbox })
}

/** One edit to a task, written with its own request. */
internal sealed interface TaskChange {
    /** The task it edits; "" for a new one. */
    val id: String
    /** Whether [tasks] already shows this change, so a lost response is not reported as a failure. */
    fun satisfiedBy(tasks: List<ScheduledTask>): Boolean

    data class Toggle(override val id: String, val enabled: Boolean) : TaskChange {
        override fun satisfiedBy(tasks: List<ScheduledTask>) = tasks.firstOrNull { it.id == id }?.enabled == enabled
    }

    /** [from] is the task as the editor opened it, [to] what is on screen now. */
    data class Edit(override val id: String, val from: TaskDraft, val to: TaskDraft) : TaskChange {
        override fun satisfiedBy(tasks: List<ScheduledTask>) = tasks.firstOrNull { it.id == id }?.let { current ->
            val wanted = current.patched(from, to)
            val expected = wanted.draft()
            val actual = current.draft()
            // Null omits an unchanged inbox in the request, so the value the hub kept cannot contradict this edit.
            expected.copy(inbox = actual.inbox.takeIf { wanted.inbox == null } ?: expected.inbox).sameAs(actual)
        } == true
    }

    data class Add(val draft: TaskDraft) : TaskChange {
        override val id: String get() = ""
        // A new task's id is the hub's: a lost answer cannot be told from a failure, so it is reported.
        override fun satisfiedBy(tasks: List<ScheduledTask>) = false
    }

    data class Delete(override val id: String) : TaskChange {
        override fun satisfiedBy(tasks: List<ScheduledTask>) = tasks.none { it.id == id }
    }
}

/** A list read from the hub: the last good value stays on screen when a refresh fails. */
internal data class Loadable<T>(val value: T? = null, val error: String? = null) {
    fun failed(message: String) = copy(error = message)
}

internal data class PendingChange(val token: Long, val change: TaskChange.Toggle)

/** A task's last run, from the history when there is one (the hub's "Run now" does not move `last_run`). */
internal data class LastRun(
    val at: Instant?, val outcome: RunOutcome, val error: String?, val conversation: SessionKey?, val manual: Boolean = false,
)

internal data class ProactiveState(
    val tasks: Loadable<List<ScheduledTask>> = Loadable(),
    val runs: Loadable<List<Run>> = Loadable(),
    /** Switches flipped on screen whose write has not come back yet. */
    val pending: List<PendingChange> = emptyList(),
    /** Actions in flight: `task:<id>` (a write), `run:<id>` ("Run now"), `fresh:<id>` ("New chat next time"). */
    val busy: Set<String> = emptySet(),
    val refreshing: Boolean = false,
    val assistantMachine: String? = null,
    /** When the tasks were last read: what is due after this is worth reading again for. */
    val readAt: Instant? = null,
) {
    /** The hub's tasks with the switches still in flight already flipped. */
    val shownTasks: List<ScheduledTask>? get() = tasks.value?.let { list ->
        pending.fold(list) { tasks, pending -> tasks.map { if (it.id == pending.change.id) it.copy(enabled = pending.change.enabled) else it } }
    }
    /** Scheduled tasks: daily and interval ones, in the hub's order. */
    val repeating: List<ScheduledTask>? get() = shownTasks?.filter { it.repeat != TaskRepeat.Once }
    /** One-off follow-ups: soonest first. */
    val oneOff: List<ScheduledTask>? get() = shownTasks?.filter { it.repeat == TaskRepeat.Once }
        ?.sortedWith(compareBy<ScheduledTask> { parseTime(it.at) }.thenBy { it.id })
    val runList: List<Run>? get() = runs.value?.sortedWith(compareByDescending<Run> { parseTime(it.at) })
    fun runsOf(ref: String): List<Run>? = runList?.filter { it.refId == ref }
    fun task(id: String) = shownTasks?.firstOrNull { it.id == id }
    fun running(id: String) = "run:$id" in busy
    fun busy(id: String) = "task:$id" in busy || running(id)
    fun freshening(id: String) = "fresh:$id" in busy
    /** True once something has been read, so the page can tell "nothing scheduled" from "not loaded yet". */
    val loaded: Boolean get() = tasks.value != null

    fun lastRunOf(task: ScheduledTask): LastRun? {
        runsOf(task.id)?.firstOrNull()?.let { run ->
            return LastRun(parseTime(run.at), run.outcome, run.error, run.conversation(assistantMachine), run.manual)
        }
        val at = parseTime(task.lastRun) ?: return null
        val session = task.lastSessionId?.takeIf { it.isNotBlank() }
        return LastRun(at, outcomeOf(task.lastStatus), task.lastError,
            session?.let { id -> assistantMachine?.let { SessionKey(it, id) } })
    }
}

/** The task was deleted meanwhile; [failureReason] says so in the page's language. */
internal class TaskGoneException : Exception("task is gone")

/**
 * The page's state and every hub call it makes. Writes go one at a time, and a write that has started finishes even
 * when the page goes away. A failed write is checked against a fresh read and only reported when the hub really does
 * not show it. [scope] should outlive the page (the pairing's).
 */
internal class ProactiveStore(
    private val api: ProactiveApi,
    private val scope: CoroutineScope,
    /** Says failures in the page's language. */
    private val say: ProactiveText,
    private val clock: () -> Instant = Instant::now,
    /** Where a message goes when no page is listening to [messages] (a write that failed after the page was left). */
    private val unheard: (String) -> Unit = {},
) {
    private val mutable = MutableStateFlow(ProactiveState())
    val state: StateFlow<ProactiveState> = mutable.asStateFlow()
    private val notices = MutableSharedFlow<String>(extraBufferCapacity = 4, onBufferOverflow = BufferOverflow.DROP_OLDEST)
    /** Failures and confirmations worth telling the user about, in words for this page. */
    val messages: SharedFlow<String> = notices.asSharedFlow()
    private val started = MutableSharedFlow<SessionKey>(extraBufferCapacity = 1, onBufferOverflow = BufferOverflow.DROP_OLDEST)
    /**
     * The conversation a "Run now" started, for the page to open. Without replay: a run that finishes after the page
     * was left is not kept for later, so nothing pulls the user away from where they went (it is in the history).
     */
    val runsToOpen: SharedFlow<SessionKey> = started.asSharedFlow()
    private val writes = Mutex()
    private var tokens = 0L

    /** Work that should not stop when the page does (a run the user asked for, a delete, a toggle). */
    fun launch(block: suspend ProactiveStore.() -> Unit): Job = scope.launch { block() }

    suspend fun refresh() {
        mutable.update { it.copy(refreshing = true) }
        try {
            coroutineScope {
                launch { loadTasks() }
                launch { loadRuns() }
                launch { loadMachine() }
            }
        } finally {
            mutable.update { it.copy(refreshing = false) }
        }
    }

    /** Waits for a write in flight, so a slow read cannot put back what that write replaced. */
    suspend fun loadTasks() = writes.withLock { readTasks() }

    private suspend fun readTasks(): List<ScheduledTask>? {
        val result = attempt { api.tasks() }
        result.onSuccess { list -> mutable.update { it.copy(tasks = Loadable(list), readAt = clock()) } }
            .onFailure { error -> mutable.update { it.copy(tasks = it.tasks.failed(loadFailure(say, error))) } }
        return result.getOrNull()
    }

    suspend fun loadRuns() {
        attempt { api.runs() }
            .onSuccess { list -> mutable.update { it.copy(runs = Loadable(list)) } }
            .onFailure { error -> mutable.update { it.copy(runs = it.runs.failed(loadFailure(say, error))) } }
    }

    private suspend fun loadMachine() {
        attempt { api.assistantMachine() }.onSuccess { id -> mutable.update { it.copy(assistantMachine = id.ifBlank { null }) } }
    }

    /** The switch moves at once; the write follows, and a failure puts back what the hub has. */
    fun toggle(id: String, enabled: Boolean) {
        val pending = PendingChange(++tokens, TaskChange.Toggle(id, enabled))
        mutable.update { it.copy(pending = it.pending + pending) }
        scope.launch {
            try {
                commit(pending.change).onFailure { error -> report(R.string.assistant_proactive_not_saved, error) }
            } finally {
                mutable.update { it.copy(pending = it.pending - pending) }
            }
        }
    }

    /** For the editor, which stays open until the hub has the change; the task as the hub has it (null once deleted). */
    suspend fun save(change: TaskChange): Result<ScheduledTask?> {
        val busy = "task:${change.id}"
        mutable.update { it.copy(busy = it.busy + busy) }
        try {
            return commit(change).onFailure { error -> report(if (change is TaskChange.Delete) R.string.assistant_proactive_not_deleted else R.string.assistant_proactive_not_saved, error) }
        } finally {
            mutable.update { it.copy(busy = it.busy - busy) }
        }
    }

    private fun put(task: ScheduledTask) = mutable.update { state ->
        val list = state.tasks.value.orEmpty()
        val next = if (list.any { it.id == task.id }) list.map { if (it.id == task.id) task else it } else list + task
        state.copy(tasks = state.tasks.copy(value = next))
    }

    private fun drop(id: String) = mutable.update { state ->
        state.copy(tasks = state.tasks.copy(value = state.tasks.value?.filterNot { it.id == id }))
    }

    /** One write under the lock; once started it is not cancelled halfway. */
    private suspend fun commit(change: TaskChange): Result<ScheduledTask?> = withContext(NonCancellable) {
        writes.withLock {
            val written: Result<ScheduledTask?> = attempt {
                when (change) {
                    is TaskChange.Add -> api.saveTask(ScheduledTask().applying(change.draft)).also(::put)
                    is TaskChange.Delete -> { api.deleteTask(change.id); drop(change.id); null }
                    is TaskChange.Toggle, is TaskChange.Edit -> {
                        // The latest the hub has (the page hears nothing of changes made elsewhere), so only this
                        // change is written over it; what the page loaded only when that read fails (quietly: the
                        // save that follows says how it went).
                        val latest = attempt { api.tasks() }.getOrNull()
                            ?.also { list -> mutable.update { it.copy(tasks = Loadable(list), readAt = clock()) } }
                        val base = (latest ?: mutable.value.tasks.value)?.firstOrNull { it.id == change.id } ?: throw TaskGoneException()
                        val next = if (change is TaskChange.Toggle) base.copy(enabled = change.enabled, inbox = null)
                            else (change as TaskChange.Edit).let { base.patched(it.from, it.to) }
                        api.saveTask(next).also(::put)
                    }
                }
            }
            if (written.isSuccess) return@withLock written
            // The answer may have been lost on the way back: believe a fresh read over the error.
            val fresh = readTasks()
            if (fresh != null && written.exceptionOrNull() !is TaskGoneException && change.satisfiedBy(fresh))
                Result.success(fresh.firstOrNull { it.id == change.id })
            else written
        }
    }

    suspend fun run(id: String): Result<SessionKey> {
        mutable.update { it.copy(busy = it.busy + "run:$id") }
        val result = try { attempt { api.runTask(id) } } finally { mutable.update { it.copy(busy = it.busy - "run:$id") } }
        result.onFailure { report(R.string.assistant_proactive_run_failed, it) }
        // A run, failed or not, changes the task's last run, its own conversation and the history (a one-off is done).
        scope.launch { loadTasks(); loadRuns() }
        return result
    }

    /** "Run now" from the page: the run goes on after the page is left, but only a page still collecting [runsToOpen] opens it. */
    fun runNow(id: String): Job = scope.launch { run(id).onSuccess { key -> started.tryEmit(key) } }

    /** "New chat next time": the hub forgets the task's own conversation, so the next run starts one. */
    suspend fun fresh(id: String): Result<ScheduledTask> {
        mutable.update { it.copy(busy = it.busy + "fresh:$id") }
        val result = try { attempt { api.freshTask(id) } } finally { mutable.update { it.copy(busy = it.busy - "fresh:$id") } }
        result.onSuccess { task ->
            put(task)
            tell(say.string(R.string.assistant_proactive_fresh_done))
        }.onFailure { report(R.string.assistant_proactive_not_saved, it) }
        return result
    }

    /** [what] is a sentence with the reason as `%1$s` ("Couldn't save: %1$s"). */
    private fun report(@StringRes what: Int, error: Throwable) {
        tell(say.string(what, failureReason(say, error)))
    }

    /** To the page when it is listening; otherwise (it was left while a write went on) to [unheard], not dropped. */
    private fun tell(message: String) {
        if (notices.subscriptionCount.value == 0 || !notices.tryEmit(message)) unheard(message)
    }
}

/**
 * Failures become results. Cancellation of the caller still propagates; the transport's own cancellation (the pairing
 * or route changed under a request) is a failure of this call, so the caller can clear its spinner and say so.
 */
internal suspend inline fun <T> attempt(block: () -> T): Result<T> = try {
    Result.success(block())
} catch (cancelled: CancellationException) {
    currentCoroutineContext().ensureActive()
    Result.failure(cancelled)
} catch (error: Exception) {
    Result.failure(error)
}

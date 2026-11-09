package persona.feature.assistant.proactive

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ScheduledTask
import persona.core.contracts.parseTime
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.feature.assistant.R
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.ZoneId

/** Survives rotation and process death; null until the person changes something. */
private val DraftSaver = Saver<TaskDraft?, Any>(
    save = { draft ->
        draft?.let {
            arrayListOf<Any>(it.title, it.prompt, it.repeat.name, it.at?.toEpochMilli() ?: -1L, it.time, it.weekdays.joinToString(","),
                it.everyMinutes, it.enabled, it.continues, it.permissionMode, it.push)
        }
    },
    restore = { saved ->
        (saved as? List<*>)?.let { values ->
            TaskDraft(
                values[0] as String, values[1] as String, TaskRepeat.valueOf(values[2] as String),
                (values[3] as Long).takeIf { it >= 0 }?.let(Instant::ofEpochMilli), values[4] as String,
                (values[5] as String).split(",").mapNotNull { it.toIntOrNull() }, values[6] as Int, values[7] as Boolean,
                values[8] as Boolean, values[9] as String, values.getOrNull(10) as? String ?: "",
            )
        }
    },
)

private const val HistoryShown = 5

/**
 * One scheduled task (iOS AssistantTaskEditor): name, on / off, when ("Once" / "Daily" / "Every"), what to do, "Run with" (model and
 * permission mode, "Allow all" asks first), "Chat" ("Continue the original chat", the task's own conversation, "New chat next time"), and for an
 * existing one "Run now", its last result, its history and "Delete". Nothing reaches the hub until "Save" / "Add"; leaving with
 * changes asks first.
 */
@Composable
internal fun TaskEditorPage(
    id: String?,
    template: TaskDraft?,
    state: ProactiveState,
    store: ProactiveStore,
    now: Instant,
    zone: ZoneId,
    onClose: () -> Unit,
    onOpenConversation: (SessionKey) -> Unit,
    onAllRuns: (String) -> Unit,
    /** Starts a run on the screen's scope: it carries on, and opens the conversation, even if this page is left. */
    onRunNow: (String) -> Unit,
    onModelConfiguration: ((String) -> Unit)? = null,
) {
    val isNew = id == null
    val features = LocalHubFeatures.current
    val found = id?.let(state::task)
    var known by remember { mutableStateOf<ScheduledTask?>(null) }
    LaunchedEffect(found) { if (found != null) known = found }
    var closing by remember { mutableStateOf(false) }
    // After "Delete" the page slides away showing what it showed, not "already gone".
    val task = found ?: known.takeIf { closing }
    val baseline = task?.draft() ?: template ?: BlankTask
    var edited by rememberSaveable(stateSaver = DraftSaver) { mutableStateOf<TaskDraft?>(null) }
    // The task as it was when the person started changing it: only their changes are written, so what Persona
    // changed meanwhile (it can edit tasks from a conversation) stays.
    var editedFrom by rememberSaveable(stateSaver = DraftSaver) { mutableStateOf<TaskDraft?>(null) }
    val draft = edited ?: baseline
    val dirty = edited?.let { !it.sameAs(editedFrom ?: baseline) } ?: false
    var saving by remember { mutableStateOf(false) }
    var askDiscard by remember { mutableStateOf(false) }
    var askDelete by remember { mutableStateOf(false) }
    var askBypass by remember { mutableStateOf(false) }
    var choosingMode by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val running = id != null && state.running(id)
    val busy = saving || running
    // A one-off has to be in the future (the hub refuses one that is already due); one left from before stays savable.
    val inTime = draft.repeat != TaskRepeat.Once || draft.at?.isAfter(now) == true || (task != null && draft.at == baseline.at)
    val canSave = draft.valid && inTime
    fun edit(next: TaskDraft) {
        if (edited == null) editedFrom = baseline
        edited = next
    }
    fun forget() { edited = null; editedFrom = null }
    fun close() { closing = true; onClose() }
    fun back() { if (!saving) { if (dirty) askDiscard = true else close() } }
    BackHandler(enabled = dirty || saving) { back() }

    fun save() {
        if (busy || !canSave) return
        saving = true
        scope.launch {
            try {
                val change = if (id == null) TaskChange.Add(draft) else TaskChange.Edit(id, editedFrom ?: baseline, draft)
                if (store.save(change).isSuccess) { forget(); close() }
            } finally {
                saving = false
            }
        }
    }
    fun runNow() {
        if (busy || !canSave) return
        // A new task is added first, then run, and the page gives way to its conversation (iOS runNow).
        if (isNew) {
            saving = true
            scope.launch {
                try {
                    store.save(TaskChange.Add(draft)).getOrNull()?.let { added -> forget(); close(); onRunNow(added.id) }
                } finally {
                    saving = false
                }
            }
            return
        }
        val target = task ?: return
        if (!dirty) return onRunNow(target.id)
        // Changes go first, so the run uses what is on screen.
        saving = true
        scope.launch {
            try {
                if (store.save(TaskChange.Edit(target.id, editedFrom ?: baseline, draft)).isSuccess) { forget(); onRunNow(target.id) }
            } finally {
                saving = false
            }
        }
    }

    val say = rememberProactiveText()
    val title = when {
        isNew -> stringResource(R.string.assistant_proactive_new_routine)
        else -> task?.displayTitle(say) ?: stringResource(R.string.assistant_proactive_routine)
    }
    val showBar = task != null || isNew
    JListPage(
        title = title, modifier = Modifier.imePadding(), onBack = ::back,
        bottomBar = if (showBar && (isNew || dirty)) ({
            JBottomBar {
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                    if (!canSave) Text(
                        when {
                            !inTime -> stringResource(R.string.assistant_proactive_pick_time)
                            !draft.timed && draft.repeat == TaskRepeat.Daily -> stringResource(R.string.assistant_proactive_time_format)
                            !draft.timed -> stringResource(R.string.assistant_proactive_pick_time)
                            draft.repeat == TaskRepeat.Once -> stringResource(R.string.assistant_proactive_fill_in_prompt)
                            else -> stringResource(R.string.assistant_proactive_fill_in)
                        },
                        style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2,
                    )
                    JButton(if (isNew) stringResource(R.string.assistant_proactive_add) else stringResource(R.string.assistant_proactive_save), ::save, Modifier.fillMaxWidth(), enabled = canSave && !running,
                        loading = saving, loadingText = stringResource(R.string.assistant_proactive_saving))
                }
            }
        }) else null,
    ) {
        if (!isNew && task == null) {
            when {
                state.tasks.value == null && state.tasks.error != null -> item("error") {
                    JNotice(state.tasks.error, title = stringResource(R.string.assistant_proactive_routine_read_failed), status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                        actionLabel = stringResource(R.string.assistant_proactive_retry), onAction = { scope.launch { store.loadTasks() } })
                }
                state.tasks.value == null -> item("loading") { JLoading() }
                else -> item("gone") {
                    JEmptyState(stringResource(R.string.assistant_proactive_error_no_routine), stringResource(R.string.assistant_proactive_gone_body), icon = PhIcons.Clock,
                        action = { JButton(stringResource(R.string.assistant_proactive_back), ::close, kind = JButtonKind.Secondary) })
                }
            }
            return@JListPage
        }
        if (task?.byAssistant == true) item("by-assistant") {
            JNotice(stringResource(R.string.assistant_proactive_by_assistant_notice), icon = PhIcons.Sparkle)
        }
        item("name") {
            JTextField(draft.title, { edit(draft.copy(title = it)) }, Modifier.padding(top = Tokens.Space.s), label = stringResource(R.string.assistant_proactive_name),
                placeholder = stringResource(if (draft.repeat == TaskRepeat.Once) R.string.assistant_proactive_name_optional else R.string.assistant_proactive_name_placeholder),
                singleLine = true, enabled = !busy)
        }
        item("enabled") {
            Spacer(Modifier.height(Tokens.Space.m))
            JGroupItem(GroupPosition.Single) {
                JSwitchRow(stringResource(R.string.assistant_proactive_enabled), draft.enabled, { edit(draft.copy(enabled = it)) },
                    subtitle = if (draft.enabled) stringResource(R.string.assistant_proactive_enabled_on) else stringResource(R.string.assistant_proactive_enabled_off), enabled = !busy)
            }
        }
        item("when-header") { JSectionHeader(stringResource(R.string.assistant_proactive_when)) }
        item("when") {
            JCard {
                val repeats = TaskRepeat.entries
                JSegmented(
                    listOf(JSegment(stringResource(R.string.assistant_proactive_repeat_once)), JSegment(stringResource(R.string.assistant_proactive_every_day)),
                        JSegment(stringResource(R.string.assistant_proactive_repeat_interval))),
                    repeats.indexOf(draft.repeat), { if (!busy) edit(draft.repeating(repeats[it], now, zone)) }, Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(Tokens.Space.l))
                when (draft.repeat) {
                    TaskRepeat.Once -> OncePicker(draft.at, now, zone, { edit(draft.copy(at = it)) })
                    TaskRepeat.Daily -> {
                        TimeWheel(draft.time, { edit(draft.copy(time = it)) })
                        Spacer(Modifier.height(Tokens.Space.l))
                        WeekdayPicker(draft.weekdays, { edit(draft.copy(weekdays = it)) })
                    }
                    TaskRepeat.Interval -> IntervalPicker(draft.everyMinutes, { edit(draft.copy(everyMinutes = it)) })
                }
            }
            val next = parseTime(task?.nextRun)?.takeIf { !dirty && draft.enabled && task?.enabled == true }
            SectionNote(next?.let { stringResource(R.string.assistant_proactive_next_run, say.moment(it, now, zone), say.until(it, now, zone)) }
                ?: when (draft.repeat) {
                    TaskRepeat.Once -> stringResource(R.string.assistant_proactive_once_note)
                    TaskRepeat.Daily -> stringResource(R.string.assistant_proactive_rule_note, say.sentence(draft.time, draft.weekdays))
                    TaskRepeat.Interval -> stringResource(R.string.assistant_proactive_interval_note, say.every(draft.everyMinutes))
                })
        }
        item("prompt-header") { JSectionHeader(stringResource(R.string.assistant_proactive_what)) }
        item("prompt") {
            JTextField(draft.prompt, { edit(draft.copy(prompt = it)) }, Modifier.fillMaxWidth(),
                placeholder = stringResource(R.string.assistant_proactive_prompt_placeholder), minLines = 6, maxLines = Int.MAX_VALUE, enabled = !busy,
                supportingText = stringResource(R.string.assistant_proactive_prompt_note))
        }
        runsWithSection(draft, isNew, task, busy, onModelConfiguration, onChooseMode = { choosingMode = true })
        // A hub without "Push when done" would not keep it; one already set on the task still shows, to be turned back.
        if (ScheduledTask.PUSH_FEATURE in features || task?.pushesOnlyAsked == true) pushSection(draft, busy, onPush = { edit(draft.pushing(it)) })
        conversationSection(draft, task, busy, state, store, onOpenConversation, onContinues = { edit(draft.copy(continues = it)) })
        if (task == null && !isNew) return@JListPage
        val runs = task?.let { state.runsOf(it.id) }.orEmpty()
        val last = task?.let(state::lastRunOf)
        // The task's own conversation has its own row above.
        val lastKey = last?.conversation?.takeIf { it != task?.ownConversation }
        val lastLine = last?.line(say, now, zone)
        val runRows = 1 + if (lastLine != null) 1 else 0
        item("run-header") { JSectionHeader(stringResource(R.string.assistant_proactive_run)) }
        item("run-now") {
            JGroupItem(GroupPosition.of(0, runRows)) {
                JRow(stringResource(R.string.assistant_proactive_run_now), subtitle = stringResource(when {
                    isNew -> R.string.assistant_proactive_run_now_new
                    dirty -> R.string.assistant_proactive_run_now_dirty
                    else -> R.string.assistant_proactive_run_now_body
                }),
                    leadingIcon = PhIcons.Play, onClick = ::runNow, enabled = !busy && canSave, chevron = false,
                    trailing = if (running) ({ JSpinner(Modifier.size(Tokens.Space.xxl)) }) else null)
            }
        }
        if (lastLine != null) item("last") {
            JGroupItem(GroupPosition.of(1, runRows)) {
                JRow(if (lastKey != null) stringResource(R.string.assistant_proactive_last_result) else stringResource(R.string.assistant_proactive_last_run_title), subtitle = lastLine, subtitleLines = 3,
                    leadingIcon = if (last.failed) PhIcons.WarningCircle else PhIcons.ChatTeardropText,
                    onClick = lastKey?.let { key -> { onOpenConversation(key) } })
            }
        }
        if (task == null) return@JListPage
        if (runs.isNotEmpty()) {
            item("history-header") { JSectionHeader(stringResource(R.string.assistant_proactive_history), count = runs.size) }
            val shown = runs.take(HistoryShown)
            val more = runs.size > HistoryShown
            itemsIndexed(shown, key = { index, run -> "history-$index-${run.id}" }) { index, run ->
                RunRow(run, parseTime(run.at)?.let { say.moment(it, now, zone) } ?: run.displayTitle(say), "",
                    GroupPosition(first = index == 0, last = index == shown.lastIndex && !more), run.conversation(state.assistantMachine), say, onOpenConversation)
            }
            if (more) item("history-more") { MoreRow(pluralStringResource(R.plurals.assistant_proactive_all_times, runs.size, runs.size), GroupPosition(first = false, last = true)) { onAllRuns(task.id) } }
        }
        item("delete") {
            Spacer(Modifier.height(Tokens.Space.xxl))
            JGroupItem(GroupPosition.Single) {
                JRow(stringResource(R.string.assistant_proactive_delete_routine), leadingIcon = PhIcons.Trash, destructive = true, onClick = { askDelete = true }, enabled = !busy, chevron = false)
            }
        }
    }
    if (choosingMode) JActionSheet(
        title = stringResource(R.string.assistant_proactive_permission_mode), onDismiss = { choosingMode = false },
        actions = TaskPermissionModes.map { mode ->
            JAction(stringResource(permissionModeTitle(mode)), if (mode == BypassMode) PhIcons.ShieldWarning else null,
                destructive = mode == BypassMode, checked = draft.permissionMode == mode) {
                if (mode == BypassMode && draft.permissionMode != mode) askBypass = true else edit(draft.copy(permissionMode = mode))
            }
        },
    )
    if (askBypass) JDialog(
        onDismiss = { askBypass = false }, title = stringResource(R.string.assistant_proactive_bypass_title), text = stringResource(R.string.assistant_proactive_bypass_body),
        destructive = true, confirm = stringResource(R.string.assistant_proactive_mode_bypass) to { askBypass = false; edit(draft.copy(permissionMode = BypassMode)) },
        dismiss = stringResource(R.string.assistant_proactive_cancel) to { askBypass = false },
    )
    if (askDiscard) JDialog(
        onDismiss = { askDiscard = false }, title = stringResource(R.string.assistant_proactive_discard_title), text = stringResource(R.string.assistant_proactive_discard_body),
        destructive = true, confirm = stringResource(R.string.assistant_proactive_discard) to { askDiscard = false; forget(); close() },
        dismiss = stringResource(R.string.assistant_proactive_keep_editing) to { askDiscard = false },
    )
    if (askDelete && task != null) JDialog(
        onDismiss = { if (!saving) askDelete = false }, title = stringResource(R.string.assistant_proactive_delete_title, task.displayTitle(say)),
        text = stringResource(R.string.assistant_proactive_delete_body), destructive = true, confirmEnabled = !saving,
        confirm = stringResource(R.string.assistant_proactive_delete) to {
            saving = true
            scope.launch {
                try {
                    if (store.save(TaskChange.Delete(task.id)).isSuccess) close()
                } finally {
                    saving = false
                    askDelete = false
                }
            }
        },
        dismiss = stringResource(R.string.assistant_proactive_cancel) to { askDelete = false },
    )
}

/** "Run with": the task's model (Settings › Models, when the app can open it) and its permission mode. */
private fun androidx.compose.foundation.lazy.LazyListScope.runsWithSection(
    draft: TaskDraft, isNew: Boolean, task: ScheduledTask?, busy: Boolean, onModelConfiguration: ((String) -> Unit)?, onChooseMode: () -> Unit,
) {
    val purpose = task?.id?.let { "task.$it" } ?: "schedule"
    val rows = 1 + if (onModelConfiguration != null) 1 else 0
    item("runs-with-header") { JSectionHeader(stringResource(R.string.assistant_proactive_runs_with)) }
    if (onModelConfiguration != null) item("model") {
        JGroupItem(GroupPosition.of(0, rows)) {
            JRow(stringResource(if (isNew) R.string.assistant_proactive_default_model else R.string.assistant_proactive_task_model),
                leadingIcon = PhIcons.Cpu, onClick = { onModelConfiguration(purpose) }, enabled = !busy)
        }
    }
    item("mode") {
        val bypass = draft.permissionMode == BypassMode
        JGroupItem(GroupPosition.of(rows - 1, rows)) {
            JRow(stringResource(R.string.assistant_proactive_permission_mode), subtitle = stringResource(permissionModeDetail(draft.permissionMode)),
                leadingIcon = if (bypass) PhIcons.ShieldWarning else PhIcons.ShieldCheck, value = stringResource(permissionModeTitle(draft.permissionMode)),
                destructive = bypass, onClick = onChooseMode, enabled = !busy)
        }
        SectionNote(when {
            draft.permissionMode == BypassMode -> stringResource(R.string.assistant_proactive_bypass_note)
            draft.continues -> stringResource(R.string.assistant_proactive_runs_with_note) + stringResource(R.string.assistant_proactive_runs_with_note_continues)
            else -> stringResource(R.string.assistant_proactive_runs_with_note)
        })
    }
}

/**
 * "Push when done" (docs/assistant/assistant.md "Push after running", iOS pushSection): every run, or only what a run sends with notify_user. Only its
 * alerts runs the task in its own conversation, so "Continue the original chat" goes.
 */
private fun androidx.compose.foundation.lazy.LazyListScope.pushSection(draft: TaskDraft, busy: Boolean, onPush: (String) -> Unit) {
    val choices = listOf("", ScheduledTask.PUSH_ASKED)
    item("push-header") { JSectionHeader(stringResource(R.string.assistant_proactive_push)) }
    item("push") {
        JCard {
            JSegmented(
                listOf(JSegment(stringResource(R.string.assistant_proactive_push_every)), JSegment(stringResource(R.string.assistant_proactive_push_asked))),
                if (draft.pushesOnlyAsked) 1 else 0, { if (!busy) onPush(choices[it]) }, Modifier.fillMaxWidth(),
            )
        }
        SectionNote(stringResource(if (draft.pushesOnlyAsked) R.string.assistant_proactive_push_asked_note else R.string.assistant_proactive_push_every_note))
    }
}

/**
 * "Chat" (docs/assistant/assistant.md "Scheduled tasks"): the conversation it came from ("Continue the original chat"), else the task's own one, which
 * the first run starts and the next ones continue; "New chat next time" lets the next run start over.
 */
private fun androidx.compose.foundation.lazy.LazyListScope.conversationSection(
    draft: TaskDraft, task: ScheduledTask?, busy: Boolean, state: ProactiveState, store: ProactiveStore,
    onOpenConversation: (SessionKey) -> Unit, onContinues: (Boolean) -> Unit,
) {
    val origin = task?.origin
    val own = task?.ownConversation
    val rows = buildList {
        if (origin != null && !draft.pushesOnlyAsked) add("continue")
        if (!draft.continues) { add("own"); if (own != null) add("fresh") }
    }
    item("conversation-header") { JSectionHeader(stringResource(R.string.assistant_proactive_conversation)) }
    rows.forEachIndexed { index, row ->
        item("conversation-$row") {
            JGroupItem(GroupPosition.of(index, rows.size)) {
                when (row) {
                    "continue" -> JSwitchRow(stringResource(R.string.assistant_proactive_continues), draft.continues, onContinues,
                        leadingIcon = PhIcons.ArrowBendUpLeft, enabled = !busy)
                    "own" -> JRow(stringResource(R.string.assistant_proactive_same_conversation), leadingIcon = PhIcons.ChatCircle,
                        onClick = own?.let { key -> { onOpenConversation(key) } })
                    else -> {
                        val freshening = task != null && state.freshening(task.id)
                        JRow(stringResource(R.string.assistant_proactive_fresh), leadingIcon = PhIcons.ChatCircleDots, chevron = false,
                            enabled = !busy && !freshening, onClick = { task?.let { t -> store.launch { fresh(t.id) } } },
                            trailing = if (freshening) ({ JSpinner(Modifier.size(Tokens.Space.xxl)) }) else null)
                    }
                }
            }
        }
    }
    item("conversation-note") {
        SectionNote(stringResource(when {
            draft.continues -> R.string.assistant_proactive_conversation_continues_note
            own != null -> R.string.assistant_proactive_conversation_own_note
            else -> R.string.assistant_proactive_conversation_first_note
        }))
    }
}

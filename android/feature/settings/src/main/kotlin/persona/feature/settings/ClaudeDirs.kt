package persona.feature.settings

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import persona.core.contracts.api.ClaudeDir
import persona.core.contracts.api.ClaudeDirsState
import persona.core.contracts.api.MachineManagement
import persona.core.designsystem.*

/**
 * The pure part of "Blocked config directories" (iOS `ClaudeDirsLogic`, web `lib/agent-settings.ts`): what a row says, and the rules
 * after a change. Blocking a row adds its path; unblocking lifts the rule that blocks it (a glob lifts every directory
 * it covers).
 */
internal object ClaudeDirsLogic {
    /** The path as the user reads it: the machine's home as ~. */
    fun shortPath(path: String, home: String?): String =
        if (home.isNullOrEmpty() || !(path == home || path.startsWith("$home/"))) path else "~" + path.removePrefix(home)

    fun samePath(pattern: String, path: String, home: String?) = pattern == path || pattern == shortPath(path, home)

    fun toggled(state: ClaudeDirsState, dir: ClaudeDir): List<String> {
        if (!dir.blocked) return if (dir.path in state.blocked) state.blocked else state.blocked + shortPath(dir.path, state.home)
        val rule = dir.blockedBy?.takeIf { it.isNotBlank() } ?: dir.path
        return state.blocked.filter { it != rule }
    }

    /** Rules that block none of the listed directories: listed on their own, to be deleted. */
    fun looseRules(state: ClaudeDirsState): List<String> = state.blocked.filter { p ->
        state.dirs.none { it.blocked && (it.blockedBy == p || samePath(p, it.path, state.home)) }
    }

    fun cleaned(input: String): String {
        var p = input.trim()
        while (p.length > 1 && p.endsWith("/")) p = p.dropLast(1)
        return p
    }

    enum class Problem { Empty, Relative, Whole, Taken }

    /** Why a typed rule cannot be added (null: it can). */
    fun problem(input: String, existing: List<String>): Problem? {
        val p = cleaned(input)
        return when {
            p.isEmpty() -> Problem.Empty
            !p.startsWith("/") && !p.startsWith("~/") -> Problem.Relative
            p == "~" || p == "/" -> Problem.Whole
            p in existing -> Problem.Taken
            else -> null
        }
    }
}

internal data class ClaudeDirsUiState(
    val machineId: String = "",
    val state: ClaudeDirsState? = null,
    val loading: Boolean = false,
    val error: ResText? = null,
    /** The row being saved (a path, a rule, or "+" for the typed one). */
    val busy: String? = null,
    /** The connection has no such call (demo mode, a test's fake): the section is left out. */
    val unsupported: Boolean = false,
)

/**
 * The state to draw for [machineId]: what was read for another computer is not this computer's folders, so until this
 * computer's own read starts the section shows it reading (iOS draws the section only for its own `ref`, #9276).
 */
internal fun ClaudeDirsUiState.forMachine(machineId: String): ClaudeDirsUiState =
    if (this.machineId == machineId) this else ClaudeDirsUiState(machineId = machineId, loading = true)

internal class ClaudeDirsModel(private val api: MachineManagement) {
    private val mutable = MutableStateFlow(ClaudeDirsUiState())
    val state = mutable.asStateFlow()

    suspend fun load(machineId: String) {
        if (machineId.isBlank()) { mutable.value = ClaudeDirsUiState(); return }
        mutable.update { if (it.machineId == machineId) it.copy(loading = true) else ClaudeDirsUiState(machineId, loading = true) }
        try {
            val dirs = api.claudeDirs(machineId)
            mutable.update { if (it.machineId == machineId) it.copy(state = dirs, error = null) else it }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: UnsupportedOperationException) { mutable.update { if (it.machineId == machineId) it.copy(unsupported = true) else it } }
        catch (error: Exception) {
            mutable.update { if (it.machineId == machineId) it.copy(error = ResText.of(R.string.settings_claude_dirs_load_failed, failureReasonText(error))) else it }
        } finally { mutable.update { if (it.machineId == machineId) it.copy(loading = false) else it } }
    }

    /**
     * Saves [blocked] for [machineId] (the computer now shown). Returns whether it worked (the notice to show is the
     * caller's). Another computer's list must not be sent, and a tap before this computer's own load must no-op (#9276).
     */
    suspend fun save(machineId: String, blocked: List<String>, key: String): Boolean {
        val s = state.value
        if (s.busy != null || machineId.isBlank() || s.machineId != machineId || s.state == null) return false
        mutable.update { it.copy(busy = key) }
        return try {
            val next = api.setBlockedClaudeDirs(machineId, blocked)
            // Another computer was picked while this write was out: keep its rows, and do not toast a success for the old one.
            if (state.value.machineId != machineId) return false
            mutable.update { if (it.machineId == machineId) it.copy(state = next, error = null) else it }
            true
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) {
            mutable.update { if (it.machineId == machineId) it.copy(error = ResText.of(R.string.settings_claude_dirs_save_failed, failureReasonText(error))) else it }
            false
        } finally { mutable.update { if (it.machineId == machineId) it.copy(busy = null) else it } }
    }
}

/**
 * Settings › Claude Code › Blocked config directories (iOS `ClaudeDirsSection`): the shown machine's Claude Code config directories,
 * each with "Block" / "Unblock", and rules typed by hand (a path, or a glob: `~/.automation/` followed by a `*`). A blocked directory is
 * not scanned or remembered and its sessions leave Agents; the files are not touched.
 */
internal fun LazyListScope.claudeDirsSection(
    ui: ClaudeDirsUiState,
    input: String,
    onInput: (String) -> Unit,
    onRetry: () -> Unit,
    onSave: (blocked: List<String>, key: String, done: ResText) -> Unit,
) {
    item("dirs-header") { JSectionHeader(stringResource(R.string.settings_claude_dirs)) }
    ui.error?.let { error ->
        item("dirs-error") {
            JInlineStatus(error.text(), Modifier.padding(vertical = Tokens.Space.s), tone = InlineTone.Failed,
                action = if (ui.state == null) ({ Action(stringResource(R.string.settings_retry_read), enabled = !ui.loading, onClick = onRetry) }) else null)
        }
    }
    val state = ui.state
    if (state == null) {
        if (ui.error == null) item("dirs-loading") { JLoading() }
    } else {
        val loose = ClaudeDirsLogic.looseRules(state)
        val count = state.dirs.size + loose.size + 1
        state.dirs.forEachIndexed { index, d ->
            item("dir-${d.path}") {
                JGroupItem(GroupPosition.of(index, count)) {
                    val label = ClaudeDirsLogic.shortPath(d.path, state.home)
                    DirRow(label, dirMeta(d, state.home), d.blocked) {
                        if (!d.isDefault) Action(stringResource(if (d.blocked) R.string.settings_claude_dirs_unblock else R.string.settings_claude_dirs_block), enabled = ui.busy == null) {
                            onSave(ClaudeDirsLogic.toggled(state, d), d.path,
                                if (d.blocked) ResText.of(R.string.settings_claude_dirs_unblocked, d.blockedBy?.takeIf { it.isNotBlank() } ?: label)
                                else ResText.of(R.string.settings_claude_dirs_blocked, label))
                        }
                    }
                }
            }
        }
        loose.forEachIndexed { index, p ->
            item("rule-$p") {
                JGroupItem(GroupPosition.of(state.dirs.size + index, count)) {
                    DirRow(p, stringResource(R.string.settings_claude_dirs_rule), blocked = true) {
                        Action(stringResource(R.string.settings_claude_dirs_delete), enabled = ui.busy == null) {
                            onSave(state.blocked.filter { it != p }, p, ResText.of(R.string.settings_claude_dirs_rule_deleted, p))
                        }
                    }
                }
            }
        }
        item("dirs-add") {
            JGroupItem(GroupPosition.of(count - 1, count)) {
                val problem = ClaudeDirsLogic.problem(input, state.blocked)
                fun add() {
                    if (problem != null) return
                    val p = ClaudeDirsLogic.cleaned(input)
                    onSave(state.blocked + p, "+", ResText.of(R.string.settings_claude_dirs_blocked, p))
                }
                Column(Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                    Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                        JTextField(input, onInput, Modifier.weight(1f), placeholder = stringResource(R.string.settings_claude_dirs_placeholder), singleLine = true,
                            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done, autoCorrectEnabled = false), keyboardActions = KeyboardActions(onDone = { add() }))
                        Action(stringResource(R.string.settings_claude_dirs_block), enabled = input.isNotEmpty() && problem == null && ui.busy == null) { add() }
                    }
                    if (input.isNotEmpty() && problem != null) SettingsCaption(stringResource(when (problem) {
                        ClaudeDirsLogic.Problem.Empty -> R.string.settings_claude_dirs_problem_empty
                        ClaudeDirsLogic.Problem.Relative -> R.string.settings_claude_dirs_problem_relative
                        ClaudeDirsLogic.Problem.Whole -> R.string.settings_claude_dirs_problem_whole
                        ClaudeDirsLogic.Problem.Taken -> R.string.settings_claude_dirs_problem_taken
                    }))
                }
            }
        }
    }
    item("dirs-note") {
        Column(Modifier.padding(horizontal = Tokens.Space.xs, vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            SettingsCaption(stringResource(R.string.settings_claude_dirs_note))
            if (state?.live == false) SettingsCaption(stringResource(R.string.settings_claude_dirs_offline))
        }
    }
}

/** Under the path: where it came from, how many sessions, how many claudes run from it, the rule blocking it. */
@Composable
private fun dirMeta(d: ClaudeDir, home: String?): String = buildList {
    add(stringResource(when {
        d.isDefault -> R.string.settings_claude_dirs_default
        d.source == "config" -> R.string.settings_claude_dirs_config
        else -> R.string.settings_claude_dirs_found
    }))
    if (d.sessions > 0) add(pluralStringResource(R.plurals.settings_claude_dirs_sessions, d.sessions, d.sessions))
    if (d.running > 0) add(pluralStringResource(R.plurals.settings_claude_dirs_running, d.running, d.running))
    val by = d.blockedBy
    if (d.blocked && !by.isNullOrBlank() && !ClaudeDirsLogic.samePath(by, d.path, home)) add(stringResource(R.string.settings_claude_dirs_blocked_by, by))
}.joinToString(" · ")

@Composable
private fun DirRow(title: String, meta: String, blocked: Boolean, trailing: @Composable () -> Unit) {
    val palette = PersonaTheme.palette
    Row(Modifier.fillMaxWidth().padding(horizontal = rowPadding, vertical = Tokens.Space.m),
        verticalAlignment = androidx.compose.ui.Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
            Text(title, style = PersonaTheme.type.code.copy(textDecoration = if (blocked) TextDecoration.LineThrough else null),
                color = if (blocked) palette.ink3 else palette.ink, maxLines = 1, overflow = TextOverflow.MiddleEllipsis)
            Text(meta, style = PersonaTheme.type.caption, color = palette.ink2)
        }
        trailing()
    }
}

/** Loads the directories whenever the shown machine changes; saving says what changed through the shell's notice. */
@Composable
internal fun rememberClaudeDirs(management: MachineManagement, machineId: String): Pair<ClaudeDirsModel, ClaudeDirsUiState> {
    val model = remember(management) { ClaudeDirsModel(management) }
    val state by model.state.collectAsState()
    LaunchedEffect(machineId) { model.load(machineId) }
    return model to state
}

@Composable
internal fun claudeDirsSaver(model: ClaudeDirsModel, machineId: () -> String, onDone: () -> Unit): (List<String>, String, ResText) -> Unit {
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    val res = androidx.compose.ui.platform.LocalResources.current
    return { blocked, key, done ->
        scope.launch {
            if (model.save(machineId(), blocked, key)) {
                feedback?.notice(done.resolve(res))
                onDone()
            }
        }
    }
}

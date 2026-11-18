package persona.feature.settings

import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import persona.core.contracts.Machine
import persona.core.contracts.api.MachineManagement
import persona.core.designsystem.*

/**
 * UI state of Settings › Claude Code › Live Replies (iOS `LiveRepliesModel`):
 * - [on] is null until the hub answers.
 * - [unsupported] is true when the hub has no such route (404 / older hub): the section is hidden.
 * - [loadError] holds why reading failed when [on] is still null; the section offers Retry.
 * - [error] holds why the last save failed, one line per computer: this one and/or the other online ones.
 */
internal data class LiveRepliesUiState(
    val machineId: String = "",
    val on: Boolean? = null,
    val loading: Boolean = false,
    val saving: Boolean = false,
    val unsupported: Boolean = false,
    val loadError: ResText? = null,
    val error: ResText? = null,
)

/**
 * Settings › Claude Code › Live Replies (#1309, #9178; iOS `LiveRepliesModel`, web `LiveReplies.tsx`):
 * whether the Claude Code sessions Persona starts on a computer send their replies while Claude writes them.
 */
internal class LiveRepliesModel(private val api: MachineManagement) {
    private val mutable = MutableStateFlow(LiveRepliesUiState())
    val state = mutable.asStateFlow()

    /** Reads the setting for [machineId]; a 404 (null) hides the section, and a failure while [on] is already shown keeps it. */
    suspend fun load(machineId: String) {
        if (machineId.isBlank()) {
            mutable.value = LiveRepliesUiState()
            return
        }
        mutable.update { if (it.machineId == machineId) it.copy(loading = true) else LiveRepliesUiState(machineId = machineId, loading = true) }
        try {
            val value = api.claudeLiveReplies(machineId)
            mutable.update {
                if (it.machineId != machineId) it
                else if (value == null) it.copy(on = null, unsupported = true, loadError = null)
                else it.copy(on = value, unsupported = false, loadError = null)
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: UnsupportedOperationException) {
            mutable.update { if (it.machineId == machineId) it.copy(unsupported = true, loadError = null) else it }
        } catch (error: Exception) {
            // A read that fails with the switch already shown keeps the switch rather than replacing it with Retry.
            mutable.update {
                if (it.machineId == machineId && it.on == null) {
                    it.copy(loadError = ResText.of(R.string.settings_live_replies_load_failed, failureReasonText(error)))
                } else {
                    it
                }
            }
        } finally {
            mutable.update { if (it.machineId == machineId) it.copy(loading = false) else it }
        }
    }

    /**
     * Shows [next] immediately and writes it to the current computer and to [others] (when "Change all online machines"
     * is on). A failure on the shown computer restores the old value; every failure is named, one per line, under the
     * switch. [shownName] words the shown computer in that list. Returns the notice to show when every target succeeded.
     */
    suspend fun set(next: Boolean, others: List<Machine> = emptyList(), shownName: String? = null): ResText? {
        val current = state.value
        val machineId = current.machineId
        val before = current.on ?: return null
        if (before == next || current.saving || current.unsupported || machineId.isBlank()) return null
        mutable.update { it.copy(on = next, saving = true, error = null) }
        val otherTargets = others.filter { it.id != machineId }.distinctBy { it.id }
        val targets = listOf(machineId) + otherTargets.map { it.id }
        val failed = mutableListOf<ResText>()
        try {
            for (id in targets) {
                try {
                    val kept = api.setClaudeLiveReplies(id, next)
                    if (id == machineId) {
                        mutable.update { if (it.machineId == machineId) it.copy(on = kept) else it }
                    }
                } catch (cancelled: CancellationException) {
                    if (id == machineId) {
                        mutable.update { if (it.machineId == machineId) it.copy(on = before) else it }
                    }
                    throw cancelled
                } catch (error: Exception) {
                    val reason = failureReasonText(error)
                    if (id == machineId) mutable.update { if (it.machineId == machineId) it.copy(on = before) else it }
                    val name = if (id == machineId) shownName else otherTargets.firstOrNull { it.id == id }?.displayName
                    failed += ResText.of(R.string.settings_src_name_reason, name ?: id, reason)
                }
            }
            if (failed.isEmpty()) {
                return if (targets.size > 1) ResText.plural(R.plurals.settings_src_claude_applied, targets.size, targets.size)
                else ResText.of(R.string.settings_src_saved)
            }
            mutable.update {
                if (it.machineId == machineId) it.copy(error = ResText.of(R.string.settings_src_claude_not_all_saved, failed.toList())) else it
            }
            return null
        } finally {
            mutable.update { if (it.machineId == machineId) it.copy(saving = false) else it }
        }
    }
}

/**
 * The state to draw for [machineId]: what was read for another computer is not this computer's switch, so until this
 * computer's own read starts the section shows it reading (iOS draws the section only when `live.ref == ref`).
 */
internal fun LiveRepliesUiState.forMachine(machineId: String): LiveRepliesUiState =
    if (this.machineId == machineId) this else LiveRepliesUiState(machineId = machineId, loading = true)

/**
 * Settings › Claude Code › Live Replies (iOS `LiveRepliesSection`): the switch for the computer picked at the top
 * of the page, with what it does and what it costs.
 */
internal fun LazyListScope.liveRepliesSection(
    ui: LiveRepliesUiState,
    onToggle: (Boolean) -> Unit,
    onRetry: () -> Unit,
) {
    item("live-replies-header") { JSectionHeader(stringResource(R.string.settings_live_replies)) }
    val on = ui.on
    if (on != null) {
        item("live-replies-switch") {
            JGroupItem(GroupPosition.Single) {
                JSwitchRow(
                    title = stringResource(R.string.settings_live_replies_toggle),
                    checked = on,
                    onCheckedChange = onToggle,
                    modifier = Modifier.testTag("live-replies"),
                    subtitle = stringResource(if (on) R.string.settings_live_replies_on else R.string.settings_live_replies_off),
                    enabled = !ui.saving,
                    subtitleLines = Int.MAX_VALUE,
                )
            }
        }
        ui.error?.let { error ->
            item("live-replies-error") {
                JInlineStatus(error.text(), Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed)
            }
        }
    } else if (ui.loadError != null) {
        item("live-replies-load-error") {
            JInlineStatus(
                ui.loadError.text(),
                Modifier.padding(vertical = Tokens.Space.s),
                tone = InlineTone.Failed,
                action = { Action(stringResource(R.string.settings_retry_read), enabled = !ui.loading, onClick = onRetry) },
            )
        }
    } else {
        item("live-replies-loading") { JLoading() }
    }
    item("live-replies-note") {
        SettingsCaption(
            stringResource(R.string.settings_live_replies_note),
            Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s),
        )
    }
}

/** Loads the Live Replies switch whenever the shown machine changes. */
@Composable
internal fun rememberLiveReplies(management: MachineManagement, machineId: String): Pair<LiveRepliesModel, LiveRepliesUiState> {
    val model = remember(management) { LiveRepliesModel(management) }
    val state by model.state.collectAsState()
    LaunchedEffect(machineId) { model.load(machineId) }
    return model to state
}

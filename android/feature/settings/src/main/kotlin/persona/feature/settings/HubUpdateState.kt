package persona.feature.settings

import android.content.res.Resources
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalResources
import persona.core.contracts.api.HubUpdateRecord
import persona.core.contracts.api.HubUpdateStatus
import persona.core.contracts.api.HubUpdates
import persona.core.contracts.api.HubUpdatesUnsupported
import persona.core.contracts.parseTime
import persona.core.designsystem.JLoadable
import persona.core.designsystem.failureReasonText
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import java.time.Instant

internal data class HubUpdateState(
    val status: HubUpdateStatus? = null,
    val history: List<HubUpdateRecord> = emptyList(),
    /** The hub predates updates in the app (no /api/v1/hub/update). */
    val unsupported: Boolean = false,
    val checking: Boolean = false,
    /** A start or settings call is on its way: its buttons wait. */
    val sending: Boolean = false,
    /** The status is being read (the first load, a retry, or the connection coming back). */
    val loading: Boolean = false,
    /** Reading the status failed: the page's whole error when it has no status, a line over the stale one when it has. */
    val error: SettingsUiText? = null,
    /** The last action that failed, for a notice over the groups. */
    val actionError: SettingsUiText? = null,
)

/**
 * Settings › Hub version (iOS `HubUpdateModel`, the web's `HubSettings`): reads the status and the history, applies every newer
 * status (an answer here or a `hub_update` push), and reads the history again when an update ends.
 */
internal class HubUpdateModel(private val api: HubUpdates) {
    private val mutable = MutableStateFlow(HubUpdateState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        mutable.update { it.copy(loading = true) }
        try {
            read()
        } finally {
            mutable.update { it.copy(loading = false) }
        }
    }

    private suspend fun read() {
        try {
            val before = state.value.status
            val read = api.status()
            // A push that came while asking is newer than the answer: keep it.
            if (state.value.status === before) apply(read)
            val history = try { api.history() } catch (cancelled: CancellationException) { throw cancelled } catch (_: Exception) { state.value.history }
            mutable.update { it.copy(history = history, error = null, unsupported = false) }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: HubUpdatesUnsupported) {
            mutable.update { it.copy(unsupported = true) }
        } catch (error: Exception) {
            mutable.update { it.copy(error = SettingsUiText.Res(R.string.settings_hub_update_read_failed, listOf(failureReasonText(error)))) }
        }
    }

    /** A newer status; true when it ends an update (the history has a new line). */
    fun apply(status: HubUpdateStatus): Boolean {
        val finished = HubUpdateText.finished(state.value.status?.state, status.state)
        mutable.update { it.copy(status = status, unsupported = false, error = null) }
        return finished
    }

    /** [apply], then the history again when it ended an update. */
    suspend fun receive(status: HubUpdateStatus) {
        if (apply(status)) reloadHistory()
    }

    private suspend fun reloadHistory() {
        try { mutable.update { it.copy(history = api.history()) } }
        catch (cancelled: CancellationException) { throw cancelled } catch (_: Exception) { }
    }

    suspend fun check() {
        if (state.value.checking) return
        mutable.update { it.copy(checking = true, actionError = null) }
        try { receive(api.check()) } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(actionError = SettingsUiText.Res(R.string.settings_hub_update_check_failed, listOf(failureReasonText(error)))) } }
        finally { mutable.update { it.copy(checking = false) } }
    }

    /** Starts the update; [now] is the confirmation's "Update now" (it does not wait for idle). */
    suspend fun update(now: Boolean) = send(R.string.settings_hub_update_start_failed) { api.start(now) }

    suspend fun setAuto(on: Boolean) = send(R.string.settings_hub_update_save_failed) { api.settings(auto = on) }

    /** "Auto-update is on" seen: not shown again. */
    suspend fun noticeSeen() = send(R.string.settings_hub_update_save_failed) { api.settings(noticeSeen = true) }

    /** [failed] is a string resource taking the reason. */
    private suspend fun send(@StringRes failed: Int, call: suspend () -> HubUpdateStatus) {
        if (state.value.sending) return
        mutable.update { it.copy(sending = true, actionError = null) }
        try { receive(call()) } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(actionError = SettingsUiText.Res(failed, listOf(failureReasonText(error)))) } }
        finally { mutable.update { it.copy(sending = false) } }
    }
}

/**
 * The page's [JLoadable]: the status with the checking flag and a failed read over it, the failure when there is no status,
 * or loading. A read in progress hides the failure it is retrying (a retry shows the spinner, not the old error);
 * [error] is that failure's words in the page's language.
 */
internal fun HubUpdateState.loadable(error: String?): JLoadable<HubUpdateStatus> = when {
    status != null -> JLoadable.Loaded(status, refreshing = checking, failure = error.takeUnless { loading })
    error != null && !loading -> JLoadable.Failed(error)
    else -> JLoadable.Loading
}

/** What the "Update" group shows (the web's `updateView`). */
internal sealed interface HubUpdateView {
    data object Docker : HubUpdateView
    data class Cannot(val why: String) : HubUpdateView
    data class Newer(val working: Boolean) : HubUpdateView
    data object Working : HubUpdateView
    data object Current : HubUpdateView
}

/** A line to read and, under it, what to copy (a Docker hub's steps). */
internal data class CopyStep(val text: String, val copy: String)

/**
 * The page's words, the same as iOS `HubUpdateStatus` / `HubSettingsPage` and the web's `lib/hub-update.ts`, in the
 * language of [Resources] (the page passes its own).
 */
internal object HubUpdateText {
    const val DockerCommand = "docker compose pull && docker compose up -d"

    fun finished(previous: String?, next: String): Boolean = previous != next && next in setOf("updated", "rolled_back", "failed")

    fun channel(channel: String, resources: Resources): String = when (channel) {
        "release" -> resources.getString(R.string.settings_hub_update_channel_release)
        "docker" -> "Docker"
        else -> resources.getString(R.string.settings_hub_update_channel_dev)
    }

    fun state(s: HubUpdateStatus, resources: Resources): String = when (s.state) {
        "checking" -> resources.getString(R.string.settings_hub_update_state_checking)
        "downloading" -> resources.getString(R.string.settings_hub_update_state_downloading)
        "waiting_idle" -> if (s.busy.isEmpty()) resources.getString(R.string.settings_hub_update_state_waiting_idle)
            else resources.getQuantityString(R.plurals.settings_hub_update_state_waiting_busy, s.busy.size, s.busy.size)
        "restarting" -> resources.getString(R.string.settings_hub_update_state_restarting)
        "confirming" -> resources.getString(R.string.settings_hub_update_state_confirming)
        "updated" -> resources.getString(R.string.settings_hub_update_state_updated)
        "rolled_back" -> resources.getString(R.string.settings_hub_update_state_rolled_back)
        "failed" -> resources.getString(R.string.settings_hub_update_state_failed)
        "available" -> resources.getString(R.string.settings_hub_update_state_available)
        else -> resources.getString(R.string.settings_hub_update_state_current)
    }

    /** The settings row's value (iOS `hubSummary`). */
    fun summary(s: HubUpdateStatus?, resources: Resources): String? = when {
        s == null -> null
        s.working -> state(s, resources)
        s.latest?.newer == true -> resources.getString(
            if (s.channel == "docker") R.string.settings_hub_update_new_image else R.string.settings_hub_update_state_available)
        else -> s.version.ifBlank { null }
    }

    @Composable
    fun summary(s: HubUpdateStatus?): String? = summary(s, LocalResources.current)

    fun view(s: HubUpdateStatus, resources: Resources): HubUpdateView = when {
        s.channel == "docker" -> HubUpdateView.Docker
        !s.canUpdate -> HubUpdateView.Cannot(s.whyNot?.takeIf { it.isNotBlank() }
            ?.let { resources.getString(R.string.settings_hub_update_cannot_why, it) }
            ?: resources.getString(R.string.settings_hub_update_cannot))
        s.latest?.newer == true -> HubUpdateView.Newer(s.working)
        s.working -> HubUpdateView.Working
        else -> HubUpdateView.Current
    }

    /** The warning under a newer release: it went back before, or auto-update holds it back. */
    fun latestWarning(s: HubUpdateStatus, resources: Resources): String? {
        val l = s.latest?.takeIf { it.newer } ?: return null
        if (l.bad) return resources.getString(R.string.settings_hub_update_warning_bad)
        if (!l.held.isNullOrBlank() && s.auto) return resources.getString(R.string.settings_hub_update_warning_held, l.held)
        return null
    }

    /** A Docker hub's lines: what to say and what to copy. */
    fun dockerSteps(s: HubUpdateStatus, resources: Resources): Pair<String?, List<CopyStep>> {
        val l = s.latest?.takeIf { it.newer }
            ?: return null to listOf(CopyStep(resources.getString(R.string.settings_hub_update_docker_plain), DockerCommand))
        val image = s.image
        val steps = if (image?.pinned == true) listOf(
            // `pull` alone keeps the version the tag names: the tag changes first.
            CopyStep(resources.getString(R.string.settings_hub_update_docker_env), "${image.name}:${l.version}"),
            CopyStep(resources.getString(R.string.settings_hub_update_docker_then), DockerCommand),
        ) else listOf(CopyStep(resources.getString(R.string.settings_hub_update_docker_run), DockerCommand))
        return resources.getString(R.string.settings_hub_update_docker_new_image, l.version) to steps
    }

    fun record(result: String, resources: Resources): String = resources.getString(when (result) {
        "updated" -> R.string.settings_hub_update_state_updated
        "rolled_back" -> R.string.settings_hub_update_state_rolled_back
        "held" -> R.string.settings_hub_update_record_held
        else -> R.string.settings_hub_update_state_failed
    })

    fun recordTime(r: HubUpdateRecord, resources: Resources, now: Instant = Instant.now()): String? =
        parseTime(r.ended ?: r.started)?.let { ChannelText.ago(resources, it, now) }

    /** "Released 3 hours ago" under a newer release. */
    fun released(s: HubUpdateStatus, resources: Resources, now: Instant = Instant.now()): String? =
        parseTime(s.latest?.released)?.let { resources.getString(R.string.settings_hub_update_released, ChannelText.ago(resources, it, now)) }

    fun lastCheck(s: HubUpdateStatus, resources: Resources, now: Instant = Instant.now()): String? =
        parseTime(s.lastCheck)?.let { ChannelText.ago(resources, it, now) }
}

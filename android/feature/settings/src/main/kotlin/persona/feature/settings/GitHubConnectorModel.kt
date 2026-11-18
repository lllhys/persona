package persona.feature.settings

import android.content.res.Resources
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import persona.core.contracts.api.GitHubAuth
import persona.core.contracts.api.GitHubInboxOptions
import persona.core.contracts.api.GitHubMachine
import persona.core.contracts.api.GitHubMissing
import persona.core.designsystem.failureReasonText

/** What Settings › Connectors › GitHub says (iOS `GitHubConnectorSections` and `GitHubInboxSettings`). */
internal object GitHubConnectorText {
    fun separator(res: Resources): String = res.getString(R.string.settings_gh_list_separator)

    fun names(res: Resources, missing: List<GitHubMissing>): String =
        missing.joinToString(separator(res)) { it.scope }

    /** The line under a computer. A state other than signed in, or a missing scope, is a warning. */
    fun machineSubtitle(res: Resources, machine: GitHubMachine): String = when (machine.state) {
        "signed_in" -> {
            val who = machine.account?.takeIf { it.isNotBlank() }?.let { res.getString(R.string.settings_gh_signed_in_as, it) }
                ?: res.getString(R.string.settings_gh_signed_in)
            val missing = machine.missing.orEmpty()
            if (missing.isEmpty()) who else res.getString(R.string.settings_gh_signed_in_missing, who, names(res, missing))
        }
        "not_signed_in" -> res.getString(R.string.settings_gh_not_signed_in)
        "no_gh" -> res.getString(R.string.settings_gh_no_gh)
        "too_old" -> res.getString(R.string.settings_gh_too_old)
        "offline" -> res.getString(R.string.settings_gh_offline)
        else -> machine.error?.takeIf { it.isNotBlank() }?.let { res.getString(R.string.settings_gh_read_error, it) }
            ?: res.getString(R.string.settings_gh_read_failed)
    }

    fun warns(machine: GitHubMachine): Boolean = machine.state != "signed_in" || !machine.missing.isNullOrEmpty()

    /** What to do about the scopes the token in use lacks. A computer with no name is "This computer". */
    fun missingAdvice(res: Resources, auth: GitHubAuth, missing: List<GitHubMissing>): String {
        val names = names(res, missing)
        if (auth.source == "device") return res.getString(R.string.settings_gh_missing_device, names)
        val machine = auth.machines.firstOrNull { it.id == auth.machine }?.name?.takeIf { it.isNotBlank() }
            ?: res.getString(R.string.settings_gh_this_computer)
        val flags = missing.joinToString(",") { it.scope }
        return res.getString(R.string.settings_gh_missing_machine, machine, names, flags)
    }

    fun ciTitle(res: Resources, ci: String): String? = when (ci) {
        GitHubInboxOptions.CI_OFF -> res.getString(R.string.settings_gh_mode_off)
        GitHubInboxOptions.CI_DEFAULT_BRANCH -> res.getString(R.string.settings_gh_mode_default_branch)
        GitHubInboxOptions.CI_MINE -> res.getString(R.string.settings_gh_mode_mine)
        GitHubInboxOptions.CI_ALL -> res.getString(R.string.settings_gh_mode_all)
        else -> null
    }

    /** The Inbox section's explanation: the CI sentence, then how failures are grouped, then mentions. */
    fun inboxFooter(res: Resources, options: GitHubInboxOptions): String {
        val ci = when (options.ci) {
            GitHubInboxOptions.CI_OFF -> res.getString(R.string.settings_gh_ci_off)
            GitHubInboxOptions.CI_DEFAULT_BRANCH -> res.getString(R.string.settings_gh_ci_default_branch)
            GitHubInboxOptions.CI_ALL -> res.getString(R.string.settings_gh_ci_all)
            else -> res.getString(R.string.settings_gh_ci_mine)
        }
        val branches = options.ignoreBranchesShown.joinToString(separator(res))
        return ci + res.getString(R.string.settings_gh_inbox_failures, branches) + res.getString(R.string.settings_gh_inbox_mentions)
    }
}

/**
 * GitHub's sign-in source on its connector page. Choosing one applies it and leaves the previous method; a failure
 * stays on screen and does not change the source already shown. A second tap while one is in flight does nothing.
 */
internal class GitHubAuthSession(
    private val scope: CoroutineScope,
    private val describe: (Throwable) -> String,
) {
    var auth by mutableStateOf<GitHubAuth?>(null)
        private set
    var choosing by mutableStateOf<String?>(null)
        private set
    var error by mutableStateOf<String?>(null)
        private set
    var loading by mutableStateOf(false)
        private set

    var fetch: (suspend () -> GitHubAuth)? = null
    var apply: (suspend (String?) -> GitHubAuth)? = null
    var onApplied: (suspend () -> Unit)? = null

    private var refreshJob: Job? = null
    /** Answers older than the last refresh or choose are ignored, including a GET that already returned. */
    private var epoch = 0

    fun refresh() {
        val fetch = fetch ?: return
        val ticket = ++epoch
        refreshJob?.cancel()
        refreshJob = scope.launch {
            loading = true
            try {
                val loaded = fetch()
                if (ticket != epoch) return@launch
                auth = loaded
                error = null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                if (ticket == epoch) error = describe(e)
            } finally {
                if (ticket == epoch) loading = false
            }
        }
    }

    /** [machine] null chooses device-code sign-in. The row key while it runs is the machine id, or `"device"`. */
    fun choose(machine: String?) {
        if (choosing != null) return
        val apply = apply ?: return
        epoch++
        refreshJob?.cancel()
        loading = false
        choosing = machine ?: "device"
        scope.launch {
            try {
                auth = apply(machine)
                error = null
                onApplied?.invoke()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = describe(e)
            } finally {
                choosing = null
            }
        }
    }
}

/**
 * GitHub's inbox options while the page edits them. One save runs at a time; a change made during it waits and is
 * saved once, and the same value again does not save. A failure keeps the message until a later save works, and
 * does not put the old options back.
 */
internal class GitHubInboxEditor(
    initial: GitHubInboxOptions,
    private val scope: CoroutineScope,
    private val describe: (Throwable) -> String,
    private val save: suspend (GitHubInboxOptions) -> Unit,
) {
    var options by mutableStateOf(initial)
        private set
    var draft by mutableStateOf("")
    var error by mutableStateOf<String?>(null)
        private set

    private val lock = Any()
    private var running = false
    private var queued: GitHubInboxOptions? = null

    fun setCi(ci: String) {
        if (ci == options.ci) return
        options = options.copy(ci = ci)
        submit(options)
    }

    /** Trims, keeps a repository only when it contains "/" and is not already muted, then saves once. */
    fun add() {
        val repo = draft.trim()
        if (!repo.contains("/") || repo in options.mutedRepos) return
        draft = ""
        options = options.copy(mutedRepos = options.mutedRepos + repo)
        submit(options)
    }

    fun remove(repo: String) {
        if (repo !in options.mutedRepos) return
        options = options.copy(mutedRepos = options.mutedRepos.filterNot { it == repo })
        submit(options)
    }

    private fun submit(next: GitHubInboxOptions) {
        synchronized(lock) {
            if (running) {
                queued = next
                return
            }
            running = true
        }
        scope.launch {
            var current: GitHubInboxOptions? = next
            while (current != null) {
                try {
                    save(current)
                    error = null
                } catch (e: CancellationException) {
                    synchronized(lock) {
                        running = false
                        queued = null
                    }
                    throw e
                } catch (e: Exception) {
                    error = describe(e)
                }
                current = synchronized(lock) {
                    val pending = queued
                    queued = null
                    if (pending == null) running = false
                    pending
                }
            }
        }
    }
}

/** The inbox editor, created once the connector's options have arrived. */
internal class GitHubInboxHolder {
    var editor by mutableStateOf<GitHubInboxEditor?>(null)
}

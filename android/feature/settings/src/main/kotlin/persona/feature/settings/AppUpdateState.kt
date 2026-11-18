package persona.feature.settings

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import persona.core.contracts.api.AppUpdate
import persona.core.contracts.api.AppUpdateLookup
import persona.core.contracts.api.AppUpdates

/** Shared by foreground discovery and Settings. The gate never queues duplicate user actions. */
class AppUpdateState(
    internal val api: AppUpdates,
    private val installed: Long,
    private val clock: () -> Long = System::currentTimeMillis,
    private val lastAttempt: () -> Long = { 0L },
    private val saveAttempt: (Long) -> Unit = {},
    private val dismissed: () -> Long = { 0L },
    private val saveDismissed: (Long) -> Unit = {},
    initialLatest: AppUpdate? = null,
    private val saveLatest: (AppUpdate?) -> Unit = {},
    private val validSource: () -> Boolean = { true },
    private val installedPackage: String? = null,
    private val installedCertificates: Set<String> = emptySet(),
    initialResult: Result? = null,
    private val saveResult: (Result) -> Unit = {},
) {
    enum class Result(val message: Int?) {
        Unknown(null),
        Available(null),
        UpToDate(R.string.settings_update_none),
        NoApk(R.string.settings_update_no_apk),
        PackageMismatch(R.string.settings_update_package_mismatch_detail),
        SignatureMismatch(R.string.settings_update_signature_mismatch_detail),
        Failed(R.string.settings_update_check_failed),
    }

    data class State(
        val latest: AppUpdate? = null,
        val checking: Boolean = false,
        val installing: Boolean = false,
        val message: Int? = null,
        val notice: Boolean = false,
        val lastChecked: Long? = null,
        val result: Result = Result.Unknown,
    )

    private data class Outcome(val latest: AppUpdate?, val result: Result)

    private val gate = Mutex()
    private var attempted = lastAttempt()
    private var hiddenVersion = dismissed()
    private val installScope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.SupervisorJob() + kotlinx.coroutines.Dispatchers.Main.immediate)
    private val restored = initialLatest?.takeIf { it.versionCode > installed }
    private val restoredResult = when {
        initialResult == Result.Failed -> Result.Failed
        restored != null -> Result.Available
        initialLatest != null && initialResult == Result.Available -> Result.UpToDate
        else -> initialResult?.takeUnless { it == Result.Available } ?: Result.Unknown
    }
    private val mutable = MutableStateFlow(State(latest = restored, notice = restored != null && restored.versionCode != hiddenVersion,
        lastChecked = attempted.takeIf { it > 0 }, result = restoredResult))
    val state = mutable.asStateFlow()

    private fun evaluate(lookup: AppUpdateLookup): Outcome {
        val update = when (lookup) {
            AppUpdateLookup.NotPublished -> return Outcome(null, Result.NoApk)
            is AppUpdateLookup.Published -> lookup.update
        } ?: return Outcome(null, Result.UpToDate)
        if (installedPackage != null && update.packageName != null && update.packageName != installedPackage) {
            return Outcome(null, Result.PackageMismatch)
        }
        val certificate = update.certificateSha256?.replace(":", "")?.lowercase()
        val installed = installedCertificates.mapTo(mutableSetOf()) { it.replace(":", "").lowercase() }
        if (certificate != null && installed.isNotEmpty() && certificate !in installed) {
            return Outcome(null, Result.SignatureMismatch)
        }
        return if (update.versionCode > this.installed) Outcome(update, Result.Available)
        else Outcome(null, Result.UpToDate)
    }

    private fun apply(outcome: Outcome, manual: Boolean, installing: Boolean = false) {
        saveLatest(outcome.latest)
        saveResult(outcome.result)
        mutable.update { it.copy(latest = outcome.latest, result = outcome.result,
            notice = outcome.latest != null && (installing || outcome.latest.versionCode != hiddenVersion),
            message = if ((manual || installing) && outcome.latest == null) outcome.result.message else null) }
    }

    private fun recordAttempt(now: Long) {
        attempted = now
        saveAttempt(now)
        mutable.update { it.copy(lastChecked = now) }
    }

    suspend fun check(manual: Boolean = false) {
        if (!gate.tryLock()) return
        try {
            if (!validSource()) return
            val now = clock()
            val previous = attempted
            if (!manual && previous > 0 && now >= previous && now - previous < CHECK_INTERVAL_MS) return
            recordAttempt(now)
            mutable.update { it.copy(checking = true, message = if (manual) null else it.message) }
            try {
                val outcome = evaluate(api.lookup())
                if (!validSource()) return
                apply(outcome, manual)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) {
                if (!validSource()) return
                saveResult(Result.Failed)
                // Automatic failure is deliberately quiet; Settings can report it and retry immediately.
                mutable.update { it.copy(result = Result.Failed, message = if (manual) R.string.settings_update_check_failed else it.message) }
            } finally { mutable.update { it.copy(checking = false) } }
        } finally { gate.unlock() }
    }

    fun dismiss() {
        state.value.latest?.let { hiddenVersion = it.versionCode; saveDismissed(it.versionCode) }
        mutable.update { it.copy(notice = false) }
    }

    fun permissionRequired() { mutable.update { it.copy(message = R.string.settings_update_allow_unknown) } }

    internal fun pendingInstallationCancelled() {
        mutable.update {
            if (!it.installing && it.message in setOf(R.string.settings_update_install_failed, R.string.settings_update_verified))
                it.copy(message = null) else it
        }
    }

    fun startInstall(action: suspend (AppUpdate) -> Unit) {
        installScope.launch { install(action) }
    }

    suspend fun install(action: suspend (AppUpdate) -> Unit) {
        if (!gate.tryLock()) return
        try {
            if (!validSource()) return
            if (state.value.latest == null) return
            recordAttempt(clock())
            mutable.update { it.copy(installing = true, message = null) }
            try {
                // Automatic discovery may be throttled for hours while releases keep advancing.
                val outcome = try { evaluate(api.lookup()) }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (error: Exception) { throw AppUpdateFailure(UpdateStage.Metadata, error) }
                if (!validSource()) return
                apply(outcome, manual = false, installing = true)
                val latest = outcome.latest ?: return
                action(latest)
                mutable.update { it.copy(message = R.string.settings_update_verified) }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) {
                if (!validSource()) return
                val stage = (error as? AppUpdateFailure)?.stage ?: UpdateStage.Install
                if (stage == UpdateStage.Metadata) {
                    saveResult(Result.Failed)
                    mutable.update { it.copy(result = Result.Failed) }
                }
                // Do not log exception messages: transport exceptions can contain credential-bearing URLs.
                android.util.Log.w("AppUpdate", "Update failed: stage=$stage type=${error.cause?.javaClass?.simpleName ?: error.javaClass.simpleName}")
                mutable.update { it.copy(message = stage.message) }
            }
            finally { mutable.update { it.copy(installing = false) } }
        } finally { gate.unlock() }
    }

    companion object { const val CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000L }
}

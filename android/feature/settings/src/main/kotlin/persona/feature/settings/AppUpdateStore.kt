package persona.feature.settings

import android.content.Context
import android.content.pm.PackageManager
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.flow.StateFlow
import org.json.JSONObject
import persona.core.contracts.api.AppUpdate
import persona.core.contracts.api.AppUpdateLookup
import persona.core.contracts.api.AppUpdates
import java.io.OutputStream
import java.security.MessageDigest

/** Keeps discovery, dismissal and cached metadata separate for each paired hub. */
class AppUpdateStore(
    private val context: Context,
    private val updates: AppUpdates,
    val binding: StateFlow<SettingsBinding?>,
) {
    private val states = mutableMapOf<String, AppUpdateState>()
    fun current(identity: String): AppUpdateState = states.getOrPut(identity) {
        val valid = { binding.value?.identity == identity }
        val scoped = object : AppUpdates {
            override suspend fun check(): AppUpdate? {
                check(valid()) { "Update source changed" }
                return updates.check().also { check(valid()) { "Update source changed" } }
            }
            override suspend fun lookup(): AppUpdateLookup {
                check(valid()) { "Update source changed" }
                return updates.lookup().also { check(valid()) { "Update source changed" } }
            }
            override suspend fun download(update: AppUpdate, destination: OutputStream) {
                check(valid()) { "Update source changed" }
                updates.download(update, destination)
                check(valid()) { "Update source changed" }
            }
        }
        persistentAppUpdateState(context, scoped, identity, valid)
    }
}

@Composable
fun rememberAppUpdateState(services: SettingsServices): AppUpdateState {
    val context = LocalContext.current.applicationContext
    val store = services.appUpdateStore
    val binding = store?.binding?.collectAsState()?.value
    return remember(store, binding?.identity, services.updates) {
        store?.current(binding?.identity.orEmpty()) ?: services.appUpdateState ?: persistentAppUpdateState(context, services.updates)
    }
}

internal fun persistentAppUpdateState(context: Context, updates: AppUpdates, identity: String = "default", valid: () -> Boolean = { true }): AppUpdateState {
    val key = MessageDigest.getInstance("SHA-256").digest(identity.toByteArray()).joinToString("") { "%02x".format(it) }
    val prefs = context.getSharedPreferences("app-update-$key", Context.MODE_PRIVATE)
    val latest = try {
        prefs.getString("latest", null)?.let { raw -> JSONObject(raw).let {
            AppUpdate(it.getLong("code"), it.getString("name"), it.getString("url"), it.getString("sha"), it.getString("notes"),
                it.optString("package").ifBlank { null }, it.optString("certificate").ifBlank { null })
        } }
    } catch (error: org.json.JSONException) {
        android.util.Log.w("AppUpdate", "Discarding invalid cached update metadata", error)
        prefs.edit().remove("latest").remove("attempt").remove("result").apply()
        null
    }
    val result = prefs.getString("result", null)?.let { name ->
        runCatching { AppUpdateState.Result.valueOf(name) }.getOrNull()
    }
    return AppUpdateState(updates, context.packageManager.getPackageInfo(context.packageName, 0).longVersionCode,
        lastAttempt = { prefs.getLong("attempt", 0) },
        saveAttempt = { prefs.edit().putLong("attempt", it).apply() },
        dismissed = { prefs.getLong("dismissed", 0) },
        saveDismissed = { prefs.edit().putLong("dismissed", it).apply() },
        initialLatest = latest,
        saveLatest = { update -> prefs.edit().apply {
            if (update == null) remove("latest") else putString("latest", JSONObject().put("code", update.versionCode)
                .put("name", update.versionName).put("url", update.downloadUrl).put("sha", update.sha256).put("notes", update.releaseNotes)
                .put("package", update.packageName).put("certificate", update.certificateSha256).toString())
        }.apply() }, validSource = valid,
        installedPackage = context.packageName, installedCertificates = installedCertificates(context),
        initialResult = result, saveResult = { prefs.edit().putString("result", it.name).apply() })
}

internal fun installedCertificates(context: Context): Set<String> {
    val info = context.packageManager.getPackageInfo(context.packageName, PackageManager.GET_SIGNING_CERTIFICATES)
    val digest = MessageDigest.getInstance("SHA-256")
    return info.signingInfo?.apkContentsSigners.orEmpty().mapTo(mutableSetOf()) {
        digest.digest(it.toByteArray()).joinToString("") { byte -> "%02x".format(byte) }
    }
}

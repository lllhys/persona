package persona.feature.settings

import persona.core.contracts.api.*
import persona.core.hub.hubResolve
import persona.core.hub.hubUrl
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import org.json.JSONObject
import java.io.OutputStream

/** Supply the hub's pinned TLS client. This adapter never creates a permissive trust manager. */
class SettingsHubApi(
    pinnedClient: OkHttpClient,
    private val baseUrl: HttpUrl,
    private val deviceId: String,
    private val token: () -> String,
    private val snapshot: suspend () -> ConnectionDiagnostics,
    private val probe: suspend () -> Unit,
    private val lockScreenTest: suspend (Int) -> Unit,
) : AppUpdates, SettingsDiagnostics {
    private val client = pinnedClient.newBuilder().followRedirects(false).followSslRedirects(false).build()

    override suspend fun check(): AppUpdate? = (lookup() as? AppUpdateLookup.Published)?.update

    override suspend fun lookup(): AppUpdateLookup = withContext(Dispatchers.IO) {
        require(baseUrl.isHttps) { "Updates require the paired TLS origin" }
        val url = baseUrl.hubUrl("/api/v1/app/android/latest")
        client.newCall(request(url)).execute().use { response ->
            if (response.code == 404 || response.code == 204) return@withContext AppUpdateLookup.NotPublished
            check(response.isSuccessful) { "Update metadata request failed" }
            val json = JSONObject(requireNotNull(response.body).string())
            AppUpdateLookup.Published(AppUpdate(
                json.getLong("version_code"), json.getString("version_name"),
                json.optString("download_url").ifBlank { "/api/v1/app/android/apk" }, json.getString("sha256"),
                json.optString("notes", json.optString("release_notes")),
                json.optString("package_name").ifBlank { null }, json.optString("certificate_sha256").ifBlank { null },
            ))
        }
    }

    override suspend fun download(update: AppUpdate, destination: OutputStream) = withContext(Dispatchers.IO) {
        val url = baseUrl.hubResolve(update.downloadUrl) ?: error("Invalid download URL")
        require(url.isHttps) { "Update must use the paired TLS origin" }
        client.newCall(request(url)).execute().use { response ->
            check(response.isSuccessful) { "Update download failed" }
            requireNotNull(response.body).byteStream().use { it.copyTo(destination) }
        }
        Unit
    }

    override suspend fun read(): ConnectionDiagnostics {
        val current = snapshot()
        val sessions = withContext(Dispatchers.IO) {
            val url = baseUrl.hubUrl("/api/v1/devices").newBuilder().addPathSegment(deviceId).addPathSegment("sessions").build()
            client.newCall(request(url)).execute().use { response ->
                check(response.isSuccessful) { "Device sessions request failed" }
                val rows = JSONObject(requireNotNull(response.body).string()).optJSONArray("sessions")
                List(rows?.length() ?: 0) { index ->
                    val row = rows!!.getJSONObject(index)
                    DeviceConnectionSession(row.optString("connected_at"), row.optString("disconnected_at").takeIf(String::isNotBlank), row.optString("reason").takeIf(String::isNotBlank))
                }
            }
        }
        return current.copy(sessions = sessions)
    }
    override suspend fun reprobe(): ConnectionDiagnostics { probe(); return read() }
    override suspend fun scheduleLockScreenTest(delaySeconds: Int) = lockScreenTest(delaySeconds)
    private fun request(url: HttpUrl): Request = Request.Builder().url(url).header("Authorization", "Bearer ${token()}").build()
}

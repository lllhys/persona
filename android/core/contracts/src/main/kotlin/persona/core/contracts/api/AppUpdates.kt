package persona.core.contracts.api

import java.io.OutputStream

data class AppUpdate(
    val versionCode: Long,
    val versionName: String,
    val downloadUrl: String,
    val sha256: String? = null,
    val releaseNotes: String = "",
    val packageName: String? = null,
    val certificateSha256: String? = null,
)

sealed interface AppUpdateLookup {
    data class Published(val update: AppUpdate?) : AppUpdateLookup
    data object NotPublished : AppUpdateLookup
}

interface AppUpdates {
    suspend fun check(): AppUpdate?
    suspend fun lookup(): AppUpdateLookup = AppUpdateLookup.Published(check())
    /** Stream through the same authenticated, pinned TLS transport as the hub. */
    suspend fun download(update: AppUpdate, destination: OutputStream) {
        throw UnsupportedOperationException("Update download is unavailable")
    }
}

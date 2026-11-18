package persona.feature.settings

import android.content.Context
import android.content.pm.PackageManager
import android.os.Bundle
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import java.net.URI

/**
 * The public getting-started page (docs/operations/getting-started.md as published), for people with no hub yet. Its address
 * is a build value (`publish.getting_started_url` in the build config; the app manifest's [MetaKey]), never a literal in
 * the code: a build without it shows no "No hub yet?" link, so there is no dead one (docs/android/android.md "Welcome page").
 */
internal object GettingStarted {
    /** The app manifest's `<meta-data>` that carries the build value. */
    const val MetaKey = "persona.getting_started_url"

    private const val MaxLength = 2048

    /** [raw] as an address to open: https only, a host, no embedded credentials; null for anything else (unset, "unset", http). */
    fun parse(raw: String?): String? {
        val value = raw?.trim().orEmpty()
        if (value.isEmpty() || value.length > MaxLength || value.any { it.isWhitespace() || it.isISOControl() }) return null
        if (!value.startsWith("https://", ignoreCase = true)) return null
        val uri = try { URI(value) } catch (_: Exception) { return null }
        if (uri.host.isNullOrBlank() || uri.userInfo != null) return null
        return value
    }

    fun fromMetaData(metaData: Bundle?): String? = parse(metaData?.getString(MetaKey))

    @Suppress("DEPRECATION")
    fun configured(context: Context): String? = try {
        fromMetaData(context.packageManager.getApplicationInfo(context.packageName, PackageManager.GET_META_DATA).metaData)
    } catch (_: PackageManager.NameNotFoundException) { null }
}

/** This build's getting-started address, null when the build has none. */
@Composable
internal fun rememberGettingStartedUrl(): String? {
    val context = LocalContext.current
    return remember(context) { GettingStarted.configured(context) }
}

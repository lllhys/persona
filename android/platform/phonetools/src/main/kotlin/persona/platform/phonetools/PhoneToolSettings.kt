package persona.platform.phonetools

import androidx.annotation.StringRes
import android.Manifest.permission.*
import android.app.NotificationManager
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat
import androidx.datastore.preferences.core.*
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import persona.core.contracts.HubScope
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import java.io.File

/** One instance per active HubScope; cancel the supplied scope when unpairing. */
class PhoneToolSettings(context: Context, scope: HubScope, coroutineScope: CoroutineScope) {
    private val store = PreferenceDataStoreFactory.create(scope = coroutineScope) {
        File(context.filesDir, "hub-data/${scope.path}/phone-tools.preferences_pb")
    }
    val enabled: StateFlow<Set<PhoneSource>> = store.data.map { values ->
        PhoneSource.entries.filter { values[booleanPreferencesKey(it.name)] == true }.toSet()
    }.stateIn(coroutineScope, SharingStarted.Eagerly, emptySet())
    suspend fun setEnabled(source: PhoneSource, enabled: Boolean) {
        store.edit { it[booleanPreferencesKey(source.name)] = enabled }
    }
}

class PhonePermissions(private val context: Context) {
    fun granted(permission: String) = ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED
    fun permissions(source: PhoneSource): Array<String> = when (source) {
        PhoneSource.Contacts -> arrayOf(READ_CONTACTS, WRITE_CONTACTS)
        PhoneSource.Calendar -> arrayOf(READ_CALENDAR, WRITE_CALENDAR)
        PhoneSource.Location -> arrayOf(ACCESS_COARSE_LOCATION, ACCESS_FINE_LOCATION)
        PhoneSource.Photos -> when {
            Build.VERSION.SDK_INT >= 34 -> arrayOf(READ_MEDIA_IMAGES, READ_MEDIA_VIDEO, READ_MEDIA_VISUAL_USER_SELECTED)
            Build.VERSION.SDK_INT >= 33 -> arrayOf(READ_MEDIA_IMAGES, READ_MEDIA_VIDEO)
            else -> arrayOf(READ_EXTERNAL_STORAGE)
        }
        PhoneSource.Notifications -> if (Build.VERSION.SDK_INT >= 33) arrayOf(POST_NOTIFICATIONS) else emptyArray()
        else -> emptyArray()
    }
    fun allowed(name: String, source: PhoneSource): Boolean = when (source) {
        PhoneSource.Contacts -> granted(READ_CONTACTS) &&
            (name !in PhoneToolCatalog.confirmedWriteNames || granted(WRITE_CONTACTS))
        PhoneSource.Calendar -> granted(READ_CALENDAR) &&
            (name !in PhoneToolCatalog.confirmedWriteNames || granted(WRITE_CALENDAR))
        PhoneSource.Location -> granted(ACCESS_COARSE_LOCATION) || granted(ACCESS_FINE_LOCATION)
        PhoneSource.Photos -> permissions(source).any(::granted)
        PhoneSource.Notifications -> name == "phone_notifications_list" ||
            (permissions(source).all(::granted) && context.getSystemService(NotificationManager::class.java).areNotificationsEnabled() &&
                context.getSystemService(NotificationManager::class.java).getNotificationChannel(PhoneNotifications.CHANNEL)?.importance != NotificationManager.IMPORTANCE_NONE)
        PhoneSource.Health -> true
        else -> permissions(source).all(::granted)
    }
    /** The permission state of [source], as a string resource; [PhoneAccess.Partial] offers to choose again. */
    fun status(source: PhoneSource, healthGranted: Set<String>? = null): PhoneAccess = when {
        source == PhoneSource.Health -> when {
            !Health.available(context) -> PhoneAccess.Unavailable
            healthGranted == null -> PhoneAccess.Granted
            healthGranted.containsAll(Health.permissions) -> PhoneAccess.Granted
            healthGranted.any { it in Health.permissions } -> PhoneAccess.Partial
            else -> PhoneAccess.Denied
        }
        source == PhoneSource.Photos && Build.VERSION.SDK_INT >= 33 &&
            !(granted(READ_MEDIA_IMAGES) && granted(READ_MEDIA_VIDEO)) && allowed("", source) -> PhoneAccess.Partial
        source == PhoneSource.Location && granted(ACCESS_COARSE_LOCATION) && !granted(ACCESS_FINE_LOCATION) -> PhoneAccess.Approximate
        source == PhoneSource.Calendar && granted(READ_CALENDAR) && !granted(WRITE_CALENDAR) -> PhoneAccess.ReadOnly
        source == PhoneSource.Contacts && granted(READ_CONTACTS) && !granted(WRITE_CONTACTS) -> PhoneAccess.ReadOnly
        permissions(source).isEmpty() && source != PhoneSource.Notifications -> PhoneAccess.NotNeeded
        allowed(if (source == PhoneSource.Calendar) "phone_calendar_create_event" else
            if (source == PhoneSource.Contacts) "phone_contacts_create" else "", source) -> PhoneAccess.Granted
        else -> PhoneAccess.Denied
    }

    /** What the tag under a data source says. Health Connect missing says why. */
    fun statusText(source: PhoneSource, access: PhoneAccess): String =
        (if (source == PhoneSource.Health && access == PhoneAccess.Unavailable) Health.unavailableReason(context) else null)
            ?: context.getString(access.label)
}

enum class PhoneAccess(@StringRes val label: Int) {
    Unavailable(R.string.phonetools_status_unavailable),
    Granted(R.string.phonetools_status_granted),
    Partial(R.string.phonetools_status_partial),
    Denied(R.string.phonetools_status_denied),
    Approximate(R.string.phonetools_status_approximate),
    ReadOnly(R.string.phonetools_status_read_only),
    NotNeeded(R.string.phonetools_status_not_needed),
}

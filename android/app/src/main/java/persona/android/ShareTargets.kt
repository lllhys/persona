package persona.android

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import kotlinx.coroutines.flow.first
import persona.core.contracts.SessionKey

/** Where a share went and when (epoch milliseconds); a null [key] is the main conversation. */
internal data class RecentShareTarget(val key: SessionKey?, val at: Long)

/** How many targets are kept; the sheet lists at most [RECENT_SHARE_FIRST] of them ahead of the other Agents. */
internal const val RECENT_SHARE_KEPT = 8
internal const val RECENT_SHARE_FIRST = 3

/** The last target is where the next share starts only this long after it was used. */
internal const val RECENT_SHARE_PRESELECT_MILLIS = 24 * 60 * 60 * 1000L

/** [targets] with [key] in front, used at [now]; each target once, at most [RECENT_SHARE_KEPT]. */
internal fun rememberShareTarget(targets: List<RecentShareTarget>, key: SessionKey?, now: Long): List<RecentShareTarget> =
    (listOf(RecentShareTarget(key, now)) + targets.filter { it.key != key }).take(RECENT_SHARE_KEPT)

/**
 * [rows] with the ones shared to most recently in front (latest first, at most [RECENT_SHARE_FIRST]), the others in
 * their own order. A target that is not among [rows] (gone, or no longer running) takes no place.
 */
internal fun <T> recentFirst(rows: List<T>, key: (T) -> SessionKey, targets: List<RecentShareTarget>): List<T> {
    val byKey = rows.associateBy(key)
    val first = targets.mapNotNull { target -> target.key?.let(byKey::get) }.distinct().take(RECENT_SHARE_FIRST)
    return first + rows.filter { it !in first }
}

/**
 * The target a sheet starts on: the last one, when it was used within [RECENT_SHARE_PRESELECT_MILLIS] and is still
 * listed ([listed]); otherwise the main conversation (null). Sending stays the user's tap.
 */
internal fun preselectedShareTarget(targets: List<RecentShareTarget>, listed: Set<SessionKey>, now: Long): SessionKey? {
    val last = targets.firstOrNull() ?: return null
    if (now - last.at !in 0..RECENT_SHARE_PRESELECT_MILLIS) return null
    return last.key?.takeIf { it in listed }
}

/** One target a line: the time, the machine and the session, tab-separated; the main conversation has both empty. */
internal fun encodeShareTargets(targets: List<RecentShareTarget>): String =
    targets.joinToString("\n") { "${it.at}\t${it.key?.machineId.orEmpty()}\t${it.key?.sessionId.orEmpty()}" }

internal fun decodeShareTargets(text: String?): List<RecentShareTarget> =
    text.orEmpty().lineSequence().mapNotNull { line ->
        val parts = line.split('\t')
        val at = parts.firstOrNull()?.toLongOrNull()
        when {
            parts.size != 3 || at == null -> null
            parts[1].isEmpty() && parts[2].isEmpty() -> RecentShareTarget(null, at)
            parts[1].isEmpty() || parts[2].isEmpty() -> null
            else -> RecentShareTarget(SessionKey(parts[1], parts[2]), at)
        }
    }.toList()

/**
 * The share sheet's recent targets, kept with the lists' settings of the pairing (`ScopedListPreferences`), whose keys
 * are the ones the sheet lists; they go when the pairing does.
 */
internal class ShareTargetStore(private val store: DataStore<Preferences>) {
    private val key = stringPreferencesKey("share-recent-targets")

    suspend fun read(): List<RecentShareTarget> = decodeShareTargets(store.data.first()[key])

    suspend fun remember(target: SessionKey?, now: Long) {
        store.edit { it[key] = encodeShareTargets(rememberShareTarget(decodeShareTargets(it[key]), target, now)) }
    }
}

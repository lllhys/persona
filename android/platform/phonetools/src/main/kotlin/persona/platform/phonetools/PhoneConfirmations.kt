package persona.platform.phonetools

import android.app.*
import android.content.*
import android.net.Uri
import android.os.Build
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap

/**
 * How a confirmation ended (#1875). Only [Denied] is the user saying no: the agent hears "declined" for that alone,
 * and for the others that nobody answered, so it neither tells the user they refused nor saves without asking.
 */
enum class PhoneConfirmation {
    Allowed,
    Denied,
    /** Persona was in the background and could not post the notification (notifications or the channel off). */
    NotShown,
    /** The phone tools closed while it was open or queued (unpaired, disconnected, turned off). */
    Withdrawn,
    /** Its notification was swiped away (or cleared with "Clear all") without an answer: not a refusal (#3861). */
    Dismissed,
}

/**
 * One confirmation card at a time, the rest queue behind it (#4456, parity with iOS `PhoneConfirmations`).
 *
 * The hub owns how long a confirmed write may wait (`phoneConfirmWait`); there is no app-side timeout that
 * withdraws the card. A second write arriving while a card is on screen waits behind it instead of taking its
 * place, and every answer is routed by the token it names: a dialog dismissal that arrives after its button
 * already answered names a card that is answered already, so it does nothing to the next one.
 *
 * `close()` finishes the current card and every queued one as [PhoneConfirmation.Withdrawn]. A cancelled
 * awaiter (the call was dropped because the connection died, a permission was revoked, the runtime is closing)
 * removes its own entry: if it was current, the next one is promoted; if it was still queued, it just leaves.
 *
 * Tokens are valid only while their entry is current or queued. Nothing is persisted.
 */
class PhoneConfirmations(context: Context) : AutoCloseable {
    private data class Entry(
        val token: String,
        val summary: String,
        val deferred: CompletableDeferred<PhoneConfirmation>,
    )

    private val context = context.applicationContext
    private val manager = context.getSystemService(NotificationManager::class.java)
    private val keyguard = context.getSystemService(KeyguardManager::class.java)

    private val lock = Any()
    private val queue = ArrayDeque<Entry>()
    private var current: Entry? = null
    private var closed = false

    private val _pending = MutableStateFlow<PendingPhoneConfirmation?>(null)
    val pending = _pending.asStateFlow()

    @Volatile private var foreground = false

    suspend fun confirm(text: PhoneConfirmationText): PhoneConfirmation {
        val summary = text(context.resources)
        val token = UUID.randomUUID().toString()
        val entry = Entry(token, summary, CompletableDeferred())

        synchronized(lock) {
            if (closed) return PhoneConfirmation.Withdrawn
            callbacks[token] = { resolve(token, it) }
            dismissals[token] = { dismiss(token) }
            queue.addLast(entry)
            if (current == null) promoteLocked()
        }
        // Settle the current card's background notification outside the lock (no-op if this entry is still
        // queued). `NotificationManager.notify` may block on a system call and must not run under the gate.
        notifyCurrent()

        return try {
            entry.deferred.await()
        } catch (cancelled: CancellationException) {
            synchronized(lock) {
                callbacks.remove(token); dismissals.remove(token)
                if (current == entry) {
                    current = null
                    _pending.value = null
                    promoteLocked()
                } else {
                    queue.remove(entry)
                }
            }
            manager.cancel(token, 0)
            // The promoted card (if any) needs its own notification attempt.
            notifyCurrent()
            throw cancelled
        }
    }

    /** The user answered [token]. Allow needs an unlocked device; Deny works from the lock screen. */
    fun resolve(token: String, allowed: Boolean) {
        val entry = synchronized(lock) {
            val e = current ?: return
            if (e.token != token) return
            if (allowed && keyguard.isDeviceLocked) return
            current = null
            _pending.value = null
            callbacks.remove(token); dismissals.remove(token)
            promoteLocked()
            e
        }
        manager.cancel(token, 0)
        entry.deferred.complete(if (allowed) PhoneConfirmation.Allowed else PhoneConfirmation.Denied)
        // Promote-then-notify: the next card only becomes visible through this path.
        notifyCurrent()
    }

    /** The notification of [token] was swiped away: end only that one (#3861). */
    fun dismiss(token: String) {
        val entry = synchronized(lock) {
            val e = current ?: return
            if (e.token != token) return
            current = null
            _pending.value = null
            callbacks.remove(token); dismissals.remove(token)
            promoteLocked()
            e
        }
        manager.cancel(token, 0)
        entry.deferred.complete(PhoneConfirmation.Dismissed)
        notifyCurrent()
    }

    fun setForeground(value: Boolean) {
        foreground = value
        if (value) {
            // Back in the foreground: the dialog takes over from the notification for the current card only.
            val entry = synchronized(lock) { current } ?: return
            manager.cancel(entry.token, 0)
        } else {
            // Going to background: settle the current card's notification. notifyCurrent() walks the queue,
            // ending entries as NotShown one by one until a card can post or the queue is empty.
            notifyCurrent()
        }
    }

    /** Promote the next queued entry to current if the slot is empty. Caller holds [lock]. */
    private fun promoteLocked() {
        if (current != null || closed) return
        val next = queue.removeFirstOrNull() ?: return
        current = next
        _pending.value = PendingPhoneConfirmation(next.token, next.summary)
    }

    /**
     * Settle the current card's background notification, outside the lock. Every path that changes `current`
     * (confirm registering, resolve, dismiss, a cancelled awaiter, setForeground(false)) ends here, so a
     * promoted card always gets its own notification attempt instead of waiting forever with only a StateFlow
     * value. If notifications are unavailable the current entry finishes as [PhoneConfirmation.NotShown] and
     * the next one is promoted, looping until a card can post or the queue drains. In the foreground the
     * in-app dialog is the surface and nothing is posted.
     */
    private fun notifyCurrent() {
        while (true) {
            val entry = synchronized(lock) { if (closed) null else current } ?: return
            if (foreground) return
            if (showNotification(entry)) return
            val promoted = synchronized(lock) {
                if (closed || current != entry) return
                current = null
                _pending.value = null
                callbacks.remove(entry.token); dismissals.remove(entry.token)
                promoteLocked()
                true
            }
            if (!promoted) return
            manager.cancel(entry.token, 0)
            entry.deferred.complete(PhoneConfirmation.NotShown)
            // Loop: the newly promoted card also needs its own notification attempt.
        }
    }

    private fun showNotification(entry: Entry): Boolean {
        // A resolve/dismiss/close that landed between the promote and this call must not resurrect the card:
        // pretend success so the caller does not then fail an entry that is already finished.
        val isCurrent = synchronized(lock) { current == entry && !closed }
        if (!isCurrent) return true
        return try {
            manager.createNotificationChannel(NotificationChannel(CHANNEL, context.getString(R.string.phonetools_confirm_channel), NotificationManager.IMPORTANCE_HIGH))
            if (!manager.areNotificationsEnabled() || manager.getNotificationChannel(CHANNEL).importance == NotificationManager.IMPORTANCE_NONE) return false
            fun action(allowed: Boolean): Notification.Action {
                val intent = Intent(context, PhoneConfirmationReceiver::class.java)
                    .setAction("persona.phone.confirm").setData(Uri.parse("persona-phone://confirmation/${entry.token}/$allowed"))
                    .putExtra("token", entry.token).putExtra("allowed", allowed)
                val pendingIntent = PendingIntent.getBroadcast(context, 0, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
                val builder = Notification.Action.Builder(null, context.getString(if (allowed) R.string.phonetools_allow else R.string.phonetools_deny), pendingIntent)
                // setAuthenticationRequired is API 31; calling it on API 30 throws NoSuchMethodError (an Error, so the
                // catch below misses it) and no confirmation ever shows. On API 30 the system does not ask for an unlock
                // before sending the button, so allow can be tapped straight from the lock screen; resolve() and the
                // receiver still refuse it while the device is locked, so that tap is dropped (the notification stays):
                // unlock, then tap allow or open the app. Trade-off: one more step on API 30 instead of no confirmation.
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) builder.setAuthenticationRequired(allowed)
                return builder.build()
            }
            val builder = Notification.Builder(context, CHANNEL).setSmallIcon(persona.core.designsystem.R.drawable.ic_stat)
                .setContentTitle(context.getString(R.string.phonetools_confirm_notification)).setContentText(entry.summary)
                .setStyle(Notification.BigTextStyle().bigText(entry.summary)).setVisibility(Notification.VISIBILITY_PRIVATE)
                .setCategory(Notification.CATEGORY_RECOMMENDATION)
                .addAction(action(false)).addAction(action(true))
                // #3861: a swipe or "Clear all" ends the confirmation at once instead of waiting for an answer that
                // will never come. The hub's `phoneConfirmWait` decides how long the call may still be answered.
                .setDeleteIntent(PendingIntent.getBroadcast(context, 0, Intent(context, PhoneConfirmationReceiver::class.java)
                    .setAction("persona.phone.confirm").setData(Uri.parse("persona-phone://confirmation/${entry.token}/dismissed"))
                    .putExtra("token", entry.token).putExtra(DISMISSED, true),
                    PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
            context.packageManager.getLaunchIntentForPackage(context.packageName)?.let { intent ->
                builder.setContentIntent(PendingIntent.getActivity(context, 0, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
            }
            beforeNotify?.invoke(entry.token)
            manager.notify(entry.token, 0, builder.build())
            // A resolve/dismiss/close that landed while the notification was being built and posted must not
            // leave the answered card's notification on screen: the answer path already ran `cancel` before
            // the post landed, so cancel again now that the post has happened. The token is already gone from
            // `callbacks`/`dismissals`, so even if the user somehow taps it nothing answers the wrong card.
            val stillCurrent = synchronized(lock) { current == entry && !closed }
            if (!stillCurrent) manager.cancel(entry.token, 0)
            true
        } catch (_: Exception) { false }
    }

    override fun close() {
        val all = synchronized(lock) {
            if (closed) return
            closed = true
            val list = buildList {
                current?.let(::add)
                addAll(queue)
            }
            queue.clear()
            current = null
            _pending.value = null
            for (e in list) {
                callbacks.remove(e.token); dismissals.remove(e.token)
            }
            list
        }
        for (e in all) {
            manager.cancel(e.token, 0)
            e.deferred.complete(PhoneConfirmation.Withdrawn)
        }
    }

    companion object {
        /** Extra on the notification's delete intent: it was dismissed, not answered. */
        internal const val DISMISSED = "dismissed"
        internal const val CHANNEL = "phone_tools_confirmation"
        internal val callbacks = ConcurrentHashMap<String, (Boolean) -> Unit>()
        internal val dismissals = ConcurrentHashMap<String, () -> Unit>()
        /** Test seam: invoked with the token right before `NotificationManager.notify` posts a confirmation.
         *  Lets a test resolve or close that entry mid-post to exercise the resurrect-then-cancel path in
         *  `showNotification`. Always null in production. */
        internal var beforeNotify: ((String) -> Unit)? = null
    }
}

data class PendingPhoneConfirmation(val token: String, val summary: String)

class PhoneConfirmationReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action !in persona.core.contracts.ProtocolCompatibility.phoneConfirmationActions) return
        val token = intent.getStringExtra("token") ?: return
        if (intent.getBooleanExtra(PhoneConfirmations.DISMISSED, false)) { PhoneConfirmations.dismissals[token]?.invoke(); return }
        val allowed = intent.getBooleanExtra("allowed", false)
        if (!allowed) { PhoneConfirmations.callbacks[token]?.invoke(false); return }
        // "Allow" needs an unlock, and the lock screen reports it a moment after it let the button through.
        val keyguard = context.getSystemService(KeyguardManager::class.java)
        val pending = goAsync()
        CoroutineScope(SupervisorJob() + Dispatchers.Main).launch {
            try { if (!lockedAfterUnlockSettles({ keyguard.isDeviceLocked })) PhoneConfirmations.callbacks[token]?.invoke(true) }
            finally { pending.finish() }
        }
    }
}

/**
 * Whether the phone is still locked once an unlock in progress has had [settleMillis] to land. SystemUI sends a
 * notification button that needs authentication as soon as the user's PIN is accepted, a moment before KeyguardManager
 * stops reporting the device as locked: read at once, "Allow" tapped on the lock screen was dropped and the call declined
 * when the confirmation timed out (docs/lessons/android.md; the same as :platform:notify's).
 */
internal suspend fun lockedAfterUnlockSettles(
    locked: () -> Boolean,
    settleMillis: Long = UNLOCK_SETTLE_MILLIS,
    pollMillis: Long = 100,
): Boolean {
    if (!locked()) return false
    return withTimeoutOrNull(settleMillis) {
        while (locked()) delay(pollMillis)
        false
    } ?: true
}

/** How long an unlock that let a button through may take to show (config value, not a measurement). */
internal const val UNLOCK_SETTLE_MILLIS = 3_000L

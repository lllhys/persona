package persona.platform.notify

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.core.app.NotificationCompat
import androidx.core.app.RemoteInput
import persona.core.contracts.NotifyEvent
import persona.core.contracts.api.EXTENSION_NOTIFICATION_REMOTE_INPUT
import persona.core.contracts.api.ExtensionNotificationActionRecord
import persona.core.contracts.api.ExtensionNotificationActionStore
import persona.core.contracts.generated.ExtensionNotification
import persona.core.contracts.generated.ExtensionNotifyAction

/** Revalidates the generated model before an alert becomes a page destination or an executable action. */
object ExtensionNotificationPayload {
    fun validate(payload: ExtensionNotification): ExtensionNotification? {
        val actions = payload.actions.orEmpty()
        if (!payload.extension.safeSegment() || payload.ui?.safeSegment() == false || payload.state.utf8Size() > 16_384 ||
            actions.size > 3 || (actions.isNotEmpty() && payload.ui == null) ||
            actions.map { it.id }.let { it.size != it.toSet().size }) return null
        val validated = actions.map { action ->
            if (action.id.isEmpty() || action.id.codePointCount(0, action.id.length) > 128 || action.id in ReservedActionIds ||
                action.title.isEmpty() || action.title.codePointCount(0, action.title.length) > 40 ||
                action.role !in setOf(null, "normal", "destructive") || action.input?.placeholder.utf8Size() > 16_384) return null
            val title = action.title.singleLine()
            if (title.isEmpty()) return null
            action.copy(title = title, input = action.input?.let { input ->
                input.copy(placeholder = input.placeholder?.singleLine()?.takeIf { it.isNotEmpty() })
            })
        }
        return payload.copy(actions = payload.actions?.let { validated })
    }

    private val ReservedActionIds = setOf("default", "dismiss",
        "com.apple.UNNotificationDefaultActionIdentifier", "com.apple.UNNotificationDismissActionIdentifier")
}

/** Every PendingIntent has a distinct data URI; extras and another button's RemoteInput can never overwrite it. */
internal class ExtensionNotificationIntents(private val context: Context) {
    fun activityIntent(record: ExtensionNotificationActionRecord, title: String, fromAction: Boolean = false): Intent {
        val link = Uri.Builder().scheme("persona").authority("open")
            .appendQueryParameter("target", "extension")
            .appendQueryParameter("extension", record.extension)
            .appendQueryParameter("manager", record.scope.manager)
            .appendQueryParameter("trigger", if (fromAction) "action" else "content")
        record.ui?.let { ui ->
            link.appendQueryParameter("ui", ui).appendQueryParameter("title", title)
            record.state?.let { link.appendQueryParameter("state", it) }
            link.appendQueryParameter("notification", record.token)
        }
        return Intent(Intent.ACTION_VIEW, link.build()).setPackage(context.packageName)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
    }

    fun broadcastIntent(record: ExtensionNotificationActionRecord): Intent =
        Intent(context, ExtensionNotificationActionReceiver::class.java).setAction(ExtensionNotificationActionReceiver.ACTION)
            .setData(Uri.Builder().scheme("persona-notify").authority("extension")
                .appendQueryParameter("token", record.token).build())

    fun contentIntent(record: ExtensionNotificationActionRecord, title: String): PendingIntent =
        PendingIntent.getActivity(context, 0, activityIntent(record, title),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)

    fun action(record: ExtensionNotificationActionRecord): NotificationCompat.Action {
        val declared = requireNotNull(record.action)
        val mutable = declared.input != null
        val flags = PendingIntent.FLAG_UPDATE_CURRENT or if (mutable) PendingIntent.FLAG_MUTABLE else PendingIntent.FLAG_IMMUTABLE
        val foreground = declared.foreground == true
        val pending = if (foreground) PendingIntent.getActivity(context, 0, activityIntent(record, declared.title, true), flags)
        else PendingIntent.getBroadcast(context, 0, broadcastIntent(record), flags)
        return NotificationCompat.Action.Builder(0, declared.title, pending)
            .setAuthenticationRequired(foreground)
            .apply {
                declared.input?.let { input ->
                    val label = input.placeholder ?: context.getString(persona.core.designsystem.R.string.designsystem_send)
                    addRemoteInput(RemoteInput.Builder(EXTENSION_NOTIFICATION_REMOTE_INPUT).setLabel(label).build())
                }
            }.build()
    }
}

internal data class ExtensionNotificationPresentation(
    val body: ExtensionNotificationActionRecord,
    val buttons: List<ExtensionNotificationActionRecord>,
)

internal fun prepareExtensionNotification(context: Context, event: NotifyEvent, tag: String): ExtensionNotificationPresentation? =
    prepareExtensionNotification(event, tag, Notify.dependencies(context).extensionNotifications(), ExtensionNotificationActionStore.shared)

/** Any replacement first retires its exact tag, including ordinary, invalid and wrong-source alerts. */
internal fun prepareExtensionNotification(
    event: NotifyEvent,
    tag: String,
    environment: ExtensionNotificationEnvironment,
    store: ExtensionNotificationActionStore,
): ExtensionNotificationPresentation? {
    if (event.kind != "alert") return null
    store.retire(tag)
    val payload = event.extensionNotification?.let(ExtensionNotificationPayload::validate) ?: return null
    val scope = extensionEventScope(environment, event.machineId, event.id) ?: return null
    val actions = payload.actions.orEmpty()
    val records = store.registerBatch(tag, payload.extension, payload.ui, payload.state,
        listOf(null) + actions, scope, environment::currentScope) ?: return null
    return ExtensionNotificationPresentation(records.first(), records.drop(1))
}

private fun String.safeSegment(): Boolean = isNotEmpty() && length <= 128 && this != "." && this != ".." &&
    all { it in 'a'..'z' || it in 'A'..'Z' || it in '0'..'9' || it == '.' || it == '_' || it == '-' }
private fun String?.utf8Size(): Int = this?.toByteArray(Charsets.UTF_8)?.size ?: 0
private fun String.singleLine(): String = buildString(length) {
    var pendingSpace = false
    for (character in this@singleLine) {
        if (Character.isWhitespace(character)) {
            if (isNotEmpty()) pendingSpace = true
        } else {
            if (pendingSpace) append(' ')
            append(character)
            pendingSpace = false
        }
    }
}

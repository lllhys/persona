package persona.platform.notify

import persona.core.contracts.Interaction
import persona.core.contracts.InteractionResolvedEvent
import persona.core.contracts.NotifyEvent
import persona.core.contracts.ServerEvent
import persona.core.contracts.SessionKey
import persona.core.contracts.api.DeviceNotificationPreferences
import persona.core.contracts.api.DoneNotification
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.SessionsRepository

/** Only hub-approved notify events may create alerts; raw interactions are state updates. */
internal class NotificationDelivery(
    private val interactions: InteractionCenter,
    private val sessions: SessionsRepository,
    private val suppressed: (SessionKey) -> Boolean,
    /** The card, and whether it comes inside this device's quiet hours (the event's `quiet`: without a sound). */
    private val postInteraction: (Interaction, Boolean) -> Unit,
    private val postNotify: (NotifyEvent) -> Unit,
    private val retract: (String) -> Unit,
    private val preferences: suspend () -> DeviceNotificationPreferences? = { DeviceNotificationPreferences() },
) {
    suspend fun accept(event: ServerEvent) {
        when (event) {
            is InteractionResolvedEvent -> retract(event.id)
            is NotifyEvent -> {
                val session = NotificationPolicy.session(event)
                if (session != null && (suppressed(session) || quiet(session))) return
                if (event.kind != "interaction" && NotificationPolicy.channel(event) == null) return
                val settings = preferences() ?: return
                if ((event.kind == "interaction" || event.kind == "alert" || event.kind == "reply") && !settings.input) return
                if (event.kind == "done" && settings.done == DoneNotification.Off) return
                // Long/Always and session mute/seen suppression remain the hub's decision.
                if (session != null && event.kind != "assistant" && sessions.sessions.value.none { it.key == session }) sessions.refresh()
                if (event.kind == "interaction") {
                    val id = event.interactionId ?: return
                    interactions.refresh()
                    // A card outside any session (`personad ask` in a plain terminal, #4515) has an empty session id: it
                    // is found by the event's own ids, and with no conversation of its own to be looking at, never held back.
                    val key = SessionKey(event.machineId, event.sessionId)
                    val ix = interactions.pending.value.firstOrNull { it.id == id && it.sessionKey == key } ?: return
                    if (NotificationPolicy.expired(ix)) { retract(id); return }
                    if (session == null || !suppressed(session)) postInteraction(ix, event.quiet)
                } else if (session == null || !suppressed(session)) postNotify(event)
            }
            else -> Unit
        }
    }

    /** A quiet task's agent (#1731): the hub sends no alert for it; one from an older hub is not shown either. */
    private fun quiet(session: SessionKey) = sessions.sessions.value.any { it.key == session && it.quiet }
}

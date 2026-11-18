package persona.feature.chat

import androidx.compose.ui.platform.LocalResources
import android.content.res.Resources
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import persona.core.contracts.Session
import persona.core.contracts.api.DeliveryState
import persona.core.contracts.api.OutboxMessage
import persona.core.designsystem.InlineTone
import persona.core.designsystem.JInlineStatus
import persona.core.designsystem.Tokens
import persona.core.domain.lines.isLiveStatus

internal data class StartingNote(val text: String, val failed: Boolean)

/**
 * A session Persona is still starting (docs/protocol/protocol.md "New session"): what is sent now goes in once the agent is ready.
 * Or why it did not start; sends then fail with the same reason and can be sent again.
 */
internal fun startingNote(res: Resources, session: Session?): StartingNote? = when {
    session == null -> null
    !session.startError.isNullOrBlank() -> StartingNote(res.getString(R.string.chat_starting_failed, session.startError.orEmpty().trim()), failed = true)
    session.starting -> StartingNote(res.getString(R.string.chat_starting, session.agentLabel), failed = false)
    else -> null
}

/** Above the composer while the session starts, or when starting it failed. */
@Composable
internal fun ChatStartingNote(session: Session?, modifier: Modifier = Modifier) {
    val note = startingNote(LocalResources.current, session) ?: return
    JInlineStatus(note.text, modifier.padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.xs),
        tone = if (note.failed) InlineTone.Failed else InlineTone.Busy)
}

/** Who an Agent's chat talks to, for its words (iOS `agentLabel`): Claude for a session not known yet. */
internal fun chatAgentName(session: Session?): String = session?.agentLabel?.takeIf(String::isNotBlank) ?: "Claude"

/** An Agent chat's empty composer names its agent (iOS `Composer` placeholder): "Message Claude", "Message Codex". */
internal fun agentPlaceholder(res: Resources, session: Session?): String =
    if (session?.awaitingFirstMessage == true) res.getString(R.string.chat_codex_first_message)
    else res.getString(R.string.chat_placeholder_agent, chatAgentName(session))

/** A message on its way to the agent: sending, or sent and not in the transcript yet (iOS `ChatAwaitingTurn.onItsWay`). */
private val DeliveryState.onItsWay get() = this == DeliveryState.Sending || this == DeliveryState.Sent

/**
 * Going in: sending, sent, or waiting for the network. Refused and turned-down messages go nowhere, and neither does one
 * whose delivery the phone has to check (iOS's failed message, `mayHaveArrived`).
 */
private val DeliveryState.goingIn get() = onItsWay || this == DeliveryState.Queued || this == DeliveryState.WaitingForNetwork

/**
 * "Claude is getting ready…" under your newest message (iOS `startingTurn`): a message of yours is still going in, no
 * work line runs for it yet ([liveWork]), and the agent is at work or waiting on you, still starting up, or the message is
 * on its way while the hub may not have caught up (its status still idle or done, the start not failed). The turn's work
 * line takes over when it comes; null when the line doesn't show. [name] says who: the agent, or in Persona's
 * conversation Persona ("Persona is getting ready…", #4339 C18).
 */
internal fun gettingReady(res: Resources, session: Session?, messages: List<OutboxMessage>, liveWork: Boolean,
                          name: String = chatAgentName(session)): String? {
    if (liveWork || messages.none { it.state.goingIn }) return null
    val status = session?.status
    val catchingUp = mayBeCatchingUp(status, !session?.startError.isNullOrBlank()) && messages.any { it.state.onItsWay }
    if (!isLiveStatus(status) && session?.starting != true && !catchingUp) return null
    return res.getString(R.string.chat_getting_ready, name)
}

package persona.android.navigation

import persona.core.contracts.SessionKey
import persona.core.navigation.AgentsRoute
import persona.core.navigation.AppTab
import persona.core.navigation.ChatRoute
import persona.core.navigation.ConversationKind
import persona.core.navigation.InboxItemRoute
import persona.core.navigation.InteractionRoute
import persona.core.navigation.RequestRef
import persona.core.navigation.ResolvedRoute
import persona.feature.assistant.NeedsYouLine

/**
 * Where a line of the Persona home's "Needs you now" opens, as a notification would open it: an agent's request in its
 * conversation on the Agents tab (a card outside any session on Agents with its sheet open), an inbox item under Persona
 * with the Inbox list as its parent. A side session's card (#7944) names the side: its parent opens with the side shown.
 */
internal fun needsYouRoute(line: NeedsYouLine): ResolvedRoute = when (line) {
    is NeedsYouLine.Agent -> sessionlessCard(line.session, line.requestId)
        ?: (line.side ?: line.session).let { ResolvedRoute(AppTab.Agents, ChatRoute(it.machineId, it.sessionId, ConversationKind.Agent)) }
    is NeedsYouLine.Inbox -> ResolvedRoute(AppTab.Persona, InboxItemRoute(line.itemId))
}

/** Where a chat's "Next: …" line (#4325) opens: that conversation with that card first, or a sessionless card on Agents. */
internal fun waitingRoute(session: SessionKey, requestId: String): ResolvedRoute = sessionlessCard(session, requestId)
    ?: ResolvedRoute(AppTab.Agents, InteractionRoute(ChatRoute(session.machineId, session.sessionId, ConversationKind.Agent), requestId))

/**
 * A card outside any session (`personad ask` in a plain terminal) has no conversation to open: Agents opens at its list
 * with that card's sheet (#4808), not a blank chat (#5701). Null for a card of a conversation.
 */
private fun sessionlessCard(session: SessionKey, requestId: String): ResolvedRoute? =
    if (session.sessionId.isBlank()) ResolvedRoute(AppTab.Agents, AgentsRoute, card = RequestRef(session.machineId, requestId)) else null

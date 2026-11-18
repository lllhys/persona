package persona.feature.agents

import persona.core.contracts.CoreText
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.SessionListStatus
import persona.core.domain.sort.AgentListGroup
import persona.core.domain.sort.AgentListResult
import persona.core.domain.sort.AgentListSettings
import persona.core.domain.sort.AgentSession

// Pages for the Agents feed's tests. The feed reads a page's sessions, its one group of rows and its status: the list's own
// projection (sorting into spaces, folding) is not what is under test, so the group draws every session in the order given.

internal val feedMac = Machine("mac", "Mac", online = true)

internal fun feedSession(id: String, state: String = "working", machine: String = "mac", title: String = id, since: String? = null) =
    Session(machineId = machine, sessionId = id, state = state, title = title, stateSince = since, updatedAt = "2026-01-01T00:00:00Z")

/** The sessions as one group that draws them all, or draws none while [folded]; the computer filter admits the group's rows only. */
internal fun feedPage(
    vararg sessions: Session, machines: List<Machine> = listOf(feedMac), paged: Boolean = false, loaded: Boolean = true,
    settings: AgentListSettings = AgentListSettings(), folded: Boolean = false,
): AgentsPageState = feedPage(sessions.toList(), machines, paged, loaded, settings, folded)

internal fun feedPage(
    sessions: List<Session>, machines: List<Machine> = listOf(feedMac), paged: Boolean = false, loaded: Boolean = true,
    settings: AgentListSettings = AgentListSettings(), folded: Boolean = false,
): AgentsPageState {
    val rows = sessions.filter { settings.machineFilter.isEmpty() || it.machineId == settings.machineFilter }.map(::AgentSession)
    val group = AgentListGroup("g", CoreText.plain("g"), rows, if (folded) emptyList() else rows, folded = folded)
    return AgentsPageState(
        sessions = sessions, machines = machines, loaded = loaded, settings = settings,
        list = AgentListResult(emptyMap(), listOf(group), emptyList()),
        pagination = SessionListStatus(paged = paged, loaded = loaded),
    )
}

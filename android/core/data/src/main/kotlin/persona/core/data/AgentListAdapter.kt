package persona.core.data

import java.time.Instant
import persona.core.contracts.DirectionTree
import persona.core.contracts.Machine
import persona.core.contracts.Todo
import persona.core.domain.sort.AgentListResult
import persona.core.domain.sort.AgentListSettings
import persona.core.domain.sort.AgentPreferences
import persona.core.domain.sort.AgentSession
import persona.core.domain.sort.buildFlatAgentList

/** One Agents-list projection boundary. Kotlin remains the only implementation that supplies rows to the UI. */
interface AgentListAdapter {
    data class Input(
        val sessions: List<AgentSession>,
        val machines: List<Machine> = emptyList(),
        val pins: AgentPreferences = AgentPreferences(),
        val mutes: AgentPreferences = AgentPreferences(),
        val settings: AgentListSettings = AgentListSettings(),
        val todos: List<Todo> = emptyList(),
        val directions: DirectionTree? = null,
        /** The open chat that was Unread when opened (iOS heldInList); absent when the host does not track it. */
        val held: String? = null,
        /** Supplied by the caller: neither implementation reads a clock. */
        val now: Instant = Instant.now(),
    )

    fun project(input: Input): AgentListResult
}

/** The production behavior, kept byte-for-byte behind the new boundary. */
class KotlinAgentListAdapter : AgentListAdapter {
    override fun project(input: AgentListAdapter.Input): AgentListResult = with(input) {
        // scopedAgentList retains range, forest and history preprocessing outside this boundary.
        val withHeld = sessions.map { it.copy(heldInList = held != null && it.id == held) }
        buildFlatAgentList(withHeld, machines, pins, mutes, settings.copy(now = now), todos, directions)
    }
}

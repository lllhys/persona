package persona.core.contracts.api

import persona.core.contracts.Machine
import persona.core.contracts.Session
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/**
 * [total] is the active filter's complete membership, not the loaded window length.
 * [counts] are state-chip totals with state omitted from [filter]. [machineCounts]
 * also omits machineId, for cross-machine chips. Neither is the active state's group count.
 */
data class SessionListStatus(
    val paged: Boolean = false,
    val loaded: Boolean = false,
    val loading: Boolean = false,
    /** The last request failed; an empty first page offers retry instead of a permanent spinner. */
    val failed: Boolean = false,
    val hasMore: Boolean = false,
    val hasPrevious: Boolean = false,
    val total: Int = 0,
    val counts: SessionListCounts = SessionListCounts(),
    val machineCounts: SessionListCounts = SessionListCounts(),
    val filter: SessionListFilter = SessionListFilter(),
)

private val legacySessionListStatus: StateFlow<SessionListStatus> = MutableStateFlow(SessionListStatus(loaded = true))

interface SessionsRepository {
    val machines: StateFlow<List<Machine>>
    val sessions: StateFlow<List<Session>>
    val listStatus: StateFlow<SessionListStatus> get() = legacySessionListStatus
    suspend fun refresh()
    suspend fun setListFilter(filter: SessionListFilter) {}
    /** Hub-group scope, separate from the server's machine-local group filter; null includes all hubs. */
    suspend fun setListNamespaces(namespaces: Set<String?>?) {}
    suspend fun loadMore() {}
    suspend fun loadPrevious() {}
    /** Aggregate metadata for a visible group; never downloads its session rows. */
    suspend fun listCounts(filter: SessionListFilter): SessionListCounts = throw SessionListsUnsupported()

    /**
     * A session the hub just created (an async start answers with it while the agent is still starting): listed at
     * once, so its chat has metadata before the next `sessions` push. A session already listed is left as it is.
     */
    suspend fun adopt(session: Session) {}

    /** A machine the hub just returned (PUT …/engine): shown at once, before the next `machines` push. */
    suspend fun applyMachine(machine: Machine) {}
}

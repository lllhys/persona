package persona.feature.agents

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import persona.core.contracts.api.AgentCreationService
import persona.core.contracts.api.AgentWorkspace

/** One pairing's read-only Paseo catalog. A failed refresh keeps its last source state; a successful empty read clears it. */
internal class AgentsWorkspaceCatalog {
    private val _rows = MutableStateFlow<List<AgentWorkspace>>(emptyList())
    val rows = _rows.asStateFlow()
    private var generation = 0
    private var scope: String? = null

    suspend fun refresh(service: AgentCreationService, machines: List<Pair<String, Boolean>>) = coroutineScope {
        if (scope != service.scope) {
            scope = service.scope
            _rows.value = emptyList()
        }
        val read = ++generation
        val old = _rows.value.groupBy { it.machineId }
        val next = machines.map { (machine, online) -> async {
            val rows = try {
                // The hub retains the source inventory in its database, so an offline computer can still restore it.
                val result = service.workspaceCatalog(machine)
                val incoming = result.workspaces
                    .filter { it.machineId == machine && it.source == "paseo" && it.sourceId.isNotEmpty() }
                    .distinctBy { it.sourceId }
                if (result.complete) incoming else {
                    val merged = old[machine].orEmpty().associateByTo(linkedMapOf()) { Triple(it.machineId, it.source, it.sourceId) }
                    incoming.forEach { merged[Triple(it.machineId, it.source, it.sourceId)] = it }
                    merged.values.toList()
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { old[machine].orEmpty() }
            if (online) rows else rows.map { it.copy(online = false, available = false) }
        } }.awaitAll().flatten().sortedWith(compareBy<AgentWorkspace> { it.machineId }.thenBy { it.sourceId })
        if (read == generation) _rows.value = next
    }
}

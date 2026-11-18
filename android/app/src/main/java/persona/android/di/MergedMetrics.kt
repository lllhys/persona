package persona.android.di

import kotlinx.coroutines.CancellationException
import persona.core.contracts.MetricsDashboard
import persona.core.contracts.api.MachineMetricsApi
import persona.core.contracts.api.MetricsUnavailable
import persona.core.hub.HttpMachineMetricsApi
import persona.core.hub.HubError
import persona.core.hub.HubFailure

internal class MetricsGroup(
    val names: GroupNames,
    val api: () -> MachineMetricsApi?,
    val cache: HttpMachineMetricsApi.Cache,
    val current: () -> Boolean,
)

internal suspend fun mergedMetrics(list: List<MetricsGroup>): MetricsDashboard {
    fun of(group: MetricsGroup) = group.api() ?: throw HubFailure(HubError.NoNetwork)
    if (list.size == 1) {
        val group = list.single()
        return of(group).dashboard().let { it.copy(machines = it.machines.map(group.names::metrics)) }
    }
    var failure: Exception? = null
    var fresh: MetricsDashboard? = null
    val parts = list.mapNotNull { group ->
        try { of(group).dashboard().let { it.copy(machines = it.machines.map(group.names::metrics)) }.also { if (fresh == null) fresh = it } }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) {
            if (failure == null) failure = error
            val previous = if (error !is MetricsUnavailable && group.current())
                group.cache.previousDashboard()?.takeIf { group.current() } else null
            previous?.let { it.copy(machines = it.machines.map(group.names::metrics)) }
        }
    }
    val successful = fresh ?: throw (failure ?: HubFailure(HubError.NoNetwork))
    return MetricsDashboard(parts.flatMap { it.machines }, successful.step)
}

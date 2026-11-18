package persona.core.hub

import persona.core.contracts.*
import persona.core.contracts.api.MachineMetricsApi
import persona.core.contracts.api.ManagerRoster
import persona.core.contracts.api.MetricsUnavailable
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.supervisorScope
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request

class HttpMachineMetricsApi(
    baseUrl: String,
    private val token: String,
    client: OkHttpClient,
    private val isCurrent: () -> Boolean = { true },
    private val dispatcher: CoroutineDispatcher = Dispatchers.Default,
    private val cache: Cache = Cache(),
) : MachineMetricsApi {
    class Cache {
        internal val lock = Mutex()
        internal val snapshots = linkedMapOf<String, MetricsDashboard>()
        internal var previous: MetricsDashboard? = null

        suspend fun previousDashboard(): MetricsDashboard? = lock.withLock { previous }
    }

    private val base = baseUrl.toHttpUrl()
    private val http = client.newBuilder().followRedirects(false).followSslRedirects(false)
        .retryOnConnectionFailure(false).build()

    private fun checkScope() { if (!isCurrent()) throw CancellationException("Pairing changed") }

    private suspend inline fun <reified T> get(url: HttpUrl): T = withContext(dispatcher) {
        checkScope()
        val request = Request.Builder().url(url).header("Authorization", "Bearer $token").build()
        http.newCall(request).awaitResponse().use { response ->
            checkScope()
            if (response.code == 404) throw MetricsUnavailable()
            if (!response.isSuccessful) throw HubFailure.from(response)
            val result = JsonCodec.decodeFromString<T>(response.body!!.string())
            checkScope()
            result
        }
    }

    override suspend fun dashboard(): MetricsDashboard = cache.lock.withLock {
        checkScope()
        val roster = try {
            get<ManagerRoster>(base.newBuilder().addPathSegments("api/v1/managers").build())
        } catch (_: MetricsUnavailable) {
            return@withLock try { readDashboard(null).also { cache.previous = it } }
            catch (unsupported: MetricsUnavailable) { cache.previous = null; throw unsupported }
        }
        checkScope()
        val installed = roster.installed
        val ids = installed.filter { it.online }.map { it.id }
        if (ids.isEmpty()) {
            cache.snapshots.keys.retainAll(installed.map { it.id }.toSet())
            return@withLock MetricsDashboard().also { cache.previous = it }
        }
        val results = supervisorScope {
            ids.map { manager -> async {
                val result = try { Result.success(readDashboard(manager)) }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (error: Exception) { Result.failure<MetricsDashboard>(error) }
                manager to result
            } }.awaitAll()
        }
        checkScope()
        val fresh = results.mapNotNull { (manager, result) -> result.getOrNull()?.let { manager to it } }
        if (fresh.isEmpty()) {
            val errors = results.mapNotNull { it.second.exceptionOrNull() }
            val error = errors.firstOrNull { it !is MetricsUnavailable } ?: errors.first()
            if (error is MetricsUnavailable) cache.previous = null
            throw error
        }
        cache.snapshots.keys.retainAll(installed.map { it.id }.toSet())
        fresh.forEach { (manager, dashboard) -> cache.snapshots[manager] = dashboard }
        val machines = ids.flatMap { cache.snapshots[it]?.machines.orEmpty() }.distinctBy { it.machineId }
        fresh.first().second.copy(machines = machines).also { cache.previous = it }
    }

    private suspend fun readDashboard(manager: String?): MetricsDashboard {
        val url = base.newBuilder().addPathSegments("api/v1/metrics")
        if (manager != null) url.addQueryParameter("manager", manager)
        return get(url.build())
    }

    override suspend fun history(machineId: String, range: MetricRange): MetricsHistory = get(base.newBuilder()
        .addPathSegments("api/v1/machines").addPathSegment(machineId).addPathSegment("metrics")
        .addQueryParameter("range", range.wire).build())
}

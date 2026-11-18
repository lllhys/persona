package persona.android.di

import java.io.Closeable
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Assert.assertEquals
import org.junit.Assert.fail
import org.junit.Test
import persona.core.hub.HttpMachineMetricsApi
import persona.core.hub.HubFailure

class MergedMetricsTest {
    @Test fun aFailedManagersHubKeepsItsNumbersWhileAnotherHubUpdates() = runBlocking {
        MetricsHub(11).use { first -> MetricsHub(22).use { second ->
            val firstCache = HttpMachineMetricsApi.Cache()
            val secondCache = HttpMachineMetricsApi.Cache()
            val groups = listOf(
                MetricsGroup(GroupNames(null), { first.api(firstCache) }, firstCache) { true },
                MetricsGroup(GroupNames("g0a1b2c3d"), { second.api(secondCache) }, secondCache) { true },
            )
            val initial = mergedMetrics(groups)
            assertEquals(listOf("same", "same~g0a1b2c3d"), initial.machines.map { it.machineId })
            val previous = initial.machines.last()
            first.cpu.set(33)
            second.refused.set(true)
            val latest = mergedMetrics(groups)
            assertEquals(listOf("same", "same~g0a1b2c3d"), latest.machines.map { it.machineId })
            assertEquals(33.0, latest.machines.first().latest!!.cpu!!, 0.0)
            assertEquals(previous, latest.machines.last())
        } }
    }

    @Test fun aCachedFirstHubDoesNotReplaceTheFreshHubsStep() = runBlocking {
        MetricsHub(11).use { first -> MetricsHub(22).use { second ->
            val firstCache = HttpMachineMetricsApi.Cache()
            val secondCache = HttpMachineMetricsApi.Cache()
            val groups = listOf(
                MetricsGroup(GroupNames(null), { first.api(firstCache) }, firstCache) { true },
                MetricsGroup(GroupNames("g0a1b2c3d"), { second.api(secondCache) }, secondCache) { true },
            )
            val previous = mergedMetrics(groups).machines.first()
            first.refused.set(true)
            second.cpu.set(44)
            second.step.set(30)
            val latest = mergedMetrics(groups)
            assertEquals(previous, latest.machines.first())
            assertEquals(44.0, latest.machines.last().latest!!.cpu!!, 0.0)
            assertEquals(30, latest.step)
            second.refused.set(true)
            try { mergedMetrics(groups); fail("An entirely failed poll must keep reporting its error") }
            catch (error: HubFailure) { assertEquals(503, error.status) }
        } }
    }

    @Test fun unsupportedMonitoringDoesNotRestoreOldNumbers() = runBlocking {
        MetricsHub(11).use { first -> MetricsHub(22).use { second ->
            val firstCache = HttpMachineMetricsApi.Cache()
            val secondCache = HttpMachineMetricsApi.Cache()
            val groups = listOf(
                MetricsGroup(GroupNames(null), { first.api(firstCache) }, firstCache) { true },
                MetricsGroup(GroupNames("g0a1b2c3d"), { second.api(secondCache) }, secondCache) { true },
            )
            assertEquals(2, mergedMetrics(groups).machines.size)
            second.status.set(404)
            assertEquals(listOf("same"), mergedMetrics(groups).machines.map { it.machineId })
            second.status.set(503)
            assertEquals(listOf("same"), mergedMetrics(groups).machines.map { it.machineId })
            second.status.set(200)
            assertEquals(2, mergedMetrics(groups).machines.size)
            second.legacy.set(true)
            second.status.set(404)
            assertEquals(listOf("same"), mergedMetrics(groups).machines.map { it.machineId })
            second.status.set(503)
            assertEquals(listOf("same"), mergedMetrics(groups).machines.map { it.machineId })
        } }
    }

    @Test fun anotherPairingCannotReadThePreviousPairingsCachedNumbers() = runBlocking {
        MetricsHub(11).use { first -> MetricsHub(22).use { second ->
            val firstCache = HttpMachineMetricsApi.Cache()
            val oldCache = HttpMachineMetricsApi.Cache()
            val primary = MetricsGroup(GroupNames(null), { first.api(firstCache) }, firstCache) { true }
            val previous = MetricsGroup(GroupNames("g0a1b2c3d"), { second.api(oldCache) }, oldCache) { true }
            assertEquals(2, mergedMetrics(listOf(primary, previous)).machines.size)
            val closed = MetricsGroup(GroupNames("g0a1b2c3d"), { null }, oldCache) { false }
            assertEquals(listOf("same"), mergedMetrics(listOf(primary, closed)).machines.map { it.machineId })
            second.refused.set(true)
            val newCache = HttpMachineMetricsApi.Cache()
            val replacement = MetricsGroup(GroupNames("g0a1b2c3d"), { second.api(newCache) }, newCache) { true }
            assertEquals(listOf("same"), mergedMetrics(listOf(primary, replacement)).machines.map { it.machineId })
        } }
    }
}

private class MetricsHub(cpu: Int) : Closeable {
    val cpu = AtomicInteger(cpu)
    val refused = AtomicBoolean(false)
    val status = AtomicInteger(200)
    val step = AtomicInteger(60)
    val legacy = AtomicBoolean(false)
    private val client = OkHttpClient()
    private val server = MockWebServer().apply {
        dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse = when (request.requestUrl!!.encodedPath) {
                "/api/v1/managers" -> if (legacy.get()) MockResponse().setResponseCode(404) else MockResponse().setBody(
                    """{"default":"primary","managers":[{"id":"primary","name":"Primary","online":true}]}""")
                "/api/v1/metrics" -> if (refused.get()) MockResponse().setResponseCode(503)
                    else if (status.get() != 200) MockResponse().setResponseCode(status.get())
                    else MockResponse().setBody("""{"machines":[{"machine_id":"same","online":true,"latest":{"t":1,"cpu":${this@MetricsHub.cpu.get()},"mem_used":111,"disk_used":10},"points":[]}],"step":${step.get()}}""")
                else -> MockResponse().setResponseCode(404)
            }
        }
    }

    fun api(cache: HttpMachineMetricsApi.Cache) = HttpMachineMetricsApi(
        server.url("/").toString(), "fixture-token", client, cache = cache)

    override fun close() {
        server.close()
        client.dispatcher.executorService.shutdownNow()
        client.connectionPool.evictAll()
    }
}

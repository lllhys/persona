package persona.core.hub

import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class MachineMetricsManagersTest {
    @Test fun dashboardReadsBothOnlineManagersWithoutAnUnscopedDuplicate() = runBlocking {
        val requests = LinkedBlockingQueue<RecordedRequest>()
        MockWebServer().use { server ->
            server.dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse {
                    if (portDiscoveryRequest(request)) {
                        return MockResponse().setResponseCode(404)
                    }
                    requests.add(request)
                    return when (request.requestUrl!!.encodedPath) {
                        "/api/v1/managers" -> MockResponse().setBody("""{"default":"primary","managers":[
                            {"id":"primary","online":true,"default":true},
                            {"id":"secondary","online":true},
                            {"id":"offline","online":false},
                            {"id":"installing","online":true,"installing":"pending"}]}""")
                        "/api/v1/metrics" -> when (request.requestUrl!!.queryParameter("manager")) {
                            null, "primary" -> MockResponse().setBody("""{"machines":[{"machine_id":"first","online":true,"latest":{"t":1,"cpu":11,"mem_used":111,"disk_used":10}}]}""")
                            "secondary" -> MockResponse().setBody("""{"machines":[{"machine_id":"second","online":true,"latest":{"t":2,"cpu":22,"mem_used":222,"disk_used":20}}]}""")
                            else -> MockResponse().setResponseCode(500)
                        }
                        else -> MockResponse().setResponseCode(404)
                    }
                }
            }
            val api = HttpMachineMetricsApi(server.url("/").toString(), "fixture-token", OkHttpClient())
            val dashboard = api.dashboard()
            assertEquals(listOf("first", "second"), dashboard.machines.map { it.machineId })
            assertEquals(listOf(11.0, 22.0), dashboard.machines.map { it.latest?.cpu })
            assertEquals(listOf(111L, 222L), dashboard.machines.map { it.latest?.memUsed })
            assertEquals(listOf(10L, 20L), dashboard.machines.map { it.latest?.diskUsed })
            val calls = requests.toList()
            assertEquals(3, calls.size)
            assertEquals("/api/v1/managers", calls.first().path)
            assertEquals(setOf("/api/v1/metrics?manager=primary", "/api/v1/metrics?manager=secondary"),
                calls.drop(1).map { it.path }.toSet())
            assertEquals(setOf("Bearer fixture-token"), calls.map { it.getHeader("Authorization") }.toSet())
            assertEquals(setOf("GET"), calls.map { it.method }.toSet())
        }
    }

    @Test fun refreshedAdaptersRetainFailedSamplesAndReplaceSuccessfulEmptySnapshots() = runBlocking {
        MetricsFixture().use { fixture ->
            val cache = HttpMachineMetricsApi.Cache()
            val client = OkHttpClient()
            val original = HttpMachineMetricsApi(fixture.url, "fixture-token", client, cache = cache).dashboard()
            val previous = original.machines.single { it.machineId == "second" }
            fixture.primaryCPU.set(33)
            fixture.refused["secondary"] = 503
            val refreshed = HttpMachineMetricsApi(fixture.url, "fixture-token", client, cache = cache).dashboard()
            assertEquals(33.0, refreshed.machines.first().latest!!.cpu!!, 0.0)
            assertEquals(previous, refreshed.machines.single { it.machineId == "second" })
            fixture.online.set(false)
            fixture.requests.clear()
            assertTrue(HttpMachineMetricsApi(fixture.url, "fixture-token", client, cache = cache).dashboard().machines.isEmpty())
            assertEquals(listOf("/api/v1/managers"), fixture.requests.map { it.path })
            fixture.online.set(true)
            val afterOffline = HttpMachineMetricsApi(fixture.url, "fixture-token", client, cache = cache).dashboard()
            assertEquals(previous, afterOffline.machines.single { it.machineId == "second" })
            fixture.refused["primary"] = 404
            try {
                HttpMachineMetricsApi(fixture.url, "fixture-token", client, cache = cache).dashboard()
                fail("A failed poll must report its non-404 error")
            } catch (error: HubFailure) { assertEquals(503, error.status) }
            fixture.refused.remove("primary")
            val recovered = HttpMachineMetricsApi(fixture.url, "fixture-token", client, cache = cache).dashboard()
            assertEquals(previous, recovered.machines.single { it.machineId == "second" })
            val newPairing = HttpMachineMetricsApi(fixture.url, "fixture-token", client, cache = HttpMachineMetricsApi.Cache()).dashboard()
            assertEquals(listOf("first"), newPairing.machines.map { it.machineId })
            fixture.refused.clear()
            fixture.emptySecondary.set(true)
            val empty = HttpMachineMetricsApi(fixture.url, "fixture-token", client, cache = cache).dashboard()
            assertEquals(listOf("first"), empty.machines.map { it.machineId })
        }
    }

    @Test fun anOldRouteCannotCommitItsPartialSamplesToThePairingsCache() = runBlocking {
        MetricsFixture().use { fixture ->
            val cache = HttpMachineMetricsApi.Cache()
            val client = OkHttpClient()
            val previous = HttpMachineMetricsApi(fixture.url, "fixture-token", client, cache = cache).dashboard()
            fixture.primaryCPU.set(33)
            fixture.blockSecondary.set(true)
            val primarySeen = CountDownLatch(1)
            fixture.primaryObserved.set(primarySeen)
            val current = AtomicBoolean(true)
            val old = async(Dispatchers.Default) {
                HttpMachineMetricsApi(fixture.url, "fixture-token", client, { current.get() }, cache = cache).dashboard()
            }
            try {
                assertTrue(primarySeen.await(5, TimeUnit.SECONDS))
                assertTrue(fixture.secondaryObserved.await(5, TimeUnit.SECONDS))
                current.set(false)
            } finally { fixture.releaseSecondary.countDown() }
            try { old.await(); fail("The replaced route must be cancelled") }
            catch (_: CancellationException) { }
            assertEquals(previous, cache.previousDashboard())
            fixture.blockSecondary.set(false)
            fixture.refused["primary"] = 503
            val next = HttpMachineMetricsApi(fixture.url, "fixture-token", client, cache = cache).dashboard()
            assertEquals(11.0, next.machines.first { it.machineId == "first" }.latest!!.cpu!!, 0.0)
            assertEquals(22.0, next.machines.first { it.machineId == "second" }.latest!!.cpu!!, 0.0)
        }
    }

    @Test fun aHubWithoutTheRosterStillReadsItsLegacyDashboardOnce() = runBlocking {
        MetricsFixture().use { fixture ->
            fixture.unsupported.set(true)
            val dashboard = HttpMachineMetricsApi(fixture.url, "fixture-token", OkHttpClient()).dashboard()
            assertEquals(listOf("first"), dashboard.machines.map { it.machineId })
            assertEquals(listOf("/api/v1/managers", "/api/v1/metrics"), fixture.requests.map { it.path })
        }
    }

    @Test fun duplicateMachineIdsUseTheDefaultManagersSample() = runBlocking {
        MetricsFixture().use { fixture ->
            fixture.sameMachine.set(true)
            val dashboard = HttpMachineMetricsApi(fixture.url, "fixture-token", OkHttpClient()).dashboard()
            assertEquals(listOf("first"), dashboard.machines.map { it.machineId })
            assertEquals(11.0, dashboard.machines.single().latest!!.cpu!!, 0.0)
        }
    }
}

private class MetricsFixture : java.io.Closeable {
    val requests = LinkedBlockingQueue<RecordedRequest>()
    val primaryCPU = AtomicInteger(11)
    val refused = ConcurrentHashMap<String, Int>()
    val online = AtomicBoolean(true)
    val emptySecondary = AtomicBoolean(false)
    val sameMachine = AtomicBoolean(false)
    val unsupported = AtomicBoolean(false)
    val blockSecondary = AtomicBoolean(false)
    val primaryObserved = AtomicReference<CountDownLatch?>()
    val secondaryObserved = CountDownLatch(1)
    val releaseSecondary = CountDownLatch(1)
    private val server = MockWebServer().apply {
        dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                if (portDiscoveryRequest(request)) return MockResponse().setResponseCode(404)
                requests.add(request)
                return when (request.requestUrl!!.encodedPath) {
                    "/api/v1/managers" -> if (unsupported.get()) MockResponse().setResponseCode(404) else
                        MockResponse().setBody("""{"default":"primary","managers":[
                            {"id":"primary","online":${online.get()},"default":true},
                            {"id":"secondary","online":${online.get()}},
                            {"id":"offline","online":false},
                            {"id":"installing","online":true,"installing":"pending"}]}""")
                    "/api/v1/metrics" -> {
                        val manager = request.requestUrl!!.queryParameter("manager") ?: "primary"
                        if (manager == "primary") primaryObserved.get()?.countDown()
                        if (manager == "secondary" && blockSecondary.get()) {
                            secondaryObserved.countDown()
                            if (!releaseSecondary.await(5, TimeUnit.SECONDS)) return MockResponse().setResponseCode(504)
                        }
                        refused[manager]?.let { return MockResponse().setResponseCode(it) }
                        when (manager) {
                            "primary" -> MockResponse().setBody("""{"machines":[{"machine_id":"first","online":true,"latest":{"t":1,"cpu":${primaryCPU.get()},"mem_used":111,"disk_used":10}}]}""")
                            "secondary" -> MockResponse().setBody(if (emptySecondary.get()) """{"machines":[]}""" else
                                """{"machines":[{"machine_id":"${if (sameMachine.get()) "first" else "second"}","online":true,"latest":{"t":2,"cpu":22,"mem_used":222,"disk_used":20}}]}""")
                            else -> MockResponse().setResponseCode(500)
                        }
                    }
                    else -> MockResponse().setResponseCode(404)
                }
            }
        }
    }
    val url: String get() = server.url("/").toString()
    override fun close() { releaseSecondary.countDown(); server.close() }
}

package persona.core.hub

import persona.core.contracts.MetricRange
import persona.core.contracts.api.MetricsUnavailable
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test

class MachineMetricsApiTest {
    @Test fun metricsUseEncodedRoutesPreserveMissingValuesAndRejectOldHubAndStaleScope() = runBlocking {
        MockWebServer().use { server ->
            var current = true
            val api = HttpMachineMetricsApi(server.url("/").toString(), "fixture-token", OkHttpClient(), { current })
            server.enqueue(MockResponse().setBody("""{"default":"primary","managers":[{"id":"primary","online":true,"default":true}]}"""))
            server.enqueue(MockResponse().setBody("""{"machines":[{"machine_id":"machine","online":true,"latest":{"t":200,"mem_used":80,"mem_total":100},"points":[]}],"step":60}"""))
            val dashboard = api.dashboard()
            assertNull(dashboard.machines.single().latest?.cpu)
            assertNull(dashboard.machines.single().latest?.netRx)
            assertEquals(80L, dashboard.machines.single().latest?.memUsed)
            assertEquals("/api/v1/managers", (server.takeRequest().path).zhAny())
            assertEquals("/api/v1/metrics?manager=primary", (server.takeRequest().path).zhAny())
            server.enqueue(MockResponse().setBody("""{"metrics":{"machine_id":"machine/part","points":[{"t":100,"cpu":0},{"t":110,"gap":true}]},"range":"24h","step":240}"""))
            val history = api.history("machine/part", MetricRange.Day)
            assertTrue(history.metrics.points.last().gap)
            assertEquals(0.0, history.metrics.points.first().cpu!!, 0.0)
            assertEquals(240, history.step)
            val request = server.takeRequest()
            assertEquals("/api/v1/machines/machine%2Fpart/metrics?range=24h", (request.path).zhAny())
            assertEquals("Bearer fixture-token", (request.getHeader("Authorization")).zhAny())
            server.enqueue(MockResponse().setResponseCode(404))
            server.enqueue(MockResponse().setResponseCode(404))
            try { api.dashboard(); fail("Old hub must be distinguishable") } catch (_: MetricsUnavailable) { }
            current = false
            try { api.dashboard(); fail("Old pairing must be cancelled") } catch (_: CancellationException) { }
            assertEquals(5, server.requestCount)
        }
    }
}

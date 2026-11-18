package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import persona.core.contracts.StandingAuthorization
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer

class StandingAuthorizationsHttpTest {
    @Test fun aListDropsARowWithoutAnIdAndAForbiddenDeviceIsNotAnEmptyList() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient()) { true }
            server.enqueue(MockResponse().setBody("""{"revision":2,"authorizations":[
                {"id":"sa_1","machine_id":"box"},
                {"directory":"/code/no-id"},
                {"id":"sa_2","actions":["merge"]}
            ]}"""))
            val list = api.standingAuthorizations()
            val listed = server.takeRequest()
            assertEquals("GET", listed.method)
            assertEquals("/api/v1/assistant/standing-authorizations", listed.path)
            assertEquals(2L, list.revision)
            assertEquals(listOf("sa_1", "sa_2"), list.authorizations.map(StandingAuthorization::id))
            assertEquals("", list.authorizations[1].machineId)

            server.enqueue(MockResponse().setResponseCode(403).setBody("""{"error":"full access required","code":"forbidden"}"""))
            try {
                api.standingAuthorizations()
                fail("a 403 is not an empty list")
            } catch (failure: HubFailure) {
                assertEquals(403, failure.status)
                assertEquals("forbidden", failure.code)
            }
        }
    }

    @Test fun revokeSendsTheIdAsOneEncodedPathSegment() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient()) { true }
            server.enqueue(MockResponse().setBody("""{"ok":true,"revision":3}"""))
            api.revokeStandingAuthorization("a/b c?#")
            val sent = server.takeRequest()
            assertEquals("DELETE", sent.method)
            assertEquals("/api/v1/assistant/standing-authorizations/a%2Fb%20c%3F%23", sent.path)
            assertTrue(!sent.path!!.contains("/b "))
        }
    }
}

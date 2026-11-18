package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.ManagersEvent
import persona.core.contracts.ServerEventSerializer
import persona.core.contracts.api.ManagersUnsupported
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test

class HttpHubManagersTest {
    // Like the pairing's connection: a status that is not 2xx is thrown as the hub's error before the client sees it.
    private fun <T> withHub(block: suspend (MockWebServer, HttpHubManagers) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpHubManagers { path, method, body ->
                val response = http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
                if (!response.isSuccessful) throw response.use { HubFailure.from(it) }
                response
            })
        }
    }

    @Test fun readsTheRosterAndToleratesWhatItDoesNotKnow() { withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"managers":[
            {"id":"assistant","name":"助手","online":true,"default":true,"builtin":true,"host":"server","os":"linux","arch":"amd64",
             "deploy":"child","version":"1.0.0","since":"2026-11-05T01:00:00Z","future":[1]},
            {"id":"m-8b04d2","name":"家里","online":false,"host":"NAS","os":"linux","deploy":"docker","last_seen":"2026-11-04T22:00:00Z","needs_update":true},
            {"id":"m-c1e9f0","name":"测试","online":false,"installing":"2026-11-05T06:07:00Z"}],"default":"assistant"}"""))
        val roster = api.roster()
        assertEquals("/api/v1/managers", server.takeRequest().path)
        assertEquals("assistant", roster.defaultId)
        assertEquals(listOf("assistant", "m-8b04d2", "m-c1e9f0"), roster.managers.map { it.id })
        val own = roster.managers[0]
        assertTrue(own.isDefault && own.builtin && own.online && !own.isInstalling)
        assertEquals("child", own.deploy)
        assertEquals("2026-11-05T01:00:00Z", own.since)
        assertEquals("2026-11-04T22:00:00Z", roster.managers[1].lastSeen)
        assertTrue(roster.managers[1].needsUpdate && !own.needsUpdate)
        assertTrue(roster.managers[2].isInstalling)
        assertNull(roster.managers[2].host)
    } }

    @Test fun aHubWithoutTheRosterIsUnsupported() { withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"not found"}"""))
        assertTrue(runCatching { api.roster() }.exceptionOrNull() is ManagersUnsupported)
    } }

    @Test fun makesAManagerWithOrWithoutCommands() { withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"manager":{"id":"m-1a2b3c","name":"工作","online":false,
            "installing":"2026-11-05T06:10:00Z"},"code":"mi_0123","expires_at":"2026-11-05T06:10:00Z",
            "commands":{"service":"curl -fsSL <hub>/i/mi_0123 | sh"}}"""))
        val made = api.create("工作")
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/managers", request.path)
        assertEquals("""{"name":"工作"}""", request.body.readUtf8())
        assertEquals("mi_0123", made.code)
        assertEquals("m-1a2b3c", made.manager?.id)
        assertEquals("curl -fsSL <hub>/i/mi_0123 | sh", made.commands?.service)
        assertNull(made.commands?.docker)

        server.enqueue(MockResponse().setBody("""{"code":"mi_4567","expires_at":"2026-11-05T06:20:00Z"}"""))
        val again = api.newCode("m-1a2b3c")
        val second = server.takeRequest()
        assertEquals("POST", second.method)
        assertEquals("/api/v1/managers/m-1a2b3c/code", second.path)
        assertEquals("{}", second.body.readUtf8())
        assertEquals("mi_4567", again.code)
        assertNull(again.commands)
    } }

    @Test fun renamesMakesDefaultAndDeletes() { withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"manager":{"id":"m-3fa21c","name":"公司","online":true}}"""))
        assertEquals("公司", api.rename("m-3fa21c", "公司").name)
        server.takeRequest().let {
            assertEquals("PATCH", it.method)
            assertEquals("/api/v1/managers/m-3fa21c", it.path)
            assertEquals("""{"name":"公司"}""", it.body.readUtf8())
        }
        server.enqueue(MockResponse().setBody("""{"manager":{"id":"m-3fa21c","name":"公司","online":true,"default":true}}"""))
        assertTrue(api.makeDefault("m-3fa21c").isDefault)
        assertEquals("""{"default":true}""", server.takeRequest().body.readUtf8())
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        api.delete("a/b")
        server.takeRequest().let {
            assertEquals("DELETE", it.method)
            // An id is one path segment, whatever is in it.
            assertEquals("/api/v1/managers/a%2Fb", it.path)
        }
    } }

    /** Show in the switcher (#9055): `{"hide": …}` to the built-in's path; the roster carries the choice and whether it is in effect. */
    @Test fun hidesTheBuiltInAndReadsTheChoice() { withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"manager":{"id":"assistant","name":"助手","builtin":true,"hide":true,"hidden":true}}"""))
        api.setHide("assistant", true)
        server.takeRequest().let {
            assertEquals("PATCH", it.method)
            assertEquals("/api/v1/managers/assistant", it.path)
            assertEquals("""{"hide":true}""", it.body.readUtf8())
        }
        server.enqueue(MockResponse().setBody("""{"manager":{"id":"assistant","name":"助手","builtin":true}}"""))
        api.setHide("assistant", false)
        assertEquals("""{"hide":false}""", server.takeRequest().body.readUtf8())
        server.enqueue(MockResponse().setBody("""{"managers":[{"id":"m-2","default":true},
            {"id":"assistant","builtin":true,"hide":true,"hidden":true}],"default":"m-2"}"""))
        val roster = api.roster()
        assertTrue(roster.managers[1].hide && roster.managers[1].hidden)
        assertFalse(roster.managers[0].hide || roster.managers[0].hidden)
        // In use (the default, or it owns a computer): the hub's refusal keeps its code.
        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"the built-in assistant is in use","code":"builtin_in_use"}"""))
        val refused = runCatching { api.setHide("assistant", true) }.exceptionOrNull() as HubFailure
        assertEquals(409, refused.status)
        assertEquals("builtin_in_use", refused.code)
    } }

    @Test fun theHubsRefusalsKeepTheirCodes() { withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"make another manager the default first","code":"default_manager"}"""))
        val refused = runCatching { api.delete("assistant") }.exceptionOrNull() as HubFailure
        assertEquals(409, refused.status)
        assertEquals("default_manager", refused.code)
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"no manager","code":"no_manager"}"""))
        // A 404 with the roster's own code is that manager gone, not an old hub.
        val gone = runCatching { api.roster() }.exceptionOrNull()
        assertTrue(gone is HubFailure && gone.code == "no_manager")
    } }

    @Test fun theRosterEventDecodes() {
        assertEquals(ManagersEvent, JsonCodec.decodeFromString(ServerEventSerializer, """{"type":"managers"}"""))
        assertEquals("""{"type":"managers"}""", JsonCodec.encodeToString(ServerEventSerializer, ManagersEvent))
    }
}

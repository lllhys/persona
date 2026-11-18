package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.JsonCodec
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonPrimitive

/** Settings › Model providers' calls (#3048): the list, one provider, hide / restore / delete, "Test availability", "Import from ~/.zshrc". */
class HttpProviderListTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpProviderSetup) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpProviderSetup { path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            })
        }
    }

    @Test fun listsAndReadsProviders() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"launchers":[{"name":"team","source":"persona","base_url":"https://gateway.example.com","model":"m",
            "has_key":true,"models":[{"id":"m"},{"id":"n","context_window":1000}]},{"name":"cc_a","source":"zshrc","machines":["mac"],"hidden":true}]}"""))
        val list = api.launchers()
        assertEquals("/api/v1/launchers", server.takeRequest().path)
        assertEquals(listOf("team", "cc_a"), list.map { it.name })
        assertTrue(list[0].managed && list[0].hasKey)
        assertEquals(listOf("n"), list[0].models.drop(1).map { it.id })
        assertTrue(list[1].hidden && !list[1].managed)

        server.enqueue(MockResponse().setBody("""{"launcher":{"name":"cc a","source":"zshrc","definitions":{"mac":"cc() {}"}}}"""))
        assertEquals("cc() {}", api.launcher("cc a").definitions["mac"])
        assertEquals("/api/v1/launchers/cc%20a", server.takeRequest().path)
    }

    @Test fun hidesRestoresAndChecks() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"ok":true,"hidden":true}"""))
        api.remove("cc_a")
        server.takeRequest().let { assertEquals("DELETE", it.method); assertEquals("/api/v1/launchers/cc_a", it.path) }
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        api.restore("cc_a")
        server.takeRequest().let { assertEquals("POST", it.method); assertEquals("/api/v1/launchers/cc_a/restore", it.path) }
        server.enqueue(MockResponse().setBody("""{"machine":"mac","check":{"ok":true,"model":"m","answer":"OK","millis":812}}"""))
        val answer = api.check("cc_a", "mac")
        assertEquals("/api/v1/launchers/cc_a/check?machine=mac", server.takeRequest().path)
        assertEquals("mac", answer.machine)
        assertTrue(answer.check.ok)
        assertEquals(812, answer.check.millis)
        // A direct API answers without a machine.
        server.enqueue(MockResponse().setBody("""{"check":{"ok":false,"agent":"api","error":"401","millis":5}}"""))
        val api2 = api.check("team", null)
        assertEquals("/api/v1/launchers/team/check", server.takeRequest().path)
        assertNull(api2.machine)
        assertEquals("401", api2.check.error)
    }

    @Test fun oneAliasIsScopedOnBothPreviewAndImport() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"machine":"mac","providers":[{"name":"cc work","aliases":["cc work"]}]}"""))
        api.importPreview("mac", "cc work")
        assertEquals("/api/v1/launchers/import?machine=mac&alias=cc%20work", server.takeRequest().path)
        server.enqueue(MockResponse().setBody("""{"created":["cc-work"],"updated":[]}"""))
        api.import("mac", listOf("cc work"), "cc work")
        val request = server.takeRequest()
        val body = JsonCodec.parseToJsonElement(request.body.readUtf8()).jsonObject
        assertEquals("cc work", body["alias"]!!.jsonPrimitive.content)
        assertEquals(listOf("cc work"), body["providers"]!!.jsonArray.map { it.jsonPrimitive.content })
    }

    @Test fun importsWhatIsTicked() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"machine":"mac","providers":[{"name":"proxy","base_url":"https://p.example.com","model":"m",
            "has_key":true,"aliases":["cc_a","cc_b"]},{"name":"team","existing":"team","aliases":[]}]}"""))
        val found = api.importPreview("mac")
        assertEquals("/api/v1/launchers/import?machine=mac", server.takeRequest().path)
        assertEquals(listOf("cc_a", "cc_b"), found[0].aliases)
        assertEquals("team", found[1].existing)
        server.enqueue(MockResponse().setBody("""{"created":["proxy"],"updated":[]}"""))
        val result = api.import("mac", listOf("proxy"))
        val request = server.takeRequest()
        assertEquals("/api/v1/launchers/import", request.path)
        val body = JsonCodec.parseToJsonElement(request.body.readUtf8()).jsonObject
        assertEquals("mac", body["machine"]!!.jsonPrimitive.content)
        assertEquals(listOf("proxy"), body["providers"]!!.jsonArray.map { it.jsonPrimitive.content })
        assertEquals(listOf("proxy"), result.created)
    }
}

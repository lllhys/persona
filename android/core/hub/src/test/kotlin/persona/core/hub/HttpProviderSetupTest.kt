package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.NewProvider
import persona.core.contracts.api.PresetModel
import persona.core.contracts.api.ProviderProbe
import persona.core.contracts.api.ProviderUpdate
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test

/** "Add model provider"'s calls: presets (none from an older hub), and the body that adds one, blank fields left out. */
class HttpProviderSetupTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpProviderSetup) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpProviderSetup { path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            })
        }
    }

    @Test fun presetsDecodeAndAnOlderHubHasNone() = withHub { server, providers ->
        server.enqueue(MockResponse().setBody("""{"presets":[{"id":"example-gateway/team","plugin":"example-gateway","name":"示例网关 · 团队",
            "kind":"launcher","base_url":"https://gateway.example.com","model":"example-pro","models":[{"id":"example-mini","name":"Example Mini"}],
            "key_hint":"以 eg- 开头","unknown":1},{"id":"example-gateway/api","kind":"api","api":"responses","base_url":"https://api.example.com/v1",
            "models":[{"id":"m1"}],"models_error":"超时"}]}"""))
        val presets = providers.presets()
        assertEquals("/api/v1/launchers/presets", (server.takeRequest().path).zhAny())
        assertEquals(listOf("example-gateway/team", "example-gateway/api"), (presets.map { it.id }).zhAny())
        assertEquals("example-pro", (presets[0].defaultModel).zhAny())
        assertEquals(listOf("example-pro", "example-mini"), (presets[0].choices.map { it.id }).zhAny())
        assertEquals(listOf("example-pro", "Example Mini"), (presets[0].choices.map { it.label }).zhAny())
        assertEquals("以 eg- 开头", (presets[0].keyHint).zhAny())
        assertEquals("m1", (presets[1].defaultModel).zhAny())
        assertEquals("超时", (presets[1].modelsError).zhAny())

        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"没有这个启动器"}"""))
        assertTrue(providers.presets().isEmpty())
        assertEquals("/api/v1/launchers/presets", (server.takeRequest().path).zhAny())
    }

    @Test fun addingSendsOnlyWhatWasFilledIn() = withHub { server, providers ->
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        providers.add(NewProvider("jv_team", "eg-secret", preset = "example-gateway/team"))
        val request = server.takeRequest()
        assertEquals("POST", (request.method).zhAny())
        assertEquals("/api/v1/launchers", (request.path).zhAny())
        val body = JsonCodec.parseToJsonElement(request.body.readUtf8()).jsonObject
        assertEquals(setOf("name", "api_key", "preset"), body.keys)
        assertEquals("example-gateway/team", (body["preset"]!!.jsonPrimitive.content).zhAny())

        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"没有这个供应商预设（提供它的扩展停用或卸载了？）"}"""))
        val refused = runCatching { providers.add(NewProvider("jv_team", "k", preset = "gone/x")) }.exceptionOrNull() as HubFailure
        assertEquals(400, refused.status)
        assertTrue(refused.message.contains("没有这个供应商预设"))
    }

    // A launcher that is slow to answer (cold CLI, slow gateway): the hub allows the check 100 s and each import step
    // 30 s, so the phone must outwait it (iOS: 110 / 45) rather than fail at the default 15 (#4813).
    @Test fun checkAndImportWaitLongerThanTheDefault() = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            val timeouts = mutableMapOf<String, Long?>()
            val providers = HttpProviderSetup { path, method, body, timeout ->
                timeouts["$method ${path.substringBefore('?')}"] = timeout
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            }
            server.enqueue(MockResponse().setBody("""{"machine":"m1","check":{}}"""))
            providers.check("jv_team", "m1")
            server.enqueue(MockResponse().setBody("""{"providers":[]}"""))
            providers.importPreview("m1")
            server.enqueue(MockResponse().setBody("""{}"""))
            providers.import("m1", listOf("eg"))
            server.enqueue(MockResponse().setBody("""{"launchers":[]}"""))
            providers.launchers()
            assertEquals(mapOf("POST /api/v1/launchers/jv_team/check" to 110L, "GET /api/v1/launchers/import" to 45L,
                "POST /api/v1/launchers/import" to 45L, "GET /api/v1/launchers" to null), timeouts)
        }
    }

    // Editing (docs/integrations/launchers.md "API"): PUT carries the base fields even when empty (the hub replaces them) and
    // leaves out what did not change, so the stored key, headers and scope stay.
    @Test fun anEditSendsTheBaseFieldsAndOnlyWhatChanged() = withHub { server, providers ->
        server.enqueue(MockResponse().setBody("""{"launcher":{"name":"jv_gw"}}"""))
        providers.update("jv_gw", ProviderUpdate("jv_gw", "launcher", "messages", "https://gateway.example.com", "gw-pro", "", "", "", "", "",
            emptyList(), auth = "bearer", sonnetModel = "gw-mid", headers = "", delivery = "push", machineKeys = mapOf("mac" to "")))
        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/launchers/jv_gw", request.path)
        val body = JsonCodec.parseToJsonElement(request.body.readUtf8()).jsonObject
        assertEquals(setOf("name", "kind", "api", "base_url", "model", "small_model", "subagent_model", "custom_model", "custom_model_name",
            "custom_model_description", "args", "auth", "sonnet_model", "headers", "delivery", "machine_keys"), body.keys)
        assertEquals("", body["small_model"]!!.jsonPrimitive.content)
        assertEquals("", body["headers"]!!.jsonPrimitive.content)
        assertEquals("", body["machine_keys"]!!.jsonObject["mac"]!!.jsonPrimitive.content)
        assertFalse("the key is kept", "api_key" in body)

        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"这台电脑不在范围内","code":"launcher_out_of_scope"}"""))
        val refused = runCatching { providers.update("jv_gw", ProviderUpdate("jv_gw", "launcher", "messages", "", "m", "", "", "", "", "", emptyList())) }
            .exceptionOrNull() as HubFailure
        assertEquals(409, refused.status)
        assertEquals("launcher_out_of_scope", refused.code)
    }

    @Test fun aManagedProvidersScopeAndParametersAreRead() = withHub { server, providers ->
        server.enqueue(MockResponse().setBody("""{"launchers":[{"name":"jv_gw","source":"persona","agent":"codex","base_url":"https://gw.example.com",
            "model":"gw-pro","models":[{"id":"gw-pro","context_window":1000000,"efforts":["high"],"default_effort":"high"}],"opus_model":"gw-max",
            "proxy":"http://127.0.0.1:7890","timeout_ms":600000,"has_env":true,"share":"selected","scope_machines":["mac"],"delivery":"push",
            "own_service":"use","key_machines":["mac"],"legacy_scope":true}]}"""))
        val l = providers.launchers().single()
        assertEquals("codex", l.harness)
        assertTrue(l.hasScope)
        assertEquals(listOf("mac"), l.scopeMachines)
        assertEquals("push", l.delivery)
        assertEquals("use", l.ownService)
        assertEquals(listOf("mac"), l.keyMachines)
        assertTrue(l.legacyScope)
        assertEquals(PresetModel("gw-pro", contextWindow = 1000000, efforts = listOf("high"), defaultEffort = "high"), l.models.single())
        assertEquals(600000, l.timeoutMs)
        assertEquals("http://127.0.0.1:7890", l.proxy)
    }

    @Test fun aProbeOfAStoredProviderGoesByName() = withHub { server, providers ->
        server.enqueue(MockResponse().setBody("""{"ok":true,"ms":12}"""))
        providers.test(ProviderProbe(baseUrl = "https://gw.example.com", auth = "bearer", model = "m", name = "jv_gw", headers = ""))
        val body = JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        assertEquals("jv_gw", body["name"]!!.jsonPrimitive.content)
        // "" sends no headers for this request, over the stored ones.
        assertEquals("", body["headers"]!!.jsonPrimitive.content)
        assertFalse("api_key" in body)
    }
}

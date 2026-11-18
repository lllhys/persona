package persona.core.hub

import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.KnowledgeSelection
import persona.core.contracts.api.KnowledgeSelectionUnsupported
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer

class HttpKnowledgeSelectionTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpKnowledgeSelection) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            val api = HttpKnowledgeSelection { path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            }
            block(server, api)
        }
    }

    @Test fun selectionReadsAndReplacesRawJsonWithoutDroppingFieldsOrNulls() = withHub { server, api ->
        val original = JsonObject(
            Json.parseToJsonElement("""{"version":1,"messages":{"future":null},"items":null,"future":{"kept":[1,null,false]}}""").jsonObject,
        )
        server.enqueue(MockResponse().setBody(original.toString()))
        assertEquals(original, api.selection())
        server.takeRequest().let {
            assertEquals("GET", it.method)
            assertEquals("/api/v1/kb/selection", it.path)
        }

        val draft = Json.parseToJsonElement("""{"version":1,"messages":{"rules":{}},"optional":null,"future":[0,null]}""").jsonObject
        val stored = Json.parseToJsonElement("""{"version":1,"messages":{"rules":{"mine":null}},"optional":null,"stored":true}""").jsonObject
        server.enqueue(MockResponse().setBody(stored.toString()))
        assertEquals(stored, api.put(draft))
        server.takeRequest().let {
            assertEquals("PUT", it.method)
            assertEquals("/api/v1/kb/selection", it.path)
            assertEquals(draft.toString(), it.body.readUtf8())
        }
        assertEquals(emptyList<Any>(), api.changes.toList())
        assertEquals(2, server.requestCount)
    }

    @Test fun candidatesUseAnEscapedSourceAndDecodeEveryFlagAndRawItem() = withHub { server, api ->
        val source = "connector & id/+#?"
        server.enqueue(MockResponse().setBody("""{"source":"$source","cached":true,"listed_at":"2026-11-15T01:02:03Z","chats":[
            {"id":"c/1","name":"Project","kind":"group","source":"filed-source","bot":true,"muted":true,"collapsed":true,"done":true,
             "estimate":12,"item":{"source":"filed-source","kind":"chat","id":"c/1","read":true,"store":false,"days":null,"future":1},
             "read":"personal","days":30,"unknown":true},
            {"id":"plain","name":"Plain","kind":"dm","source":"connector","item":null,"read":"none"}],"future":true}"""))
        val answer = api.candidates(source)
        assertEquals(source, answer.source)
        assertTrue(answer.cached)
        assertEquals("2026-11-15T01:02:03Z", answer.listedAt)
        val full = answer.chats.first()
        assertEquals("filed-source", full.source)
        assertTrue(full.bot && full.muted && full.collapsed && full.done)
        assertEquals(12, full.estimate)
        assertEquals("personal", full.read)
        assertEquals(30, full.days)
        assertEquals(
            Json.parseToJsonElement("""{"source":"filed-source","kind":"chat","id":"c/1","read":true,"store":false,"days":null,"future":1}"""),
            full.item,
        )
        val plain = answer.chats[1]
        assertFalse(plain.bot || plain.muted || plain.collapsed || plain.done)
        assertEquals(0, plain.estimate)
        assertEquals(0, plain.days)
        assertNull(plain.item)
        server.takeRequest().let {
            assertEquals("GET", it.method)
            assertEquals("/api/v1/kb/selection/candidates", it.requestUrl?.encodedPath)
            assertEquals("source=connector%20%26%20id%2F%2B%23%3F", it.requestUrl?.encodedQuery)
            assertEquals(source, it.requestUrl?.queryParameter("source"))
        }
    }

    @Test fun documentPickerReadsTheExistingListAndEscapedDetailEnvelope() = withHub { server, api ->
        val id = "doc/# id"
        server.enqueue(MockResponse().setBody("""{"items":[{"kind":"docs","id":"$id","name":"Design","one_line":null,"tags":null}],"tags":null,"future":true}"""))
        val list = api.documents()
        assertEquals(id, list.items.single().id)
        assertEquals("Design", list.items.single().name)
        assertNull(list.tags)
        server.takeRequest().let {
            assertEquals("GET", it.method)
            assertEquals("/api/v1/kb/docs", it.path)
        }

        server.enqueue(MockResponse().setBody("""{"detail":{"kind":"docs","id":"$id","path":"data/docs/doc",
            "meta":{"source":"connector","id":"original/id","nullable":null},"summary":null},"agents":null,"future":true}"""))
        val detail = api.document(id)
        assertEquals("docs", detail.kind)
        assertEquals(id, detail.id)
        assertEquals("connector", detail.meta?.get("source")?.jsonPrimitive?.content)
        assertEquals("original/id", detail.meta?.get("id")?.jsonPrimitive?.content)
        assertTrue(detail.meta?.containsKey("nullable") == true)
        server.takeRequest().let {
            assertEquals("GET", it.method)
            assertEquals("/api/v1/kb/docs/doc%2F%23%20id", it.path)
        }
    }

    @Test fun onlySelectionAndCandidatesNotFoundBecomeUnsupported() = withHub { server, api ->
        val selectionCalls = listOf<suspend KnowledgeSelection.() -> Any>(
            { selection() },
            { put(Json.parseToJsonElement("""{"version":1}""").jsonObject) },
            { candidates("chat") },
        )
        for (call in selectionCalls) {
            server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"no such selection route"}"""))
            assertTrue(api.runCatching { api.call() }.exceptionOrNull() is KnowledgeSelectionUnsupported)
            server.takeRequest()
        }

        for ((status, reason) in listOf(400 to "invalid selection", 403 to "read-only device")) {
            server.enqueue(MockResponse().setResponseCode(status).setBody("""{"error":"$reason"}"""))
            val failure = api.runCatching {
                put(Json.parseToJsonElement("""{"version":1}""").jsonObject)
            }.exceptionOrNull() as HubFailure
            assertEquals(status, failure.status)
            assertTrue(failure.message.contains(reason))
            server.takeRequest()
        }

        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"source is required"}"""))
        val badSource = api.runCatching { candidates("") }.exceptionOrNull() as HubFailure
        assertEquals(400, badSource.status)
        assertTrue(badSource.message.contains("source is required"))
        server.takeRequest()

        for (call in listOf<suspend KnowledgeSelection.() -> Any>({ documents() }, { document("missing") })) {
            server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"document not found"}"""))
            val failure = api.runCatching { api.call() }.exceptionOrNull()
            assertTrue(failure is HubFailure)
            assertFalse(failure is KnowledgeSelectionUnsupported)
            assertEquals(404, (failure as HubFailure).status)
            server.takeRequest()
        }
    }

    @Test fun malformedSuccessBodiesRemainFailures() = withHub { server, api ->
        val calls = listOf<suspend KnowledgeSelection.() -> Any>(
            { selection() },
            { put(Json.parseToJsonElement("""{"version":1}""").jsonObject) },
            { candidates("chat") },
            { documents() },
            { document("doc") },
        )
        for (call in calls) {
            server.enqueue(MockResponse().setBody("""{"unfinished":"""))
            val failure = api.runCatching { api.call() }.exceptionOrNull()
            assertTrue(failure is SerializationException)
            assertFalse(failure is KnowledgeSelectionUnsupported)
            server.takeRequest()
        }
        server.enqueue(MockResponse().setBody("[]"))
        assertTrue(api.runCatching { selection() }.exceptionOrNull() is IllegalArgumentException)
        server.takeRequest()
        assertEquals(6, server.requestCount)
    }
}

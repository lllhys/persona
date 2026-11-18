package persona.core.rustcore

import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Real Kotlin -> JNI -> shared pure Rust state fixture adapter. Expected objects come directly from the unchanged
 * JSON, never from Rust output. This is host evidence only: no production state adapter, NAPI or device is involved.
 */
class StateBindingFixtureTest {
    private val fixtures: File = generateSequence(File(".").canonicalFile) { it.parentFile }
        .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory }
        ?.resolve("spec/fixtures/sort") ?: error("Cannot locate spec/fixtures/sort")

    private val documents = mapOf(
        "session_state" to listOf("client_state_precedence", "waiting_requires_quiet_state"),
        "hub_state" to listOf(
            "hub_blocked", "hub_pending_offline", "hub_working_seen", "hub_done_unseen",
            "hub_done_seen_before_tolerance", "hub_done_seen_at_tolerance", "hub_done_seen_equal",
            "hub_done_seen_after", "hub_idle", "hub_offline", "hub_unknown",
        ),
    )

    private fun project(kind: String, input: JsonObject): String = NativeBridge.projectState(
        buildJsonObject {
            put("kind", kind)
            put("input", input)
        }.toString(),
    )

    @Test fun replayEveryStateFixtureThroughJni() {
        val replayed = mutableSetOf<String>()
        for ((kind, names) in documents) {
            val document = Json.parseToJsonElement(File(fixtures, "$kind.json").readText()).jsonObject
            assertEquals(1, document.getValue("schema_version").jsonPrimitive.content.toInt())
            assertEquals(kind, document.getValue("operation").jsonPrimitive.content)
            val cases = document.getValue("cases").jsonArray
            assertEquals("$kind: exact case count", names.size, cases.size)
            assertEquals("$kind: exact ordered case manifest", names, cases.map { it.jsonObject.getValue("name").jsonPrimitive.content })
            for (case in cases) {
                val value = case.jsonObject
                val name = value.getValue("name").jsonPrimitive.content
                val actual = project(kind, value.getValue("input").jsonObject)
                assertTrue("$kind/$name: JNI returned no projection", actual.isNotEmpty())
                assertEquals("$kind/$name: complete expected object", value.getValue("expected"), Json.parseToJsonElement(actual))
                assertTrue("$kind/$name: duplicate case", replayed.add(name))
            }
        }
        assertEquals("2 session_state + 11 hub_state", 13, replayed.size)
        println("JNI state host replay: ${replayed.size} complete expected objects")
    }

    @Test fun malformedAndWrongExpectationsAreRejected() {
        val document = Json.parseToJsonElement(File(fixtures, "hub_state.json").readText()).jsonObject
        val first = document.getValue("cases").jsonArray.first().jsonObject
        val actual = project("hub_state", first.getValue("input").jsonObject)
        assertEquals(first.getValue("expected"), Json.parseToJsonElement(actual))
        val wrong = buildJsonObject { put("state", "idle") }
        assertNotEquals("a deliberately wrong expected object must differ", wrong, Json.parseToJsonElement(actual))

        for (input in listOf(
            "{}",
            "[]",
            "not JSON",
            "{\"kind\":\"unknown\",\"input\":{}}",
            "{\"kind\":1,\"input\":{}}",
            "{\"kind\":\"hub_state\"}",
            "{\"kind\":\"hub_state\",\"input\":{},\"extra\":true}",
            "{\"kind\":\"session_state\",\"input\":{\"sessions\":{}}}",
            "{\"kind\":\"hub_state\",\"input\":{\"session\":{\"status\":\"done\",\"pending_interactions\":\"0\"}}}",
            "{\"kind\":\"hub_state\",\"input\":{\"session\":{\"status\":\"done\",\"pending_interactions\":0,\"updated_at\":\"invalid\"}}}",
        )) {
            assertEquals("malformed input must return the JNI failure value: $input", "", NativeBridge.projectState(input))
        }
    }
}

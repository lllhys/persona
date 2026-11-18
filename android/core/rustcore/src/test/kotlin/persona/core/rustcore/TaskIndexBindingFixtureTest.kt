package persona.core.rustcore

import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

/** Real Kotlin/JNI replay of the fixture-facing TaskIndex projection, never a Rust-generated expected object. */
class TaskIndexBindingFixtureTest {
    private val root: File = generateSequence(File(".").canonicalFile) { it.parentFile }
        .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory }
        ?: error("Cannot locate the repository")
    private val fixtures = root.resolve("spec/fixtures/lines")

    private fun replay(case: JsonObject): JsonElement {
        val name = case.getValue("name").jsonPrimitive.content
        val output = NativeBridge.projectTaskIndex(case.getValue("input").jsonObject.toString())
        assertTrue("$name: JNI returned no index", output.isNotEmpty())
        return Json.parseToJsonElement(output)
    }

    private fun assertIndexEqual(case: JsonObject, expected: JsonElement, actual: JsonElement) {
        assertEquals(case.getValue("name").jsonPrimitive.content, expected, actual)
    }

    @Test fun replayEveryTaskIndexCaseThroughJni() {
        val cases = requireNotNull(fixtures.listFiles { file -> file.isFile && file.extension == "json" })
            .sortedBy { it.name }
            .flatMap { Json.parseToJsonElement(it.readText()).jsonObject.getValue("cases").jsonArray }
            .map { it.jsonObject }
        assertEquals("the exact task-index manifest", expectedCases, cases.filter {
            it.getValue("expected").jsonObject.containsKey("task_index")
        }.map { it.getValue("name").jsonPrimitive.content }.toSet())
        val replayed = mutableSetOf<String>()
        for (case in cases) {
            val name = case.getValue("name").jsonPrimitive.content
            val actual = replay(case).jsonObject
            if (name == "background_running_badge") {
                assertEquals("the background receipt starts its call without a notice", listOf("call_a"),
                    actual.getValue("started").jsonArray.map { it.jsonPrimitive.content })
                assertEquals("no task notice means no calls", JsonObject(emptyMap()), actual.getValue("calls"))
                assertEquals("no task notice means no statuses", JsonObject(emptyMap()), actual.getValue("statuses"))
                assertEquals("the original tool description is retained", buildJsonObject {
                    put("call_a", "检查示例文件")
                }, actual.getValue("descriptions"))
                assertEquals("no tool timestamp means no started_at", JsonObject(emptyMap()), actual.getValue("started_at"))
                continue
            }
            val expected = case.getValue("expected").jsonObject["task_index"] ?: continue
            assertIndexEqual(case, expected, actual)
            assertTrue("$name: duplicate replay", replayed.add(name))
        }
        assertEquals(expectedCases, replayed)
        println("JNI TaskIndex host replay: ${replayed.size} complete expected objects; one no-notice case")
    }

    @Test fun boundaryRejectsMalformedInputAndTheComparisonRejectsAWrongExpectation() {
        assertEquals("", NativeBridge.projectTaskIndex("{\"invalid\":true}"))
        assertEquals("", NativeBridge.projectTaskIndex("not JSON"))
        val case = fixture("task_notices_find_calls")
        val actual = replay(case)
        val wrong = buildJsonObject {
            put("started", kotlinx.serialization.json.JsonArray(emptyList()))
            put("calls", buildJsonObject {})
            put("descriptions", buildJsonObject {})
            put("started_at", buildJsonObject {})
            put("statuses", buildJsonObject {})
        }
        assertNotEquals("the negative index must differ", wrong, actual)
        val failure = assertThrows("the complete-index comparison accepted a wrong expectation", AssertionError::class.java) {
            assertIndexEqual(case, wrong, actual)
        }
        assertTrue(failure.message.orEmpty().contains("task_notices_find_calls"))
        for ((timestamp, expected) in mapOf(
            "2026-01-01T08:01:00+08:00" to "2026-01-01T00:01:00Z",
            "2026-01-01T00:01:00.500Z" to "2026-01-01T00:01:00Z",
            "2025-12-31T19:01:00-05:00" to "2026-01-01T00:01:00Z",
        )) {
            val input = buildJsonObject {
                put("items", kotlinx.serialization.json.JsonArray(listOf(
                    buildJsonObject {
                        put("id", "tool#0")
                        put("seq", 1)
                        put("role", "assistant")
                        put("kind", "tool_use")
                        put("tool_use_id", "call_a")
                        put("ts", timestamp)
                    },
                    buildJsonObject {
                        put("id", "result#0")
                        put("seq", 2)
                        put("role", "user")
                        put("kind", "tool_result")
                        put("tool_use_id", "call_a")
                        put("text", "Command running in background with ID: task_a")
                    },
                )))
            }
            val actual = NativeBridge.projectTaskIndex(input.toString())
            assertEquals("Apple RFC3339 offsets and whole-second UTC: $timestamp", buildJsonObject {
                put("call_a", expected)
            }, Json.parseToJsonElement(actual).jsonObject.getValue("started_at"))
        }
        for (timestamp in listOf(
            "2026-01-01T00:01:00+99:00", "2026-01-01T00:01:00+00:99",
            "2026-01-01T00:01:00+0800", "2026-01-01T00:01:00+0A:00",
            "2026-01-01T00:01:00+01:0A", "2026-01-01T00:01:00+08:0",
        )) {
            val invalidInput = buildJsonObject {
                put("items", kotlinx.serialization.json.JsonArray(listOf(
                    buildJsonObject {
                        put("id", "tool#0")
                        put("seq", 1)
                        put("role", "assistant")
                        put("kind", "tool_use")
                        put("tool_use_id", "call_a")
                        put("ts", timestamp)
                    },
                    buildJsonObject {
                        put("id", "result#0")
                        put("seq", 2)
                        put("role", "user")
                        put("kind", "tool_result")
                        put("tool_use_id", "call_a")
                        put("text", "Command running in background with ID: task_a")
                    },
                )))
            }
            val actual = Json.parseToJsonElement(NativeBridge.projectTaskIndex(invalidInput.toString())).jsonObject
            assertEquals("$timestamp: receipt still starts the call", listOf("call_a"),
                actual.getValue("started").jsonArray.map { it.jsonPrimitive.content })
            assertEquals("$timestamp: invalid offset is omitted, never normalized as UTC",
                JsonObject(emptyMap()), actual.getValue("started_at"))
        }
    }

    private fun fixture(name: String): JsonObject = requireNotNull(fixtures.listFiles { file -> file.isFile && file.extension == "json" })
        .flatMap { Json.parseToJsonElement(it.readText()).jsonObject.getValue("cases").jsonArray }
        .map { it.jsonObject }.first { it.getValue("name").jsonPrimitive.content == name }

    private companion object {
        val expectedCases = setOf(
            "task_notices_find_calls", "task_title_containing_interrupt_is_not_interrupt",
            "monitor_event_uses_first_detail_line", "task_without_loaded_call_uses_title", "task_status_outcomes",
        )
    }
}

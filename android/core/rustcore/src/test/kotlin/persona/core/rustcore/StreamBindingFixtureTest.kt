package persona.core.rustcore

import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
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

/** Kotlin drives the real Rust split through JNI. Expected complete objects remain the checked-in Apple-derived oracle. */
class StreamBindingFixtureTest {
    private val root: File = generateSequence(File(".").canonicalFile) { it.parentFile }
        .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory }
        ?: error("Cannot locate the repository")
    private val fixturePath = root.resolve("spec/fixtures/stream-chunks/split.json")
    private val document = Json.parseToJsonElement(fixturePath.readText()).jsonObject
    private val cases = document.getValue("cases").jsonArray.map { it.jsonObject }

    private fun replay(case: JsonObject): JsonElement {
        val name = case.getValue("name").jsonPrimitive.content
        val output = NativeBridge.projectStreamChunks(case.getValue("input").jsonObject.toString())
        assertTrue("$name: JNI returned no projection", output.isNotEmpty())
        return Json.parseToJsonElement(output)
    }

    private fun assertChunksEqual(case: JsonObject, expected: JsonElement, actual: JsonElement) {
        assertEquals(case.getValue("name").jsonPrimitive.content, expected, actual)
    }

    @Test fun replayEveryStreamSplitThroughJni() {
        assertTrue("the checked-in stream fixture is required", fixturePath.isFile)
        assertEquals(1, document.getValue("schema_version").jsonPrimitive.content.toInt())
        assertEquals("stream_chunks_split", document.getValue("operation").jsonPrimitive.content)
        assertEquals("the exact source-cited case manifest", expectedCases, cases.map {
            it.getValue("name").jsonPrimitive.content
        }.toSet())
        val replayed = mutableSetOf<String>()
        for (case in cases) {
            val name = case.getValue("name").jsonPrimitive.content
            assertTrue("$name: description", case.getValue("description").jsonPrimitive.content.isNotEmpty())
            assertTrue("$name: Apple source", case.getValue("source").jsonArray.isNotEmpty())
            assertEquals("$name: exactly one provenance marker", case.containsKey("from_test"), !case.containsKey("derived"))
            if (case.containsKey("derived")) assertEquals(true, case.getValue("derived").jsonPrimitive.content.toBoolean())
            else assertTrue(case.getValue("from_test").jsonPrimitive.content.isNotEmpty())
            val actual = replay(case)
            assertChunksEqual(case, case.getValue("expected"), actual)
            if (name != "space_padded_separator_is_omitted") {
                val chunks = actual.jsonObject
                val rebuilt = chunks.getValue("done").jsonArray.map { it.jsonPrimitive.content }
                    .plus(chunks.getValue("tail").jsonPrimitive.content).joinToString("\n\n")
                assertEquals("$name: reconstruction is secondary, not the expected oracle",
                    case.getValue("input").jsonObject.getValue("text").jsonPrimitive.content, rebuilt)
            }
            assertTrue("$name: duplicate replay", replayed.add(name))
        }
        assertEquals(expectedCases, replayed)
        println("JNI stream split replay: ${replayed.size} complete expected objects")
    }

    @Test fun boundaryRejectsMalformedInputAndTheComparisonRejectsAWrongExpectation() {
        assertEquals("", NativeBridge.projectStreamChunks("{\"invalid\":true}"))
        assertEquals("", NativeBridge.projectStreamChunks("not JSON"))
        val case = cases.first { it.getValue("name").jsonPrimitive.content == "cuts_after_blank_line_once_next_block_starts" }
        val actual = replay(case)
        val wrong = buildJsonObject {
            put("done", JsonArray(emptyList()))
            put("tail", "wrong")
        }
        assertNotEquals("the negative fixture must actually differ", wrong, actual)
        val failure = assertThrows("the complete-chunk comparison accepted a wrong expectation", AssertionError::class.java) {
            assertChunksEqual(case, wrong, actual)
        }
        assertTrue(failure.message.orEmpty().contains("cuts_after_blank_line_once_next_block_starts"))
    }

    private companion object {
        val expectedCases = setOf(
            "blank_line_at_end_is_not_cut_yet", "carriage_return_is_not_a_line_separator",
            "consecutive_blank_lines_keep_their_newline", "crlf_carriage_return_is_not_a_blank_line",
            "crlf_fence_is_not_recognized", "cuts_after_blank_line_once_next_block_starts", "empty_input",
            "indented_fence_preserves_original_spaces", "inline_backticks_do_not_open_a_fence",
            "keeps_indented_continuation_with_block", "language_suffixed_line_does_not_close_fence",
            "leading_blank_line_can_finish_an_empty_block", "longer_fence_ignores_shorter_inner_closer",
            "never_cuts_inside_code_fence", "non_ascii_space_is_not_raw_indentation", "prewarm_uses_the_tail",
            "reassembles_full_text", "space_padded_separator_is_omitted",
            "tab_indented_continuation_stays_with_block", "tilde_fence_requires_tilde_closer",
            "unclosed_fence_keeps_remainder_in_tail",
        )
    }
}

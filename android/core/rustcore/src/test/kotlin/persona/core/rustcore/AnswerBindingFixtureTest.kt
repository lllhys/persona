package persona.core.rustcore

import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
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
import persona.core.domain.answer.AnswerBuilder

/** Real Kotlin/JNI consumers for the current-iOS question transition and display-only recommendation projection. */
class AnswerBindingFixtureTest {
    private val root: File = generateSequence(File(".").canonicalFile) { it.parentFile }
        .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory }
        ?: error("Cannot locate the repository")
    private val fixtures = root.resolve("spec/fixtures/answer")

    private fun project(input: JsonObject): JsonElement {
        val output = NativeBridge.projectAnswer(input.toString())
        assertTrue("JNI returned no answer transition", output.isNotEmpty())
        return Json.parseToJsonElement(output)
    }

    private fun display(label: String): JsonElement {
        val output = NativeBridge.displayAnswer(label)
        assertTrue("JNI returned no option display", output.isNotEmpty())
        return Json.parseToJsonElement(output)
    }

    private fun actual(case: JsonObject, recommended: Boolean): JsonObject {
        val input = case.getValue("input").jsonObject
        val transition = project(input).jsonObject
        val operation = input.getValue("operation").jsonObject
        return buildJsonObject {
            put("allowed", transition.getValue("allowed"))
            put("resolution", transition.getValue("resolution"))
            if (transition.getValue("page") != operation.getValue("page")) {
                put("next_page", transition.getValue("page"))
            }
            val multiSelect = input.getValue("interaction").jsonObject.getValue("questions").jsonArray
                .get(operation.getValue("page").jsonPrimitive.content.toInt()).jsonObject
                .getValue("multi_select").jsonPrimitive.content.toBoolean()
            if (operation.getValue("action").jsonPrimitive.content == "pick_option" &&
                transition.getValue("allowed").jsonPrimitive.content.toBoolean() && multiSelect
            ) {
                put("picks", transition.getValue("picks"))
            }
            if (recommended) {
                val label = input.getValue("interaction").jsonObject.getValue("questions").jsonArray
                    .get(operation.getValue("page").jsonPrimitive.content.toInt()).jsonObject
                    .getValue("options").jsonArray
                    .get(operation.getValue("option_index").jsonPrimitive.content.toInt()).jsonObject
                    .getValue("label").jsonPrimitive.content
                put("display", display(label))
            }
        }
    }

    private fun assertExpected(case: JsonObject, expected: JsonElement, actual: JsonElement) {
        assertEquals(case.getValue("name").jsonPrimitive.content, expected, actual)
    }

    @Test fun replayEveryQuestionAndRecommendationThroughJni() {
        for ((file, expectedCases) in manifests) {
            val document = Json.parseToJsonElement(fixtures.resolve(file).readText()).jsonObject
            assertEquals(1, document.getValue("schema_version").jsonPrimitive.content.toInt())
            val cases = document.getValue("cases").jsonArray.map { it.jsonObject }
            assertEquals("$file: exact manifest", expectedCases, cases.map {
                it.getValue("name").jsonPrimitive.content
            }.toSet())
            val replayed = mutableSetOf<String>()
            for (case in cases) {
                val name = case.getValue("name").jsonPrimitive.content
                assertEquals("$name: source-derived profile", true, case.getValue("derived").jsonPrimitive.content.toBoolean())
                assertTrue(!case.containsKey("from_test"))
                assertTrue(case.getValue("source").jsonArray.isNotEmpty())
                assertEquals("ios_current", case.getValue("input").jsonObject.getValue("profile").jsonPrimitive.content)
                assertExpected(case, case.getValue("expected"), actual(case, file == "recommended.json"))
                assertTrue("$name: duplicate replay", replayed.add(name))
            }
            assertEquals(expectedCases, replayed)
            println("JNI answer host replay: $file: ${replayed.size} complete expected objects")
        }
    }

    @Test fun boundaryRejectsInvalidInputAndTheComparisonRejectsAWrongExpectation() {
        assertEquals("", NativeBridge.projectAnswer("{\"invalid\":true}"))
        assertEquals("", NativeBridge.projectAnswer("not JSON"))
        val question = fixture("questions.json", "single_tap_sends_original_label")
        val invalidPage = question.getValue("input").jsonObject.toMutableMap()
        invalidPage["operation"] = buildJsonObject {
            for ((key, value) in question.getValue("input").jsonObject.getValue("operation").jsonObject) put(key, value)
            put("page", 99)
        }
        assertEquals("", NativeBridge.projectAnswer(JsonObject(invalidPage).toString()))
        val invalidOption = question.getValue("input").jsonObject.toMutableMap()
        invalidOption["operation"] = buildJsonObject {
            for ((key, value) in question.getValue("input").jsonObject.getValue("operation").jsonObject) put(key, value)
            put("option_index", 99)
        }
        assertEquals("an out-of-range option must not be selected", "",
            NativeBridge.projectAnswer(JsonObject(invalidOption).toString()))
        val actual = actual(question, recommended = false)
        val wrong = buildJsonObject {
            put("allowed", false)
            put("resolution", JsonNull)
        }
        assertNotEquals("the negative fixture must differ", wrong, actual)
        val failure = assertThrows("the complete-answer comparison accepted a wrong expectation", AssertionError::class.java) {
            assertExpected(question, wrong, actual)
        }
        assertTrue(failure.message.orEmpty().contains("single_tap_sends_original_label"))
        assertEquals(buildJsonObject {
            put("allowed", false)
            put("page", 0)
            put("picks", buildJsonObject {})
            put("other", buildJsonObject { put("One", JsonPrimitive(" ​ ")) })
            put("resolution", JsonNull)
        }, project(buildJsonObject {
            put("interaction", buildJsonObject {
                put("kind", "question")
                put("questions", kotlinx.serialization.json.JsonArray(listOf(buildJsonObject {
                    put("question", "One")
                    put("options", kotlinx.serialization.json.JsonArray(listOf(buildJsonObject { put("label", "Yes") })))
                })))
            })
            put("operation", buildJsonObject {
                put("action", "submit")
                put("page", 0)
                put("picks", buildJsonObject {})
                put("other", buildJsonObject { put("One", JsonPrimitive(" ​ ")) })
            })
        }))
        assertEquals(buildJsonObject { put("text", "A\n"); put("recommended", true) },
            display("​ A\n (Recommended)"))
    }

    @Test fun kotlinAndJniMatchTheIosWhitespaceDisplayBoundary() {
        val case = fixture("recommended.json", "recommended_corefoundation_boundary")
        val interaction = case.getValue("input").jsonObject.getValue("interaction").jsonObject
        val label = interaction.getValue("questions").jsonArray[0].jsonObject
            .getValue("options").jsonArray[0].jsonObject.getValue("label").jsonPrimitive.content
        val expected = case.getValue("expected").jsonObject.getValue("display")
        val kotlin = AnswerBuilder.display(label)
        val kotlinJson = buildJsonObject { put("text", kotlin.text); put("recommended", kotlin.recommended) }
        assertEquals(expected, kotlinJson)
        assertEquals(kotlinJson, display(label))
    }

    private fun fixture(file: String, name: String): JsonObject =
        Json.parseToJsonElement(fixtures.resolve(file).readText()).jsonObject.getValue("cases").jsonArray
            .map { it.jsonObject }.first { it.getValue("name").jsonPrimitive.content == name }

    private companion object {
        val manifests = mapOf(
            "questions.json" to setOf(
                "all_answered_can_submit_from_earlier_page", "blank_other_cannot_submit", "blank_other_uses_picks",
                "empty_answer_cannot_submit", "last_page_missing_answer_returns_to_gap", "multiple_other_overrides_picks",
                "multiple_pick_toggles_off", "multiple_pick_waits_for_submit", "multiple_picks_follow_option_order",
                "multiple_questions_send_original_keys", "next_page_does_not_send", "other_answer_trimmed",
                "sending_disables_option", "single_pick_clears_other", "single_tap_sends_original_label",
            ),
            "recommended.json" to setOf(
                "recommended_case_sensitive", "recommended_chinese_ascii", "recommended_chinese_fullwidth",
                "recommended_corefoundation_boundary",
                "recommended_suffix_only", "recommended_spaced", "recommended_trailing_space",
                "recommended_trims_prefix_space", "recommended_unspaced",
            ),
        )
    }
}

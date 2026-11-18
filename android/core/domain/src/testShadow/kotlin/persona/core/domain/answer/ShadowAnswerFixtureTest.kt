package persona.core.domain.answer

import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Interaction
import persona.core.contracts.JsonCodec
import persona.core.rustcore.RustCoreAnswer

/** Replays the current 15 + 9 fixtures through the current Kotlin path and the real host JNI boundary. */
class ShadowAnswerFixtureTest {
    private val root = generateSequence(File(".").canonicalFile) { it.parentFile }
        .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory } ?: error("Cannot locate repository")
    private val kotlin = KotlinAnswerAdapter()

    @Test fun everyFixtureIsComparedCompletelyAndKnownDifferencesStayExplicit() {
        val totals = mutableMapOf<String, Int>()
        for ((file, expectedCases) in manifests) {
            val cases = Json.parseToJsonElement(root.resolve("spec/fixtures/answer/$file").readText()).jsonObject
                .getValue("cases").jsonArray.map { it.jsonObject }
            assertEquals("$file: exact manifest", expectedCases, cases.map { it.name }.toSet())
            for (case in cases) {
                val input = case.getValue("input").jsonObject
                val expected = case.getValue("expected")
                val kotlinActual = kotlinResult(case, file == "recommended.json")
                val rustActual = rustResult(case, file == "recommended.json")
                assertEquals(case.name, expected, kotlinActual)
                val knownMismatch = knownDifferences[case.name]
                if (knownMismatch == null) assertEquals(case.name, expected, rustActual)
                else {
                    assertEquals(case.name, knownMismatch, rustActual)
                    assertNotEquals("${case.name}: this known mismatch must not silently become parity", kotlinActual, rustActual)
                }
                totals[file] = totals.getOrDefault(file, 0) + 1
            }
        }
        assertEquals(mapOf("questions.json" to 15, "recommended.json" to 9), totals)
        println("Kotlin and real host-JNI answer replay: ${totals.values.sum()} complete expected objects; known differences are asserted separately")
    }

    @Test fun kotlinAdapterAndThePublicDefaultBoundaryStayEquivalent() {
        AnswerAdapters.install(KotlinAnswerAdapter())
        for (file in manifests.keys) {
            val cases = Json.parseToJsonElement(root.resolve("spec/fixtures/answer/$file").readText()).jsonObject
                .getValue("cases").jsonArray.map { it.jsonObject }
            for (case in cases) {
                assertEquals(case.name, case.getValue("expected"), publicResult(case, file == "recommended.json"))
            }
        }
    }

    private fun publicResult(case: JsonObject, recommended: Boolean): JsonObject {
        val input = case.getValue("input").jsonObject
        val interaction = JsonCodec.decodeFromJsonElement<Interaction>(input.getValue("interaction"))
        val operation = input.getValue("operation").jsonObject
        val draft = operation.draft()
        val result = AnswerBuilder.build(interaction, operation.action(), draft, AnswerContext(sending = operation.flag("sending")))
        return result.json(operation, interaction, recommended) { AnswerBuilder.display(it) }
    }

    private fun kotlinResult(case: JsonObject, recommended: Boolean): JsonObject {
        val input = case.getValue("input").jsonObject
        val interaction = JsonCodec.decodeFromJsonElement<Interaction>(input.getValue("interaction"))
        val operation = input.getValue("operation").jsonObject
        val result = kotlin.build(interaction, operation.action(), operation.draft(), AnswerContext(sending = operation.flag("sending")))
        return result.json(operation, interaction, recommended) { kotlin.display(it) }
    }

    private fun rustResult(case: JsonObject, recommended: Boolean): JsonObject {
        val input = case.getValue("input").jsonObject
        val transition = Json.parseToJsonElement(RustCoreAnswer.project(input.toString())).jsonObject
        val operation = input.getValue("operation").jsonObject
        val interaction = JsonCodec.decodeFromJsonElement<Interaction>(input.getValue("interaction"))
        val multi = interaction.questions.orEmpty()[operation.page()].multiSelect
        return buildJsonObject {
            put("allowed", transition.getValue("allowed"))
            put("resolution", transition.getValue("resolution"))
            if (transition.getValue("page") != operation.getValue("page")) put("next_page", transition.getValue("page"))
            if (operation.action() == AnswerAction.PickOption && multi && transition.getValue("allowed").jsonPrimitive.content.toBoolean()) {
                put("picks", transition.getValue("picks"))
            }
            if (recommended) {
                val label = interaction.questions.orEmpty()[operation.page()].options[operation.optionIndex()].label
                put("display", Json.parseToJsonElement(RustCoreAnswer.display(label)))
            }
        }
    }

    private fun AnswerResult.json(operation: JsonObject, interaction: Interaction, recommended: Boolean, display: (String) -> OptionDisplay): JsonObject = buildJsonObject {
        put("allowed", allowed)
        put("resolution", resolution?.let { JsonCodec.encodeToJsonElement(it) } ?: JsonNull)
        nextPage?.let { put("next_page", it) }
        val current = interaction.questions.orEmpty()[operation.page()]
        if (operation.action() == AnswerAction.PickOption && current.multiSelect && allowed) {
            put("picks", JsonObject(draft.picks.mapValues { (_, values) -> JsonArray(values.sorted().map(::JsonPrimitive)) }))
        }
        if (recommended) {
            val shown = display(current.options[operation.optionIndex()].label)
            put("display", buildJsonObject { put("text", shown.text); put("recommended", shown.recommended) })
        }
    }

    private val JsonObject.name get() = getValue("name").jsonPrimitive.content
    private fun JsonObject.page() = getValue("page").jsonPrimitive.content.toInt()
    private fun JsonObject.optionIndex() = getValue("option_index").jsonPrimitive.content.toInt()
    private fun JsonObject.flag(name: String) = this[name]?.jsonPrimitive?.content?.toBoolean() ?: false
    private fun JsonObject.action() = AnswerAction.entries.single { it.wire == getValue("action").jsonPrimitive.content }
    private fun JsonObject.draft() = AnswerDraft(
        page = page(),
        optionIndex = this["option_index"]?.jsonPrimitive?.content?.toInt() ?: 0,
        picks = this["picks"]?.jsonObject?.mapValues { (_, value) -> value.jsonArray.map { it.jsonPrimitive.content }.toSet() }.orEmpty(),
        other = this["other"]?.jsonObject?.mapValues { (_, value) -> value.jsonPrimitive.content }.orEmpty(),
    )

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
                "recommended_suffix_only", "recommended_spaced", "recommended_trailing_space",
                "recommended_trims_prefix_space", "recommended_unspaced", "recommended_corefoundation_boundary",
            ),
        )
        val knownDifferences = emptyMap<String, JsonObject>()
    }
}

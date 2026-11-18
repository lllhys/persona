package persona.core.domain.answer

import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Interaction
import persona.core.contracts.JsonCodec
import persona.core.contracts.Question
import persona.core.contracts.QuestionOption
import persona.core.rustcore.RustCoreAnswer

class ShadowAnswerCodecTest {
    @Test fun inputMatchesThePureCoreContractWithoutDroppingSelectedOrOtherAnswers() {
        val interaction = Interaction(id = "example", kind = "question", questions = listOf(
            Question(question = "One", multiSelect = true, options = listOf(QuestionOption("A"), QuestionOption("B"))),
        ))
        val draft = AnswerDraft(page = 0, picks = mapOf("One" to setOf("B", "A")), other = mapOf("One" to "custom"), optionIndex = 1)
        val encoded = encodeBuild(interaction, AnswerAction.Submit, draft, AnswerContext(sending = true))
        val decoded = JsonCodec.decodeFromString<CoreTransition>(RustCoreAnswer.project(encoded.toString()))
        assertEquals(false, decoded.allowed)
        assertEquals(mapOf("One" to setOf("A", "B")), decoded.picks)
        assertEquals(mapOf("One" to "custom"), decoded.other)
        assertEquals("submit", encoded.getValue("operation").toString().let {
            kotlinx.serialization.json.Json.parseToJsonElement(it).jsonObject.getValue("action").toString().trim('"')
        })
    }

    @Test fun whitespaceBoundariesAreComparedNotTrimmedByTheAdapter() {
        val reports = mutableListOf<AnswerMismatch>()
        val shadow = ShadowAnswerAdapter(sink = reports::add)
        val kotlin = shadow.display("​ A\n (Recommended)")
        shadow.awaitIdle()
        assertTrue(kotlin.recommended)
        assertEquals("A\n", kotlin.text)
        assertEquals(reports.map { it.rule }.toString(), true, reports.none { it.rule == "display" })
        assertEquals(reports.map { it.rule }.toString(), true, reports.none { it.rule == "rust-failure" || it.rule == "rust-output" })
    }

    @Test fun realJniComparisonReportsEveryRuleWithoutContent() {
        val reports = mutableListOf<AnswerMismatch>()
        val shadow = ShadowAnswerAdapter(sink = reports::add)
        val interaction = Interaction(id = "example", kind = "question", questions = listOf(
            Question(question = "One", options = listOf(QuestionOption("A"), QuestionOption("B"))),
            Question(question = "Two", options = listOf(QuestionOption("C"), QuestionOption("D"))),
        ))
        val picked = shadow.build(interaction, AnswerAction.PickOption, AnswerDraft(optionIndex = 1, other = mapOf("One" to "custom")))
        shadow.awaitIdle()
        val encodedPick = encodeBuild(interaction, AnswerAction.PickOption,
            AnswerDraft(optionIndex = 1, other = mapOf("One" to "custom")), AnswerContext()).toString()
        val direct = RustCoreAnswer.project(encodedPick)
        assertTrue("the direct real JNI pick must return before the queued comparison: $encodedPick", direct.isNotEmpty())
        val corePick = direct
        val decodedPick = JsonCodec.decodeFromString<CoreTransition>(corePick)
        // The core keeps the emptied Other entry, as iOS's QuestionForm.pick does (other[q.question] = ""), while Kotlin's
        // builder removes the key. #9238 decided this with the iOS oracle: the difference stays reported as `other-state`,
        // and this test pins both sides so neither is changed to match the other by accident.
        assertEquals("core pick's other key count", 1, decodedPick.other.size)
        assertEquals("core pick's empty value", true, decodedPick.other.values.single().isEmpty())
        assertTrue(picked.allowed)
        assertTrue(picked.draft.other.isEmpty())
        assertEquals(reports.map { it.rule }.toString(), true, reports.any { it.rule == "other-state" })
        assertTrue(reports.all { JsonCodec.encodeToString(it).let { json ->
            !json.contains("custom") && !json.contains("One") && !json.contains("Two") && !json.contains("example")
        } })
    }
}

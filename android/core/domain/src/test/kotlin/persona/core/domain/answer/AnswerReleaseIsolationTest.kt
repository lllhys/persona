package persona.core.domain.answer

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Question
import persona.core.contracts.QuestionOption

class AnswerReleaseIsolationTest {
    @Test fun defaultAndExplicitKotlinSelectionHaveTheOriginalResultAndNoShadowClass() {
        val interaction = persona.core.contracts.Interaction(id = "example", kind = "question", questions = listOf(
            Question(question = "One", options = listOf(QuestionOption(label = "Yes (Recommended)"))),
        ))
        AnswerAdapters.install(KotlinAnswerAdapter())
        val result = AnswerBuilder.build(interaction, AnswerAction.PickOption)
        assertEquals(mapOf("One" to "Yes (Recommended)"), result.resolution?.answers)
        assertEquals(OptionDisplay("Yes", true), AnswerBuilder.display("Yes (Recommended)"))
        assertEquals("persona.core.domain.answer.KotlinAnswerAdapter", AnswerAdapters.current().javaClass.name)
        for (name in listOf("ShadowAnswerAdapter", "ShadowAnswerDiagnostics", "AnswerMismatch")) {
            val found = runCatching { Class.forName("persona.core.domain.answer.$name") }.isSuccess
            assertFalse("The production source set must not contain $name", found)
        }
    }

    @Test fun sourceTreeDoesNotReferenceShadowOrNativeFromTheProductionJar() {
        val root = generateSequence(java.io.File(".").canonicalFile) { it.parentFile }
            .firstOrNull { java.io.File(it, "src/main/kotlin/persona/core/domain/answer").isDirectory } ?: error("No module")
        val mainFiles = root.resolve("src/main/kotlin").walkTopDown().filter { it.extension == "kt" }.toList()
        assertTrue(mainFiles.isNotEmpty())
        val adapter = root.resolve("src/main/kotlin/persona/core/domain/answer/AnswerAdapter.kt").readText()
        assertFalse(adapter.contains("shadow", ignoreCase = true))
        assertFalse(adapter.contains("RustCoreAnswer"))
        assertFalse(root.resolve("src/main/kotlin/persona/core/domain/answer/ShadowAnswerAdapter.kt").exists())
    }
}

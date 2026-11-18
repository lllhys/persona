package persona.core.domain.answer

import java.util.concurrent.CountDownLatch
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Interaction
import persona.core.contracts.JsonCodec
import persona.core.contracts.Question
import persona.core.contracts.QuestionOption
import persona.core.contracts.Resolution

class ShadowAnswerAdapterTest {
    private val one = Interaction(id = "example_request", kind = "question", questions = listOf(
        Question(question = "Choose", options = listOf(QuestionOption(label = "Yes (Recommended)"), QuestionOption(label = "No"))),
    ))

    @Test fun kotlinReturnsBeforeNativeAndEveryReportContainsOnlyShape() {
        val caller = Thread.currentThread()
        val nativeThread = AtomicReference<Thread>()
        val nativeStarted = CountDownLatch(1)
        val releaseNative = CountDownLatch(1)
        val reports = CopyOnWriteArrayList<AnswerMismatch>()
        val shadow = ShadowAnswerAdapter(projectRust = {
            nativeThread.set(Thread.currentThread())
            nativeStarted.countDown()
            assertTrue(releaseNative.await(30, java.util.concurrent.TimeUnit.SECONDS))
            """{"allowed":false,"page":0,"picks":{},"other":{},"resolution":null}"""
        }, displayRust = {
            nativeThread.set(Thread.currentThread())
            nativeStarted.countDown()
            assertTrue(releaseNative.await(30, java.util.concurrent.TimeUnit.SECONDS))
            """{"text":"different","recommended":false}"""
        }, sink = reports::add)
        val expected = shadow.build(one, AnswerAction.PickOption)
        assertTrue(expected.allowed)
        val display = shadow.display("Yes (Recommended)")
        assertEquals(OptionDisplay("Yes", true), display)
        assertTrue(nativeStarted.await(30, java.util.concurrent.TimeUnit.SECONDS))
        releaseNative.countDown()
        shadow.awaitIdle()
        assertNotNull(nativeThread.get())
        assertFalse(caller == nativeThread.get())
        assertEquals("PersonaAnswerShadow", nativeThread.get().name)
        assertTrue(shadow.mismatchCount > 0)
        reports.forEach {
            val json = JsonCodec.encodeToString(it)
            assertTrue(json.startsWith("""{"domain":"question-answer","rule":"""))
            assertFalse(json.contains("example_request"))
            assertFalse(json.contains("Choose"))
            assertFalse(json.contains("Yes"))
            assertFalse(json.contains("Recommended"))
            assertFalse(json.contains("exception", ignoreCase = true))
            assertTrue(it.inputShape.contains("questions=1") || it.inputShape.startsWith("labelChars="))
            assertTrue(it.inputShape.contains("action=pick_option") || it.inputShape.startsWith("labelChars="))
        }
    }

    @Test fun invalidPageAndNativeFailureCannotChangeKotlinOrEscapeContent() {
        val reports = CopyOnWriteArrayList<AnswerMismatch>()
        val expected = KotlinAnswerAdapter().build(one, AnswerAction.PickOption, AnswerDraft(page = 99))
        for (native in listOf<() -> String>({ "" }, { "not-json" }, { throw IllegalStateException("private native text") })) {
            val shadow = ShadowAnswerAdapter(projectRust = { native() }, sink = reports::add)
            val actual = shadow.build(one, AnswerAction.PickOption, AnswerDraft(page = 99))
            assertEquals(expected, actual)
            shadow.awaitIdle()
        }
        assertEquals(3, reports.size)
        reports.forEach { assertFalse(JsonCodec.encodeToString(it).contains("private native text")) }
    }

    @Test fun unsupportedOperationsAreNotReportedAsParity() {
        val reports = CopyOnWriteArrayList<AnswerMismatch>()
        val shadow = ShadowAnswerAdapter(projectRust = { error("must not call Rust") }, sink = reports::add)
        shadow.build(one.copy(kind = "permission"), AnswerAction.Allow)
        shadow.build(one, AnswerAction.PickOption, context = AnswerContext(entry = AnswerEntry.Notification))
        shadow.awaitIdle()
        assertEquals(listOf("unsupported-operation", "unsupported-entry"), reports.map { it.rule })
    }

    @Test fun publicBoundaryStillReturnsTheOriginalObjectAndCallerErrorsAreNotHidden() {
        val expected = AnswerResult(true, Resolution(answers = mapOf("Choose" to "Yes")), AnswerDraft())
        val adapter = object : AnswerAdapter {
            override fun build(interaction: Interaction, action: AnswerAction, draft: AnswerDraft, context: AnswerContext): AnswerResult = expected
            override fun display(label: String): OptionDisplay = OptionDisplay(label, false)
        }
        val shadow = ShadowAnswerAdapter(adapter, projectRust = { error("not reached") })
        assertSame(expected, shadow.build(one, AnswerAction.PickOption))
        val failure = object : AnswerAdapter by adapter {
            override fun build(interaction: Interaction, action: AnswerAction, draft: AnswerDraft, context: AnswerContext): AnswerResult {
                throw IllegalArgumentException("original caller failure")
            }
        }
        val thrown = runCatching { ShadowAnswerAdapter(failure).build(one, AnswerAction.PickOption) }.exceptionOrNull()
        assertTrue(thrown is IllegalArgumentException)
        assertEquals("original caller failure", thrown?.message)
    }
}

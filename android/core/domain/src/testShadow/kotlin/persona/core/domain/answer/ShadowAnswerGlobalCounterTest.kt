package persona.core.domain.answer

import org.junit.Assert.assertEquals
import org.junit.Test

/** Runs in its own JVM so the process-wide counter is not mixed with other fixture/shadow comparisons. */
class ShadowAnswerGlobalCounterTest {
    @Test fun defaultSinkCountsEachMismatchOnce() {
        val shadow = ShadowAnswerAdapter(displayRust = { """{"text":"different","recommended":false}""" })
        ShadowAnswerDiagnostics.resetForTest()
        shadow.display("A")
        shadow.awaitIdle()
        assertEquals(1, shadow.mismatchCount)
        assertEquals(1, ShadowAnswerDiagnostics.mismatchCount)
    }
}

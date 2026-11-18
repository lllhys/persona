package persona.core.data

import org.junit.Assert.assertEquals
import org.junit.rules.ExternalResource

/** Drains even chats released by a cancelled repository scope; no pending work is counted as a clean run. */
class ShadowParityRule : ExternalResource() {
    override fun after() {
        ShadowContinuityDiagnostics.awaitIdle()
        assertEquals("Repository shadow mismatches: ${ShadowContinuityDiagnostics.lastReport()}", 0, ShadowContinuityDiagnostics.count)
        println("continuity shadow mismatches=${ShadowContinuityDiagnostics.count}")
    }
}

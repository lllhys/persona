package persona.feature.chat

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class ComposerUploadLimiterTest {
    @Test fun threeUploadAndTheOthersWaitForSlots() = runTest {
        val limiter = ComposerUploadLimiter()
        val release = CompletableDeferred<Unit>()
        val started = mutableListOf<Int>()
        var active = 0
        var maximum = 0
        val jobs = List(5) { index ->
            async {
                limiter.withSlot {
                    started += index
                    active++
                    maximum = maxOf(maximum, active)
                    release.await()
                    active--
                }
            }
        }
        runCurrent()
        assertEquals(listOf(0, 1, 2), started)
        assertEquals(3, active)
        release.complete(Unit)
        jobs.forEach { it.await() }
        assertEquals(listOf(0, 1, 2, 3, 4), started)
        assertEquals(3, maximum)
        assertEquals(0, active)
    }
}

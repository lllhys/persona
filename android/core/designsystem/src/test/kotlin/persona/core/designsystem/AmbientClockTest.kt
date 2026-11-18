package persona.core.designsystem

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AmbientClockTest {
    private val second = 1_000_000_000L
    /** An arbitrary uptime, like the frame clock's nanoseconds on a phone that has been on for a while. */
    private val boot = 9_876 * second + 123_456_789L

    private class FakeTime(var nanos: Long) { val now = { nanos } }

    @Test
    fun theLightRestsFiveSecondsAfterTheLastInput() {
        assertEquals(5 * second, AmbientIdleAfterNanos)
    }

    @Test
    fun whileInUseTheTimeIsTheFrameClocksSoTheLightMovesAsBefore() {
        val time = FakeTime(boot)
        val clock = AmbientClock(now = time.now)
        clock.touch(boot)
        var at = boot
        while (at < boot + 5 * second) {
            assertFalse(clock.isIdle(at))
            assertEquals(at, clock.activeNanos(at))
            at += second / 12
        }
    }

    @Test
    fun itKeepsMovingForFiveSecondsAfterAnInputThenHoldsStill() {
        val clock = AmbientClock(now = { boot })
        clock.touch(boot)
        clock.touch(boot + 2 * second)
        val deadline = boot + 7 * second
        assertTrue(clock.activeNanos(deadline - 1) < clock.activeNanos(deadline))
        assertFalse(clock.isIdle(deadline - 1))
        assertTrue(clock.isIdle(deadline))
        val stopped = clock.activeNanos(deadline)
        for (later in listOf(1L, second, 60 * second, 3_600 * second)) assertEquals(stopped, clock.activeNanos(deadline + later))
    }

    @Test
    fun anInputCarriesOnFromWhereItStoppedWithoutAJump() {
        val time = FakeTime(boot)
        val clock = AmbientClock(now = time.now)
        clock.touch(boot)
        val stopped = clock.activeNanos(boot + 5 * second)
        val wake = boot + 95 * second
        clock.touch(wake)
        // The first value after the input is the one it stopped at, also for a frame stamped just before the input.
        assertEquals(stopped, clock.activeNanos(wake))
        assertEquals(stopped, clock.activeNanos(wake - second / 120))
        assertEquals(lightSeconds(stopped), lightSeconds(clock.activeNanos(wake)), 0.0)
        // Then it runs at the frame clock's rate again: the same steps and the same path, from where it was.
        for (k in 1..60) assertEquals(stopped + k * second / 12, clock.activeNanos(wake + k * second / 12))
        // A second pause and a second input stack up the same way.
        val again = clock.activeNanos(wake + 5 * second)
        assertEquals(stopped + 5 * second, again)
        clock.touch(wake + 600 * second)
        assertEquals(again, clock.activeNanos(wake + 600 * second))
        assertEquals(again + second, clock.activeNanos(wake + 601 * second))
    }

    @Test
    fun inputsWhileMovingChangeNothingButTheDeadline() {
        val clock = AmbientClock(now = { boot })
        clock.touch(boot)
        for (k in 1..40) clock.touch(boot + k * second / 10)
        assertEquals(boot + 4 * second, clock.activeNanos(boot + 4 * second))
        assertFalse(clock.isIdle(boot + 8 * second))
        assertTrue(clock.isIdle(boot + 9 * second))
    }

    @Test
    fun twoReadersAtTheSameMomentReadTheSameTime() = runTest {
        // Panes side by side each run a loop on the one clock. One starts later; at every frame they both draw they
        // agree, while moving, when stopped and after resuming. Frames come every 100 ms here so the two loops meet.
        val now = { testScheduler.currentTime * 1_000_000L + boot }
        val clock = AmbientClock(now = now)
        clock.touch()
        val vsync: suspend ((Long) -> Unit) -> Unit = { onFrame -> delay(100 - testScheduler.currentTime % 100); onFrame(now()) }
        val a = sortedMapOf<Long, Double>()
        val b = sortedMapOf<Long, Double>()
        backgroundScope.launch { driveLight(clock, vsync) { a[now()] = it } }
        advanceTimeBy(3_000)
        backgroundScope.launch { driveLight(clock, vsync) { b[now()] = it } }
        advanceTimeBy(10_000)
        val resumedAt = now()
        assertEquals("stopped at the same value", a.getValue(a.lastKey()), b.getValue(b.lastKey()), 0.0)
        clock.touch()
        advanceTimeBy(3_000)
        val shared = a.keys intersect b.keys
        assertTrue("${shared.size} shared frames", shared.count { it > resumedAt } > 3 && shared.count { it < resumedAt } > 3)
        for (at in shared) assertEquals(a.getValue(at), b.getValue(at), 0.0)
    }

    @OptIn(ExperimentalCoroutinesApi::class)
    @Test
    fun aStoppedLightRequestsNoFramesUntilTheNextInput() = runTest {
        val now = { testScheduler.currentTime * 1_000_000L + boot }
        val clock = AmbientClock(now = now)
        clock.touch()
        var frames = 0
        val published = mutableListOf<Double>()
        // One frame every 16 ms after it is asked for, stamped like the frame clock.
        val frame: suspend ((Long) -> Unit) -> Unit = { onFrame -> delay(16); frames++; onFrame(now()) }
        backgroundScope.launch { driveLight(clock, frame) { published += it } }

        advanceTimeBy(4_000)
        assertTrue("moving: $frames frames in 4 s", frames in 35..45)
        assertTrue(published.zipWithNext().all { (a, b) -> b > a })
        advanceTimeBy(1_200)
        val stoppedFrames = frames
        val stopped = published.last()
        advanceTimeBy(60_000)
        assertEquals("no frames while stopped", stoppedFrames, frames)
        assertEquals(stopped, published.last(), 0.0)

        clock.touch()
        runCurrent()
        advanceTimeBy(17)
        assertEquals(stoppedFrames + 1, frames)
        // The clock gives back exactly the stopped value at the input (anInputCarriesOn...); this frame comes 16 ms
        // later, so it may be one step on, never more.
        assertTrue("resumes where it stopped", published.last() - stopped in 0.0..(1.0 / 12 + 1e-9))
        advanceTimeBy(1_000)
        assertTrue(frames > stoppedFrames + 5)
        assertTrue(published.last() > stopped && published.last() - stopped < 1.2)
    }

    @Test
    fun aParkedLoopWakesAtTheNextInput() = runTest {
        val time = FakeTime(boot)
        val clock = AmbientClock(now = time.now)
        clock.touch()
        time.nanos += 6 * second
        var woke = false
        val waiter = launch { clock.awaitActivity(); woke = true }
        testScheduler.runCurrent()
        assertFalse(woke)
        clock.touch()
        testScheduler.runCurrent()
        assertTrue(woke)
        waiter.join()
        // While moving it does not wait at all.
        clock.awaitActivity()
    }
}

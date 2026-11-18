package persona.core.designsystem

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first

/** How long the ambient light keeps moving after the last input that reached the app: five seconds. */
internal const val AmbientIdleAfterNanos = 5_000_000_000L

/**
 * The time the ambient light ([AmbientLight]) moves on. While someone is using the app it runs with the clock it is
 * given (the frame clock's nanoseconds), so the light moves exactly as it always has; [idleAfterNanos] after the last
 * [touch] it stops where it is, and the next [touch] carries on from that value, so the light neither jumps back to
 * where it rests nor leaps ahead by the time it stood still. Every copy of the light reads one clock, so panes side
 * by side stop at, and resume from, the same moment.
 *
 * Light time = `at - offset`, never below [floor]. Stopping holds `at` at the deadline; resuming adds the pause to
 * `offset` and keeps the stopped value as [floor], so a frame stamped slightly before the input that resumed it
 * cannot step the light back. Plain Kotlin with an injectable [now] (nanoseconds on the same timeline as the frame
 * times passed to [activeNanos]) so it can be tested without Compose or Android.
 */
internal class AmbientClock(
    private val idleAfterNanos: Long = AmbientIdleAfterNanos,
    private val now: () -> Long = System::nanoTime,
) {
    private var lastInput = now()
    private var offset = 0L
    private var floor = Long.MIN_VALUE
    /** Bumped each time an input wakes the clock, for the light's loops parked in [awaitActivity]. */
    private val wakes = MutableStateFlow(0)

    /** Input reached the app at [at]: keep moving, or carry on from where the light stopped. */
    @Synchronized
    fun touch(at: Long = now()) {
        val deadline = lastInput + idleAfterNanos
        if (at >= deadline) {
            floor = deadline - offset
            offset += at - deadline
            wakes.value += 1
        }
        if (at > lastInput) lastInput = at
    }

    /** True once [idleAfterNanos] have passed since the last input: the value no longer changes. */
    @Synchronized
    fun isIdle(at: Long): Boolean = at >= lastInput + idleAfterNanos

    /** Nanoseconds of light time at the clock reading [at]: equal to [at] until the first pause, frozen while idle. */
    @Synchronized
    fun activeNanos(at: Long): Long = maxOf(floor, minOf(at, lastInput + idleAfterNanos) - offset)

    /** Returns when the clock is moving: at once if it is, otherwise at the next [touch]. Requests no frames. */
    suspend fun awaitActivity() {
        val seen = wakes.value
        if (!isIdle(now())) return
        wakes.first { it != seen }
    }

    companion object {
        /** The app's one clock: every [AmbientLight] reads it and the activities report input to it. */
        val Shared = AmbientClock()
    }
}

/**
 * Input reached the app (a touch, a key, a mouse wheel or touchpad, the app coming to the front): the ambient light
 * keeps moving for another five seconds, or starts again from where it stopped. Activities that show pages call it.
 */
fun keepAmbientLightMoving() = AmbientClock.Shared.touch()

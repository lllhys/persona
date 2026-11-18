package persona.core.data

import java.util.concurrent.ConcurrentLinkedQueue
import persona.core.rustcore.RustCore

/** JVM-only scheduler selected by persona.continuity.frameScheduler; production never waits for the worker. */
class FlushContinuityFrameScheduler : ContinuityFrameScheduler {
    private val frames = ConcurrentLinkedQueue<() -> Unit>()
    override val synchronous = true

    // Creation must start before dispatch can reach submitted; production defers this until after a frame.
    override fun afterFirstFrame(start: () -> Unit) = start()
    override fun schedule(frame: () -> Unit) { frames.add(frame) }

    override suspend fun submitted(core: RustCore) {
        check(core.flush(5_000)) { "The continuity worker did not finish within five seconds" }
        repeat(1_000) { (frames.poll() ?: return).invoke() }
        error("Continuity frames did not become idle")
    }
}

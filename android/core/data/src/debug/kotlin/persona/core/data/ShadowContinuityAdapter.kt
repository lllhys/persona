package persona.core.data

import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.Serializable
import persona.core.rustcore.RustCore

/** Only these three fields cross the diagnostics boundary; values never contain payloads or identifiers. */
@OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)
@Serializable
data class ContinuityMismatch(@kotlinx.serialization.EncodeDefault val domain: String = "continuity", val rule: String, val inputShape: String)

/** Shared diagnostics and a separate FIFO worker, never the repository dispatcher. */
object ShadowContinuityDiagnostics {
    private val worker = Executors.newSingleThreadExecutor { task ->
        Thread(task, "PersonaCoreShadow").apply { isDaemon = true }
    }
    private val mismatches = AtomicInteger()
    private val last = java.util.concurrent.atomic.AtomicReference<ContinuityMismatch>()
    val count: Int get() = mismatches.get()
    internal fun lastReport(): ContinuityMismatch? = last.get()
    fun report(mismatch: ContinuityMismatch) {
        mismatches.incrementAndGet()
        last.set(mismatch)
    }
    internal fun enqueue(work: () -> Unit) { worker.execute(work) }

    /** Test barrier only. Includes close tasks, and fails rather than treating unfinished work as parity. */
    fun awaitIdle() { worker.submit {}.get(30, TimeUnit.SECONDS) }
}

/** Kotlin is authoritative. Inputs and its exact Applied values are compared on a private FIFO worker. */
class ShadowContinuityAdapter(
    private val chat: String,
    private val sink: (ContinuityMismatch) -> Unit = ShadowContinuityDiagnostics::report,
    private val afterFirstFrame: (() -> Unit) -> Unit = AndroidContinuityFrameScheduler()::afterFirstFrame,
    private val createShadow: ((ContinuityFrameScheduler, () -> Unit) -> ContinuityAdapter)? = null,
    private val started: () -> Unit = {},
    private val progress: (compared: Int, mismatches: Int) -> Unit = { _, _ -> },
) : ContinuityAdapter {
    private val kotlin = KotlinContinuityAdapter(chat)
    private var shadow: ContinuityAdapter? = null
    private var driver: RustContinuityDriver? = null
    private var disabled = false
    @Volatile private var rejected = false
    private var closed = false
    val mismatchCount: Int get() = count.get()
    private val count = AtomicInteger()
    private var compared = 0
    override val state get() = kotlin.state
    override val behind get() = kotlin.behind

    override suspend fun accept(input: ContinuityAdapter.Input, operation: Long?): ContinuityAdapter.Applied {
        val result = kotlin.accept(input, operation)
        if (!closed) ShadowContinuityDiagnostics.enqueue {
            if (!disabled) {
                val shape = input.shape()
                var rule = "rust-creation"
                try {
                    val other = shadow ?: run {
                        val scheduler = ShadowFrames(afterFirstFrame)
                        (createShadow?.invoke(scheduler) { rejected = true } ?: run {
                            val owned = RustContinuityDriver(scheduler, onRejected = { rejected = true })
                            driver = owned
                            RustContinuityAdapter(chat, owned)
                        }).also { shadow = it }
                    }
                    rule = "rust-failure"
                    val actual = runBlocking { requireNotNull(other.accept(input, operation)) }
                    if (rejected || actual.rejected) {
                        report("rust-rejected", shape)
                        disable()
                    } else compare(result, actual, shape)
                } catch (_: Throwable) {
                    report(if (rejected) "rust-rejected" else rule, shape)
                    disable()
                }
            }
        }
        return result
    }

    private fun compare(expected: ContinuityAdapter.Applied, actual: ContinuityAdapter.Applied, shape: String) {
        val a = expected.state
        val b = actual.state
        if (a.items.map { it.id } != b.items.map { it.id }) report("item-ids-order", shape)
        if (a.items != b.items) report("items", shape)
        if (a.covered != b.covered) report("covered", shape)
        if (a.history != b.history) report("history", shape)
        if (a.hasMore != b.hasMore) report("hasMore", shape)
        if (a.loadedOnce != b.loadedOnce) report("loadedOnce", shape)
        // Kotlin's gap/verified describe this step; Rust's verified is persistent core authority.
        // Cache restores and live pushes intentionally differ. Their contract is tested separately.
        if (expected.requests != actual.requests) report("requests", shape)
        compared++
        if (compared % 10 == 0) reportProgress()
    }

    private fun reportProgress() {
        try { progress(compared, mismatchCount) } catch (_: Throwable) {}
    }

    private fun report(rule: String, shape: String) {
        count.incrementAndGet()
        // A failing diagnostics sink cannot affect Kotlin, kill the worker or print an exception's payload.
        try { sink(ContinuityMismatch(rule = rule, inputShape = shape)) } catch (_: Throwable) {}
    }

    private fun disable() {
        disabled = true
        try { shadow?.close() } catch (_: Throwable) {}
        try { driver?.close() } catch (_: Throwable) {}
        shadow = null
        driver = null
    }

    override fun close() {
        if (closed) return
        closed = true
        kotlin.close()
        // Do not discard already accepted inputs when the repository releases this chat.
        ShadowContinuityDiagnostics.enqueue {
            disable()
            reportProgress()
        }
    }

    private inner class ShadowFrames(private val firstFrame: (() -> Unit) -> Unit) : ContinuityFrameScheduler {
        private val frames = ConcurrentLinkedQueue<() -> Unit>()
        override val synchronous = true
        override fun afterFirstFrame(start: () -> Unit) {
            val ready = CountDownLatch(1)
            firstFrame { ready.countDown() }
            check(ready.await(10, TimeUnit.SECONDS))
            start() // This is the shadow worker, after traversal; never the frame's main-thread callback.
            started()
        }
        override fun schedule(frame: () -> Unit) { frames.add(frame) }
        override suspend fun submitted(core: RustCore) {
            check(core.flush(5_000))
            repeat(1_000) { (frames.poll() ?: return).invoke() }
            error("Shadow frames did not become idle")
        }
    }
}

private fun Any?.presence() = if (this == null) "unset" else "set"
private fun ContinuityAdapter.Batch.shape() =
    "items=${items.size} history=${history.presence()} fromSeq=${fromSeq.presence()} toSeq=${toSeq.presence()} " +
        "backlog=$backlog hasMore=$hasMore moreAfter=$moreAfter"

private fun ContinuityAdapter.Input.shape(): String = when (this) {
    is ContinuityAdapter.Input.Restore -> "restore items=${state.items.size} history=${state.history.presence()} " +
        "covered=${(state.covered != 0)} hasMore=${state.hasMore} loadedOnce=${state.loadedOnce}"
    is ContinuityAdapter.Input.Push -> "push ${batch.shape()}"
    is ContinuityAdapter.Input.Latest -> "latest ${batch.shape()}"
    is ContinuityAdapter.Input.After -> "after ${batch.shape()}"
    is ContinuityAdapter.Input.Reload -> "reload items=${items.size} history=${history.presence()} toSeq=${toSeq.presence()} hasMore=$hasMore"
    is ContinuityAdapter.Input.Older -> "older items=${items.size} hasMore=$hasMore"
    is ContinuityAdapter.Input.OpenFold -> "openFold id=set"
    is ContinuityAdapter.Input.Range -> "range items=${items.size} more=$more foldId=${foldId.presence()}"
    ContinuityAdapter.Input.HubChanged -> "hubChanged"
    ContinuityAdapter.Input.ContinueFill -> "continueFill"
    ContinuityAdapter.Input.Reconnect -> "reconnect"
    ContinuityAdapter.Input.LoadOlder -> "loadOlder"
    ContinuityAdapter.Input.FillFailed -> "fillFailed"
    ContinuityAdapter.Input.OlderFailed -> "olderFailed"
    is ContinuityAdapter.Input.RangeFailed -> "rangeFailed foldId=${foldId.presence()}"
}

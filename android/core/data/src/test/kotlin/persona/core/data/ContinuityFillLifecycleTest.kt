package persona.core.data

import java.io.IOException
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.data.ContinuityAdapter.Input
import persona.core.domain.ContinuitySync.Action
import persona.core.rustcore.RustCore

/** Worker completion never applies a frame; every repository continuation uses the real async adapter. */
class ContinuityFillLifecycleTest {
    @Test fun catchUpWithoutFirstKeepsTheQueuedFillUntilItsAppliedFetchRuns() = runBlocking {
        withRepository({ query ->
            if (query.afterSeq == null) page(1) else page(2, from = 1)
        }) {
            val first = reconcile()
            val second = reconcile()
            waitFor { submitted(Input.ContinueFill).size == 2 }
            val fills = submitted(Input.ContinueFill)

            assertTrue(adapter.asynchronous)
            assertTrue(fills.all { it.queued })
            assertTrue(fills.all { adapter.requestsFor(it) == null })
            assertFalse(first.isCompleted)
            assertFalse(second.isCompleted)
            assertTrue(afterSeqs().isEmpty())
            assertEquals(0, submitted(Input.FillFailed).size)

            frames.run()
            waitFor { adapter.submissions.any { it.input is Input.After } }
            assertEquals(listOf(1), afterSeqs())
            assertEquals(listOf(Action.FetchAfter(1)), adapter.requestsFor(fills.first()))
            assertEquals(emptyList<Action>(), adapter.requestsFor(fills.last()))
            assertEquals(0, submitted(Input.FillFailed).size)
            waitFor(applyFrames = true) { snapshot.get().verified && sequences() == listOf(1, 2) }
            first.await()
            second.await()
            assertEquals(listOf(1), afterSeqs())
        }
    }

    @Test fun transportFailureWithAnUnappliedAfterStillReleasesTheCoreFillGate() = runBlocking {
        val latest = AtomicInteger()
        withRepository({ query ->
            when (query.afterSeq) {
                null -> if (latest.incrementAndGet() == 1) page(1) else throw IOException("Synthetic latest failure")
                1 -> page(2, from = 1, more = true)
                else -> page(3, from = query.afterSeq)
            }
        }) {
            // One frame delivers both real requests. The first queues After; the second transfer then fails.
            revision(2)
            legacyReload()
            waitFor { submitted(Input.HubChanged).size == 1 && adapter.submissions.any { it.input is Input.Push } }
            frames.run()
            waitFor { submitted(Input.FillFailed).size == 1 && snapshot.get().error != null }

            val inputs = adapter.submissions.map { it.input }
            assertTrue(inputs.any { it is Input.After })
            assertTrue(inputs.indexOfFirst { it is Input.After } < inputs.indexOf(Input.FillFailed))
            assertEquals(listOf(null, 1, null), queries.map { it.afterSeq })
            assertEquals(1, adapter.state.covered)
            assertEquals(listOf(1), sequences())
            val after = adapter.submissions.single { it.input is Input.After }
            assertTrue(after.queued)
            assertNull(adapter.requestsFor(after))

            val recovery = reconcile()
            waitFor { submitted(Input.ContinueFill).size == 1 }
            val retry = submitted(Input.ContinueFill).single()
            frames.run()
            waitFor { adapter.requestsFor(retry) != null }
            // The pending After also emitted a fetch. Only this operation proves that failure released the gate.
            assertEquals(listOf(Action.FetchAfter(2)), adapter.requestsFor(retry))
            waitFor(applyFrames = true) { snapshot.get().verified && sequences() == listOf(1, 2, 3) }
            recovery.await()
            assertTrue(afterSeqs().contains(2))
            assertEquals(1, submitted(Input.FillFailed).size)
        }
    }

    @Test fun laterPageFailureDoesNotCancelAQueuedFillBeforeTheTransferFails() = runBlocking {
        val attempts = AtomicInteger()
        val laterPage = CompletableDeferred<ItemsResponse>()
        withRepository({ query ->
            if (query.afterSeq == null) page(1) else when (attempts.incrementAndGet()) {
                1 -> page(2, from = 1, more = true)
                2 -> laterPage.await()
                else -> page(3, from = query.afterSeq)
            }
        }) {
            val first = reconcile()
            waitFor { submitted(Input.ContinueFill).size == 1 }
            frames.run()
            waitFor { adapter.submissions.any { it.input is Input.After } }
            assertEquals(1, attempts.get())
            assertEquals(1, adapter.state.covered)
            assertEquals(0, submitted(Input.FillFailed).size)

            // Reconciliation does not own the successful page's still-queued continuation.
            val overlapping = reconcile()
            waitFor { submitted(Input.ContinueFill).size == 2 }
            val overlap = submitted(Input.ContinueFill).last()
            assertEquals(0, submitted(Input.FillFailed).size)
            frames.run()
            waitFor { attempts.get() == 2 }
            assertEquals(emptyList<Action>(), adapter.requestsFor(overlap))
            assertEquals(0, submitted(Input.FillFailed).size)

            laterPage.completeExceptionally(IOException("Synthetic later-page failure"))
            waitFor { submitted(Input.FillFailed).size == 1 && snapshot.get().error != null }
            assertEquals(listOf(1, 2), afterSeqs())
            val recovery = reconcile()
            waitFor { submitted(Input.ContinueFill).size == 3 }
            val retry = submitted(Input.ContinueFill).last()
            waitFor(applyFrames = true) { snapshot.get().verified && sequences() == listOf(1, 2, 3) }
            assertEquals(listOf(Action.FetchAfter(2)), adapter.requestsFor(retry))
            assertEquals(listOf(1, 2, 2), afterSeqs())
            assertEquals(1, submitted(Input.FillFailed).size)
            first.await()
            overlapping.await()
            recovery.await()
        }
    }

    @Test fun historyChangingReloadIsVerifiedAtItsOwnToSeq() = runBlocking {
        val latest = AtomicInteger()
        withRepository({ query ->
            check(query.afterSeq == null) { "A complete reload must not require another fill" }
            if (latest.incrementAndGet() == 1) page(5) else page(2, history = "replacement")
        }) {
            legacyReload()
            waitFor(applyFrames = true) {
                snapshot.get().verified && adapter.state.history == "replacement" && sequences() == listOf(2)
            }
            assertEquals(2, adapter.state.covered)
            assertEquals(2, latest.get())
            assertTrue(afterSeqs().isEmpty())
            assertEquals(0, submitted(Input.FillFailed).size)
        }
    }

    @Test fun hubChangesDuringMultiplePagesAreReadAfterTheLastCappedPage() = runBlocking {
        val attempts = AtomicInteger()
        val firstPage = CompletableDeferred<ItemsResponse>()
        val secondPage = CompletableDeferred<ItemsResponse>()
        withRepository({ query ->
            if (query.afterSeq == null) page(1) else when (attempts.incrementAndGet()) {
                1 -> firstPage.await()
                2 -> secondPage.await()
                else -> ItemsResponse(emptyList(), history = "h", fromSeq = query.afterSeq, toSeq = 3)
            }
        }) {
            revision(2)
            waitFor(applyFrames = true) { attempts.get() == 1 }
            revision(3)
            waitFor { submitted(Input.HubChanged).size == 2 }
            firstPage.complete(page(2, from = 1, more = true))
            waitFor(applyFrames = true) { attempts.get() == 2 }

            revision(4)
            waitFor { submitted(Input.HubChanged).size == 3 }
            secondPage.complete(page(3, from = 2))
            waitFor(applyFrames = true) {
                attempts.get() == 3 && adapter.submissions.count { it.input is Input.After } == 3 &&
                    adapter.submissions.filter { it.input is Input.After }.all { adapter.requestsFor(it) != null }
            }
            assertEquals(listOf(1, 2, 3), afterSeqs())
            assertEquals(listOf(1, 2, 3), sequences())
            assertTrue(snapshot.get().verified)
            assertEquals(0, submitted(Input.FillFailed).size)
        }
    }

    private suspend fun withRepository(
        fetch: suspend (ItemsQuery) -> ItemsResponse,
        test: suspend Harness.() -> Unit,
    ) {
        val harness = Harness(fetch)
        try {
            harness.waitFor(applyFrames = true) {
                harness.snapshot.get().verified && harness.submitted(Input.Reconnect).isNotEmpty()
            }
            harness.test()
        } finally {
            harness.close()
        }
    }

    private fun page(to: Int, from: Int? = null, more: Boolean = false, history: String = "h") =
        ItemsResponse(listOf(Item("$to#0", to, role = "assistant", kind = "text")),
            history = history, fromSeq = from, toSeq = to, moreAfter = more)

    private class Harness(fetch: suspend (ItemsQuery) -> ItemsResponse) {
        private val key = SessionKey("fixture", "fill-lifecycle")
        val frames = Frames()
        private val driver = RustContinuityDriver(frames)
        private val job = SupervisorJob()
        private val scope = CoroutineScope(job + Dispatchers.Unconfined)
        private val connection = FakeConnection()
        val queries = ConcurrentLinkedQueue<ItemsQuery>()
        val snapshot = AtomicReference(ChatSnapshot(verified = false))
        lateinit var adapter: RecordingAdapter
            private set
        private val transport = FakeChatTransport().also { transport ->
            transport.fetch = { query -> queries.add(query); fetch(query) }
        }
        private val repository = HubChatRepository(
            transport, connection, FakeItemsCache(CachedChat()), FakeSessions(), scope,
            continuityFactory = { chat, applied -> RecordingAdapter(chat, driver, applied).also { adapter = it } },
        )
        private val observer = scope.launch { repository.observe(key).collect { snapshot.set(it) } }

        fun reconcile() = scope.async { repository.reconcileOpened() }
        suspend fun revision(value: Long) = connection.events.emit(RevsEvent(key.machineId, mapOf(key.sessionId to value)))
        suspend fun legacyReload() = connection.events.emit(
            ItemsEvent(key.machineId, key.sessionId, emptyList(), "h", moreAfter = true),
        )
        fun submitted(input: Input) = adapter.submissions.filter { it.input == input }
        fun afterSeqs() = queries.mapNotNull { it.afterSeq }
        fun sequences() = snapshot.get().items.map { it.seq }

        suspend fun waitFor(applyFrames: Boolean = false, done: () -> Boolean) = withTimeout(5_000) {
            while (!done()) {
                if (applyFrames) frames.run()
                delay(1)
            }
        }

        suspend fun close() {
            observer.cancelAndJoin()
            job.cancelAndJoin()
            driver.close()
        }
    }

    private data class Submission(val input: Input, val operation: Long, val queued: Boolean)

    /** Synthetic operation IDs only identify effects; no input, state, or callback is replaced. */
    private class RecordingAdapter(
        chat: String,
        driver: RustContinuityDriver,
        applied: (ContinuityAdapter.Applied) -> Unit,
    ) : ContinuityAdapter {
        private val nextOperation = AtomicLong()
        val submissions = ConcurrentLinkedQueue<Submission>()
        private val views = ConcurrentLinkedQueue<ContinuityAdapter.Applied>()
        private val delegate = RustContinuityAdapter(chat, driver) { result -> views.add(result); applied(result) }
        override val state get() = delegate.state
        override val behind get() = delegate.behind
        override val asynchronous get() = delegate.asynchronous
        override val parallelFolds get() = delegate.parallelFolds

        override suspend fun accept(input: Input, operation: Long?): ContinuityAdapter.Applied? {
            val id = operation ?: nextOperation.decrementAndGet()
            return delegate.accept(input, id).also { submissions.add(Submission(input, id, it == null)) }
        }

        fun requestsFor(submission: Submission): List<Action>? = views.asSequence()
            .flatMap { it.operations.asSequence() }.firstOrNull { it.id == submission.operation }?.requests

        override fun close() = delegate.close()
    }

    private class Frames : ContinuityFrameScheduler {
        private val frames = ConcurrentLinkedQueue<() -> Unit>()
        override val synchronous = false
        override fun afterFirstFrame(start: () -> Unit) = start()
        override fun schedule(frame: () -> Unit) { frames.add(frame) }
        override suspend fun submitted(core: RustCore) {
            check(core.flush(5_000)) { "The continuity worker did not finish within five seconds" }
        }
        fun run() {
            // Callbacks enqueued by this frame are deliberately left for the next frame.
            repeat(frames.size) { frames.poll()?.invoke() }
        }
    }
}

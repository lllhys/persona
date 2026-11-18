package persona.core.data

import java.io.IOException
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.data.ContinuityAdapter.Input
import persona.core.rustcore.RustCore

/** Regression barriers for the final review: no test treats a worker flush as an applied frame. */
class ContinuityFinalReviewTest {
    private val key = SessionKey("fixture", "final-review")
    private fun item(n: Int) = Item("$n#0", n, role = "assistant", kind = "text")
    private fun page(n: Int, history: String = "h", from: Int? = null) =
        ItemsResponse(listOf(item(n)), history = history, fromSeq = from, toSeq = n, hasMore = true)

    @Test fun failedInitialReadMustNotLoseItsErrorToAnUnrelatedAckFrame() = runBlocking {
        Harness().use { h ->
            h.transport.fetch = { throw IOException("Synthetic read failure") }
            h.observe()
            waitFor { h.snapshot.get().error != null }
            val error = h.snapshot.get().error
            h.frames.run()
            assertEquals("An acknowledgement is not a successful read", error, h.snapshot.get().error)
        }
    }

    @Test fun oldAfterCannotRevertAnAlreadySubmittedHistory() = staleFill(reload = false)
    @Test fun oldReloadCannotRevertAnAlreadySubmittedHistory() = staleFill(reload = true)
    private fun staleFill(reload: Boolean) = runBlocking {
        Harness().use { h ->
            h.open()
            val held = CompletableDeferred<ItemsResponse>()
            val reads = AtomicInteger()
            h.transport.fetch = { if (reads.incrementAndGet() == 1) held.await() else page(20, "new") }
            if (reload) h.connection.events.emit(ItemsEvent(key.machineId, key.sessionId, emptyList(), "h", moreAfter = true))
            else h.revision()
            h.pump { reads.get() == 1 }
            h.connection.events.emit(ItemsEvent(key.machineId, key.sessionId, listOf(item(20)), "new", toSeq = 20, backlog = true))
            assertEquals("h", h.adapter.state.history)
            held.complete(page(2, from = 1))
            h.pump { reads.get() >= 2 || h.inputs.any { it is Input.After && it.batch.history == "h" || it is Input.Reload && it.history == "h" } }
            assertFalse("The stale HTTP body must not be submitted", h.inputs.any {
                it is Input.After && it.batch.history == "h" || it is Input.Reload && it.history == "h"
            })
            h.pump { h.snapshot.get().verified && h.adapter.state.history == "new" }
        }
    }

    @Test fun cancellationAfterHttpBeforeQueueingReleasesTheFillGate() = runBlocking {
        Harness().use { h ->
            h.open()
            val submitting = CompletableDeferred<Job>()
            val block = CompletableDeferred<Unit>()
            h.before = { input -> if (input is Input.After) {
                submitting.complete(currentCoroutineContext()[Job]!!)
                block.await()
            } }
            h.transport.fetch = { page(2, from = 1) }
            h.revision()
            h.pump { submitting.isCompleted }
            submitting.await().cancelAndJoin()
            assertEquals(1, h.inputs.count { it == Input.FillFailed })
            h.before = {}
            val attempts = AtomicInteger()
            h.transport.fetch = { attempts.incrementAndGet(); page(2, from = 1) }
            h.revision()
            h.pump { attempts.get() > 0 && h.snapshot.get().verified }
        }
    }

    @Test fun anOvertakenAfterGetsABoundedFollowUpWithoutAnotherHubChange() = runBlocking {
        Harness().use { h ->
            h.open()
            val reads = AtomicInteger()
            h.transport.fetch = { query ->
                if (reads.incrementAndGet() == 1) ItemsResponse(emptyList(), history = "h", fromSeq = 1, toSeq = 1)
                else ItemsResponse(listOf(item(2), item(3), item(4)), history = "h", fromSeq = query.afterSeq, toSeq = 4)
            }
            h.connection.events.emit(ItemsEvent(key.machineId, key.sessionId, listOf(item(4)), "h", fromSeq = 3, toSeq = 4))
            h.pump { h.inputs.any { it is Input.After } }
            repeat(6) { h.frames.run(); yield() }
            assertEquals("After must get the same overtaken follow-up as the synchronous path", 2, reads.get())
            h.pump { h.snapshot.get().verified }
        }
    }

    @Test fun stalledAfterFollowUpsStopAndANewHubChangeCanRetry() = runBlocking {
        Harness().use { h ->
            h.open()
            val reads = AtomicInteger()
            h.transport.fetch = { reads.incrementAndGet(); ItemsResponse(emptyList(), history = "h", fromSeq = 1, toSeq = 1) }
            h.connection.events.emit(ItemsEvent(key.machineId, key.sessionId, listOf(item(4)), "h", fromSeq = 3, toSeq = 4))
            h.pump { reads.get() >= 2 }
            repeat(20) { h.frames.run(); yield() }
            assertEquals(2, reads.get())
            assertFalse(h.snapshot.get().verified)
            h.revision()
            h.pump { reads.get() >= 3 }
        }
    }

    @Test fun progressingAfterFollowUpsStillHaveAnEightRequestBudget() = runBlocking {
        Harness().use { h ->
            h.open()
            val reads = AtomicInteger()
            h.transport.fetch = { query -> reads.incrementAndGet(); page(requireNotNull(query.afterSeq) + 1, from = query.afterSeq) }
            h.connection.events.emit(ItemsEvent(key.machineId, key.sessionId, listOf(item(100)), "h", fromSeq = 99, toSeq = 100))
            h.pump { reads.get() >= 9 }
            repeat(20) { h.frames.run(); yield() }
            assertEquals("One initial request plus eight bounded follow-ups", 9, reads.get())
            assertFalse(h.snapshot.get().verified)
        }
    }

    @Test fun anUnrelatedAckDoesNotFinishReconciliationWhileItsHttpIsHeld() = runBlocking {
        Harness().use { h ->
            h.open()
            val held = CompletableDeferred<ItemsResponse>()
            var reading = false
            h.transport.fetch = { reading = true; held.await() }
            val reconcile = h.scope.async { h.repository.reconcileOpened() }
            h.pump { reading }
            assertFalse("Old verified state in an Ack is not this reconciliation's completion", reconcile.isCompleted)
            held.complete(page(2, from = 1))
            h.pump { reconcile.isCompleted }
            reconcile.await()
            assertEquals(2, h.adapter.state.covered)
        }
    }

    @Test fun cancellingFirstAcceptBeforeCreationAllowsTheSameAdapterToRetry() = runBlocking {
        val frames = Frames(manualCreation = true)
        val driver = RustContinuityDriver(frames)
        val applied = ConcurrentLinkedQueue<ContinuityAdapter.Applied>()
        val adapter = RustContinuityAdapter("retry-open", driver, applied::add)
        try {
            val first = async { adapter.accept(Input.ContinueFill) }
            waitFor { frames.pending > 0 }
            first.cancelAndJoin()
            val retry = async { adapter.accept(Input.ContinueFill) }
            waitFor(frames) { retry.isCompleted }
            retry.await()
            frames.run()
            assertEquals(0L, driver.rejections.count)
            assertTrue(applied.flatMap { it.requests }.isNotEmpty())
        } finally { adapter.close(); driver.close() }
    }

    @Test fun failedQueueDoesNotCommitSubmittedHistory() = runBlocking {
        Harness().use { h ->
            h.open()
            h.before = { if (it is Input.After) throw IOException("Synthetic pre-queue failure") }
            h.transport.fetch = { page(2, "not-queued", 1) }
            h.revision()
            h.pump { h.inputs.any { it is Input.After } && h.snapshot.get().error != null }
            val entries = h.repository.javaClass.getDeclaredField("entries").apply { isAccessible = true }.get(h.repository) as Map<*, *>
            val entry = entries[key]!!
            val history = entry.javaClass.getDeclaredField("submittedHistory").apply { isAccessible = true }.get(entry)
            assertEquals("History changes only after successful queue submission", "h", history)
        }
    }

    @Test fun acknowledgedNoOpsReuseTheListAndDoNotPersistAgain() = runBlocking {
        Harness().use { h ->
            h.open()
            val list = h.adapter.state.items
            val saves = h.saves.get()
            h.adapter.accept(Input.Reconnect)
            h.frames.run()
            assertSame("Ack-only processing must not copy a full transcript", list, h.adapter.state.items)
            assertEquals("Ack-only processing must not save a full transcript", saves, h.saves.get())
        }
    }

    private inner class Harness : AutoCloseable {
        val frames = Frames()
        val driver = RustContinuityDriver(frames)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val connection = FakeConnection()
        val transport = FakeChatTransport().also { it.fetch = { page(1) } }
        val snapshot = AtomicReference(ChatSnapshot())
        val inputs = ConcurrentLinkedQueue<Input>()
        lateinit var adapter: RustContinuityAdapter
        var before: suspend (Input) -> Unit = {}
        val saves = AtomicInteger()
        private val cache = object : ChatItemsCache {
            override suspend fun load(session: SessionKey) = CachedChat()
            override suspend fun save(session: SessionKey, snapshot: CachedChat) { saves.incrementAndGet() }
        }
        val repository = HubChatRepository(transport, connection, cache, FakeSessions(), scope,
            continuityFactory = { chat, callback ->
                RustContinuityAdapter(chat, driver, callback).also { adapter = it }.let { delegate ->
                    object : ContinuityAdapter by delegate {
                        override suspend fun accept(input: Input, operation: Long?): ContinuityAdapter.Applied? {
                            inputs.add(input)
                            before(input)
                            return delegate.accept(input, operation)
                        }
                    }
                }
            })
        fun observe() { scope.launch { repository.observe(key).collect { snapshot.set(it) } } }
        suspend fun open() { observe(); pump { snapshot.get().verified }; frames.run(); inputs.clear() }
        suspend fun revision() = connection.events.emit(RevsEvent(key.machineId, mapOf(key.sessionId to 2L)))
        suspend fun pump(done: () -> Boolean) = waitFor(frames, done)
        override fun close() { scope.cancel(); driver.close() }
    }

    private suspend fun waitFor(frames: Frames? = null, done: () -> Boolean) = withTimeout(5_000) {
        while (!done()) { frames?.run(); delay(1) }
    }
    private class Frames(private val manualCreation: Boolean = false) : ContinuityFrameScheduler {
        private val frames = ConcurrentLinkedQueue<() -> Unit>()
        val pending get() = frames.size
        override fun afterFirstFrame(start: () -> Unit) { if (manualCreation) schedule(start) else start() }
        override fun schedule(frame: () -> Unit) { frames.add(frame) }
        override suspend fun submitted(core: RustCore) { check(core.flush(5_000)) }
        fun run() { repeat(frames.size) { frames.poll()?.invoke() } }
    }
}

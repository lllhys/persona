package persona.core.data

import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Item
import persona.core.domain.ContinuitySync.Action
import persona.core.rustcore.CoreHost
import persona.core.rustcore.RustCore
import persona.core.rustcore.wire.Action as CoreAction
import persona.core.rustcore.wire.ChatPatch
import persona.core.rustcore.wire.ChatSnapshot as CoreSnapshot
import persona.core.rustcore.wire.Config
import persona.core.rustcore.wire.Effect
import persona.core.rustcore.wire.Request

/** Real JNI with manual frames: dispatch completion and published state are separate barriers. */
class RustContinuityAdapterContractTest {
    @Test fun unusedAdaptersAndDriversCloseWithoutStartingTheCore() {
        val frames = ManualFrames()
        val creates = AtomicInteger()
        val driver = RustContinuityDriver(frames) {
            creates.incrementAndGet()
            error("An unused driver must not create a core")
        }
        val adapter = RustContinuityAdapter("unused", driver)
        adapter.close()
        driver.close()
        assertEquals(0, creates.get())
        assertEquals(0, frames.pending)
    }

    @Test fun creationWaitsForAFrameAndStateWaitsForTheFollowingDrain() = runBlocking {
        val frames = ManualFrames()
        val creates = AtomicInteger()
        val creationThread = AtomicReference<Thread>()
        val driver = RustContinuityDriver(frames) { host ->
            creates.incrementAndGet()
            creationThread.set(Thread.currentThread())
            RustCore(Config(), host)
        }
        val views = mutableListOf<ContinuityAdapter.Applied>()
        val adapter = RustContinuityAdapter("chat", driver, views::add)
        try {
            assertTrue(adapter.asynchronous)
            assertEquals(0, creates.get())
            assertEquals(0, frames.pending)
            val initial = ContinuityAdapter.State(listOf(item(1)), "h", 1, false, true)
            val accepting = async(Dispatchers.Default) { adapter.accept(ContinuityAdapter.Input.Restore(initial)) }
            frames.awaitScheduled()
            assertEquals("The first scheduled frame only starts creation", 0, creates.get())
            assertFalse(accepting.isCompleted)
            frames.runNext()
            assertNull(withTimeout(5_000) { accepting.await() })
            assertEquals(1, creates.get())
            assertNotSame(Thread.currentThread(), creationThread.get())
            assertEquals(ContinuityAdapter.State(), adapter.state)
            assertTrue(views.isEmpty())
            assertEquals(1, frames.pending)
            frames.runNext()
            assertEquals(initial, adapter.state)
            assertEquals(listOf(initial), views.map { it.state })
            assertEquals("chat", views.single().chat)

            assertNull(adapter.accept(ContinuityAdapter.Input.Push(batch(2))))
            assertNull(adapter.accept(ContinuityAdapter.Input.Push(batch(3))))
            assertEquals("Worker completion does not publish a frame", initial, adapter.state)
            assertEquals("A burst schedules one drain", 1, frames.pending)
            frames.runNext()
            assertEquals(listOf(1, 2, 3), adapter.state.items.map { it.seq })
            assertEquals(2, views.size)
            assertEquals(1, creates.get())
        } finally {
            adapter.close()
            driver.close()
        }
    }

    @Test fun targetedWakesCoalesceAndRequestsStayWithTheirOwner() = runBlocking {
        val frames = ManualFrames()
        val host = AtomicReference<CoreHost>()
        val driver = RustContinuityDriver(frames) {
            host.set(it)
            RustCore(Config(), it)
        }
        val first = mutableListOf<Pair<ChatPatch, List<Request>>>()
        val second = mutableListOf<Pair<ChatPatch, List<Request>>>()
        driver.register("first") { patch, requests -> first += patch to requests }
        driver.register("second") { patch, requests -> second += patch to requests }
        try {
            val opening = async(Dispatchers.Default) { driver.dispatch(CoreAction.OpenChat("first", emptySnapshot())) }
            frames.awaitScheduled()
            frames.runNext()
            withTimeout(5_000) { opening.await() }
            frames.runNext()
            driver.dispatch(CoreAction.OpenChat("second", emptySnapshot()))
            frames.runNext()
            first.clear()
            second.clear()

            repeat(10) { driver.changedChat("first", it.toLong()) }
            host.get().run(Effect.Fetch("first", Request.FetchAfter(3)))
            assertEquals(1, frames.pending)
            assertTrue(first.isEmpty())
            assertTrue(second.isEmpty())
            frames.runNext()
            assertEquals(1, first.size)
            assertEquals(listOf(Request.FetchAfter(3)), first.single().second)
            assertTrue("A targeted wake must not drain another chat", second.isEmpty())

            first.clear()
            host.get().run(Effect.Fetch("first", Request.FetchAfter(4)))
            host.get().run(Effect.Fetch("second", Request.Subscribe(7, true)))
            assertEquals(1, frames.pending)
            frames.runNext()
            assertEquals(listOf(Request.FetchAfter(4)), first.single().second)
            assertEquals(listOf(Request.Subscribe(7, true)), second.single().second)

            first.clear()
            second.clear()
            driver.changed(100)
            driver.changed(101)
            assertEquals(1, frames.pending)
            frames.runNext()
            assertEquals("A global wake conservatively drains every owner", 1, first.size)
            assertEquals(1, second.size)
        } finally {
            driver.close()
        }
    }

    @Test fun frameCoalescedUpdatesToTwoChatsKeepBothLists() = runBlocking {
        val frames = ManualFrames()
        RustContinuityDriver(frames).use { driver ->
            val first = RustContinuityAdapter("first", driver)
            val second = RustContinuityAdapter("second", driver)
            val opening = async(Dispatchers.Default) {
                first.accept(ContinuityAdapter.Input.Restore(ContinuityAdapter.State(listOf(item(1)), "h", 1, false, true)))
            }
            frames.awaitScheduled()
            frames.runNext()
            withTimeout(5_000) { opening.await() }
            frames.runNext()
            second.accept(ContinuityAdapter.Input.Restore(ContinuityAdapter.State(listOf(item(10)), "h", 10, false, true)))
            frames.runNext()
            repeat(5) { index ->
                first.accept(ContinuityAdapter.Input.Push(batch(index + 2)))
                second.accept(ContinuityAdapter.Input.Push(batch(index + 11)))
            }
            assertEquals(1, frames.pending)
            assertEquals(listOf(1), first.state.items.map { it.seq })
            assertEquals(listOf(10), second.state.items.map { it.seq })
            frames.runNext()
            assertEquals((1..6).toList(), first.state.items.map { it.seq })
            assertEquals((10..15).toList(), second.state.items.map { it.seq })
            first.accept(ContinuityAdapter.Input.Push(batch(7)))
            frames.runNext()
            assertEquals((1..7).toList(), first.state.items.map { it.seq })
            assertEquals((10..15).toList(), second.state.items.map { it.seq })
            first.close(); second.close()
        }
    }

    @Test fun kotlinRestoreRetainsPromotionFromAnEarlierDuplicateCachedItem() = runBlocking {
        val adapter = KotlinContinuityAdapter("cached")
        val old = item(1).copy(text = "old", supervisionUser = true)
        val newer = item(1).copy(text = "new")
        adapter.accept(ContinuityAdapter.Input.Restore(ContinuityAdapter.State(listOf(old, newer), "h", 1, false)))
        val pushed = adapter.accept(ContinuityAdapter.Input.Push(ContinuityAdapter.Batch(emptyList(), "h", toSeq = 1)))
        assertEquals(listOf(newer.copy(supervisionUser = true)), pushed.state.items)
        val latest = adapter.accept(ContinuityAdapter.Input.Latest(ContinuityAdapter.Batch(listOf(item(1)), "h", toSeq = 1)))
        assertEquals(listOf(item(1).copy(supervisionUser = true)), latest.state.items)
    }

    @Test fun bothAdaptersRetainNewerPushesAcrossOverlappingReloads() = runBlocking {
        RustContinuityDriver(FlushContinuityFrameScheduler()).use { driver ->
            val adapters = listOf(KotlinContinuityAdapter("reload"), RustContinuityAdapter("reload", driver))
            for (adapter in adapters) try {
                adapter.accept(ContinuityAdapter.Input.Restore(ContinuityAdapter.State(listOf(item(1)), "h", 1, false, true)))
                adapter.accept(ContinuityAdapter.Input.Push(ContinuityAdapter.Batch(listOf(item(4)), "h", 1, 4)))
                val first = requireNotNull(adapter.accept(ContinuityAdapter.Input.Reload(listOf(item(2)), "h", 2, false)))
                assertEquals(listOf(2, 4), first.state.items.map { it.seq })
                assertEquals(2, first.state.covered)
                val second = requireNotNull(adapter.accept(ContinuityAdapter.Input.Reload(listOf(item(3)), "h", 3, false)))
                assertEquals(listOf(3, 4), second.state.items.map { it.seq })
                assertEquals(3, second.state.covered)
            } finally {
                adapter.close()
            }
        }
    }

    @Test fun testSchedulerReturnsAppliedRequestsWithoutPublishingAsyncCallbacks() = runBlocking {
        RustContinuityDriver(FlushContinuityFrameScheduler()).use { driver ->
            val adapter = RustContinuityAdapter("chat", driver) { error("A synchronous test frame must return its result") }
            try {
                assertFalse(adapter.asynchronous)
                adapter.accept(ContinuityAdapter.Input.Restore(ContinuityAdapter.State(listOf(item(1)), "h", 1, false, true)))
                val result = requireNotNull(adapter.accept(ContinuityAdapter.Input.Push(batch(3))))
                assertEquals("chat", result.chat)
                assertEquals(listOf(1, 3), result.state.items.map { it.seq })
                assertEquals(listOf(Action.FetchAfter(1)), result.requests)
                assertEquals(result.state, adapter.state)
            } finally {
                adapter.close()
            }
        }
    }

    private class ManualFrames : ContinuityFrameScheduler {
        private val frames = ConcurrentLinkedQueue<() -> Unit>()
        private val scheduled = Channel<Unit>(Channel.UNLIMITED)
        val pending get() = frames.size

        override fun schedule(frame: () -> Unit) {
            frames.add(frame)
            check(scheduled.trySend(Unit).isSuccess)
        }

        override suspend fun submitted(core: RustCore) {
            assertTrue("The worker did not finish", core.flush(5_000))
        }

        suspend fun awaitScheduled() = withTimeout(5_000) { scheduled.receive() }
        fun runNext() = checkNotNull(frames.poll()) { "No continuity frame was scheduled" }.invoke()
    }

    private fun item(seq: Int) = Item("$seq#0", seq, role = "assistant", kind = "text", text = "line $seq")
    private fun batch(seq: Int) = ContinuityAdapter.Batch(listOf(item(seq)), "h", seq - 1, seq)
    private fun emptySnapshot() = CoreSnapshot("[]", "h", 0, false, true)
}

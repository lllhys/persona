package persona.core.data

import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.rustcore.RustCore

/** Real async adapter: transport completion and a manually released frame are separate. */
class ContinuityAsyncRepositoryTest {
    private val key = SessionKey("fixture", "async-chat")
    private fun item(seq: Int) = Item("$seq#0", seq, role = "assistant", kind = "text")

    @Test fun failedCatchUpReleasesItsGateAndTheNextRevisionReadsAgain() = runBlocking {
        val frames = Frames()
        val driver = RustContinuityDriver(frames)
        val job = SupervisorJob()
        val scope = CoroutineScope(job + Dispatchers.Unconfined)
        val attempts = AtomicInteger()
        val connection = FakeConnection()
        val transport = FakeChatTransport().also { it.fetch = { query ->
            if (query.afterSeq == null) ItemsResponse(listOf(item(1)), history = "h", toSeq = 1)
            else {
                if (attempts.incrementAndGet() == 1) error("Synthetic transport failure")
                ItemsResponse(listOf(item(2)), history = "h", fromSeq = 1, toSeq = 2)
            }
        } }
        val snapshot = AtomicReference(ChatSnapshot())
        val repository = HubChatRepository(transport, connection, FakeItemsCache(CachedChat()), FakeSessions(), scope,
            continuityFactory = { chat, applied -> RustContinuityAdapter(chat, driver, applied) })
        val observer = scope.launch { repository.observe(key).collect { snapshot.set(it) } }
        try {
            pump(frames) { snapshot.get().verified }
            connection.events.emit(RevsEvent(key.machineId, mapOf(key.sessionId to 2L)))
            pump(frames) { attempts.get() == 1 && snapshot.get().error != null }
            connection.events.emit(RevsEvent(key.machineId, mapOf(key.sessionId to 3L)))
            pump(frames) { snapshot.get().items.map { it.seq } == listOf(1, 2) && snapshot.get().verified }
            // Without a hub-change acknowledgement, Rust may conservatively re-read once after recovery.
            assertTrue(attempts.get() in 2..3)
        } finally {
            observer.cancelAndJoin(); job.cancelAndJoin(); driver.close()
        }
    }

    @Test fun anOvertakenAsyncReloadRequestsMissingCoverageOnItsAppliedFrame() = runBlocking {
        val frames = Frames()
        val driver = RustContinuityDriver(frames)
        val job = SupervisorJob()
        val scope = CoroutineScope(job + Dispatchers.Unconfined)
        val connection = FakeConnection()
        val latest = AtomicInteger()
        val afters = ConcurrentLinkedQueue<Int>()
        val transport = FakeChatTransport().also { it.fetch = { query ->
            when {
                query.afterSeq != null -> {
                    afters.add(query.afterSeq!!)
                    ItemsResponse(listOf(item(3), item(4)), history = "h", fromSeq = query.afterSeq, toSeq = 4)
                }
                latest.incrementAndGet() == 1 -> ItemsResponse(listOf(item(1)), history = "h", toSeq = 1)
                else -> ItemsResponse(listOf(item(2)), history = "h", toSeq = 2)
            }
        } }
        val snapshot = AtomicReference(ChatSnapshot())
        val repository = HubChatRepository(transport, connection, FakeItemsCache(CachedChat()), FakeSessions(), scope,
            continuityFactory = { chat, applied -> RustContinuityAdapter(chat, driver, applied) })
        val observer = scope.launch { repository.observe(key).collect { snapshot.set(it) } }
        try {
            pump(frames) { snapshot.get().verified }
            connection.events.emit(ItemsEvent(key.machineId, key.sessionId, listOf(item(4)), "h", 1, 4))
            pump(frames) { snapshot.get().items.any { it.seq == 4 } }
            // A legacy capped event asks for a reload whose page ends before the retained push.
            connection.events.emit(ItemsEvent(key.machineId, key.sessionId, emptyList(), "h", moreAfter = true))
            pump(frames) { afters.contains(2) }
            pump(frames) { snapshot.get().items.map { it.seq } == listOf(2, 3, 4) }
            assertTrue(afters.contains(2))
        } finally {
            observer.cancelAndJoin(); job.cancelAndJoin(); driver.close()
        }
    }

    private suspend fun pump(frames: Frames, done: () -> Boolean) = withTimeout(5_000) {
        while (!done()) { frames.run(); delay(1) }
    }

    private class Frames : ContinuityFrameScheduler {
        private val frames = ConcurrentLinkedQueue<() -> Unit>()
        override fun afterFirstFrame(start: () -> Unit) = start()
        override fun schedule(frame: () -> Unit) { frames.add(frame) }
        override suspend fun submitted(core: RustCore) { check(core.flush(5_000)) }
        fun run() {
            // Work scheduled by this drain belongs to the following frame.
            val count = frames.size
            repeat(count) { frames.poll()?.invoke() }
        }
    }
}

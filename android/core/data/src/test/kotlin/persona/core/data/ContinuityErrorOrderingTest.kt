package persona.core.data

import java.io.IOException
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.data.ContinuityAdapter.Input
import persona.core.rustcore.RustCore

class ContinuityErrorOrderingTest {
    private val key = SessionKey("fixture", "r2-review")
    private fun item(n: Int, text: String = "item") = Item("$n#0", n, role = "assistant", kind = "text", text = text)
    private fun page(n: Int) = ItemsResponse(listOf(item(n)), history = "h", toSeq = n, hasMore = true)

    @Test fun asyncOldContentAckMustNotEraseANewerHttpFailure() = oldContentError(true)
    @Test fun kotlinOldContentMustNotEraseANewerHttpFailure() = oldContentError(false)

    @Test fun asyncLaterContentClearsTheFailureWhenCoalescedWithAnOlderInput() = runBlocking {
        Harness().use { h ->
            h.open()
            val held = CompletableDeferred<ItemsResponse>()
            var reading = false
            h.transport.fetch = { reading = true; held.await() }
            h.revision()
            h.pump { reading }
            h.connection.events.emit(ItemsEvent(key.machineId, key.sessionId, listOf(item(1, "before failure")),
                "h", fromSeq = 1, toSeq = 1))
            held.completeExceptionally(IOException("Synthetic HTTP failure"))
            waitFor { h.snapshot.get().error != null }
            h.connection.events.emit(ItemsEvent(key.machineId, key.sessionId, listOf(item(1, "after failure")),
                "h", fromSeq = 1, toSeq = 1))
            h.pump { h.snapshot.get().error == null }
            assertEquals("after failure", h.snapshot.get().items.single().text)
            assertTrue(h.snapshot.get().verified)
        }
    }

    private fun oldContentError(asynchronous: Boolean) = runBlocking {
        Harness(asynchronous).use { h ->
            h.open()
            val held = CompletableDeferred<ItemsResponse>()
            var reading = false
            h.transport.fetch = { reading = true; held.await() }
            h.revision()
            h.pump { reading }
            h.connection.events.emit(ItemsEvent(key.machineId, key.sessionId, listOf(item(1, "earlier push")),
                "h", fromSeq = 1, toSeq = 1))
            held.completeExceptionally(IOException("newer HTTP failure"))
            waitFor { h.snapshot.get().error != null }
            val error = h.snapshot.get().error
            h.frames.run()
            yield()
            assertEquals("A content input submitted before a failed HTTP read cannot clear that newer failure", error, h.snapshot.get().error)
            assertFalse("Failed sync remains unverified", h.snapshot.get().verified)
        }
    }

    private inner class Harness(asynchronous: Boolean = true) : AutoCloseable {
        val frames = Frames()
        val driver = RustContinuityDriver(frames)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val connection = FakeConnection()
        val transport = FakeChatTransport().also { it.fetch = { page(1) } }
        val snapshot = AtomicReference(ChatSnapshot())
        val inputs = ConcurrentLinkedQueue<Input>()
        val repository = HubChatRepository(transport, connection, FakeItemsCache(CachedChat()), FakeSessions(), scope,
            continuityFactory = { chat, callback ->
                val delegate: ContinuityAdapter = if (asynchronous) RustContinuityAdapter(chat, driver, callback) else KotlinContinuityAdapter(chat)
                object : ContinuityAdapter by delegate {
                    override suspend fun accept(input: Input, operation: Long?): ContinuityAdapter.Applied? {
                        inputs.add(input)
                        return delegate.accept(input, operation)
                    }
                }
            })
        suspend fun open() {
            scope.launch { repository.observe(key).collect { snapshot.set(it) } }
            pump { snapshot.get().verified }
            frames.run()
            inputs.clear()
        }
        suspend fun revision() = connection.events.emit(RevsEvent(key.machineId, mapOf(key.sessionId to 2L)))
        suspend fun pump(done: () -> Boolean) = waitFor(frames, done)
        override fun close() { scope.cancel(); driver.close() }
    }

    private suspend fun waitFor(frames: Frames? = null, done: () -> Boolean) = withTimeout(5_000) {
        while (!done()) { frames?.run(); delay(1) }
    }
    private class Frames : ContinuityFrameScheduler {
        private val frames = ConcurrentLinkedQueue<() -> Unit>()
        override fun afterFirstFrame(start: () -> Unit) = start()
        override fun schedule(frame: () -> Unit) { frames.add(frame) }
        override suspend fun submitted(core: RustCore) { check(core.flush(5_000)) }
        fun run() { repeat(frames.size) { frames.poll()?.invoke() } }
    }
}

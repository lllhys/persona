package persona.core.data

import java.lang.reflect.InvocationTargetException
import java.util.concurrent.ConcurrentLinkedQueue
import kotlin.coroutines.intrinsics.suspendCoroutineUninterceptedOrReturn
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.selects.select
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.ChatSnapshot
import persona.core.contracts.api.ItemsQuery
import persona.core.data.ContinuityAdapter.Batch
import persona.core.data.ContinuityAdapter.Input

@OptIn(ExperimentalCoroutinesApi::class)
class ContinuitySynchronousLockTest {
    private val key = SessionKey("fixture", "synchronous-lock")
    private fun item(seq: Int) = Item("$seq#0", seq = seq, role = "assistant", kind = "tool_use")

    @Test fun kotlinAcceptSharesEntryLockWithConcurrentPushPublishAndPersist(): Unit = runBlocking {
        withTimeout(15_000) {
            val pushCount = 256
            val lastSeq = pushCount + 1
            val failures = ConcurrentLinkedQueue<Throwable>()
            val lockViolations = ConcurrentLinkedQueue<String>()
            val saved = ConcurrentLinkedQueue<CachedChat>()
            val published = ConcurrentLinkedQueue<ChatSnapshot>()
            val failed = CompletableDeferred<Unit>()
            val lastPushApplied = CompletableDeferred<Unit>()
            val lockReady = CompletableDeferred<Mutex>()
            val readerOwner = Any()
            fun recordFailure(failure: Throwable) {
                failures.add(failure)
                failed.complete(Unit)
            }
            suspend fun attempt(block: suspend () -> Unit) {
                try { block() }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (failure: Throwable) { recordFailure(failure) }
            }
            fun checkLock(lock: Mutex, input: Input) {
                // isLocked alone could mistake a competing reader's ownership for the writer's.
                if (!lock.isLocked || lock.holdsLock(readerOwner)) {
                    lockViolations.add("${input.javaClass.simpleName}: accept does not own entry.lock")
                }
            }
            val repositoryJob = SupervisorJob(coroutineContext[Job])
            val repositoryScope = CoroutineScope(repositoryJob + Dispatchers.Default +
                CoroutineExceptionHandler { _, failure -> recordFailure(failure) })
            try {
                val cache = object : ChatItemsCache {
                    override suspend fun load(session: SessionKey) =
                        CachedChat(listOf(item(1)), history = "h", covered = 1, hasMore = false)
                    override suspend fun save(session: SessionKey, snapshot: CachedChat) {
                        saved.add(snapshot)
                    }
                }
                val connection = FakeConnection()
                val repository = HubChatRepository(
                    FakeChatTransport(), connection, cache, FakeSessions(), repositoryScope,
                    continuityFactory = { chat, _ ->
                        val kotlin = KotlinContinuityAdapter(chat)
                        object : ContinuityAdapter by kotlin {
                            // persist suppresses exceptions; capture state-read failures before that happens.
                            override val state: ContinuityAdapter.State
                                get() = try { kotlin.state }
                                catch (failure: Throwable) { recordFailure(failure); throw failure }

                            override suspend fun accept(input: Input, operation: Long?): ContinuityAdapter.Applied {
                                val lock = lockReady.await()
                                checkLock(lock, input)
                                val result = try { kotlin.accept(input, operation) }
                                catch (failure: Throwable) { recordFailure(failure); throw failure }
                                checkLock(lock, input)
                                if (input is Input.Push && input.batch.toSeq == lastSeq) lastPushApplied.complete(Unit)
                                return result
                            }
                        }
                    },
                )
                // A retained entry avoids unrelated initial HTTP and reconnect work from observe().
                val entry = entry(repository)
                val lock = entry.field<Mutex>("lock")
                lockReady.complete(lock)
                entry.field<CompletableDeferred<Unit>>("ready").await()
                val state = entry.field<StateFlow<ChatSnapshot>>("state")

                // UNDISPATCHED reaches the real accept call before we inspect it. On a correct
                // implementation it suspends on entry.lock, rather than entering under this reader.
                val (probe, waitedForReader) = lock.withLock(readerOwner) {
                    val probe = async(start = CoroutineStart.UNDISPATCHED) {
                        invokeSuspend(repository, "pushSynchronous", entry,
                            Input.Push(Batch(listOf(item(1)), "h", fromSeq = 0, toSeq = 1,
                                backlog = true, hasMore = false)))
                    }
                    probe to !probe.isCompleted
                }
                probe.await()

                val start = CompletableDeferred<Unit>()
                val workers = mutableListOf<Deferred<Unit>>()
                workers += async(Dispatchers.Default) {
                    start.await()
                    for (seq in 2..lastSeq) {
                        connection.events.emit(ItemsEvent(key.machineId, key.sessionId,
                            listOf(item(seq)), history = "h", fromSeq = seq - 1, toSeq = seq,
                            backlog = true, hasMore = false))
                        yield()
                    }
                }
                repeat(2) {
                    workers += async(Dispatchers.Default) {
                        start.await()
                        repeat(pushCount) { round ->
                            attempt {
                                repository.recordTurnDuration(key, (round % 2).toLong())
                                published.add(state.value)
                            }
                            yield()
                        }
                    }
                    workers += async(Dispatchers.Default) {
                        start.await()
                        repeat(pushCount) {
                            attempt { invokeSuspend(repository, "persist", entry) }
                            yield()
                        }
                    }
                }
                start.complete(Unit)
                workers.awaitAll()
                // SharedFlow.emit acknowledges delivery, not application. A collector failure must
                // also release this barrier, so the test reports the exception instead of timing out.
                select<Unit> {
                    lastPushApplied.onAwait { }
                    failed.onAwait { }
                }
                attempt { repository.recordTurnDuration(key, null) }
                attempt { invokeSuspend(repository, "persist", entry) }

                assertTrue("Concurrent repository failures: ${failures.joinToString()}", failures.isEmpty())
                assertTrue("Kotlin inputs entered without their state lock: ${lockViolations.take(8)}",
                    lockViolations.isEmpty())
                assertTrue("A push entered while another coroutine owned entry.lock", waitedForReader)
                assertTrue("The last push must be applied before checking the final snapshot", lastPushApplied.isCompleted)
                val snapshots = saved.toList()
                assertTrue("Every explicit persist must reach the cache", snapshots.size >= pushCount * 2 + 1)
                for (snapshot in snapshots) {
                    // Sequential pushes make each legal cache snapshot exactly one complete prefix.
                    // This detects torn items/covered metadata and stale ordered-list caches, not just crashes.
                    assertEquals("h", snapshot.history)
                    assertEquals(false, snapshot.hasMore)
                    assertTrue(snapshot.covered in 1..lastSeq)
                    assertEquals((1..snapshot.covered).map(::item), snapshot.items)
                }
                assertEquals(pushCount * 2, published.size)
                for (snapshot in published) {
                    assertEquals("h", snapshot.history)
                    assertNull(snapshot.error)
                    assertTrue(snapshot.items.isNotEmpty())
                    assertEquals((1..snapshot.items.size).map(::item), snapshot.items)
                }
                val expected = (1..lastSeq).map(::item)
                assertEquals(expected, state.value.items)
                assertEquals(CachedChat(expected, "h", lastSeq, false), snapshots.last())
            } finally {
                withContext(NonCancellable) { repositoryJob.cancelAndJoin() }
            }
        }
    }

    @Test fun kotlinFoldRangeKeepsLatestHttpWaitingUntilAllPagesFinish(): Unit = runTest {
        val firstRangeEntered = CompletableDeferred<Unit>()
        val secondRangeEntered = CompletableDeferred<Unit>()
        val reloadEntered = CompletableDeferred<Unit>()
        val firstRange = CompletableDeferred<ItemsResponse>()
        val secondRange = CompletableDeferred<ItemsResponse>()
        val reloadPage = CompletableDeferred<ItemsResponse>()
        val folded = item(2).copy(id = "fold:2#0", kind = "fold",
            fold = Fold(items = 2, throughSeq = 3, throughId = "3#0"))
        val initial = listOf(item(1), folded, item(4))
        val requests = mutableListOf<ItemsQuery>()
        val transport = FakeChatTransport().also { fake ->
            fake.fetch = { query ->
                requests.add(query)
                when {
                    requests.size == 1 -> {
                        assertEquals(ItemsQuery(), query)
                        ItemsResponse(initial, history = "h", toSeq = 4)
                    }
                    query == ItemsQuery(fromSeq = 2, throughSeq = 3) -> {
                        firstRangeEntered.complete(Unit)
                        firstRange.await()
                    }
                    query == ItemsQuery(fromSeq = 3, throughSeq = 3) -> {
                        secondRangeEntered.complete(Unit)
                        secondRange.await()
                    }
                    query == ItemsQuery() -> {
                        reloadEntered.complete(Unit)
                        reloadPage.await()
                    }
                    else -> error("Unexpected HTTP query: $query")
                }
            }
        }
        val cache = FakeItemsCache(CachedChat())
        val repository = HubChatRepository(transport, FakeConnection(), cache, FakeSessions(), backgroundScope,
            continuityFactory = { chat, _ -> KotlinContinuityAdapter(chat) })
        var snapshot = ChatSnapshot()
        // Let constructor collectors run before observe creates an active entry.
        runCurrent()
        backgroundScope.launch { repository.observe(key).collect { snapshot = it } }
        runCurrent()
        assertEquals(initial, snapshot.items)
        assertEquals(listOf(ItemsQuery()), requests)

        val fold = async { repository.expandFold(key, 2, 3) }
        runCurrent()
        assertTrue(firstRangeEntered.isCompleted)
        // Execute latest up to its first suspension: either the shared requests mutex (correct),
        // or the held HTTP response (regression). No sleep or scheduling-luck assertion is needed.
        val reload = async(start = CoroutineStart.UNDISPATCHED) {
            invokeSuspend(repository, "latest", entry(repository), false)
        }
        try {
            assertFalse("Reload HTTP started during the first range request", reloadEntered.isCompleted)
            assertFalse(reload.isCompleted)
            firstRange.complete(ItemsResponse(listOf(item(2)), history = "h", more = true))
            runCurrent()
            assertTrue(secondRangeEntered.isCompleted)
            assertFalse("Reload HTTP started between range pages", reloadEntered.isCompleted)
            assertFalse(reload.isCompleted)
            assertEquals(initial, snapshot.items)

            secondRange.complete(ItemsResponse(listOf(item(3)), history = "h"))
            assertEquals(listOf(item(2), item(3)), fold.await())
            runCurrent()
            assertTrue("Reload must resume after the complete fold operation", reloadEntered.isCompleted)
            assertEquals((1..4).map(::item), snapshot.items)
            assertEquals(snapshot.items, cache.saved.items)
            assertEquals(listOf(ItemsQuery(), ItemsQuery(fromSeq = 2, throughSeq = 3),
                ItemsQuery(fromSeq = 3, throughSeq = 3), ItemsQuery()), requests)

            val refreshed = (1..4).map(::item) + item(5)
            reloadPage.complete(ItemsResponse(refreshed, history = "h", toSeq = 5))
            reload.await()
            runCurrent()
            assertEquals(refreshed, snapshot.items)
            assertEquals(CachedChat(refreshed, "h", 5, false), cache.saved)
            assertNull(snapshot.error)
        } finally {
            fold.cancelAndJoin()
            reload.cancelAndJoin()
        }
    }

    private fun entry(repository: HubChatRepository): Any = HubChatRepository::class.java
        .getDeclaredMethod("entry", SessionKey::class.java).apply { isAccessible = true }
        .invoke(repository, key)

    @Suppress("UNCHECKED_CAST")
    private fun <T> Any.field(name: String): T = javaClass.getDeclaredField(name)
        .apply { isAccessible = true }.get(this) as T

    // Preserve COROUTINE_SUSPENDED and the caller's continuation when invoking private suspend methods.
    private suspend fun invokeSuspend(repository: HubChatRepository, name: String, vararg args: Any?): Any? =
        suspendCoroutineUninterceptedOrReturn { continuation ->
            val method = HubChatRepository::class.java.declaredMethods.single {
                it.name == name && it.parameterCount == args.size + 1
            }.apply { isAccessible = true }
            try { method.invoke(repository, *args, continuation) }
            catch (failure: InvocationTargetException) { throw failure.targetException }
        }
}

package persona.core.data

import java.io.IOException
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.data.ContinuityAdapter.Input

@OptIn(ExperimentalCoroutinesApi::class)
class ContinuityKotlinParityTest {
    private val key = SessionKey("fixture", "r2-kotlin-race")
    private fun item(n: Int) = Item("$n#0", n, role = "assistant", kind = "text")

    @Test fun slowSeenMustNotSilentlyDiscardKotlinOlder() = runTest {
        val seenHeld = CompletableDeferred<Unit>()
        val seenStarted = CompletableDeferred<Unit>()
        val requests = mutableListOf<ItemsQuery>()
        val transport = FakeChatTransport().also { fake ->
            fake.fetch = { query ->
                requests += query
                if (query.beforeSeq != null) ItemsResponse(listOf(item(4)), history = "h")
                else ItemsResponse(listOf(item(5)), history = "h", toSeq = 5, hasMore = true)
            }
        }
        val repository = HubChatRepository(transport, FakeConnection(), FakeItemsCache(CachedChat()), FakeSessions(), backgroundScope,
            seen = { seenStarted.complete(Unit); seenHeld.await() },
            continuityFactory = { chat, _ -> KotlinContinuityAdapter(chat) })
        runCurrent()
        backgroundScope.launch { repository.observe(key).collect {} }
        runCurrent()
        val mark = async { repository.markSeen(key, "5#0") }
        runCurrent()
        assertTrue(seenStarted.isCompleted)
        val older = async { repository.loadOlder(key) }
        runCurrent()
        advanceTimeBy(3_001)
        runCurrent()
        seenHeld.complete(Unit)
        mark.await()
        older.await()
        assertEquals("A synchronous operation must wait for entry.lock rather than expire before HTTP", 1,
            requests.count { it.beforeSeq == 5 })
    }

    @Test fun seenRollbackMustNotRestoreFinishedOlderLoadingFlag() = runTest {
        val seenHeld = CompletableDeferred<Unit>()
        val seenStarted = CompletableDeferred<Unit>()
        lateinit var repository: HubChatRepository
        var mark: Deferred<Unit>? = null
        var snapshot = ChatSnapshot()
        val transport = FakeChatTransport().also { fake ->
            fake.fetch = { query ->
                if (query.beforeSeq != null) ItemsResponse(listOf(item(4)), history = "h")
                else ItemsResponse(listOf(item(5)), history = "h", toSeq = 5, hasMore = true)
            }
        }
        repository = HubChatRepository(transport, FakeConnection(), FakeItemsCache(CachedChat()), FakeSessions(), backgroundScope,
            seen = { seenStarted.complete(Unit); seenHeld.await(); throw IOException("Synthetic seen failure") },
            continuityFactory = { chat, _ ->
                val delegate = KotlinContinuityAdapter(chat)
                object : ContinuityAdapter by delegate {
                    override suspend fun accept(input: Input, operation: Long?): ContinuityAdapter.Applied {
                        val result = delegate.accept(input, operation)
                        if (input == Input.OlderFailed) {
                            // Register a real markSeen mutex waiter while OlderFailed owns entry.lock.
                            mark = backgroundScope.async(Dispatchers.Unconfined) {
                                try { repository.markSeen(key, "5#0") } catch (_: IOException) { }
                            }
                        }
                        return result
                    }
                }
            })
        runCurrent()
        backgroundScope.launch { repository.observe(key).collect { snapshot = it } }
        runCurrent()
        repository.loadOlder(key)
        runCurrent()
        assertTrue("markSeen entered between releasing entry.lock and the flag update", seenStarted.isCompleted)
        assertFalse("The transfer has finished", snapshot.loadingOlder)
        seenHeld.complete(Unit)
        mark!!.await()
        runCurrent()
        assertFalse("A failed read receipt must not resurrect a completed pagination spinner", snapshot.loadingOlder)
    }

    @Test fun pushedFoldMemberMustNotDiscardDownloadedExpansionItems() = runTest {
        val held = CompletableDeferred<ItemsResponse>()
        val folded = item(2).copy(id = "fold:2#0", kind = "fold", fold = Fold(items = 2, throughSeq = 3, throughId = "3#0"))
        val initial = listOf(item(1), folded, item(4))
        var reading = false
        val connection = FakeConnection()
        val transport = FakeChatTransport().also { fake ->
            fake.fetch = { query ->
                if (query.fromSeq != null) { reading = true; held.await() }
                else ItemsResponse(initial, history = "h", toSeq = 4)
            }
        }
        val repository = HubChatRepository(transport, connection, FakeItemsCache(CachedChat()), FakeSessions(), backgroundScope,
            continuityFactory = { chat, _ -> KotlinContinuityAdapter(chat) })
        runCurrent()
        backgroundScope.launch { repository.observe(key).collect {} }
        runCurrent()
        val expansion = async { repository.expandFold(key, 2, 3) }
        runCurrent()
        assertTrue(reading)
        connection.events.emit(ItemsEvent(key.machineId, key.sessionId, listOf(item(3)), "h", fromSeq = 3, toSeq = 3))
        runCurrent()
        held.complete(ItemsResponse(listOf(item(2), item(3)), history = "h"))
        assertEquals("The expansion caller needs the full downloaded response even if a push removed its fold", listOf(item(2), item(3)), expansion.await())
    }
}

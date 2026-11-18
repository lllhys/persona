package persona.core.data

import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.ItemsQuery
import persona.core.contracts.api.ChatSnapshot

/** Both synchronous test selections retain serialized HTTP; real async fold concurrency has separate coverage. */
@OptIn(ExperimentalCoroutinesApi::class)
class ContinuityFoldFlowTest {
    @get:org.junit.Rule val shadowParity = ShadowParityRule()
    private val key = SessionKey("fixture", "fold-flow")
    private fun work(seq: Int) = Item("$seq#0", seq = seq, role = "assistant", kind = "tool_use")
    private fun fold(seq: Int) = work(seq).copy(id = "fold:$seq#0", kind = "fold",
        fold = Fold(items = 2, throughSeq = seq + 1, throughId = "${seq + 1}#0"))

    @Test fun repeatedOpenOfOneFoldSharesOneFetchSequence() = runTest {
        val held = CompletableDeferred<ItemsResponse>()
        val requests = mutableListOf<ItemsQuery>()
        val transport = FakeChatTransport().also { it.fetch = { query ->
            if (query.fromSeq != null) { requests += query; held.await() }
            else ItemsResponse(listOf(fold(2)), history = "h", toSeq = 3)
        } }
        val repository = HubChatRepository(transport, FakeConnection(), FakeItemsCache(CachedChat()), FakeSessions(), backgroundScope)
        var snapshot = ChatSnapshot()
        backgroundScope.launch { repository.observe(key).collect { snapshot = it } }
        runCurrent()
        val first = async { repository.expandFold(key, 2, 3) }
        val second = async { repository.expandFold(key, 2, 3) }
        runCurrent()
        assertEquals(1, requests.size)
        held.complete(ItemsResponse(listOf(work(2), work(3))))
        first.await(); second.await()
        runCurrent()
        assertEquals(1, requests.size)
        assertEquals(listOf(2, 3), snapshot.items.map { it.seq })
        assertTrue(snapshot.items.none { it.kind == "fold" })
    }

    @Test fun aDifferentFoldQueuedDuringTheFirstCompletesWithoutLosingWork() = runTest {
        val held = CompletableDeferred<ItemsResponse>()
        val requests = mutableListOf<ItemsQuery>()
        val transport = FakeChatTransport().also { it.fetch = { query ->
            when (query.fromSeq) {
                2 -> { requests += query; held.await() }
                5 -> { requests += query; ItemsResponse(listOf(work(5), work(6))) }
                else -> ItemsResponse(listOf(fold(2), fold(5)), history = "h", toSeq = 6)
            }
        } }
        val repository = HubChatRepository(transport, FakeConnection(), FakeItemsCache(CachedChat()), FakeSessions(), backgroundScope)
        var snapshot = ChatSnapshot()
        backgroundScope.launch { repository.observe(key).collect { snapshot = it } }
        runCurrent()
        val first = async { repository.expandFold(key, 2, 3) }
        val second = async { repository.expandFold(key, 5, 6) }
        runCurrent()
        // Both selected test adapters are synchronous here; only the real async adapter enables parallel folds.
        assertEquals(listOf(2), requests.map { it.fromSeq })
        held.complete(ItemsResponse(listOf(work(2), work(3))))
        first.await(); second.await()
        runCurrent()
        assertEquals(listOf(2, 5), requests.map { it.fromSeq })
        assertEquals(listOf(2, 3, 5, 6), snapshot.items.map { it.seq })
        assertTrue(snapshot.items.none { it.kind == "fold" })
    }
}

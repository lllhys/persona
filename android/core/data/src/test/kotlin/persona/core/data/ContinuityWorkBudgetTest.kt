package persona.core.data

import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.data.ContinuityAdapter.Input
import persona.core.domain.ContinuityReducer

@OptIn(ExperimentalCoroutinesApi::class)
class ContinuityWorkBudgetTest {
    private val key = SessionKey("fixture", "work-budget")
    private fun item(seq: Int) = Item("$seq#0", seq, role = "assistant", kind = "text")

    @Test fun kotlinControlInputsDoNotTraverseCoveredHistory() = runTest {
        val adapter = KotlinContinuityAdapter(key.path)
        adapter.accept(Input.Latest(ContinuityAdapter.Batch(List(512) { item(it + 1) }, "h", toSeq = 512)))
        val reducer = adapter.javaClass.getDeclaredField("reducer").apply { isAccessible = true }
            .get(adapter) as ContinuityReducer
        val items = CountingItems(List(512) { item(it + 1) })
        reducer.javaClass.getDeclaredField("ordered").apply { isAccessible = true }.set(reducer, items)
        listOf(Input.HubChanged, Input.ContinueFill, Input.Reconnect, Input.FillFailed,
            Input.OlderFailed, Input.RangeFailed()).forEach { input ->
            adapter.accept(input)
            assertEquals("$input must not rescan history", 0, items.reads)
        }
    }

    @Test fun kotlinIntermediateRangeDoesNotTraverseCoveredHistory() = runTest {
        val adapter = KotlinContinuityAdapter(key.path)
        val fold = item(2).copy(id = "fold:2#0", kind = "fold",
            fold = Fold(items = 2, throughSeq = 3, throughId = "3#0"))
        adapter.accept(Input.Latest(ContinuityAdapter.Batch(listOf(item(1), fold) +
            (4..512).map(::item), "h", toSeq = 512)))
        adapter.accept(Input.OpenFold(fold.id))
        val reducer = adapter.javaClass.getDeclaredField("reducer").apply { isAccessible = true }
            .get(adapter) as ContinuityReducer
        val items = CountingItems(adapter.state.items)
        reducer.javaClass.getDeclaredField("ordered").apply { isAccessible = true }.set(reducer, items)
        adapter.accept(Input.Range(listOf(item(2)), more = true))
        assertEquals("An intermediate range page must not rescan the transcript", 0, items.reads)
    }

    @Test fun ordinaryAsyncAckDoesNotTraverseHistory() = runTest {
        var deliver: (ContinuityAdapter.Applied) -> Unit = {}
        val repository = HubChatRepository(FakeChatTransport(), FakeConnection(), FakeItemsCache(CachedChat()),
            FakeSessions(), backgroundScope, continuityFactory = { _, callback ->
                deliver = callback
                object : ContinuityAdapter {
                    override val state = ContinuityAdapter.State()
                    override val behind = false
                    override val asynchronous = true
                    override suspend fun accept(input: Input, operation: Long?): ContinuityAdapter.Applied? = null
                }
            })
        retainedEntry(repository)
        runCurrent()
        val items = CountingItems(List(512) { item(it + 1) })
        deliver(ContinuityAdapter.Applied(key.path, ContinuityAdapter.State(items, "h", 512),
            verified = true, stateChanged = false))
        runCurrent()
        assertEquals("A control Ack has no coverage work", 0, items.reads)
    }

    @Test fun synchronousCoverageReadsOneStateNotOnePerItem() = runTest {
        suspend fun readsFor(size: Int): Int {
            var reads = 0
            val transport = FakeChatTransport().also { it.fetch = {
                ItemsResponse(List(size) { item(it + 1) }, history = "h", toSeq = size)
            } }
            val repository = HubChatRepository(transport, FakeConnection(), FakeItemsCache(CachedChat()),
                FakeSessions(), backgroundScope, continuityFactory = { chat, _ ->
                    val delegate = KotlinContinuityAdapter(chat)
                    object : ContinuityAdapter by delegate {
                        override val state: ContinuityAdapter.State get() { reads++; return delegate.state }
                        override suspend fun accept(input: Input, operation: Long?): ContinuityAdapter.Applied {
                            assertNull("Synchronous inputs have no receipt IDs", operation)
                            return delegate.accept(input, operation)
                        }
                    }
                })
            val observer = backgroundScope.launch { repository.observe(key).collect {} }
            runCurrent()
            observer.cancelAndJoin()
            return reads
        }
        assertEquals("State reads must not grow with the number of covered items", readsFor(8), readsFor(512))
    }

    private fun retainedEntry(repository: HubChatRepository): Any = repository.javaClass
        .getDeclaredMethod("entry", SessionKey::class.java).apply { isAccessible = true }.invoke(repository, key)

    private class CountingItems(private val items: List<Item>) : AbstractList<Item>() {
        var reads = 0
        override val size get() = items.size
        override fun get(index: Int): Item { reads++; return items[index] }
    }
}

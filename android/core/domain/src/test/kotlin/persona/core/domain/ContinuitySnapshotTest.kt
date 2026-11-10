package persona.core.domain

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Item

class ContinuitySnapshotTest {
    @Test fun unchangedAndDuplicateReadsReuseTheSnapshotButOlderPagesPublishANewOne() {
        val reducer = ContinuityReducer()
        val recent = Item("recent#0", 3, kind = "text", text = "Recent")
        reducer.restore(listOf(recent), "history", 3, true)
        val before = reducer.items
        repeat(100) { assertSame(before, reducer.items) }
        reducer.apply(listOf(recent), "history", 3, 3)
        assertSame(before, reducer.items)
        reducer.older(listOf(Item("older#0", 1, kind = "text", text = "Older")), false)
        assertNotSame(before, reducer.items)
        assertEquals(listOf(1, 3), reducer.items.map { it.seq })
        assertEquals(listOf(recent), before)
        reducer.replace(listOf(recent.copy(text = "Replaced")), "history", 3, false)
        assertEquals("Replaced", reducer.items.single().text)
        reducer.restore(emptyList(), "new-history", 0)
        assertTrue(reducer.items.isEmpty())
    }
}

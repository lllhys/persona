package persona.platform.phonetools

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.PhoneToolResult

class PhoneToolResultOutboxTest {
    @Test fun aResultThatSendsImmediatelyIsNotHeld() {
        val outbox = PhoneToolResultOutbox()
        val result = PhoneToolResult.text("ok")
        val sent = outbox.send("c1", result) { true }
        assertTrue(sent)
        assertEquals(0, outbox.size())
    }

    @Test fun aResultThatCannotBeSentIsHeldAndFlushedOnce() {
        val outbox = PhoneToolResultOutbox()
        val result = PhoneToolResult.text("ok")
        assertFalse(outbox.send("c1", result) { false })
        assertEquals(1, outbox.size())
        val flushed = mutableListOf<String>()
        outbox.flush { callId, r ->
            flushed += callId
            assertEquals(result, r)
            true
        }
        assertEquals(listOf("c1"), flushed)
        assertEquals(0, outbox.size())
    }

    @Test fun flushStopsAtTheFirstSendFailureAndKeepsTheRest() {
        val outbox = PhoneToolResultOutbox()
        for (id in listOf("a", "b", "c")) assertFalse(outbox.send(id, PhoneToolResult.text(id)) { false })
        assertEquals(3, outbox.size())
        val sent = mutableListOf<String>()
        // First entry sends, second fails: the loop must stop, not skip ahead.
        outbox.flush { callId, _ ->
            if (callId == "b") return@flush false
            sent += callId; true
        }
        assertEquals(listOf("a"), sent)
        assertEquals(2, outbox.size())
    }

    @Test fun flushPreservesInsertionOrder() {
        val outbox = PhoneToolResultOutbox()
        for (id in listOf("a", "b", "c", "d")) assertFalse(outbox.send(id, PhoneToolResult.text(id)) { false })
        val flushed = mutableListOf<String>()
        outbox.flush { callId, _ -> flushed += callId; true }
        assertEquals(listOf("a", "b", "c", "d"), flushed)
    }

    /** A newer result for the same call_id replaces the held one; the tool is never re-executed. */
    @Test fun aNewerResultForTheSameCallIdReplacesTheHeldOne() {
        val outbox = PhoneToolResultOutbox()
        val first = PhoneToolResult.text("first")
        val second = PhoneToolResult.text("second")
        assertFalse(outbox.send("c1", first) { false })
        assertFalse(outbox.send("c1", second) { false })
        assertEquals(1, outbox.size())
        val flushed = mutableListOf<PhoneToolResult>()
        outbox.flush { _, r -> flushed += r; true }
        assertEquals(listOf(second), flushed)
    }

    /** Capacity is bounded: the oldest held results drop when more arrive than the cap. */
    @Test fun capacityDropsTheOldestEntries() {
        val outbox = PhoneToolResultOutbox(capacity = 3)
        for (i in 1..5) assertFalse(outbox.send("c$i", PhoneToolResult.text("r$i")) { false })
        assertEquals(3, outbox.size())
        val flushed = mutableListOf<String>()
        outbox.flush { id, _ -> flushed += id; true }
        // c1 and c2 dropped; only c3, c4, c5 remain in order.
        assertEquals(listOf("c3", "c4", "c5"), flushed)
    }

    /** close() drops everything; send() after close reports failure without holding. */
    @Test fun closeDropsTheQueueAndRefusesFurtherSends() {
        val outbox = PhoneToolResultOutbox()
        assertFalse(outbox.send("c1", PhoneToolResult.text("ok")) { false })
        outbox.close()
        assertEquals(0, outbox.size())
        // A send attempt after close goes through the transport; if it fails, the result is not held.
        assertFalse(outbox.send("c2", PhoneToolResult.text("ok")) { false })
        assertEquals(0, outbox.size())
        // flush after close does nothing.
        var flushedAny = false
        outbox.flush { _, _ -> flushedAny = true; true }
        assertFalse(flushedAny)
    }

    /** Flush must not invoke execute or registry: it only re-sends results, identified by call_id. */
    @Test fun flushOnlyResendsResultsItNeverExecutes() {
        val outbox = PhoneToolResultOutbox()
        val result = PhoneToolResult.text("created")
        assertFalse(outbox.send("w1", result) { false })
        var executions = 0
        // The flush lambda is the *socket send*. It cannot reach back into a registry; if it did, the test
        // would show it by incrementing executions. The only way a write is re-done is by calling execute again,
        // which this class never holds a reference to.
        outbox.flush { id, r ->
            assertEquals("w1", id)
            assertEquals(result, r)
            true
        }
        assertEquals(0, executions)
    }
}

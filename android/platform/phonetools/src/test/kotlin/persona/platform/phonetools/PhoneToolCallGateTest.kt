package persona.platform.phonetools

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.PhoneToolResult

@OptIn(ExperimentalCoroutinesApi::class)
class PhoneToolCallGateTest {
    @Test fun theFirstCallerBecomesLeaderAndLaterCallersShareTheResult() = runTest {
        val gate = PhoneToolCallGate()
        val leader = gate.begin("c1")
        assertTrue(leader is PhoneToolCallGate.Begin.Leader)
        val follower = gate.begin("c1")
        assertTrue(follower is PhoneToolCallGate.Begin.Follower)
        val result = PhoneToolResult.text("done")
        gate.complete("c1", result)
        assertEquals(result, (follower as PhoneToolCallGate.Begin.Follower).deferred.await())
        // After completion a third caller replays the cached result without becoming a leader.
        val third = gate.begin("c1")
        assertTrue(third is PhoneToolCallGate.Begin.Cached)
        assertEquals(result, (third as PhoneToolCallGate.Begin.Cached).result)
    }

    @Test fun aCompletedCallIdIsCachedUpToTheCapacity() = runTest {
        val gate = PhoneToolCallGate(capacity = 4)
        repeat(6) { i ->
            assertTrue(gate.begin("c$i") is PhoneToolCallGate.Begin.Leader)
            gate.complete("c$i", PhoneToolResult.text("r$i"))
        }
        // 6 results went in, only the last 4 are kept: c0 and c1 are forgotten, c2..c5 are cached.
        assertEquals(4, gate.cachedCount())
        assertTrue(gate.begin("c0") is PhoneToolCallGate.Begin.Leader)
        assertTrue(gate.begin("c1") is PhoneToolCallGate.Begin.Leader)
        assertTrue(gate.begin("c2") is PhoneToolCallGate.Begin.Cached)
        assertTrue(gate.begin("c5") is PhoneToolCallGate.Begin.Cached)
    }

    @Test fun aFailedLeaderReleasesTheSlotAndCancelsFollowers() = runTest {
        val gate = PhoneToolCallGate()
        assertTrue(gate.begin("c1") is PhoneToolCallGate.Begin.Leader)
        val follower = gate.begin("c1") as PhoneToolCallGate.Begin.Follower
        val cancelled = CancellationException("leader died")
        gate.fail("c1", cancelled)
        // The follower observes the cancellation instead of waiting forever.
        val thrown = runCatching { follower.deferred.await() }.exceptionOrNull()
        assertTrue(thrown is CancellationException)
        // No cached result was written; a new caller becomes the leader again.
        assertEquals(0, gate.cachedCount())
        assertTrue(gate.begin("c1") is PhoneToolCallGate.Begin.Leader)
        assertEquals(1, gate.inFlightCount())
    }

    /**
     * A follower's own cancellation must not cancel the leader's deferred: `await` is per-awaiter and only the
     * follower's scope dies. The leader continues and later completes normally.
     */
    @Test fun aCancelledFollowerDoesNotDisturbTheLeader() = runTest {
        val gate = PhoneToolCallGate()
        assertTrue(gate.begin("c1") is PhoneToolCallGate.Begin.Leader)
        val followerDeferred = (gate.begin("c1") as PhoneToolCallGate.Begin.Follower).deferred
        val followerJob = async { followerDeferred.await() }
        runCurrent()
        followerJob.cancel()
        runCurrent()
        assertTrue(followerJob.isCancelled)
        // The leader is unaffected: it can still complete, and its completion reaches any later follower.
        val result = PhoneToolResult.text("done")
        gate.complete("c1", result)
        assertEquals(result, followerDeferred.await())
    }

    @Test fun differentCallIdsAreIndependent() = runTest {
        val gate = PhoneToolCallGate()
        assertTrue(gate.begin("a") is PhoneToolCallGate.Begin.Leader)
        assertTrue(gate.begin("b") is PhoneToolCallGate.Begin.Leader)
        gate.complete("a", PhoneToolResult.text("ra"))
        gate.complete("b", PhoneToolResult.text("rb"))
        assertEquals("ra", ((gate.begin("a") as PhoneToolCallGate.Begin.Cached).result.content.single()
            .jsonObject.getValue("text").jsonPrimitive.content))
        assertEquals("rb", ((gate.begin("b") as PhoneToolCallGate.Begin.Cached).result.content.single()
            .jsonObject.getValue("text").jsonPrimitive.content))
    }
}

package persona.platform.phonetools

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import persona.core.contracts.api.PhoneToolResult

/**
 * Coordinates same-call-id deduplication and in-flight sharing inside one pairing runtime.
 *
 * The lock here only guards the maps; it never wraps `confirm`, `execute` or any socket send. Reads without
 * confirmation run truly concurrently and confirmed writes reach `PhoneConfirmations` concurrently too (which
 * serialises the user-facing card, not the execution lock). A second delivery of the same `call_id` either replays
 * the cached result or waits for the in-flight leader's result; the leader's cancellation is never propagated to
 * followers as a leader failure.
 */
internal class PhoneToolCallGate(
    private val capacity: Int = COMPLETED_CAPACITY,
) {
    sealed interface Begin {
        /** The cached result of a call that already finished in this pairing scope. */
        data class Cached(val result: PhoneToolResult) : Begin

        /** This caller runs the call and must `complete` / `fail` in a `finally`. */
        data object Leader : Begin

        /** Another caller is running this call; await its result without cancelling the leader. */
        data class Follower(val deferred: CompletableDeferred<PhoneToolResult>) : Begin
    }

    private val lock = Mutex()
    private val completed = LinkedHashMap<String, PhoneToolResult>()
    private val inFlight = mutableMapOf<String, CompletableDeferred<PhoneToolResult>>()

    suspend fun begin(callId: String): Begin = lock.withLock {
        completed[callId]?.let { return@withLock Begin.Cached(it) }
        inFlight[callId]?.let { return@withLock Begin.Follower(it) }
        val deferred = CompletableDeferred<PhoneToolResult>()
        inFlight[callId] = deferred
        Begin.Leader
    }

    /** The leader finished with a concrete result (success, refusal, denial, failure, uncertain). */
    suspend fun complete(callId: String, result: PhoneToolResult) = lock.withLock {
        completed[callId] = result
        while (completed.size > capacity) completed.remove(completed.keys.first())
        // Followers must not re-execute; hand them the same result the leader produced.
        inFlight.remove(callId)?.complete(result)
    }

    /**
     * The leader was cancelled or failed fatally. Followers must not wait forever: cancellation is propagated as
     * cancellation, while any other [Throwable] completes the deferred exceptionally without caching a tool result.
     */
    suspend fun fail(callId: String, cause: Throwable) = lock.withLock {
        val deferred = inFlight.remove(callId) ?: return@withLock
        if (cause is CancellationException) deferred.cancel(cause) else deferred.completeExceptionally(cause)
    }

    /** Test seam: how many call-ids have a cached terminal result. */
    internal suspend fun cachedCount(): Int = lock.withLock { completed.size }

    /** Test seam: how many call-ids currently have an in-flight leader. */
    internal suspend fun inFlightCount(): Int = lock.withLock { inFlight.size }

    private companion object { const val COMPLETED_CAPACITY = 128 }
}

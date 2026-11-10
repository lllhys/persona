package persona.core.hub

import persona.core.contracts.Endpoint
import persona.core.contracts.Health
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

private sealed interface PickAnswer {
    data class Reached(val candidate: HubRoute.Candidate) : PickAnswer
    data class Missed(val candidate: HubRoute.Candidate) : PickAnswer
    data object GraceOver : PickAnswer
}

private sealed interface SelectOutcome {
    data class Finished(val endpoint: Endpoint, val result: Result<HubRoute>) : SelectOutcome
    data object GraceOver : SelectOutcome
}

data class HubRoute(val endpoint: Endpoint, val health: Health) {
    data class Candidate(
        val endpoint: Endpoint,
        val hub: String? = null,
        val center: Boolean = false,
    ) {
        constructor(url: String, hub: String? = null, center: Boolean = false) :
            this(Endpoint(url), hub, center)
    }

    companion object {
        /** How long a member that answered first waits for the center to answer too. */
        const val CENTER_GRACE_MILLIS = 1500L
        /** How often a socket on a member asks whether the center answers again. */
        const val CENTER_RECHECK_MILLIS = 300_000L

        fun centerId(config: HubConnectionConfig, members: List<HubMember>): String? =
            config.centerHubId ?: if (config.center) config.hubId else members.firstOrNull { it.center }?.hubId

        /**
         * Asks every candidate at once and takes the center when it answers, else the quickest that answers:
         * a member that answers first is taken once every center probe failed, or once [graceMillis] went by
         * without the center answering. None answering: the first (the center when there is one).
         */
        suspend fun pick(
            candidates: List<Candidate>,
            graceMillis: Long = CENTER_GRACE_MILLIS,
            probe: suspend (Candidate) -> Boolean,
        ): Candidate? = coroutineScope {
            if (candidates.isEmpty()) return@coroutineScope null
            if (candidates.size == 1) return@coroutineScope candidates.first()

            val channel = Channel<PickAnswer>(Channel.UNLIMITED)
            var centersLeft = candidates.count { it.center }
            var member: Candidate? = null
            var graceJob: Job? = null

            val probeJobs = candidates.map { candidate ->
                launch {
                    val reached = try {
                        probe(candidate)
                    } catch (e: CancellationException) {
                        throw e
                    } catch (_: Exception) {
                        false
                    }
                    channel.send(if (reached) PickAnswer.Reached(candidate) else PickAnswer.Missed(candidate))
                }
            }

            try {
                var completedProbes = 0
                while (completedProbes < candidates.size) {
                    val answer = channel.receive()
                    if (answer is PickAnswer.GraceOver) {
                        if (member != null) return@coroutineScope member
                        continue
                    }
                    completedProbes++
                    when (answer) {
                        is PickAnswer.Reached -> {
                            if (answer.candidate.center || centersLeft == 0) {
                                return@coroutineScope answer.candidate
                            }
                            if (member == null) {
                                member = answer.candidate
                                graceJob = launch {
                                    delay(graceMillis)
                                    channel.send(PickAnswer.GraceOver)
                                }
                            }
                        }
                        is PickAnswer.Missed -> {
                            if (answer.candidate.center) {
                                centersLeft--
                            }
                            if (centersLeft == 0 && member != null) {
                                return@coroutineScope member
                            }
                        }
                        PickAnswer.GraceOver -> Unit
                    }
                }
                member ?: candidates.first()
            } finally {
                graceJob?.cancel()
                probeJobs.forEach { it.cancel() }
                channel.close()
            }
        }
    }
}

class HubRouteSelector internal constructor(
    private val clockMillis: () -> Long,
    private val probe: suspend (Endpoint) -> Health,
) {
    constructor(http: HubHttpClient, clockMillis: () -> Long = { System.nanoTime() / 1_000_000 }) :
        this(clockMillis, { http.health(it) })

    /**
     * Races pinned health probes strictly in the order given (the order settings lists; nothing is re-sorted here).
     * Each candidate starts once the one before it has failed or [STEP_MILLIS] after that one started, whichever is
     * first, so every candidate has had that long on its own before the next one can overtake it. The first to pass
     * the identity check wins. [probeTimeoutMillis] gives up on a probe that has not passed by then (a Timeout failure).
     *
     * When [centerEndpoints] is provided, candidate probes run concurrently with center preference: the center wins
     * when it answers; a member that answers first is taken once all center probes fail, or after [graceMillis].
     */
    suspend fun select(
        ordered: List<Endpoint>,
        probeTimeoutMillis: Long? = null,
        centerEndpoints: Set<String> = emptySet(),
        graceMillis: Long = HubRoute.CENTER_GRACE_MILLIS,
        onProbeMeasured: (Endpoint, Long, Result<HubRoute>) -> Unit = { _, _, _ -> },
        onProbe: (Endpoint, Result<HubRoute>) -> Unit = { _, _ -> },
    ): HubRoute = coroutineScope {
        val candidates = ordered.distinctBy { endpointKey(it.url) }
        if (candidates.isEmpty()) throw HubFailure(HubError.Unreachable)

        if (centerEndpoints.isEmpty()) {
            val started = List(candidates.size) { CompletableDeferred<Unit>() }
            val failed = List(candidates.size) { CompletableDeferred<Unit>() }
            val results = Channel<Result<HubRoute>>(Channel.UNLIMITED)
            val probes = candidates.mapIndexed { index, endpoint ->
                launch {
                    if (index > 0) {
                        started[index - 1].await()
                        withTimeoutOrNull(STEP_MILLIS) { failed[index - 1].await() }
                    }
                    started[index].complete(Unit)
                    val begin = clockMillis()
                    val result = try {
                        val health = if (probeTimeoutMillis == null) probe(endpoint)
                            else withTimeoutOrNull(probeTimeoutMillis) { probe(endpoint) } ?: throw HubFailure(HubError.Timeout)
                        Result.success(HubRoute(endpoint, health))
                    } catch (cancelled: CancellationException) {
                        throw cancelled
                    } catch (error: Exception) {
                        Result.failure(HubFailure.from(error))
                    }
                    if (result.isFailure) failed[index].complete(Unit)
                    onProbeMeasured(endpoint, (clockMillis() - begin).coerceAtLeast(0), result)
                    onProbe(endpoint, result)
                    results.send(result)
                }
            }
            try {
                val failures = mutableListOf<HubFailure>()
                repeat(candidates.size) {
                    val result = results.receive()
                    result.getOrNull()?.let { winner -> return@coroutineScope winner }
                    failures += HubFailure.from(result.exceptionOrNull()!!)
                }
                throw failures.firstOrNull { it.kind == HubError.CertificateMismatch }
                    ?: failures.firstOrNull { it.kind == HubError.HubMismatch }
                    ?: failures.firstOrNull { it.kind == HubError.AccessRejected || it.kind == HubError.AccessRequired }
                    ?: failures.first()
            } finally {
                probes.forEach { it.cancel() }
                results.close()
            }
        } else {
            val channel = Channel<SelectOutcome>(Channel.UNLIMITED)
            var centersLeft = candidates.count { endpointKey(it.url) in centerEndpoints }
            var memberWinner: HubRoute? = null
            var graceJob: Job? = null

            val probes = candidates.map { endpoint ->
                launch {
                    val begin = clockMillis()
                    val result = try {
                        val health = if (probeTimeoutMillis == null) probe(endpoint)
                            else withTimeoutOrNull(probeTimeoutMillis) { probe(endpoint) } ?: throw HubFailure(HubError.Timeout)
                        Result.success(HubRoute(endpoint, health))
                    } catch (cancelled: CancellationException) {
                        throw cancelled
                    } catch (error: Exception) {
                        Result.failure(HubFailure.from(error))
                    }
                    onProbeMeasured(endpoint, (clockMillis() - begin).coerceAtLeast(0), result)
                    onProbe(endpoint, result)
                    channel.send(SelectOutcome.Finished(endpoint, result))
                }
            }

            try {
                val failures = mutableListOf<HubFailure>()
                var completedCount = 0
                while (completedCount < candidates.size) {
                    val outcome = channel.receive()
                    if (outcome is SelectOutcome.GraceOver) {
                        if (memberWinner != null) return@coroutineScope memberWinner
                        continue
                    }
                    completedCount++
                    val (endpoint, result) = (outcome as SelectOutcome.Finished)
                    val isCenter = endpointKey(endpoint.url) in centerEndpoints

                    if (result.isSuccess) {
                        val winner = result.getOrNull()!!
                        if (isCenter || centersLeft == 0) {
                            return@coroutineScope winner
                        }
                        if (memberWinner == null) {
                            memberWinner = winner
                            graceJob = launch {
                                delay(graceMillis)
                                channel.send(SelectOutcome.GraceOver)
                            }
                        }
                    } else {
                        failures += HubFailure.from(result.exceptionOrNull()!!)
                        if (isCenter) {
                            centersLeft--
                            if (centersLeft == 0 && memberWinner != null) {
                                return@coroutineScope memberWinner
                            }
                        }
                    }
                }
                memberWinner?.let { return@coroutineScope it }
                throw failures.firstOrNull { it.kind == HubError.CertificateMismatch }
                    ?: failures.firstOrNull { it.kind == HubError.HubMismatch }
                    ?: failures.firstOrNull { it.kind == HubError.AccessRejected || it.kind == HubError.AccessRequired }
                    ?: failures.firstOrNull()
                    ?: HubFailure(HubError.Unreachable)
            } finally {
                graceJob?.cancel()
                probes.forEach { it.cancel() }
                channel.close()
            }
        }
    }

    companion object {
        /** How long a candidate has on its own before the next one starts; a configured value, not measured. */
        const val STEP_MILLIS = 1000L
    }
}

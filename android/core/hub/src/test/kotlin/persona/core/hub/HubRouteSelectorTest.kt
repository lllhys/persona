package persona.core.hub

import persona.core.contracts.Endpoint
import persona.core.contracts.Health
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.test.TestCoroutineScheduler
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class HubRouteSelectorTest {
    private val ipv6 = Endpoint("https://[2001:db8::1]:18662", "ipv6")
    private val name = Endpoint("https://hub.example.com:18662", "name")
    private val lan = Endpoint("https://hub.local:18662", "lan")
    private val ipv4 = Endpoint("https://192.0.2.10:18662", "ipv4")
    private val step = HubRouteSelector.STEP_MILLIS

    /** Probes on virtual time: each endpoint answers after its delay, or fails with its error then. */
    private class Probes(private val scheduler: TestCoroutineScheduler, private val plan: Map<Endpoint, Pair<Long, HubError?>>) {
        /** When each probe started, in the order they started. */
        val started = linkedMapOf<Endpoint, Long>()
        suspend fun probe(endpoint: Endpoint): Health {
            started[endpoint] = scheduler.currentTime
            val (millis, error) = plan.getValue(endpoint)
            delay(millis)
            if (error != null) throw HubFailure(error)
            return Health(ok = true, hubId = "test-hub")
        }
    }

    private fun answers(millis: Long) = millis to null
    private fun fails(millis: Long, error: HubError = HubError.ConnectionRefused) = millis to error

    private fun TestCoroutineScheduler.selector(probes: Probes) = HubRouteSelector({ currentTime }, probes::probe)

    private suspend fun failure(block: suspend () -> Unit): HubFailure {
        try { block() } catch (error: HubFailure) { return error }
        throw AssertionError("Expected HubFailure")
    }

    @Test fun probesStartStrictlyInTheGivenOrderEachWhenThePreviousFailedOrHadItsStep() = runTest {
        // Neither kind nor scheme re-sorts anything here: the order given is the order tried.
        val probes = Probes(testScheduler, mapOf(ipv4 to fails(50), lan to fails(5000, HubError.Timeout), name to fails(30), ipv6 to answers(10)))
        val route = testScheduler.selector(probes).select(listOf(ipv4, lan, name, ipv6))
        assertEquals(ipv6, route.endpoint)
        // lan neither answered nor failed within its step, so name started a step after it; name failed fast, so ipv6 at once.
        assertEquals(listOf(ipv4 to 0L, lan to 50L, name to 50L + step, ipv6 to 80L + step), probes.started.toList())
        assertEquals(90L + step, testScheduler.currentTime)
    }

    @Test fun aSlowButAliveEarlierCandidateBeatsAFastLaterOneWithinItsStep() = runTest {
        val probes = Probes(testScheduler, mapOf(ipv4 to answers(step - 100), ipv6 to answers(10)))
        val route = testScheduler.selector(probes).select(listOf(ipv4, ipv6))
        assertEquals(ipv4, route.endpoint)
        assertEquals(listOf(ipv4 to 0L), probes.started.toList())
        assertEquals(step - 100, testScheduler.currentTime)
    }

    @Test fun aLaterCandidateCanWinOnceTheEarlierOneHadItsStepAlone() = runTest {
        val probes = Probes(testScheduler, mapOf(ipv4 to answers(5000), ipv6 to answers(10)))
        val route = testScheduler.selector(probes).select(listOf(ipv4, ipv6))
        assertEquals(ipv6, route.endpoint)
        assertEquals(listOf(ipv4 to 0L, ipv6 to step), probes.started.toList())
        assertEquals(step + 10, testScheduler.currentTime)
        // The first answer wins, whoever started first: here the earlier one, a little after its step.
        val begun = testScheduler.currentTime
        val later = Probes(testScheduler, mapOf(ipv4 to answers(step + 200), ipv6 to answers(500)))
        assertEquals(ipv4, testScheduler.selector(later).select(listOf(ipv4, ipv6)).endpoint)
        assertEquals(listOf(ipv4 to begun, ipv6 to begun + step), later.started.toList())
    }

    @Test fun failuresMoveOnAtOnceAndHangingCandidatesStartOneStepApart() = runTest {
        val failing = Probes(testScheduler, mapOf(ipv6 to fails(30), name to fails(20), lan to fails(10), ipv4 to answers(5)))
        assertEquals(ipv4, testScheduler.selector(failing).select(listOf(ipv6, name, lan, ipv4)).endpoint)
        assertEquals(listOf(ipv6 to 0L, name to 30L, lan to 50L, ipv4 to 60L), failing.started.toList())
        val begun = testScheduler.currentTime
        val hanging = Probes(testScheduler, listOf(ipv6, name, lan, ipv4).associateWith { answers(10_000) })
        assertEquals(ipv6, testScheduler.selector(hanging).select(listOf(ipv6, name, lan, ipv4)).endpoint)
        assertEquals(listOf(0L, step, 2 * step, 3 * step).map { it + begun }, hanging.started.values.toList())
    }

    @Test fun aProbeTimeoutGivesUpOnAnswersThatComeLater() = runTest {
        val probes = Probes(testScheduler, mapOf(ipv6 to answers(step + 500), ipv4 to answers(300)))
        assertEquals(ipv4, testScheduler.selector(probes).select(listOf(ipv6, ipv4), probeTimeoutMillis = step).endpoint)
        assertEquals(listOf(ipv6 to 0L, ipv4 to step), probes.started.toList())
        // Given up on, a probe is a timeout, measured at the limit.
        val begun = testScheduler.currentTime
        val measured = mutableListOf<Pair<Endpoint, Long>>()
        val slow = Probes(testScheduler, mapOf(ipv6 to answers(step + 500)))
        val failure = failure {
            testScheduler.selector(slow).select(listOf(ipv6), step, onProbeMeasured = { endpoint, millis, _ -> measured += endpoint to millis })
        }
        assertEquals(HubError.Timeout, failure.kind)
        assertEquals(listOf(ipv6 to step), measured)
        assertEquals(begun + step, testScheduler.currentTime)
        // An answer in time counts as always.
        val quick = Probes(testScheduler, mapOf(ipv6 to answers(step - 1)))
        assertEquals(ipv6, testScheduler.selector(quick).select(listOf(ipv6), step).endpoint)
    }

    @Test fun errorsFavourCertificateMismatchThenHubMismatchThenTheFirstFailure() = runTest {
        fun plan(vararg errors: Pair<Endpoint, HubError>) = Probes(testScheduler, errors.associate { (endpoint, error) -> endpoint to fails(10, error) })
        suspend fun error(probes: Probes, order: List<Endpoint>) = failure { testScheduler.selector(probes).select(order) }.kind
        assertEquals(HubError.CertificateMismatch, error(plan(ipv6 to HubError.ConnectionRefused, name to HubError.HubMismatch,
            ipv4 to HubError.CertificateMismatch), listOf(ipv6, name, ipv4)))
        assertEquals(HubError.HubMismatch, error(plan(ipv6 to HubError.Timeout, name to HubError.HubMismatch), listOf(ipv6, name)))
        assertEquals(HubError.Timeout, error(plan(ipv6 to HubError.Timeout, name to HubError.ConnectionRefused), listOf(ipv6, name)))
        assertEquals(HubError.ConnectionRefused, error(plan(ipv6 to HubError.Timeout, name to HubError.ConnectionRefused), listOf(name, ipv6)))
    }

    @Test fun aPrivateAddressThatAnswersAsAnotherDeviceIsAnUnreachableRouteNotAChangedCertificate() = runTest {
        // Away from home a LAN address can be another device: its certificate says nothing about the hub.
        val home = Endpoint("https://[fd00::20]:18662", "lan")
        val ula = Endpoint("https://[fd00::91]:18662", "unknown")
        fun plan(vararg errors: Pair<Endpoint, HubError>) = Probes(testScheduler, errors.associate { (endpoint, error) -> endpoint to fails(10, error) })
        suspend fun error(probes: Probes, order: List<Endpoint>) = failure { testScheduler.selector(probes).select(order) }
        val away = error(plan(home to HubError.CertificateMismatch, name to HubError.Timeout), listOf(home, name))
        assertEquals(HubError.Unreachable, away.kind)
        assertEquals(PRIVATE_ADDRESS_MISMATCH, away.code)
        assertTrue(away.retryable)
        assertEquals(HubError.Unreachable, error(plan(ula to HubError.HubMismatch, ipv6 to HubError.ConnectionRefused), listOf(ula, ipv6)).kind)
        // A public route with the wrong key is still a changed certificate, whatever the private one said.
        assertEquals(HubError.CertificateMismatch, error(plan(home to HubError.CertificateMismatch, name to HubError.CertificateMismatch), listOf(home, name)).kind)
        // Only private addresses tried: nothing else could say more, so the mismatch stands.
        assertEquals(HubError.CertificateMismatch, error(plan(home to HubError.CertificateMismatch), listOf(home)).kind)
        // A route that answers still wins over a private mismatch.
        val probes = Probes(testScheduler, mapOf(home to fails(10, HubError.CertificateMismatch), name to answers(20)))
        assertEquals(name, testScheduler.selector(probes).select(listOf(home, name)).endpoint)
    }

    @Test fun thePrivateRuleAlsoHoldsWhenACenterIsPreferred() = runTest {
        val home = Endpoint("https://hub.local:18662", "lan")
        val probes = Probes(testScheduler, mapOf(home to fails(10, HubError.CertificateMismatch), name to fails(20, HubError.Timeout)))
        val error = failure { testScheduler.selector(probes).select(listOf(home, name), centerEndpoints = setOf(endpointKey(name.url))) }
        assertEquals(HubError.Unreachable, error.kind)
    }

    @Test fun eachKeyIsProbedOnceAndNoCandidateIsUnreachable() = runTest {
        val spelled = Endpoint("https://HUB.example.com:18662/", "name")
        val probes = Probes(testScheduler, mapOf(name to fails(10), ipv4 to answers(10)))
        assertEquals(ipv4, testScheduler.selector(probes).select(listOf(name, spelled, ipv4)).endpoint)
        assertEquals(listOf(name, ipv4), probes.started.keys.toList())
        assertEquals(HubError.Unreachable, failure { testScheduler.selector(probes).select(emptyList()) }.kind)
    }

    @Test fun everyProbeIsReportedWithItsDuration() = runTest {
        val probes = Probes(testScheduler, mapOf(ipv6 to fails(40), ipv4 to answers(25)))
        val measured = mutableListOf<Triple<Endpoint, Long, Boolean>>()
        val reported = mutableListOf<Endpoint>()
        testScheduler.selector(probes).select(listOf(ipv6, ipv4), onProbeMeasured = { endpoint, millis, result ->
            measured += Triple(endpoint, millis, result.isSuccess)
        }) { endpoint, _ -> reported += endpoint }
        assertEquals(listOf(Triple(ipv6, 40L, false), Triple(ipv4, 25L, true)), measured)
        assertEquals(listOf(ipv6, ipv4), reported)
    }
}

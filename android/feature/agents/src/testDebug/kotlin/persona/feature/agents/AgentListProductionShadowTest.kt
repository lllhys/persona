package persona.feature.agents

import java.time.Instant
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.AgentState
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.data.AgentListAdapter
import persona.core.data.AgentListMismatch
import persona.core.data.KotlinAgentListAdapter
import persona.core.data.ShadowAgentListAdapter
import persona.core.data.ShadowAgentListDiagnostics
import persona.core.domain.sort.AgentGrouping
import persona.core.domain.sort.AgentListSettings
import persona.core.domain.sort.AgentSession

/** The actual production entry, not just a fixture decoder, reaches the DEBUG shadow off its caller thread. */
class AgentListProductionShadowTest {
    @Test fun runningProjectListUsesTheAdapterAndReturnsKotlinImmediately() {
        val called = CountDownLatch(1)
        val reports = mutableListOf<AgentListMismatch>()
        val shadow = ShadowAgentListAdapter(projectRust = {
            called.countDown()
            error("Synthetic failure: production must already have returned Kotlin")
        }, sink = reports::add)
        val rows = listOf(AgentSession(Session(machineId = "m", sessionId = "a", state = "idle", updatedAt = "2026-01-01T00:00:00Z")))
        val expected = scopedAgentList(rows, listOf(Machine("m")), AgentsPreferencesSnapshot(),
            AgentListSettings(grouping = AgentGrouping.Repo))
        val actual = scopedAgentList(rows, listOf(Machine("m")), AgentsPreferencesSnapshot(),
            AgentListSettings(grouping = AgentGrouping.Repo), adapter = shadow, now = Instant.EPOCH)
        assertEquals(expected, actual)
        assertTrue(called.await(30, TimeUnit.SECONDS))
        ShadowAgentListDiagnostics.awaitIdle()
        assertTrue(reports.isNotEmpty())
    }

    @Test fun nativeFailureDoesNotChangeProductionProjection() {
        val shadow = ShadowAgentListAdapter(projectRust = { error("native failure") })
        val rows = listOf(AgentSession(Session(machineId = "m", sessionId = "a", state = "idle")))
        val expected = scopedAgentList(rows, listOf(Machine("m")), AgentsPreferencesSnapshot(),
            AgentListSettings(grouping = AgentGrouping.State))
        val actual = scopedAgentList(rows, listOf(Machine("m")), AgentsPreferencesSnapshot(),
            AgentListSettings(grouping = AgentGrouping.State), adapter = shadow, now = Instant.EPOCH)
        assertEquals(expected, actual)
        ShadowAgentListDiagnostics.awaitIdle()
        assertEquals(1, shadow.mismatchCount)
    }
}

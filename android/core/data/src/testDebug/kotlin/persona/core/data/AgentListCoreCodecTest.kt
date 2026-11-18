package persona.core.data

import java.time.Instant
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.JsonCodec
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.domain.sort.AgentGrouping
import persona.core.domain.sort.AgentListSettings
import persona.core.domain.sort.AgentPreferences
import persona.core.domain.sort.AgentSession
import persona.core.domain.sort.AgentSort

class AgentListCoreCodecTest {
    @Test fun convertsProductionFieldsAndHeldInput() {
        val row = AgentSession(Session(
            machineId = "machine-a~g1234abc", sessionId = "session-a", title = "synthetic title", cwd = "/synthetic/work",
            state = "working", status = "working", updatedAt = "2026-01-01T00:00:00Z", backgroundWaiting = true,
        ), stateSince = "2025-12-31T23:59:00Z")
        val encoded = AgentListCoreCodec.encode(AgentListAdapter.Input(
            sessions = listOf(row), machines = listOf(Machine("machine-a~g1234abc", "synthetic machine")),
            pins = AgentPreferences(sessions = listOf("machine-a~g1234abc/session-a"), spaces = listOf("machine-a~g1234abc/space")),
            mutes = AgentPreferences(sessions = listOf("machine-a~g1234abc/session-a"), spaces = listOf("machine-a~g1234abc/space"),
                allowed = setOf("machine-a~g1234abc/session-a")),
            settings = AgentListSettings(grouping = AgentGrouping.Space, sort = AgentSort.Updated, query = "synthetic query",
                folded = setOf("machine-a~g1234abc/space"), expandedIdle = setOf("machine-a~g1234abc/space"),
                expandedQuiet = setOf("machine-a~g1234abc/space")),
            held = "machine-a~g1234abc/session-a", now = Instant.parse("2026-01-01T00:01:00Z"),
        ))
        val json = JsonCodec.parseToJsonElement(encoded).jsonObject
        val session = json.getValue("sessions").jsonArray.single().jsonObject
        assertEquals("machine-a", session.getValue("machine_id").jsonPrimitive.content)
        assertEquals("g1234abc", session.getValue("app_hub_group").jsonPrimitive.content)
        assertEquals("working", session.getValue("state").jsonPrimitive.content)
        assertEquals("2025-12-31T23:59:00Z", session.getValue("state_since").jsonPrimitive.content)
        assertEquals(true, session.getValue("background_waiting").jsonPrimitive.booleanOrNull)
        val settings = json.getValue("settings").jsonObject
        assertEquals("space", settings.getValue("grouping").jsonPrimitive.content)
        assertEquals("updated", settings.getValue("sort").jsonPrimitive.content)
        assertEquals("g1234abc|machine-a/session-a", settings.getValue("held").jsonPrimitive.content)
        assertEquals("2026-01-01T00:01:00Z", settings.getValue("now").jsonPrimitive.content)
        assertEquals("g1234abc|machine-a/session-a", json.getValue("pins").jsonObject.getValue("sessions").jsonArray.single().jsonPrimitive.content)
        assertEquals("g1234abc|machine-a/space", json.getValue("pins").jsonObject.getValue("spaces").jsonArray.single().jsonPrimitive.content)
        assertEquals("g1234abc|machine-a/space", settings.getValue("folded").jsonArray.single().jsonPrimitive.content)
        assertEquals("g1234abc|machine-a/space", settings.getValue("expanded_idle").jsonArray.single().jsonPrimitive.content)
        assertEquals("g1234abc|machine-a/space", settings.getValue("expanded_quiet").jsonArray.single().jsonPrimitive.content)
    }

    @Test fun rejectsUnsupportedGroupingWithoutPassingContentToRust() {
        var called = false
        val shadow = ShadowAgentListAdapter(projectRust = { called = true; "" })
        val input = AgentListAdapter.Input(sessions = emptyList(),settings = AgentListSettings(grouping = AgentGrouping.Repo))
        assertEquals(KotlinAgentListAdapter().project(input), shadow.project(input))
        ShadowAgentListDiagnostics.awaitIdle()
        assertEquals(1, shadow.mismatchCount)
    }

    @Test fun nativeFailureAndDiagnosticFailureCannotChangeKotlinResult() {
        val input = AgentListAdapter.Input(sessions = emptyList(),)
        val expected = KotlinAgentListAdapter().project(input)
        val shadow = ShadowAgentListAdapter(projectRust = { error("sensitive native failure") }, sink = { error("sensitive sink failure") })
        assertEquals(expected, shadow.project(input))
        ShadowAgentListDiagnostics.awaitIdle()
        assertEquals(1, shadow.mismatchCount)
    }

    @Test fun mismatchReportContainsOnlyShapeEnumsCountsAndFlags() {
        val reports = mutableListOf<AgentListMismatch>()
        val shadow = ShadowAgentListAdapter(projectRust = { """{"counts":{},"groups":[],"unread_shown":[]}""" }, sink = reports::add)
        shadow.project(AgentListAdapter.Input(
            sessions = listOf(AgentSession(Session(machineId = "sensitive-machine", sessionId = "sensitive-session", title = "sensitive-title", cwd = "/sensitive/path", state = "unread", status = "done", updatedAt = "2026-01-01T00:00:00Z"))),
            settings = AgentListSettings(grouping = AgentGrouping.State), held = "sensitive-machine/sensitive-session",
        ))
        ShadowAgentListDiagnostics.awaitIdle()
        assertTrue("reports=${reports.joinToString { "${it.rule}:${it.inputShape}" }}", reports.isNotEmpty())
        for (report in reports) {
            val serialized = JsonCodec.encodeToString(report)
            assertEquals("agents-list-sort", report.domain)
            assertTrue(report.rule.isNotEmpty())
            assertTrue(report.inputShape.contains("held=true"))
            assertFalse(serialized.contains("sensitive"))
        }
    }

    @Test fun unsupportedMultiHubInputIsReportedBeforeNativeLoading() {
        val shadow = ShadowAgentListAdapter(projectRust = { error("must not load") })
        shadow.project(AgentListAdapter.Input(sessions = listOf(AgentSession(Session(machineId = "machine~g1234abc", sessionId = "one"))),
            machines = listOf(Machine("machine~g1234abc"))))
        ShadowAgentListDiagnostics.awaitIdle()
        assertTrue(shadow.mismatchCount > 0) // #9027: core pin identity does not match iOS multi-hub SessionKey.path.
    }
}

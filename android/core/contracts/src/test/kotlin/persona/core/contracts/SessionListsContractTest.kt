package persona.core.contracts

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.*

class SessionListsContractTest {
    @Test fun compactWorkflowProgressRoundTripsWithoutDetailArrays() {
        val summary = JsonCodec.decodeFromString<Session>("""{"session_id":"worker","waiting":true,"background_tasks_count":2,"running_subagents":3,"running_workflows":1,"workflow_progress":{"task_id":"flow","status":"running","running":true,"phase":"Adversarial review","agents_done":5,"agents_started":6,"agents_running":1}}""")
        assertEquals(2, summary.backgroundTasksCount)
        assertEquals("Adversarial review", summary.workflowProgress?.phase)
        assertEquals(5, summary.workflowProgress?.agentsDone)
        assertNull(summary.workflows); assertNull(summary.background); assertNull(summary.subagents)
        assertEquals(summary, JsonCodec.decodeFromString<Session>(JsonCodec.encodeToString(summary)))
        val ended = JsonCodec.decodeFromString<Session>("""{"session_id":"worker","waiting":false,"background_tasks_count":0}""")
        assertEquals(0, ended.backgroundTasksCount); assertNull(ended.workflowProgress)
        assertNull(JsonCodec.decodeFromString<Session>("""{"session_id":"legacy"}""").backgroundTasksCount)
    }

    @Test fun listInvalidationDecodesAndEncodesWithoutBecomingABaseline() {
        val event = ServerEvent.decode("""{"type":"sessions_list","revision":"checkpoint","event_seq":42}""")
        assertEquals(SessionsListEvent("checkpoint"), event)
        val encoded = JsonCodec.encodeToString<ServerEvent>(SessionsListEvent("checkpoint"))
        assertEquals(event, ServerEvent.decode(encoded))
        assertNull(ServerEvent.decode("""{"type":"sessions_list"}"""))
        assertNull(ServerEvent.decode("""{"type":"sessions_list","revision":""}"""))
        assertNull(ServerEvent.decode("""{"type":"sessions_list","revision":42}"""))
    }

    @Test fun summariesUseScalarsAndLegacyRowsStillCountTheirDetailedArrays() {
        val summary = JsonCodec.decodeFromString<Session>("""{"session_id":"s","waiting":true,"background_count":7,"running_subagents":2,"running_workflows":3,"background_working":true}""")
        assertEquals(true, summary.waiting)
        assertEquals(7, summary.backgroundCount)
        assertEquals(2, summary.runningSubagents)
        assertEquals(3, summary.runningWorkflowsCount)
        assertEquals(true, summary.backgroundWorking)
        assertNull(summary.background)
        assertNull(summary.subagents)
        assertNull(summary.workflows)
        assertEquals(summary, JsonCodec.decodeFromString<Session>(JsonCodec.encodeToString(summary)))
        val legacy = JsonCodec.decodeFromString<Session>("""{"session_id":"s","background":[{"id":"a","kind":"task"},{"id":"b","kind":"workflow"}]}""")
        assertNull(legacy.waiting)
        assertNull(legacy.backgroundCountHint)
        assertNull(legacy.runningSubagents)
        assertNull(legacy.runningWorkflowsCount)
        assertNull(legacy.backgroundWorking)
        assertEquals(2, legacy.backgroundCount)
        assertTrue(legacy.legacyWorkflowRunning)
        assertEquals(0, legacy.copy(backgroundCountHint = 0).backgroundCount)
        val emptySummary = JsonCodec.decodeFromString<Session>("""{"session_id":"s","waiting":false}""")
        assertEquals(false, emptySummary.waiting)
        assertEquals(0, emptySummary.backgroundCount)
    }

    @Test fun omittedPageFieldsHaveDefaultsAndOpaqueRemovalStaysIntact() {
        val page = JsonCodec.decodeFromString<SessionListPage>("""{"revision":"r"}""")
        assertEquals(emptyList<Session>(), page.sessions)
        assertEquals(emptyList<String>(), page.remove)
        assertFalse(page.hasMore)
        assertNull(page.nextCursor)
        assertEquals(0, page.total)
        assertEquals(SessionListCounts(), JsonCodec.decodeFromString<SessionListCounts>("{}"))
        assertEquals(SessionListFilter(), SessionListFilter(query = ""))
    }

    @Test fun existingChatTransportFakesDefaultToUnsupported() {
        val legacy = object : ChatTransport {
            override suspend fun machines() = emptyList<Machine>()
            override suspend fun sessions() = emptyList<Session>()
            override suspend fun items(session: SessionKey, query: ItemsQuery) = ItemsResponse()
            override suspend fun interactions() = emptyList<Interaction>()
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
            override suspend fun image(session: SessionKey, itemId: String) = byteArrayOf()
            override suspend fun fullItem(session: SessionKey, itemId: String): Item = error("unused")
        }
        assertThrows(SessionListsUnsupported::class.java) { runBlocking { legacy.sessionPage() } }
        assertThrows(SessionListsUnsupported::class.java) { runBlocking { legacy.sessionCounts() } }
    }
}

package persona.android.di

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import persona.core.contracts.SessionKey
import persona.core.contracts.StandingAuthorization
import persona.core.contracts.StandingAuthorizationList
import persona.core.contracts.api.AssistantManagement
import persona.core.contracts.api.AssistantNotes
import persona.core.contracts.api.MemoryFile
import persona.core.contracts.api.Run
import persona.core.contracts.api.ScheduledTask
import persona.core.contracts.api.StandingAuthorizationsSource
import persona.core.contracts.api.forManager

/** The settings source itself, not only [AssistantManagement.inGroup]. */
class SelectedGroupStandingAuthorizationsTest {
    private class HubApi(private val label: String, private val calls: MutableList<String>) : AssistantManagement {
        override suspend fun warm() = error("unused")
        override suspend fun endConversation(session: SessionKey) = error("unused")
        override suspend fun tasks(): List<ScheduledTask> = error("unused")
        override suspend fun saveTask(task: ScheduledTask): ScheduledTask = error("unused")
        override suspend fun deleteTask(id: String) = error("unused")
        override suspend fun runTask(id: String): SessionKey = error("unused")
        override suspend fun freshTask(id: String): ScheduledTask = error("unused")
        override suspend fun runs(): List<Run> = error("unused")
        override suspend fun notes(): AssistantNotes = error("unused")
        override suspend fun setNotes(text: String) = error("unused")
        override suspend fun memory(): List<MemoryFile> = error("unused")
        override suspend fun saveMemory(name: String, content: String) = error("unused")
        override suspend fun deleteMemory(name: String) = error("unused")
        override suspend fun standingAuthorizations(): StandingAuthorizationList {
            calls += "$label list"
            return StandingAuthorizationList(1, listOf(StandingAuthorization(id = "sa", machineId = "mac-mini")))
        }
        override suspend fun revokeStandingAuthorization(id: String) {
            calls += "$label revoke $id"
        }
    }

    @Test fun translatesTheMachineAndRevokesOnTheHubThatListedTheRow() = runBlocking {
        val calls = mutableListOf<String>()
        var hub = StandingHub("hub-a", GroupNames("g0a1b2c3d"), HubApi("A", calls))
        val source = SelectedGroupStandingAuthorizations({ hub }, MutableStateFlow("hub-a"))
        val pinned = source.forManager("work").pinned()
        val list = pinned.list()
        assertEquals("mac-mini~g0a1b2c3d", list.authorizations.single().machineId)
        hub = StandingHub("hub-b", GroupNames("g0b0b0b0b"), HubApi("B", calls))
        pinned.revoke("sa")
        val later = source.pinned()
        assertEquals("mac-mini~g0b0b0b0b", later.list().authorizations.single().machineId)
        assertEquals(listOf("A list", "A revoke sa", "B list"), calls)
    }

    @Test fun thePrimaryGroupKeepsTheHubsOwnId() = runBlocking {
        val calls = mutableListOf<String>()
        val source = SelectedGroupStandingAuthorizations(
            { StandingHub("hub-a", GroupNames(null), HubApi("A", calls)) },
            MutableStateFlow("hub-a"),
        )
        assertEquals("mac-mini", source.pinned().list().authorizations.single().machineId)
    }

    @Test fun noSelectedStackPinsAnOfflineSource() = runBlocking {
        val source = SelectedGroupStandingAuthorizations({ null }, MutableStateFlow(""))
        val pinned = source.pinned()
        assertEquals("", pinned.hubIdentity.value)
        val listed = runCatching { pinned.list() }.exceptionOrNull()
        val revoked = runCatching { pinned.revoke("sa") }.exceptionOrNull()
        val direct = runCatching { source.list() }.exceptionOrNull()
        assertTrue(listed is HubFailure && listed.kind == HubError.NoNetwork)
        assertTrue(revoked is HubFailure && revoked.kind == HubError.NoNetwork)
        assertTrue(direct is HubFailure && direct.kind == HubError.NoNetwork)
    }
}

package persona.feature.agents

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.*
import persona.core.contracts.SessionKey
import persona.core.contracts.api.HubPreferences
import persona.core.contracts.api.HubPreferencesSnapshot
import persona.core.domain.sort.AgentGrouping
import persona.core.domain.sort.AgentListDisplay
import persona.core.domain.sort.AgentRowStyle
import persona.core.domain.sort.AgentSort
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class AgentsPreferencesTest {
    @Test fun storedLegacyValuesReadAsTheNewDefaultAndStateRemainsSelectable() = runTest {
        val store = object : DataStore<Preferences> {
            override val data = MutableStateFlow<Preferences>(emptyPreferences())
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                transform(data.value).also { data.value = it }
        }
        val hub = object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }
        val preferences = AgentsPreferences(store, hub)
        assertEquals("a phone that never chose a grouping gets 按仓库", AgentGrouping.Repo, preferences.snapshots.first().grouping)
        for (value in listOf("Space", "removed-value")) {
            store.data.value = preferencesOf(stringPreferencesKey("grouping") to value)
            assertEquals(AgentGrouping.MachineSpace, preferences.snapshots.first().grouping)
        }
        store.data.value = preferencesOf(stringPreferencesKey("grouping") to "Machine")
        assertEquals(AgentGrouping.Machine, preferences.snapshots.first().grouping)
        preferences.grouping(AgentGrouping.State)
        assertEquals(AgentGrouping.State, preferences.snapshots.first().grouping)
        preferences.grouping(AgentGrouping.MachineSpace)
        assertEquals("MachineSpace", store.data.value[stringPreferencesKey("grouping")])
        assertEquals(AgentGrouping.MachineSpace, preferences.snapshots.first().grouping)
        // "Status first" until "Recently updated" is chosen (iOS dev.sort); botmux shown until hidden (#2786)
        assertEquals(AgentSort.State, preferences.snapshots.first().sort)
        assertEquals(false, preferences.snapshots.first().hideBotmux)
        preferences.sort(AgentSort.Updated); preferences.hideBotmux(true)
        assertEquals(AgentSort.Updated, preferences.snapshots.first().sort)
        assertEquals(true, preferences.snapshots.first().hideBotmux)
        store.data.value = preferencesOf(stringPreferencesKey("sort") to "removed-value")
        assertEquals(AgentSort.State, preferences.snapshots.first().sort)
    }

    /**
     * The row styles (#4338 A9) are kept in the two values iOS keeps them in: the `compact` the phone always kept, and the
     * style. No one's choice is lost: Compact stays Compact List, Detailed reads as Avatar (the row Glass's Detailed list
     * drew), and Compact List keeps the style chosen before it.
     */
    @Test fun theDisplayKeepsTheCompactChoiceAndTheStyleUnderIt() = runTest {
        val store = object : DataStore<Preferences> {
            override val data = MutableStateFlow<Preferences>(emptyPreferences())
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                transform(data.value).also { data.value = it }
        }
        val hub = object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }
        val preferences = AgentsPreferences(store, hub)
        suspend fun shown() = preferences.snapshots.first().display.also { assertEquals(it, preferences.display.first()) }
        // A phone that never chose: Avatar.
        assertEquals(AgentListDisplay.Avatar, shown())
        // Chosen before the styles came: Compact is Compact List, Detailed is Avatar.
        store.data.value = preferencesOf(booleanPreferencesKey("compact") to true)
        assertEquals(AgentListDisplay.CompactList, shown())
        store.data.value = preferencesOf(booleanPreferencesKey("compact") to false)
        assertEquals(AgentListDisplay.Avatar, shown())
        // A style, then Compact List over it: the style stays and comes back when the list is not compact.
        preferences.setDisplay(AgentListDisplay.Cards)
        assertEquals(AgentListDisplay.Cards, shown())
        assertEquals(false, store.data.value[booleanPreferencesKey("compact")])
        assertEquals("Cards", store.data.value[stringPreferencesKey("row-style")])
        preferences.setDisplay(AgentListDisplay.CompactList)
        assertEquals(AgentListDisplay.CompactList, shown())
        assertEquals(AgentRowStyle.Cards, preferences.snapshots.first().rowStyle)
        // Brief and Board's Compact / Detailed switch the same value.
        preferences.compact(false)
        assertEquals(AgentListDisplay.Cards, shown())
        preferences.setDisplay(AgentListDisplay.TwoLines)
        assertEquals(AgentListDisplay.TwoLines, shown())
        preferences.compact(true)
        assertEquals(AgentListDisplay.CompactList, shown())
        // A style this version doesn't know reads as Avatar.
        store.data.value = preferencesOf(stringPreferencesKey("row-style") to "removed-value")
        assertEquals(AgentListDisplay.Avatar, shown())
    }
}

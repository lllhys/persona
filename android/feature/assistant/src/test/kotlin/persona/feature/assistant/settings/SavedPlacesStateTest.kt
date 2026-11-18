package persona.feature.assistant.settings

import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.yield
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.AssistantConfig
import persona.core.contracts.api.AssistantConfigUpdate
import persona.core.contracts.api.MemoryFile
import persona.core.contracts.api.SavedPlace
import java.util.Locale

/** Saved Places page model (#9293): load, add, edit, delete, error kept, no double save. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class SavedPlacesStateTest {
    private class FakeApi : AssistantSettingsApi {
        var config = AssistantConfig(machineId = "server")
        var places = listOf(
            SavedPlace("p1", "Home", "1 Main St", 31.23, 121.47, 150),
            SavedPlace("p2", "Office", "2 Work Rd", 1.0, 2.0, 300),
        )
        val saved = mutableListOf<SavedPlace>()
        val deleted = mutableListOf<String>()
        var failLoad = false
        var failSave = false
        var failDelete = false
        var saveDelayCalls = 0
        var saveError: String? = null
        var holdSave: CompletableDeferred<Unit>? = null

        override suspend fun config() = config
        override suspend fun updateConfig(update: AssistantConfigUpdate) = config
        override suspend fun memory() = emptyList<MemoryFile>()
        override suspend fun saveMemory(name: String, content: String) {}
        override suspend fun deleteMemory(name: String) {}
        override suspend fun places(): List<SavedPlace> {
            check(!failLoad) { "places offline" }
            return places
        }
        override suspend fun savePlace(place: SavedPlace): SavedPlace {
            holdSave?.await()
            saveError?.let { throw Exception(it) }
            check(!failSave) { "save refused" }
            saveDelayCalls++
            val out = if (place.id.isBlank()) place.copy(id = "p${places.size + 1}") else place
            places = if (places.any { it.id == out.id }) places.map { if (it.id == out.id) out else it } else places + out
            saved += out
            return out
        }
        override suspend fun deletePlace(id: String) {
            check(!failDelete) { "delete refused" }
            deleted += id
            places = places.filterNot { it.id == id }
        }
    }

    private class Zone : TimezoneChoice {
        override val phoneZone = "Asia/Shanghai"
        override var followPhone = true
    }

    @Test fun loadKeepsErrorWhenEmptyAndStillMarksLoaded() = runTest {
        val api = FakeApi().apply { failLoad = true }
        val state = AssistantSettingsState(api, Zone())
        state.loadPlaces()
        assertTrue(state.state.value.places.failed)
        assertNull(state.state.value.places.entries)
        assertFalse(state.state.value.places.loading)
    }

    @Test fun loadListsPlaces() = runTest {
        val api = FakeApi()
        val state = AssistantSettingsState(api, Zone())
        state.loadPlaces()
        assertEquals(listOf("Home", "Office"), state.state.value.places.entries?.map { it.name })
        assertFalse(state.state.value.places.failed)
    }

    @Test fun addTrimsAndAppends() = runTest {
        val api = FakeApi()
        val state = AssistantSettingsState(api, Zone())
        state.loadPlaces()
        val saved = state.savePlace(SavedPlace(name = "  Cafe  ", address = " 3 St ", latitude = 3.0, longitude = 4.0))
        assertEquals("Cafe", saved?.name)
        assertEquals("3 St", saved?.address)
        assertEquals(3, state.state.value.places.entries?.size)
        assertEquals("Cafe", api.saved.single().name)
    }

    @Test fun editReplacesTheRow() = runTest {
        val api = FakeApi()
        val state = AssistantSettingsState(api, Zone())
        state.loadPlaces()
        val updated = state.savePlace(SavedPlace("p1", "Home", "New addr", 31.23, 121.47, 500))
        assertEquals(500, updated?.radiusMeters)
        assertEquals(500, state.state.value.places.entries?.first { it.id == "p1" }?.radiusMeters)
        assertEquals(2, state.state.value.places.entries?.size)
    }

    @Test fun saveFailureKeepsListAndReports() = runTest {
        val api = FakeApi().apply { failSave = true }
        val state = AssistantSettingsState(api, Zone())
        state.loadPlaces()
        assertNull(state.savePlace(SavedPlace(name = "X", latitude = 1.0, longitude = 2.0)))
        assertEquals(2, state.state.value.places.entries?.size)
        assertTrue(state.state.value.notice?.error == true)
    }

    @Test fun saveShowsHubMessageOn409() = runTest {
        val reason = "There is already a place called \"Home\"."
        val api = FakeApi().apply { saveError = reason }
        val state = AssistantSettingsState(api, Zone())
        state.loadPlaces()
        assertNull(state.savePlace(SavedPlace(name = "Home", latitude = 1.0, longitude = 2.0)))
        assertTrue(state.state.value.notice?.error == true)
        assertEquals(SettingsText.plain(reason), state.state.value.notice?.text)
    }

    @Test fun noDoubleSaveWhileBusy() = runTest {
        val api = FakeApi()
        val state = AssistantSettingsState(api, Zone())
        state.loadPlaces()
        assertFalse(state.state.value.busy)
        state.savePlace(SavedPlace(name = "A", latitude = 1.0, longitude = 2.0))
        assertEquals(1, api.saveDelayCalls)
    }

    @Test fun deleteOptimisticAndRollsBackOnFailure() = runTest {
        val api = FakeApi()
        val state = AssistantSettingsState(api, Zone())
        state.loadPlaces()
        assertTrue(state.deletePlace(SavedPlace("p1", "Home", "1 Main St", 31.23, 121.47)))
        assertEquals(listOf("Office"), state.state.value.places.entries?.map { it.name })
        assertEquals(listOf("p1"), api.deleted)

        api.failDelete = true
        api.places = listOf(SavedPlace("p2", "Office", "2 Work Rd", 1.0, 2.0, 300))
        state.loadPlaces()
        assertFalse(state.deletePlace(SavedPlace("p2", "Office", "2 Work Rd", 1.0, 2.0, 300)))
        assertEquals(listOf("Office"), state.state.value.places.entries?.map { it.name })
    }

    @Test fun deleteWhileBusyLeavesListAlone() = runTest {
        val gate = CompletableDeferred<Unit>()
        val api = FakeApi().apply { holdSave = gate }
        val state = AssistantSettingsState(api, Zone())
        state.loadPlaces()
        val saving = async { state.savePlace(SavedPlace(name = "A", latitude = 1.0, longitude = 2.0)) }
        while (!state.state.value.busy) yield()
        assertFalse(state.deletePlace(SavedPlace("p1", "Home", "1 Main St", 31.23, 121.47)))
        assertEquals(listOf("Home", "Office"), state.state.value.places.entries?.map { it.name })
        assertTrue(api.deleted.isEmpty())
        gate.complete(Unit)
        saving.await()
    }

    @Test fun canSaveAndRadiiMatchIos() {
        assertFalse(SavedPlaceText.canSave(SavedPlace(name = "  "), located = true))
        assertFalse(SavedPlaceText.canSave(SavedPlace(name = "Home"), located = false))
        assertTrue(SavedPlaceText.canSave(SavedPlace(name = "Home"), located = true))
        assertEquals(listOf(100, 150, 300, 500, 1000, 2000), SavedPlaceText.radii)
        assertEquals(listOf(100, 150, 250, 300, 500, 1000, 2000), SavedPlaceText.radiiWith(250))
        assertEquals("150 m", SavedPlaceText.radius(150, Locale.US))
        assertEquals("1 km", SavedPlaceText.radius(1000, Locale.US))
        assertTrue(
            "zh radius should use a localized meter unit, got ${SavedPlaceText.radius(150, Locale.SIMPLIFIED_CHINESE)}",
            SavedPlaceText.radius(150, Locale.SIMPLIFIED_CHINESE).contains("150") &&
                !SavedPlaceText.radius(150, Locale.SIMPLIFIED_CHINESE).endsWith(" m"),
        )
        assertEquals(
            "1 Main St",
            SavedPlaceText.detail(SavedPlace(address = "1 Main St", latitude = 1.0, longitude = 2.0)),
        )
        assertEquals(
            "1.00000, 2.00000",
            SavedPlaceText.detail(SavedPlace(latitude = 1.0, longitude = 2.0)),
        )
    }

    @Test fun sameEditIgnoresTimestamps() {
        val base = SavedPlace("p1", "Home", "1 Main St", 31.23, 121.47, 150)
        val withTimes = base.copy(createdAt = "2026-01-01T00:00:00Z", updatedAt = "2026-01-02T00:00:00Z")
        assertTrue(SavedPlaceText.sameEdit(base, withTimes))
        assertFalse(SavedPlaceText.sameEdit(base, base.copy(name = "Office")))
        assertFalse(SavedPlaceText.sameEdit(base, base.copy(radiusMeters = 300)))
        assertFalse(SavedPlaceText.sameEdit(base, base.copy(latitude = 0.0)))
    }

    @Test fun editorReadyNeedsLoadedPlace() {
        assertTrue(SavedPlaceText.editorReady(null, null))
        assertTrue(SavedPlaceText.editorReady(null, emptyList()))
        assertFalse(SavedPlaceText.editorReady("p1", null))
        assertFalse(SavedPlaceText.editorReady("p1", emptyList()))
        assertTrue(SavedPlaceText.editorReady("p1", listOf(SavedPlace("p1", "Home"))))
        assertFalse(SavedPlaceText.editorReady("missing", listOf(SavedPlace("p1", "Home"))))
    }

    @Test fun saveWithPlaceIdUpdatesInsteadOfPostingDuplicate() = runTest {
        val api = FakeApi()
        val state = AssistantSettingsState(api, Zone())
        state.loadPlaces()
        // After process death the list may not be loaded yet in the UI, but the draft still carries placeId.
        val updated = state.savePlace(
            SavedPlace("p1", "Home", "New addr", 31.23, 121.47, 150, createdAt = null, updatedAt = null),
        )
        assertEquals("p1", updated?.id)
        assertEquals(1, api.saved.size)
        assertEquals("p1", api.saved.single().id)
        assertEquals(2, state.state.value.places.entries?.size)
    }
}

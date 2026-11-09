package persona.feature.settings

import persona.core.contracts.api.*
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import org.junit.Assert.*
import org.junit.Test

class DeviceSettingsStateTest {
    @Test fun liveUpdatesDefaultOnCanBeSavedAndReloadedWithoutChangingPrivacy() = runBlocking {
        var saved = DeviceNotificationPreferences()
        var failSave = false
        val api = object : DeviceSettings {
            override suspend fun devices() = PairedDevices(emptyList(), null)
            override suspend fun preferences() = saved
            override suspend fun savePreferences(preferences: DeviceNotificationPreferences): DeviceNotificationPreferences {
                if (failSave) error("Save failed")
                saved = preferences
                return saved
            }
            override suspend fun revoke(credentialId: String) = Unit
        }
        val controller = DeviceSettingsState(api) {}
        controller.loadPreferences()
        assertTrue(controller.state.value.preferences!!.liveActivity)
        assertFalse(controller.state.value.preferences!!.lockDetails)
        controller.savePreferences(saved.copy(liveActivity = false))
        val reopened = DeviceSettingsState(api) {}
        reopened.loadPreferences()
        assertFalse(reopened.state.value.preferences!!.liveActivity)
        assertFalse(reopened.state.value.preferences!!.lockDetails)
        failSave = true
        reopened.savePreferences(saved.copy(liveActivity = true))
        assertFalse(reopened.state.value.preferences!!.liveActivity)
        assertNotNull(reopened.state.value.message)
        failSave = false
        reopened.savePreferences(saved.copy(liveActivity = true))
        assertTrue(saved.liveActivity)
        assertFalse(saved.lockDetails)
    }

    @Test fun failedSaveRollsBackAndCurrentRevocationRetriesOnlyLocalCleanup() = runBlocking {
        val current = PairedDevice("current", "示例手机", "phone", "full")
        val write = CompletableDeferred<Unit>()
        var writes = 0
        var revocations = 0
        var cleanupAttempts = 0
        val original = DeviceNotificationPreferences(done = DoneNotification.Always, longTurnSeconds = 120)
        val api = object : DeviceSettings {
            override suspend fun devices() = PairedDevices(listOf(current), current.id)
            override suspend fun preferences() = original
            override suspend fun savePreferences(preferences: DeviceNotificationPreferences): DeviceNotificationPreferences {
                writes++
                write.await()
                error("Save failed")
            }
            override suspend fun revoke(credentialId: String) { assertEquals(current.id, credentialId); revocations++ }
        }
        val controller = DeviceSettingsState(api) {
            cleanupAttempts++
            if (cleanupAttempts == 1) error("Cleanup failed")
        }
        controller.loadPreferences()
        val changed = original.copy(input = false)
        val pending = launch { controller.savePreferences(changed) }
        yield()
        assertEquals(changed, controller.state.value.preferences)
        assertTrue(controller.state.value.busy)
        controller.savePreferences(original.copy(done = DoneNotification.Off))
        assertEquals(1, writes)
        write.complete(Unit)
        pending.join()
        assertEquals(original, controller.state.value.preferences)
        assertFalse(controller.state.value.busy)
        assertNotNull(controller.state.value.message)
        controller.loadDevices()
        controller.revoke(current)
        assertTrue(controller.state.value.needsCleanup)
        assertTrue(controller.state.value.devices!!.devices.isEmpty())
        controller.disconnect()
        assertEquals(1, revocations)
        assertEquals(2, cleanupAttempts)
        assertFalse(controller.state.value.needsCleanup)
        assertNull(controller.state.value.preferences)
    }

    // Quiet hours (#1863): only the window goes, in this phone's zone; a failed save puts the old one back.
    @Test fun quietHoursSaveAloneInThisPhonesZoneAndRollBack() = runBlocking {
        val sent = mutableListOf<QuietHours>()
        var fail = false
        val api = object : DeviceSettings {
            override suspend fun devices() = PairedDevices(emptyList(), null)
            override suspend fun preferences() = DeviceNotificationPreferences(quiet = QuietHours(tz = "Asia/Tokyo"))
            override suspend fun savePreferences(preferences: DeviceNotificationPreferences) = error("Not the whole prefs")
            override suspend fun revoke(credentialId: String) = Unit
            override suspend fun saveQuietHours(quiet: QuietHours): QuietHours {
                if (fail) error("Save failed")
                sent += quiet
                return quiet
            }
        }
        val controller = DeviceSettingsState(api) {}
        controller.loadPreferences()
        controller.saveQuietHours(QuietHours(enabled = true, tz = "Asia/Tokyo"), zone = "Europe/Berlin")
        assertEquals(listOf(QuietHours(true, "23:00", "07:00", "Europe/Berlin")), sent)
        assertEquals(sent.last(), controller.state.value.preferences!!.quiet)
        fail = true
        controller.saveQuietHours(sent.last().copy(start = "22:00"), zone = "Europe/Berlin")
        assertEquals("23:00", controller.state.value.preferences!!.quiet!!.start)
        assertNotNull(controller.state.value.message)
        // A hub without quiet hours has no section, and nothing is sent.
        val older = DeviceSettingsState(object : DeviceSettings by api {
            override suspend fun preferences() = DeviceNotificationPreferences()
        }) {}
        older.loadPreferences()
        fail = false
        older.saveQuietHours(QuietHours(enabled = true))
        assertEquals(1, sent.size)
    }
}

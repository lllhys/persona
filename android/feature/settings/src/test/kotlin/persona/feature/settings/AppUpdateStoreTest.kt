package persona.feature.settings

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.AppUpdate
import persona.core.contracts.api.AppUpdates
import java.io.ByteArrayOutputStream
import java.io.OutputStream

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class AppUpdateStoreTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val release = AppUpdate(2_000_000_000, "new", "/apk", "a".repeat(64), "notes")

    @Test fun metadataThrottleAndDismissalSurviveRecreationAndAreIsolatedByHub() = runTest {
        var calls = 0
        val api = object : AppUpdates { override suspend fun check(): AppUpdate { calls++; return release } }
        val first = persistentAppUpdateState(context, api, "hub-a")
        first.check()
        val restored = persistentAppUpdateState(context, api, "hub-a")
        assertEquals(release, restored.state.value.latest)
        assertTrue(restored.state.value.notice)
        assertEquals(AppUpdateState.Result.Available, restored.state.value.result)
        assertNotNull(restored.state.value.lastChecked)
        restored.check()
        assertEquals(1, calls)
        restored.dismiss()
        assertFalse(persistentAppUpdateState(context, api, "hub-a").state.value.notice)
        val other = persistentAppUpdateState(context, api, "hub-b")
        assertNull(other.state.value.latest)
        other.check()
        assertEquals(2, calls)
        assertTrue(other.state.value.notice)
    }

    @Test fun switchingHubsRejectsAnOldControllersCheckAndDownload() = runTest {
        val binding = MutableStateFlow<SettingsBinding?>(SettingsBinding("a", null, null))
        var checks = 0
        var downloads = 0
        val blocked = CompletableDeferred<Unit>()
        val api = object : AppUpdates {
            override suspend fun check(): AppUpdate { checks++; blocked.await(); return release }
            override suspend fun download(update: AppUpdate, destination: OutputStream) { downloads++ }
        }
        val store = AppUpdateStore(context, api, binding)
        val old = store.current("a")
        assertSame(old, store.current("a"))
        val inFlight = launch(start = CoroutineStart.UNDISPATCHED) { old.check() }
        binding.value = SettingsBinding("b", null, null)
        assertNotSame(old, store.current("b"))
        blocked.complete(Unit)
        inFlight.join()
        assertNull(old.state.value.latest)
        assertEquals(AppUpdateState.Result.Unknown, old.state.value.result)
        old.check(manual = true)
        assertEquals(1, checks)
        assertThrows(IllegalStateException::class.java) {
            kotlinx.coroutines.runBlocking { old.api.download(release, ByteArrayOutputStream()) }
        }
        assertEquals(0, downloads)
    }
}

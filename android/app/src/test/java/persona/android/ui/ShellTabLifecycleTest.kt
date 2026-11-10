package persona.android.ui

import android.app.Application
import android.content.ComponentName
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.awaitCancellation
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(application = Application::class, sdk = [35])
class ShellTabLifecycleTest {
    @get:Rule val rule = createEmptyComposeRule()

    @Test fun hiddenTabStopsItsWorkerAndRetainsItsCompositionUntilShownAgain() {
        val app = ApplicationProvider.getApplicationContext<Application>()
        Shadows.shadowOf(app.packageManager).addActivityIfNotPresent(ComponentName(app, ComponentActivity::class.java))
        var shown by mutableStateOf(true)
        var identity: Any? = null
        var worker = false
        var starts = 0
        ActivityScenario.launch(ComponentActivity::class.java).use { scenario ->
            scenario.onActivity { activity -> activity.setContent {
                ShellTabLifecycle(shown) {
                    identity = remember { Any() }
                    val owner = LocalLifecycleOwner.current
                    LaunchedEffect(owner) {
                        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
                            starts++
                            worker = true
                            try { awaitCancellation() } finally { worker = false }
                        }
                    }
                }
            } }
            rule.waitUntil { worker }
            val kept = identity
            rule.runOnIdle { shown = false }
            rule.waitForIdle()
            rule.waitUntil { !worker }
            assertSame(kept, identity)
            rule.runOnIdle { shown = true }
            rule.waitForIdle()
            rule.waitUntil { worker && starts == 2 }
            assertSame(kept, identity)
        }
    }
}

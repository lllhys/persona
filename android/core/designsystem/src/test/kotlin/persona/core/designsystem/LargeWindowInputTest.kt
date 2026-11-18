package persona.core.designsystem

import android.app.Application
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(application = Application::class, sdk = [35])
class LargeWindowInputTest {
    @get:Rule val rule = createComposeRule()

    @Test
    fun defaultsOffAndFollowsTheRootWindowValue() {
        var defaultValue = true
        var regularValue = false
        var widePhoneValue = true
        rule.setContent {
            defaultValue = LocalLargeWindowInput.current
            CompositionLocalProvider(LocalLargeWindowInput provides true) {
                regularValue = LocalLargeWindowInput.current
            }
            CompositionLocalProvider(LocalLargeWindowInput provides false) {
                widePhoneValue = LocalLargeWindowInput.current
            }
        }
        rule.runOnIdle {
            assertFalse(defaultValue)
            assertTrue(regularValue)
            assertFalse(widePhoneValue)
        }
    }
}

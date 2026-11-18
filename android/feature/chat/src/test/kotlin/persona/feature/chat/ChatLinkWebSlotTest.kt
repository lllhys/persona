package persona.feature.chat

import android.content.Context
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertSame
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/** The card's page slot leaving and re-entering composition must not take the page with it. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ChatLinkWebSlotTest {
    @get:Rule val rule = createComposeRule()

    private val link = "https://example.invalid/articles/original"
    private val context: Context get() = ApplicationProvider.getApplicationContext()
    private val pool = ChatWebViewPool(context, NoWebFeatures) { RecordingWebView(it) }

    @After fun close() = pool.destroy()

    @Test fun theHeldViewIsStillTheLiveOneWhenTheSlotComesBack() {
        val view = pool.acquire(link) as RecordingWebView
        val shown = mutableStateOf(true)
        rule.setContent { if (shown.value) ChatLinkWebSlot(view) }
        rule.runOnIdle { assertNotNull(view.parent) }

        rule.runOnIdle { shown.value = false }
        rule.waitForIdle()
        // The slot going away only detached the view: the pool still holds it and nothing destroyed it.
        rule.runOnIdle {
            assertEquals(0, view.destroyCalls)
            assertSame(view, pool.existing(link))
        }

        rule.runOnIdle { shown.value = true }
        rule.waitForIdle()
        rule.runOnIdle {
            assertNotNull(view.parent)
            assertEquals(0, view.destroyCalls)
            assertSame(view, pool.existing(link))
            assertEquals(listOf(link), view.loads)
        }
    }
}

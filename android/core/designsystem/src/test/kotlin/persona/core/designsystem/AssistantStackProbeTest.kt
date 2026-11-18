package persona.core.designsystem

import android.app.Application
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(application = Application::class, sdk = [35])
class AssistantStackProbeTest {
    @get:Rule val rule = createComposeRule()

    private class Backend : AssistantStackProbeBackend {
        var route: String? = null
        var selectedKey: String? = null
        var draft: String? = null
        var focused = false
        var touches = 0
        override fun publishNavigation(route: String, selectedKey: String?) {
            this.route = route
            this.selectedKey = selectedKey
        }
        override fun publishDraft(page: String, draft: String, focused: Boolean) {
            this.draft = draft
            this.focused = focused
        }
        override fun removeDraft(page: String) { draft = null }
        override fun touch() { touches++ }
    }

    @Test
    fun publishesNavigationAndDraftOnlyWhileInstalled() {
        val backend = Backend()
        AssistantStackProbe.install(backend)
        try {
            val showDraft = mutableStateOf(true)
            rule.setContent {
                AssistantStackNavigationProbe("inbox/mail", "mail-item")
                if (showDraft.value) AssistantStackDraftProbe("Mail", "reply", true)
            }
            rule.runOnIdle {
                assertTrue(AssistantStackProbe.enabled)
                assertEquals("inbox/mail", backend.route)
                assertEquals("mail-item", backend.selectedKey)
                assertEquals("reply", backend.draft)
                assertTrue(backend.focused)
                showDraft.value = false
            }
            rule.runOnIdle { assertNull(backend.draft) }
        } finally {
            AssistantStackProbe.uninstall(backend)
        }
    }
}

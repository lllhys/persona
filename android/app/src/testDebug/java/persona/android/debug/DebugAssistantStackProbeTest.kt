package persona.android.debug

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(application = Application::class, sdk = [35])
class DebugAssistantStackProbeTest {
    private val app: Application
        get() = ApplicationProvider.getApplicationContext()

    @Before
    @After
    fun cleanUp() {
        DebugAssistantStackProbe.disable()
    }

    @Test
    fun writesRealNavigationAndOnlyHashedDrafts() {
        val plaintext = "reply plaintext"
        DebugAssistantStackProbe.enable(app)
        DebugAssistantStackProbe.publishNavigation("inbox/mail", "mail-item")
        DebugAssistantStackProbe.publishDraft("Mail", plaintext, true)
        val file = File(app.noBackupFilesDir, "hub-data/assistant-stack-probe.json")
        val evidence = file.readText()
        assertTrue(evidence.contains("\"route\":\"inbox/mail\""))
        assertTrue(evidence.contains("\"selectedKey\":\"mail-item\""))
        assertTrue(evidence.contains("\"draftHmac\":"))
        assertTrue(evidence.contains("\"focused\":true"))
        assertFalse(evidence.contains(plaintext))
        DebugAssistantStackProbe.disable()
        assertFalse(file.exists())
    }
}

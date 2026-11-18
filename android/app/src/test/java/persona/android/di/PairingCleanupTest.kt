package persona.android.di

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File

/** Disconnecting clears the local data the dialog promises (#4990). */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = Application::class)
class PairingCleanupTest {
    private val app: Application = ApplicationProvider.getApplicationContext()
    private val opened = listOf("files-audio", "files-share", "shared", "files-pdf", "files-pdf-whole", "files-image", "files-video", "files-office", "files-sheet")

    @Test fun disconnectingRemovesFilesOpenedFromTheHub() {
        opened.forEach { File(app.cacheDir, "$it/opened.bin").apply { parentFile!!.mkdirs() }.writeText("hub file") }
        clearUnversionedPairingData(app, disconnect = true)
        opened.forEach { assertFalse("cache/$it should be gone", File(app.cacheDir, it).exists()) }
    }

    /** The GitHub star prompts are this phone's, not the pairing's (#5761): a re-pairing asks nothing again. */
    @Test fun disconnectingKeepsTheStarPrompts() {
        persona.core.designsystem.StarPromptStore(app).run { launched(); starred() }
        clearUnversionedPairingData(app, disconnect = true)
        val kept = persona.core.designsystem.StarPromptStore(app).state.value
        assertTrue(kept.done)
        assertEquals(1, kept.launches)
    }

    @Test fun startingUpKeepsThem() {
        clearUnversionedPairingData(app)
        opened.forEach { File(app.cacheDir, "$it/opened.bin").apply { parentFile!!.mkdirs() }.writeText("hub file") }
        clearUnversionedPairingData(app)
        opened.forEach { assertTrue(File(app.cacheDir, "$it/opened.bin").exists()) }
    }
}

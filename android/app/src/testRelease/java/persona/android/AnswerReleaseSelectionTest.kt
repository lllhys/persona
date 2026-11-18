package persona.android

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import persona.core.domain.answer.KotlinAnswerAdapter

@RunWith(RobolectricTestRunner::class)
class AnswerReleaseSelectionTest {
    @Test fun releaseAlwaysChoosesKotlinAndHasNoDebugObserver() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        assertTrue(AnswerSelection.choose(context) is KotlinAnswerAdapter)
        for (name in listOf(
            "persona.core.domain.answer.ShadowAnswerAdapter",
            "persona.core.domain.answer.ShadowAnswerDiagnostics",
            "persona.android.debug.DebugAnswer",
            "persona.core.rustcore.RustCoreAnswer",
        )) {
            val present = runCatching { Class.forName(name) }.isSuccess
            assertFalse("Release must not contain $name", present)
        }
    }
}

package persona.android.debug

import android.app.Application
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import persona.android.BuildConfig
import persona.core.data.KotlinContinuityAdapter
import persona.core.data.ShadowContinuityAdapter
import persona.core.data.ShadowContinuityDiagnostics

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [35])
class DebugContinuityTest {
    private val context get() = RuntimeEnvironment.getApplication()
    private fun restartSelection() {
        DebugContinuity::class.java.getDeclaredField("selection").apply { isAccessible = true }.set(null, null)
    }
    @Before fun clear() {
        context.getSharedPreferences("debug-continuity", Application.MODE_PRIVATE).edit().clear().commit()
        restartSelection()
    }
    @After fun reset() {
        clear()
        ShadowContinuityDiagnostics.awaitIdle()
    }

    @Test fun kotlinDefaultIgnoresJvmRustSwitchAndSavedShadowNeedsNextProcess() {
        assertTrue(BuildConfig.DEBUG)
        val old = System.getProperty("persona.continuity")
        try {
            System.setProperty("persona.continuity", "rust")
            assertTrue(DebugContinuity.factory(context)("chat", {}) is KotlinContinuityAdapter)
            DebugContinuity.persist(context, "shadow")
            assertTrue(DebugContinuity.factory(context)("chat", {}) is KotlinContinuityAdapter)
            restartSelection()
            val shadow = DebugContinuity.factory(context)("chat", {})
            assertTrue(shadow is ShadowContinuityAdapter)
            shadow.close() // No accepted input, so no native library is loaded by this test.
            DebugContinuity.persist(context, "kotlin")
            restartSelection()
            assertTrue(DebugContinuity.factory(context)("chat", {}) is KotlinContinuityAdapter)
        } finally {
            if (old == null) System.clearProperty("persona.continuity") else System.setProperty("persona.continuity", old)
        }
    }

    @Test fun shadowCloseLogsOnlyCountsWithoutLoadingNative() {
        DebugContinuity.persist(context, "shadow")
        val shadow = DebugContinuity.factory(context)("secret-chat", {})
        org.robolectric.shadows.ShadowLog.clear()
        shadow.close()
        ShadowContinuityDiagnostics.awaitIdle()
        assertEquals(listOf("compared=0 mismatches=0"),
            org.robolectric.shadows.ShadowLog.getLogsForTag("PersonaCoreShadow").map { it.msg })
    }

    @Test fun rustAndUnknownAppSelectionsAreRejectedBeforeWritingPreferences() {
        for (value in listOf("rust", "unknown")) {
            assertThrows(IllegalArgumentException::class.java) { DebugContinuity.persist(context, value) }
        }
        assertTrue(DebugContinuity.factory(context)("chat", {}) is KotlinContinuityAdapter)
    }
}

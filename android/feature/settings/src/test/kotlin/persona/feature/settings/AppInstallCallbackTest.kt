package persona.feature.settings

import android.app.ActivityOptions
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.os.Bundle
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.util.ReflectionHelpers
import org.robolectric.util.ReflectionHelpers.ClassParameter

@RunWith(RobolectricTestRunner::class)
class AppInstallCallbackTest {
    private val context get() = ApplicationProvider.getApplicationContext<Context>()

    @Test @Config(sdk = [30])
    fun oldPlatformsKeepTheCompatibleCallback() {
        assertNull(shadowOf(installResultCallback(context, 42)).options)
        assertCallbackIdentity()
    }

    @Suppress("DEPRECATION")
    @Test @Config(sdk = [34, 35])
    fun creatorExplicitlyGrantsItsExistingPrivilegesOnLegacyOptInPlatforms() {
        val options = callbackOptions()
        assertEquals(ActivityOptions.MODE_BACKGROUND_ACTIVITY_START_ALLOWED,
            options.pendingIntentCreatorBackgroundActivityStartMode)
        assertCallbackIdentity()
    }

    @Test @Config(sdk = [36])
    fun currentPlatformsOnlyAllowTheCallbackWhileTheCreatorIsVisible() {
        val options = callbackOptions()
        assertEquals(ActivityOptions.MODE_BACKGROUND_ACTIVITY_START_ALLOW_IF_VISIBLE,
            options.pendingIntentCreatorBackgroundActivityStartMode)
        assertCallbackIdentity()
    }

    private fun callbackOptions(): ActivityOptions = ReflectionHelpers.callStaticMethod(
        ActivityOptions::class.java, "fromBundle",
        ClassParameter.from(Bundle::class.java, shadowOf(installResultCallback(context, 42)).options),
    )

    private fun assertCallbackIdentity() {
        val callback = shadowOf(installResultCallback(context, 42))
        assertTrue(callback.isActivityIntent)
        assertEquals(42, callback.requestCode)
        assertEquals(InstallResultActivity::class.java.name, callback.savedIntent.component?.className)
        assertEquals(context.packageName, callback.savedIntent.component?.packageName)
        assertEquals(42, callback.savedIntent.getIntExtra(PackageInstaller.EXTRA_SESSION_ID, -1))
        assertTrue(callback.savedIntent.flags and Intent.FLAG_ACTIVITY_NEW_TASK != 0)
        assertTrue(callback.flags and PendingIntent.FLAG_MUTABLE != 0)
        assertTrue(callback.flags and PendingIntent.FLAG_IMMUTABLE == 0)
    }
}

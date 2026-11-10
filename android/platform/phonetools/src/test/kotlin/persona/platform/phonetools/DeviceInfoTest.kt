package persona.platform.phonetools

import android.content.Context
import android.content.pm.ApplicationInfo
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/** phone_device_info carries the app's version, build and channel under iOS DeviceTool's keys. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class DeviceInfoTest {
    @Test fun reportsTheAppVersion() = runTest {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val result = AndroidPhoneTools(context) { true }.execute("phone_device_info", buildJsonObject {})
        assertFalse(result.isError)
        val text = result.content[0].jsonObject["text"]!!.jsonPrimitive.content
        val info = Json.parseToJsonElement(text).jsonObject
        val app = context.packageManager.getPackageInfo(context.packageName, 0)
        assertEquals(app.versionName.orEmpty(), info["app_version"]!!.jsonPrimitive.content)
        assertEquals(app.longVersionCode.toString(), info["app_build"]!!.jsonPrimitive.content)
        assertTrue(info["app_channel"]!!.jsonPrimitive.content in setOf("debug", "release"))
    }

    @Test fun aDebuggableBuildIsDebug() {
        assertEquals("debug", appChannel(ApplicationInfo.FLAG_DEBUGGABLE or ApplicationInfo.FLAG_HAS_CODE))
        assertEquals("release", appChannel(ApplicationInfo.FLAG_HAS_CODE))
    }
}

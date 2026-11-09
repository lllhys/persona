package persona.android

import android.app.Application
import android.content.ComponentName
import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.SessionKey
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * The share sheet's "Send to" with no recent share and after one (#2395), in each style, light and dark. PNGs go to
 * `build/share-shots/` (or `-Dpersona.shots=<dir>`). The plain Application keeps Hilt's PersonaApp out of the test.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(application = Application::class, sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
class ShareTargetsShotsTest {
    @get:Rule val rule = createEmptyComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/share-shots").also { it.mkdirs() }

    @Test fun recentTargetComesFirstAndIsChosen() {
        val hour = 60 * 60 * 1000L
        val now = 1_000 * hour
        // In the list's own order (needs you, working, done, idle).
        val rows = listOf(
            ShareTargetRow(SessionKey("laptop", "a"), "重构登录流程", "MacBook"),
            ShareTargetRow(SessionKey("devbox", "b"), "排查 /orders 接口变慢", "开发机"),
            ShareTargetRow(SessionKey("laptop", "c"), "天气 App 发 1.3 版", "MacBook"),
            ShareTargetRow(SessionKey("devbox", "d"), "博客迁移到 Astro", "开发机"),
        )
        // Shared to c two hours ago, to the main chat yesterday morning, to d the day before.
        val recents = listOf(RecentShareTarget(rows[2].key, now - 2 * hour), RecentShareTarget(null, now - 30 * hour),
            RecentShareTarget(rows[3].key, now - 50 * hour))
        val after = recentFirst(rows, { it.key }, recents)
        val chosen = preselectedShareTarget(recents, rows.mapTo(HashSet()) { it.key }, now)
        assertEquals(listOf("c", "d", "a", "b"), after.map { it.key.sessionId })
        assertEquals(rows[2].key, chosen)

        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var recent by mutableStateOf(false)
        val app = ApplicationProvider.getApplicationContext<Application>()
        Shadows.shadowOf(app.packageManager).addActivityIfNotPresent(ComponentName(app, ComponentActivity::class.java))
        val scenario = ActivityScenario.launch(ComponentActivity::class.java)
        scenario.onActivity { activity -> activity.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                // The sheet's surface and its side margins.
                Column(Modifier.testTag("shot").width(400.dp).background(MaterialTheme.colorScheme.surfaceContainerLow)
                    .padding(horizontal = 24.dp, vertical = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    ShareTargetList(if (recent) after else rows, if (recent) chosen else null,
                        mainEnabled = true, enabled = true, select = {})
                }
            }
        } }
        for (s in AppStyle.entries) for (d in listOf(false, true)) for (r in listOf(false, true)) {
            style = s; dark = d; recent = r
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "share-${if (r) "recent" else "none"}-${s.name.lowercase()}-${if (d) "dark" else "light"}.png")
                .outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        scenario.close()
    }
}

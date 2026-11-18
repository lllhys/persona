package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.icons.PhIcons
import java.io.File

/**
 * Start, mid-spring and end of the Glass pill (`Theme.Motion.state`, `SwitchMotion`).
 * PNGs in `build/sliding-pill-shots/`. The foldable still uses the horizontal top switch.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-mdpi")
class SlidingPillShotsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun segmentedPhone() {
        rule.mainClock.autoAdvance = false
        var selected by mutableIntStateOf(0)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = dark, reduceMotion = false) {
                Box(Modifier.testTag("shot").background(PersonaTheme.palette.background).padding(24.dp)) {
                    JSegmented(listOf(JSegment("Day"), JSegment("This week")), selected, { selected = it })
                }
            }
        }
        frames(prefix = "segmented-phone") { isDark, at ->
            dark = isDark
            selected = at
        }
    }

    @Test fun homePhone() = homeFrames("home-phone")

    @Config(sdk = [35], qualifiers = "en-w673dp-h951dp-mdpi")
    @Test fun homeFoldableUpright() = homeFrames("home-foldable")

    private fun homeFrames(prefix: String) {
        rule.mainClock.autoAdvance = false
        var selected by mutableStateOf("assistant")
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = dark, reduceMotion = false) {
                Box(Modifier.fillMaxWidth().testTag("shot").background(PersonaTheme.palette.background)) {
                    HomeNavigation(
                        items = listOf(
                            NavItem("assistant", "Assistant", PhIcons.ChatCircle),
                            NavItem("agents", "Agents", PhIcons.Robot),
                        ),
                        selected = selected, onSelect = { selected = it }, leading = {},
                    )
                }
            }
        }
        frames(prefix) { isDark, at ->
            dark = isDark
            selected = if (at == 0) "assistant" else "agents"
        }
    }

    private fun frames(prefix: String, assign: (dark: Boolean, index: Int) -> Unit) {
        val out = File(System.getProperty("persona.shots") ?: "build/sliding-pill-shots").also { it.mkdirs() }
        for (isDark in listOf(false, true)) {
            val mode = if (isDark) "dark" else "light"
            rule.runOnIdle { assign(isDark, 0) }
            Snapshot.sendApplyNotifications()
            rule.mainClock.advanceTimeBy(600)
            capture(out, "$prefix-$mode-start")
            rule.runOnIdle { assign(isDark, 1) }
            Snapshot.sendApplyNotifications()
            rule.mainClock.advanceTimeBy(80)
            capture(out, "$prefix-$mode-mid")
            rule.mainClock.advanceTimeBy(920)
            capture(out, "$prefix-$mode-end")
        }
    }

    private fun capture(out: File, name: String) {
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}

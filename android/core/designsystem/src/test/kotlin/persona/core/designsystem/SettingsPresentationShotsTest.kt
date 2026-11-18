package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w840dp-h900dp-xhdpi")
class SettingsPresentationShotsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun expandedEveryStyleAndAppearance() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            CompositionLocalProvider(LocalLargeWindowInput provides true) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    JSettingsSheet(onDismiss = {}) {
                        JListPage("Settings") {
                            item { JGroupItem(GroupPosition.Single) { JRow("Interface style", onClick = {}) } }
                            item { JGroupItem(GroupPosition.Single) { JRow("Language", onClick = {}) } }
                        }
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s
            dark = d
            rule.waitForIdle()
            rule.onNodeWithTag("settings-page-card").assertExists()
            val name = "settings-page-${s.name.lowercase()}-${if (d) "dark" else "light"}.png"
            save(rule.onNodeWithTag("settings-page-card").captureToImage().asAndroidBitmap(), name)
            save(rule.onNode(isDialog()).captureToImage().asAndroidBitmap(), name.replace("settings-page-", "settings-window-"))
        }
    }

    @Test fun pageSizedWindowCaptureIncludesBackgroundAroundCard() {
        rule.setContent {
            CompositionLocalProvider(LocalLargeWindowInput provides true) {
                PersonaTheme(darkTheme = false, reduceMotion = true) {
                    JSettingsSheet(onDismiss = {}) { JListPage("Settings") {} }
                }
            }
        }
        rule.onNodeWithTag("settings-page-card").assertExists()
        val card = rule.onNodeWithTag("settings-page-card").captureToImage().asAndroidBitmap()
        val window = rule.onNode(isDialog()).captureToImage().asAndroidBitmap()
        assertTrue(window.width > card.width && window.height > card.height)
        save(window, "settings-window-with-background.png")
    }

    @Config(sdk = [35], qualifiers = "en-w600dp-h480dp-xhdpi")
    @Test fun minimumRegularPage() {
        rule.setContent {
            CompositionLocalProvider(LocalLargeWindowInput provides true) {
                PersonaTheme(darkTheme = false, reduceMotion = true) {
                    JSettingsSheet(onDismiss = {}) { JListPage("Settings") {} }
                }
            }
        }
        rule.onNodeWithTag("settings-page-card").assertExists()
        save(rule.onNodeWithTag("settings-page-card").captureToImage().asAndroidBitmap(), "settings-page-regular-600.png")
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
    @Test fun compactFullWindowSheet() {
        rule.setContent {
            CompositionLocalProvider(LocalLargeWindowInput provides false) {
                PersonaTheme(darkTheme = false, reduceMotion = true) {
                    JSettingsSheet(onDismiss = {}) { JListPage("Settings") {} }
                }
            }
        }
        rule.onNodeWithTag("settings-page-card").assertDoesNotExist()
        save(rule.onNode(isDialog()).captureToImage().asAndroidBitmap(), "settings-full-window-compact.png")
    }

    private fun save(bitmap: Bitmap, name: String) {
        val out = File(System.getProperty("persona.shots") ?: "build/settings-presentation-shots").also { it.mkdirs() }
        File(out, name).outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}

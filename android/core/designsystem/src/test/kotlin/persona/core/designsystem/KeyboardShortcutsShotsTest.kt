package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.Density
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w840dp-h900dp-xhdpi")
class KeyboardShortcutsShotsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun english() = shots("en", "Keyboard shortcuts", "Close", "Terminal has priority", large = false)
    @Test fun englishLargestText() = shots("en", "Keyboard shortcuts", "Close", "Terminal has priority", large = true)

    @Config(sdk = [35], qualifiers = "zh-w840dp-h900dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", "键盘快捷键", "关闭", "终端优先接收", large = false)

    @Config(sdk = [35], qualifiers = "zh-w840dp-h900dp-xhdpi")
    @Test fun simplifiedChineseLargestText() = shots("zh", "键盘快捷键", "关闭", "终端优先接收", large = true)

    @Config(sdk = [35], qualifiers = "ja-w840dp-h900dp-xhdpi")
    @Test fun japanese() = shots("ja", "キーボードショートカット", "閉じる", "ターミナルが優先するキー", large = false)

    @Config(sdk = [35], qualifiers = "ja-w840dp-h900dp-xhdpi")
    @Test fun japaneseLargestText() = shots("ja", "キーボードショートカット", "閉じる", "ターミナルが優先するキー", large = true)

    private fun shots(language: String, title: String, close: String, terminal: String, large: Boolean) {
        val fontScale = if (large) 1.6f else 1f
        RuntimeEnvironment.setFontScale(fontScale)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            val density = LocalDensity.current
            CompositionLocalProvider(LocalDensity provides Density(density.density, fontScale)) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    KeyboardShortcutsSheet(onDismiss = {})
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s
            dark = d
            rule.waitForIdle()
            rule.onNodeWithText(title).assertExists()
            rule.onNodeWithText(close).assertExists()
            rule.onNodeWithText(terminal).assertExists()
            val name = "shortcuts-$language-${s.name.lowercase()}-${if (d) "dark" else "light"}-${if (large) "large" else "default"}.png"
            save(rule.onNode(isDialog()).captureToImage().asAndroidBitmap(), name)
        }
    }

    private fun save(bitmap: Bitmap, name: String) {
        val out = File(System.getProperty("persona.shots") ?: "build/hardware-shortcut-shots").also { it.mkdirs() }
        File(out, name).outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}

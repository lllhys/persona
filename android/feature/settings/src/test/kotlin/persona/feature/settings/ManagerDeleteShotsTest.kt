package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * Settings › Managers › Delete says what goes with the manager, as the web does: how many of its computers go back to
 * the hub's own manager (only when it has some) and, in the caution colour, that its sources are deleted with it. Not
 * for one still installing. Three styles × light and dark, written to `build/managers-shots/managers-delete-*.png`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class ManagerDeleteShotsTest {
    @get:Rule val rule = createComposeRule()

    private val machines = "它的 2 台电脑会回到 hub 自己的助手。"
    private val sources = "它的来源（比如邮箱账号）存在它自己的库里，不先移到别的助手，就会随它一起删掉。"

    @Test fun theDeleteConfirmSaysWhatGoesWithIt() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var count by mutableStateOf(2)
        var installing by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                ManagerDeleteDialog("家里", installing = installing, machines = count, busy = false, onDismiss = {}, onConfirm = {})
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText(machines).assertExists()
        rule.onNodeWithText(sources).assertExists()
        val dir = File(System.getProperty("persona.shots") ?: "build/managers-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val image = rule.onNode(isDialog()).captureToImage().asAndroidBitmap()
            File(dir, "managers-delete-${s.key}-${if (d) "dark" else "light"}.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        // No computers of its own: only the sources line.
        style = AppStyle.Glass; dark = false; count = 0
        rule.waitForIdle()
        assertEquals(0, rule.onAllNodesWithText("台电脑", substring = true).fetchSemanticsNodes().size)
        rule.onNodeWithText(sources).assertExists()
        // Still installing: neither, it has nothing yet.
        count = 2; installing = true
        rule.waitForIdle()
        assertEquals(0, rule.onAllNodesWithText(sources).fetchSemanticsNodes().size)
        assertEquals(0, rule.onAllNodesWithText(machines).fetchSemanticsNodes().size)
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
    @Test fun englishSaysOneComputerAndSeveral() {
        var count by mutableStateOf(1)
        rule.setContent { PersonaTheme(reduceMotion = true) { ManagerDeleteDialog("Home", false, count, false, {}, {}) } }
        rule.waitForIdle()
        rule.onNodeWithText("Its 1 computer goes back to the hub's own assistant.").assertExists()
        count = 3
        rule.waitForIdle()
        rule.onNodeWithText("Its 3 computers go back to the hub's own assistant.").assertExists()
    }
}

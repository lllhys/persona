package persona.feature.assistant

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.AppStyle
import persona.core.designsystem.JDialog
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * #6214: the 设为常驻 / 取消常驻 confirmation (JDialog) drawn on the JVM with Robolectric's native graphics (no
 * emulator), in every style, light and dark. Unpin is destructive and says the chat returns to 最近 and its open tasks
 * go back to the main chat; pin is the ordinary primary action. PNGs go to `build/leader-confirm-shots/`
 * (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class LeaderConfirmShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File =
        File(System.getProperty("persona.shots") ?: "build/leader-confirm-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        // The dialog is its own window (Compose AlertDialog): capture the dialog node, not the host box.
        val bitmap = rule.onNode(isDialog()).captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7)
            if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun drawTheLeaderConfirmationsInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var unpin by mutableStateOf(true)
        rule.setContent {
            val resources = LocalContext.current.resources
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(760.dp).background(PersonaTheme.palette.background)) {
                    if (unpin) {
                        JDialog(onDismiss = {},
                            title = resources.getString(R.string.assistant_end_leader_title),
                            text = resources.getString(R.string.assistant_end_leader_text, "增长方向"),
                            confirm = resources.getString(R.string.assistant_end_leader) to {},
                            dismiss = resources.getString(R.string.assistant_cancel) to {})
                    } else {
                        JDialog(onDismiss = {},
                            title = resources.getString(R.string.assistant_make_leader_title),
                            text = resources.getString(R.string.assistant_make_leader_text),
                            confirm = resources.getString(R.string.assistant_make_leader) to {},
                            dismiss = resources.getString(R.string.assistant_cancel) to {})
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            androidx.compose.runtime.snapshots.Snapshot.sendApplyNotifications()
            capture("leader-unpin-confirm-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
        unpin = false
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            androidx.compose.runtime.snapshots.Snapshot.sendApplyNotifications()
            capture("leader-make-confirm-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }
}

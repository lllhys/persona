package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.graphics.toPixelMap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/**
 * Letting go of a swipe-to-reply springs the message back on [Tokens.Motion.SpringBack] (0.3 s, bounce 0.25) in every
 * style. iOS `MessageActionBarHost.ended` (`MessageMenu.swift:470`) always uses that spring and does not read
 * Reduce Motion, so animations off still spring. A mid-spring frame is written to `build/swipe-reply-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class SwipeReplySpringTest {
    @get:Rule val rule = createComposeRule()

    @Test fun theSpringBackTokenIsTheIosSpring() {
        assertEquals(300, Tokens.Motion.SpringBack.duration)
        assertEquals(0.25f, Tokens.Motion.SpringBack.bounce)
        val spring = IosSpring(Tokens.Motion.SpringBack.duration, Tokens.Motion.SpringBack.bounce)
        assertEquals(0.75f, spring.dampingRatio, 0.0001f)
    }

    @Test fun glassSpringsBack() = springs(AppStyle.Glass, "glass-light")
    @Test fun briefSpringsBack() = springs(AppStyle.Brief, "brief-light")
    @Test fun boardSpringsBack() = springs(AppStyle.Board, "board-light")
    @Test fun glassDarkSpringsBack() = springs(AppStyle.Glass, "glass-dark", dark = true)

    @Test fun animationsOffStillSpring() = springs(AppStyle.Glass, "glass-light-reduced", reduce = true)

    /** Shows the row, drags it, and returns the resting right edge of the green bar. */
    private fun show(style: AppStyle, reduce: Boolean = false, dark: Boolean = false): Int {
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = reduce, style = style) {
                Column(Modifier.background(Color.White).padding(8.dp).testTag("shot")) {
                    Box(Modifier.testTag("row").width(280.dp).height(64.dp).swipeToReply("Reply") {}) {
                        Box(Modifier.fillMaxSize().background(Color(0f, 0.78f, 0.32f)))
                    }
                }
            }
        }
        rule.mainClock.advanceTimeBy(100)
        return edge()
    }

    private fun springs(style: AppStyle, name: String, dark: Boolean = false, reduce: Boolean = false) {
        val rest = show(style, reduce = reduce, dark = dark)
        pull()
        rule.mainClock.advanceTimeBy(50)
        val pulled = edge()
        val travel = rest - pulled
        assertTrue("$name travel $travel", travel > 40)
        release()
        rule.mainClock.advanceTimeBy(80)
        val mid = edge()
        val remain = (rest - mid).toFloat() / travel
        capture(name)
        // 80 ms into SpringBack still has about 0.42 of the drag left (a frame of clock slop stays under 0.60).
        // A 240 ms tween would still have about two thirds.
        assertTrue("$name remaining $remain (mid $mid, pulled $pulled, rest $rest)", remain > 0.25f && remain < 0.60f)
        rule.mainClock.advanceTimeBy(700)
        assertEquals(rest.toFloat(), edge().toFloat(), 2f)
    }

    private fun pull() = rule.onNodeWithTag("row").performTouchInput {
        down(center)
        moveBy(androidx.compose.ui.geometry.Offset(-30f, 0f))
        moveBy(androidx.compose.ui.geometry.Offset(-170f, 0f))
    }

    private fun release() = rule.onNodeWithTag("row").performTouchInput { up() }

    /** The rightmost green pixel: the bar's trailing edge after the swipe shifts it. */
    private fun edge(): Int {
        val map = rule.onNodeWithTag("row").captureToImage().toPixelMap()
        var max = -1
        for (y in 0 until map.height step 2) {
            for (x in map.width - 1 downTo 0) {
                val color = map[x, y]
                if (color.green > 0.6f && color.red < 0.3f && color.alpha > 0.7f) {
                    if (x > max) max = x
                    break
                }
            }
        }
        return max
    }

    private fun capture(name: String) {
        val out = File("build/swipe-reply-shots").also { it.mkdirs() }
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out, "swipe-$name-mid.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}

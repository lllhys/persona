package persona.core.designsystem

import android.graphics.Color as AndroidColor
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * `.unfolding` takes its full height on the first frame and only fades (#9574, #3005).
 * With reduced motion it is opaque on that first frame.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
class UnfoldingTest {
    @get:Rule val rule = createComposeRule()

    @Test fun openingIsFullHeightOnTheFirstFrameAndOnlyFades() {
        rule.mainClock.autoAdvance = false
        var open by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, reduceMotion = false) {
                val edge = rememberOpenEdge()
                val revealing = edge.revealing(open)
                Column(Modifier.testTag("box").width(160.dp).background(Color.White)) {
                    if (open) Box(
                        Modifier.testTag("detail").unfolding(revealing).fillMaxWidth().height(100.dp).background(Color.Black),
                    )
                }
            }
        }
        rule.mainClock.advanceTimeByFrame()
        val closed = height("box")
        rule.runOnIdle { open = true }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        val first = height("box")
        assertTrue("opened $first from $closed", first > closed)
        assertTrue(height("detail") > 0)
        val faded = pixel("box")
        rule.mainClock.advanceTimeBy(2_000)
        assertEquals("height must not animate", first, height("box"))
        val solid = pixel("box")
        assertNotEquals("mid fade $faded settled $solid", faded, solid)
        assertEquals(0, AndroidColor.red(solid))
        assertTrue(AndroidColor.alpha(solid) > 250)
    }

    @Test fun whatWasAlreadyOpenDoesNotFade() {
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, reduceMotion = false) {
                val edge = rememberOpenEdge()
                val revealing = edge.revealing(true)
                Column(Modifier.testTag("box").width(80.dp).background(Color.White)) {
                    Box(Modifier.unfolding(revealing).fillMaxWidth().height(40.dp).background(Color.Black))
                }
            }
        }
        rule.mainClock.advanceTimeByFrame()
        val first = pixel("box")
        rule.mainClock.advanceTimeBy(2_000)
        assertEquals(first, pixel("box"))
        assertEquals(0, AndroidColor.red(first))
        assertTrue(AndroidColor.alpha(first) > 250)
    }

    @Test fun closingDropsItsSpaceOnTheNextFrame() {
        rule.mainClock.autoAdvance = false
        var open by mutableStateOf(true)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, reduceMotion = false) {
                val edge = rememberOpenEdge()
                val revealing = edge.revealing(open)
                Column(Modifier.testTag("box").width(120.dp).background(Color.White)) {
                    Text("Header")
                    if (open) Box(Modifier.testTag("detail").unfolding(revealing).fillMaxWidth().height(80.dp).background(Color.Black))
                }
            }
        }
        rule.mainClock.advanceTimeByFrame()
        val openHeight = height("box")
        rule.runOnIdle { open = false }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        assertEquals(0, rule.onAllNodesWithTag("detail").fetchSemanticsNodes().size)
        val closed = height("box")
        assertTrue("$closed < $openHeight", closed < openHeight)
        rule.mainClock.advanceTimeBy(140)
        assertEquals("no mid-height close", closed, height("box"))
    }

    @Test fun reducedMotionIsOpaqueAndFullOnTheFirstFrame() {
        rule.mainClock.autoAdvance = false
        var open by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, reduceMotion = true) {
                val edge = rememberOpenEdge()
                val revealing = edge.revealing(open)
                Column(Modifier.testTag("box").width(120.dp).background(Color.White)) {
                    if (open) Box(Modifier.testTag("detail").unfolding(revealing).fillMaxWidth().height(60.dp).background(Color.Black))
                }
            }
        }
        rule.mainClock.advanceTimeByFrame()
        rule.runOnIdle { open = true }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        val first = height("detail")
        val ink = pixel("detail")
        rule.mainClock.advanceTimeBy(2_000)
        assertEquals(first, height("detail"))
        assertEquals(ink, pixel("detail"))
        assertEquals(0, AndroidColor.red(ink))
        assertTrue(AndroidColor.alpha(ink) > 250)
    }

    private fun height(tag: String): Int = rule.onNodeWithTag(tag).fetchSemanticsNode().size.height

    private fun pixel(tag: String): Int {
        val bitmap = rule.onNodeWithTag(tag).captureToImage().asAndroidBitmap()
        return bitmap.getPixel(bitmap.width / 2, bitmap.height / 2)
    }
}

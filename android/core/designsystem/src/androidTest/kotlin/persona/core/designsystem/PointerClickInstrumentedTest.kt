package persona.core.designsystem

import android.os.SystemClock
import android.view.InputDevice
import android.view.MotionEvent
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/** Real mouse-button injection; adb hover or console events are not acceptance evidence for this path. */
@RunWith(AndroidJUnit4::class)
class PointerClickInstrumentedTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    @Test fun secondaryButtonAnchorsTheMenuAtWindowEdges() {
        var anchor by mutableStateOf<Offset?>(null)
        var targetBounds by mutableStateOf(Rect.Zero)
        var menuScreenBounds by mutableStateOf(Rect.Zero)
        var activityOrigin by mutableStateOf(Offset.Zero)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                Box(Modifier.fillMaxSize().onGloballyPositioned { targetBounds = it.boundsInWindow() }
                    .onSecondaryClickAt { anchor = it }.testTag("target")) {
                    anchor?.let {
                        JPointerMenu(it, { anchor = null }, (1..20).map { index -> JAction("Action $index") {} },
                            Modifier.testTag("pointer-menu").onGloballyPositioned {
                                val origin = it.localToScreen(Offset.Zero)
                                menuScreenBounds = Rect(origin.x, origin.y,
                                    origin.x + it.size.width, origin.y + it.size.height)
                            })
                    }
                }
            }
        }
        rule.runOnUiThread {
            val location = IntArray(2)
            rule.activity.window.decorView.getLocationOnScreen(location)
            activityOrigin = Offset(location[0].toFloat(), location[1].toFloat())
        }
        val margin = 24 * InstrumentationRegistry.getInstrumentation().targetContext.resources.displayMetrics.density
        val points = listOf(
            Offset(targetBounds.right - margin, targetBounds.top + margin),
            Offset(targetBounds.left + margin, targetBounds.bottom - margin),
            Offset(targetBounds.left + margin, targetBounds.top + margin),
            Offset(targetBounds.right - margin, targetBounds.bottom - margin),
        )
        for ((index, point) in points.withIndex()) {
            secondaryClick(point)
            rule.waitForIdle()
            val actual = requireNotNull(anchor)
            assertEquals(point.x, actual.x, 1f)
            assertEquals(point.y, actual.y, 1f)
            val menu = Rect(menuScreenBounds.left - activityOrigin.x, menuScreenBounds.top - activityOrigin.y,
                menuScreenBounds.right - activityOrigin.x, menuScreenBounds.bottom - activityOrigin.y)
            assertTrue(menu.left >= targetBounds.left && menu.top >= targetBounds.top)
            assertTrue(menu.right <= targetBounds.right && menu.bottom <= targetBounds.bottom)
            val anchorX = actual.x
            val anchorY = actual.y
            if (index == 0 || index == 3) assertTrue("right edge flips left", menu.right <= anchorX + 2f)
            if (index == 1 || index == 3) assertTrue("bottom edge flips up", menu.bottom <= anchorY + 2f)
            if (index == 2) {
                assertTrue("top-left grows right: menu=$menu anchor=$actual", menu.left >= anchorX - 2f)
                assertTrue("top-left grows down", menu.top >= anchorY - 2f)
            }
            rule.runOnUiThread { anchor = null }
            rule.waitForIdle()
        }
    }

    private fun secondaryClick(windowPoint: Offset) {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val location = IntArray(2)
        rule.runOnUiThread { rule.activity.window.decorView.getLocationOnScreen(location) }
        val x = location[0] + windowPoint.x
        val y = location[1] + windowPoint.y
        val downTime = SystemClock.uptimeMillis()
        val down = mouseEvent(downTime, downTime, MotionEvent.ACTION_DOWN, x, y, MotionEvent.BUTTON_SECONDARY)
        val up = mouseEvent(downTime, downTime + 10, MotionEvent.ACTION_UP, x, y, 0)
        try {
            instrumentation.sendPointerSync(down)
            instrumentation.sendPointerSync(up)
            instrumentation.waitForIdleSync()
        } finally {
            down.recycle()
            up.recycle()
        }
    }

    private fun mouseEvent(downTime: Long, eventTime: Long, action: Int, x: Float, y: Float, buttons: Int): MotionEvent {
        val properties = MotionEvent.PointerProperties().apply {
            id = 0
            toolType = MotionEvent.TOOL_TYPE_MOUSE
        }
        val coordinates = MotionEvent.PointerCoords().apply {
            this.x = x
            this.y = y
        }
        return MotionEvent.obtain(downTime, eventTime, action, 1, arrayOf(properties), arrayOf(coordinates),
            0, buttons, 1f, 1f, 0, 0, InputDevice.SOURCE_MOUSE, 0)
    }
}

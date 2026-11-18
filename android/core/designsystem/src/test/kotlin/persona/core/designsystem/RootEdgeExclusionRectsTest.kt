package persona.core.designsystem

import android.graphics.Rect
import android.view.View
import android.view.ViewGroup
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.core.graphics.Insets
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * Production `Modifier.systemGestureExclusion` wiring for #9803: reads the Android view's
 * [View.getSystemGestureExclusionRects], not only the pure rect helper.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w411dp-h891dp-mdpi")
class RootEdgeExclusionRectsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun phoneRootPublishesOneLeadingRectAndClearsWhenOpenedOrPushed() {
        var opened by mutableStateOf(false)
        var dragEnabled by mutableStateOf(true)
        lateinit var host: View
        rule.setContent {
            host = LocalView.current
            PersonaTheme(reduceMotion = true, style = AppStyle.Glass) {
                JShellDrawer(
                    opened = opened,
                    onDismiss = { opened = false },
                    modifier = Modifier.testTag("shell").width(360.dp).height(640.dp),
                    dragEnabled = dragEnabled,
                    onOpen = { opened = true },
                    drawer = { Box(Modifier.fillMaxSize().background(Color.Red)) },
                ) { Box(Modifier.fillMaxSize().background(Color.Blue)) }
            }
        }
        val insets = WindowInsetsCompat.Builder()
            .setInsets(WindowInsetsCompat.Type.systemGestures(), Insets.of(48, 0, 48, 0))
            .setInsets(WindowInsetsCompat.Type.mandatorySystemGestures(), Insets.of(48, 0, 48, 0))
            .build()
        rule.runOnIdle { ViewCompat.dispatchApplyWindowInsets(host, insets) }
        rule.waitForIdle()
        val atRoot = leadingRects(host)
        assertEquals("phone root should publish one leading exclusion", 1, atRoot.size)
        assertTrue("leading edge ${atRoot[0]}", atRoot[0].left <= 0 && atRoot[0].right in 1..48)
        assertTrue("band height ${atRoot[0].height()}", atRoot[0].height() <= 200)

        opened = true
        rule.waitForIdle()
        assertTrue("opened drawer clears exclusion", leadingRects(host).isEmpty())

        opened = false
        dragEnabled = false
        rule.waitForIdle()
        assertTrue("pushed page clears exclusion", leadingRects(host).isEmpty())
    }

    @Config(sdk = [35], qualifiers = "en-w951dp-h673dp-mdpi")
    @Test fun foldableColumnPublishesAtRootAndClearsWhenShownOrPushedOrHiddenPane() {
        var columnShown by mutableStateOf(false)
        var detailAtRoot by mutableStateOf(true)
        var paneShown by mutableStateOf(true)
        lateinit var host: View
        rule.setContent {
            host = LocalView.current
            PersonaTheme(reduceMotion = true, style = AppStyle.Glass) {
                Box(Modifier.fillMaxSize().testTag("shell").background(Color.Gray)) {
                    JSidebarColumn(
                        sidebar = { Box(Modifier.fillMaxSize().background(Color.Green)) },
                        width = 320.dp,
                        pageStart = 320.dp,
                        shown = columnShown,
                        overlay = false,
                        drags = true,
                        detailAtRoot = detailAtRoot,
                        paneShown = paneShown,
                    ) { Box(Modifier.fillMaxSize().background(Color.Blue)) }
                }
            }
        }
        rule.waitForIdle()
        val zone = with(rule.density) { (Tokens.MinTouch.ios / 2).roundToPx() }
        val atRoot = leadingRects(host)
        assertEquals("column at root", 1, atRoot.size)
        assertTrue("open zone width ${atRoot[0].width()} zone=$zone", atRoot[0].width() >= zone)
        assertTrue(atRoot[0].height() <= 200)

        columnShown = true
        rule.waitForIdle()
        assertTrue("column shown clears", leadingRects(host).isEmpty())

        columnShown = false
        detailAtRoot = false
        rule.waitForIdle()
        assertTrue("pushed detail clears", leadingRects(host).isEmpty())

        detailAtRoot = true
        paneShown = false
        rule.waitForIdle()
        assertTrue("unselected tab clears", leadingRects(host).isEmpty())
    }

    @Config(sdk = [35], qualifiers = "en-w951dp-h673dp-mdpi")
    @Test fun cutoutExtendsExclusionToWindowZero() {
        lateinit var host: View
        var cutoutPx = 0
        rule.setContent {
            host = LocalView.current
            cutoutPx = with(LocalDensity.current) { 48.dp.roundToPx() }
            PersonaTheme(reduceMotion = true, style = AppStyle.Glass) {
                // Same horizontal pad as Root.kt's safeDrawing sides around the column.
                Box(Modifier.fillMaxSize().padding(start = 48.dp).testTag("shell")) {
                    JSidebarColumn(
                        sidebar = { Box(Modifier.fillMaxSize()) },
                        width = 320.dp,
                        pageStart = 320.dp,
                        shown = false,
                        drags = true,
                        detailAtRoot = true,
                        paneShown = true,
                    ) { Box(Modifier.fillMaxSize().background(Color.Blue)) }
                }
            }
        }
        rule.waitForIdle()
        val rects = leadingRects(host)
        assertEquals(1, rects.size)
        val rect = rects[0]
        // Published in the ComposeView's coordinates: left must reach the window/cutout origin.
        assertTrue("cutout left ${rect.left} cutout=$cutoutPx", rect.left <= 1)
        assertTrue("covers past the pad ${rect.right}", rect.right >= cutoutPx)
    }

    /** Leading-edge exclusion rects from [host] or any descendant that published them. */
    private fun leadingRects(host: View): List<Rect> {
        val found = mutableListOf<Rect>()
        fun walk(view: View) {
            for (rect in view.systemGestureExclusionRects) {
                if (rect.width() > 0 && rect.left <= 80) found += Rect(rect)
            }
            if (view is ViewGroup) {
                for (i in 0 until view.childCount) walk(view.getChildAt(i))
            }
        }
        walk(host)
        walk(host.rootView)
        return found.distinctBy { "${it.left},${it.top},${it.right},${it.bottom}" }
    }
}

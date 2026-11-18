package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.shadows.ShadowLooper
import java.io.File
import kotlin.math.roundToInt

/**
 * Start, finger at half the column, and the frame after release. Glass, light and dark.
 * Upright 673dp is over; 951dp-wide landscape is beside (`columnPlacement`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35])
class SidebarColumnDragShotsTest {
    @get:Rule val rule = createComposeRule()

    @Before fun hardwareSnapshots() {
        columnDragTestHooks = true
        columnDragSnapshotForTest = ::columnDragHardwareSnapshot
    }

    @After fun clearHardwareSnapshots() {
        columnDragSnapshotForTest = null
        columnDragTestHooks = false
    }

    @Config(sdk = [35], qualifiers = "en-w673dp-h951dp-mdpi")
    @Test fun overFoldable() = frames(over = true, prefix = "over")

    @Config(sdk = [35], qualifiers = "en-w951dp-h673dp-mdpi")
    @Test fun besideFoldable() = frames(over = false, prefix = "beside")

    @Config(sdk = [35], qualifiers = "en-w673dp-h951dp-mdpi")
    @Test fun restingOverMatches() = resting(overlay = true)

    @Config(sdk = [35], qualifiers = "en-w951dp-h673dp-mdpi")
    @Test fun restingBesideMatches() = resting(overlay = false)

    @Config(sdk = [35], qualifiers = "en-w673dp-h951dp-mdpi")
    @Test fun overDoesNotShowTheFullColumnBeforeTheOverlay() = layoutStaysUntilCovered(over = true)

    @Config(sdk = [35], qualifiers = "en-w951dp-h673dp-mdpi")
    @Test fun besideDoesNotShiftThePageBeforeTheOverlay() = layoutStaysUntilCovered(over = false)

    @Config(sdk = [35], qualifiers = "en-w673dp-h951dp-mdpi")
    @Test fun closingOverKeepsTheColumnUntilTheOverlay() = columnStaysUntilCovered()

    /** No frame from finger-down until the overlay draws shows the snapped layout (`SplitColumnControl.swift:204-216`). */
    private fun layoutStaysUntilCovered(over: Boolean) {
        val pageColor = 0xFF2244AA.toInt()
        val columnColor = 0xFF22AA44.toInt()
        val markerColor = 0xFFFFEE00.toInt()
        var shown by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, reduceMotion = false) {
                Box(Modifier.fillMaxSize().testTag("shot")) {
                    JSidebarColumn(sidebar = {
                        Box(Modifier.fillMaxSize().background(Color(columnColor)))
                    }, width = 320.dp, pageStart = 320.dp, shown = shown, overlay = over, drags = true,
                        onDragSettle = { shown = it }) {
                        Box(Modifier.fillMaxSize().background(Color(pageColor))) {
                            Box(Modifier.width(8.dp).fillMaxHeight().background(Color(markerColor)))
                        }
                    }
                }
            }
        }
        rule.waitForIdle()
        val widthPx = with(rule.density) { 320.dp.toPx() }
        val controller = columnDragControllerForTest ?: error("missing controller")
        rule.mainClock.autoAdvance = false
        rule.runOnUiThread {
            controller.begin(ColumnDragZone.Open, over = over, widthPx = widthPx)
            Snapshot.sendApplyNotifications()
        }
        var covered = false
        var frame = 0
        while (frame < 24 && !covered) {
            rule.mainClock.advanceTimeByFrame()
            val bmp = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            val marker = bmp.getPixel(2, 80)
            val edge = bmp.getPixel(300, 400)
            assertEquals("frame $frame shifted the page before the overlay covered it", markerColor, marker)
            assertEquals("frame $frame showed the column at full width before the overlay", pageColor, edge)
            if (columnDragOverlayDrawn) covered = true
            ShadowLooper.idleMainLooper()
            frame++
        }
        assertTrue("overlay never drew", covered)
    }

    /** Closing over the page hides the column only after the overlay is up (`:225` after `:204`). */
    private fun columnStaysUntilCovered() {
        val pageColor = 0xFF2244AA.toInt()
        val columnColor = 0xFF22AA44.toInt()
        var shown by mutableStateOf(true)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, reduceMotion = false) {
                Box(Modifier.fillMaxSize().testTag("shot")) {
                    JSidebarColumn(sidebar = {
                        Box(Modifier.fillMaxSize().background(Color(columnColor)))
                    }, width = 320.dp, pageStart = 320.dp, shown = shown, overlay = true, drags = true,
                        onDragSettle = { shown = it }) {
                        Box(Modifier.fillMaxSize().background(Color(pageColor)))
                    }
                }
            }
        }
        rule.waitForIdle()
        val widthPx = with(rule.density) { 320.dp.toPx() }
        val controller = columnDragControllerForTest ?: error("missing controller")
        rule.mainClock.autoAdvance = false
        rule.runOnUiThread {
            controller.begin(ColumnDragZone.Close, over = true, widthPx = widthPx)
            Snapshot.sendApplyNotifications()
        }
        var covered = false
        var frame = 0
        while (frame < 24 && !covered) {
            rule.mainClock.advanceTimeByFrame()
            val bmp = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            assertEquals("frame $frame hid the column before the overlay", columnColor, bmp.getPixel(80, 400))
            if (columnDragOverlayDrawn) covered = true
            ShadowLooper.idleMainLooper()
            frame++
        }
        assertTrue("overlay never drew", covered)
    }

    private fun frames(over: Boolean, prefix: String) {
        var dark by mutableStateOf(false)
        var shown by mutableStateOf(false)
        val accent = IntArray(1)
        val column = IntArray(1)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = dark, reduceMotion = false) {
                val accentColor = PersonaTheme.palette.accent
                val columnColor = PersonaTheme.palette.positive
                SideEffect {
                    accent[0] = accentColor.toArgb()
                    column[0] = columnColor.toArgb()
                }
                Box(Modifier.fillMaxSize().testTag("shot")) {
                    // Child layers and a list, so a picture that keeps live nodes goes blank once the column leaves.
                    JSidebarColumn(sidebar = {
                        Box(Modifier.fillMaxSize()) {
                            Box(Modifier.fillMaxSize().graphicsLayer { }.background(columnColor))
                            LazyColumn(Modifier.fillMaxSize()) {
                                items(6) {
                                    Box(Modifier.fillMaxWidth().height(64.dp).graphicsLayer { }.background(columnColor))
                                }
                            }
                        }
                    }, width = 320.dp, pageStart = 320.dp, shown = shown, overlay = over, drags = true,
                        onDragSettle = { shown = it }) {
                        Box(Modifier.fillMaxSize().background(Color(0xFF88AACC))) {
                            Box(Modifier.fillMaxSize().graphicsLayer { }.background(Color(0xFF88AACC)))
                            Box(Modifier.align(Alignment.CenterEnd).width(32.dp).fillMaxHeight().graphicsLayer { }
                                .background(Color(0xFFE25B4A)))
                        }
                    }
                }
            }
        }
        val out = File("build/column-drag-shots").also { it.mkdirs() }
        println("column-drag-shots ${out.absolutePath}")
        val widthPx = with(rule.density) { 320.dp.toPx() }
        for (isDark in listOf(false, true)) {
            val mode = if (isDark) "dark" else "light"
            rule.mainClock.autoAdvance = true
            rule.runOnIdle { dark = isDark; shown = false }
            rule.waitForIdle()
            val start = save(out, "$prefix-$mode-start")
            val controller = columnDragControllerForTest ?: error("missing controller")
            assertEquals(DragStep.Idle, controller.step)
            rule.runOnIdle { controller.begin(ColumnDragZone.Open, over = over, widthPx = widthPx) }
            var i = 0
            while (i < 24 && controller.step != DragStep.Follow) {
                rule.onNodeWithTag("shot").captureToImage()
                rule.waitForIdle()
                ShadowLooper.idleMainLooper()
                i++
            }
            assertEquals("$prefix $mode stuck at ${controller.step} page=${controller.pageSeen} col=${controller.columnSeen}",
                DragStep.Follow, controller.step)
            rule.runOnIdle { controller.move(widthPx / 2f) }
            rule.waitForIdle()
            val mid = save(out, "$prefix-$mode-mid")
            // The guide is 3px centered on the line (`Theme.Split.guide`). Sample its middle, not the antialiased edge.
            val center = (widthPx / 2f).roundToInt().coerceIn(1, mid.width - 2)
            assertEquals("$prefix $mode guide", accent[0], mid.getPixel(center, mid.height / 2))
            // Inside the column, clear of the guide and of the blur at the line. A live node would not be this colour.
            assertEquals("$prefix $mode column", column[0], mid.getPixel(80, 400))
            val farX = mid.width - 2
            val y = mid.height / 2
            if (over) {
                assertTrue("$prefix $mode dim", luminance(mid.getPixel(farX, y)) < luminance(start.getPixel(farX, y)) - 4)
            } else {
                assertEquals("$prefix $mode page edge", start.getPixel(farX, y), mid.getPixel(farX, y))
            }
            // Hold the clock on the fade's first frame: the spring has finished, the overlay is still up (`:278`).
            rule.mainClock.autoAdvance = false
            rule.runOnIdle {
                controller.end(2000f)
                Snapshot.sendApplyNotifications()
            }
            var frames = 0
            while (frames < 80 && controller.step != DragStep.Fade) {
                rule.mainClock.advanceTimeByFrame()
                ShadowLooper.idleMainLooper()
                frames++
            }
            assertEquals("$prefix $mode did not release (${controller.step})", DragStep.Fade, controller.step)
            val release = save(out, "$prefix-$mode-release")
            assertEquals("$prefix $mode release column", column[0], release.getPixel(80, 400))
            var fade = 0
            while (fade < 40 && controller.step != DragStep.Idle) {
                rule.mainClock.advanceTimeByFrame()
                ShadowLooper.idleMainLooper()
                fade++
            }
            assertEquals(DragStep.Idle, controller.step)
            assertTrue("$prefix $mode kept a still", controller.pagePicture == null && controller.columnPicture == null)
            assertTrue("$prefix $mode stayed shut", shown)
            rule.mainClock.autoAdvance = true
            rule.runOnIdle { controller.begin(ColumnDragZone.Close, over = over, widthPx = widthPx) }
            var shut = 0
            while (shut < 24 && controller.step != DragStep.Follow) {
                rule.onNodeWithTag("shot").captureToImage()
                rule.waitForIdle()
                ShadowLooper.idleMainLooper()
                shut++
            }
            assertEquals("$prefix $mode shut stuck at ${controller.step}", DragStep.Follow, controller.step)
            rule.runOnIdle { controller.move(-widthPx / 2f) }
            rule.waitForIdle()
            val closing = save(out, "$prefix-$mode-shut")
            assertEquals("$prefix $mode shut column", column[0], closing.getPixel(80, 400))
            rule.mainClock.autoAdvance = false
            rule.runOnIdle {
                controller.end(-2000f)
                Snapshot.sendApplyNotifications()
            }
            var shutFrames = 0
            while (shutFrames < 80 && controller.step != DragStep.Idle) {
                rule.mainClock.advanceTimeByFrame()
                ShadowLooper.idleMainLooper()
                shutFrames++
            }
            assertEquals(DragStep.Idle, controller.step)
            assertTrue("$prefix $mode shut kept a still", controller.pagePicture == null && controller.columnPicture == null)
            assertTrue("$prefix $mode shut did not close", !shown)
        }
    }

    private fun resting(overlay: Boolean) {
        var dark by mutableStateOf(false)
        var shown by mutableStateOf(false)
        var drags by mutableStateOf(false)
        var style by mutableStateOf(AppStyle.Glass)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.fillMaxSize().testTag("shot").background(PersonaTheme.palette.background)) {
                    JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize().background(PersonaTheme.palette.positive)) },
                        width = 320.dp, pageStart = 320.dp, shown = shown, overlay = overlay, drags = drags) {
                        Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background))
                    }
                }
            }
        }
        for (chosen in AppStyle.entries) for (isDark in listOf(false, true)) for (isShown in listOf(false, true)) {
            style = chosen
            dark = isDark
            shown = isShown
            drags = false
            val off = capture()
            drags = true
            val on = capture()
            assertSame(off, on, "style=$chosen overlay=$overlay shown=$isShown dark=$isDark")
        }
    }

    private fun capture(): Bitmap {
        rule.waitForIdle()
        return rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
    }

    private fun save(out: File, name: String): Bitmap {
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        println("wrote ${File(out, "$name.png").absolutePath} ${bitmap.width}x${bitmap.height}")
        return bitmap
    }

    private fun assertSame(a: Bitmap, b: Bitmap, label: String) {
        assertEquals("$label width", a.width, b.width)
        assertEquals("$label height", a.height, b.height)
        var diff = 0
        for (y in 0 until a.height) for (x in 0 until a.width) if (a.getPixel(x, y) != b.getPixel(x, y)) diff++
        assertEquals(label, 0, diff)
    }

    private fun luminance(pixel: Int): Int {
        val r = (pixel shr 16) and 0xFF
        val g = (pixel shr 8) and 0xFF
        val b = pixel and 0xFF
        return (r * 3 + g * 6 + b) / 10
    }
}

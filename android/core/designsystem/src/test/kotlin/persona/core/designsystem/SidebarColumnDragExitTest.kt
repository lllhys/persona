package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.layer.GraphicsLayer
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.shadows.ShadowLooper

/**
 * A failed snapshot never starts the drag (`SplitColumnControl.swift:191-195`). Idle keeps no stills.
 * A window change cancels with velocity 0 (`:178`) and drops the overlay.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w1000dp-h1000dp-mdpi")
class SidebarColumnDragExitTest {
    @get:Rule val rule = createComposeRule()

    @Before fun hooks() {
        columnDragTestHooks = true
        columnDragSnapshotForTest = ::columnDragHardwareSnapshot
    }

    @After fun clearHooks() {
        columnDragSnapshotForTest = null
        columnDragTestHooks = false
    }

    @Test fun aNullSnapshotLeavesTheColumnWhereItWas() = failedSnapshotLeavesIdle { null }

    @Test fun aThrownSnapshotLeavesTheColumnWhereItWas() = failedSnapshotLeavesIdle {
        error("snapshot failed")
    }

    @Test fun idleHoldsNoPictures() {
        var shown by mutableStateOf(false)
        rule.setContent { column(shown = shown, onSettle = { shown = it }) }
        val controller = begin(ColumnDragZone.Open, over = true)
        until("follow") { controller.step == DragStep.Follow }
        assertEquals(DragStep.Follow, controller.step)
        assertTrue(controller.pagePicture != null && controller.columnPicture != null)
        rule.runOnUiThread {
            controller.move(controller.widthPx)
            controller.end(2000f)
            Snapshot.sendApplyNotifications()
        }
        until("idle") { controller.step == DragStep.Idle }
        assertEquals(DragStep.Idle, controller.step)
        assertNull(controller.pagePicture)
        assertNull(controller.columnPicture)
        assertTrue(shown)
    }

    @Test fun closingBesideSharesOnePicture() {
        var shown by mutableStateOf(true)
        rule.setContent { column(shown = shown, overlay = false, onSettle = { shown = it }) }
        val controller = begin(ColumnDragZone.Close, over = false)
        until("follow") { controller.step == DragStep.Follow }
        assertEquals(DragStep.Follow, controller.step)
        assertTrue(controller.pagePicture != null && controller.pagePicture === controller.columnPicture)
    }

    @Test fun aWindowSizeChangeDropsTheOverlay() {
        var shown by mutableStateOf(false)
        var wide by mutableStateOf(600.dp)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Box(Modifier.size(wide, 800.dp).testTag("shot")) {
                    JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize().background(Color(0xFF22AA44))) },
                        width = 320.dp, pageStart = 320.dp, shown = shown, overlay = true, drags = true,
                        onDragSettle = { shown = it }) {
                        Box(Modifier.fillMaxSize().background(Color(0xFF2244AA)))
                    }
                }
            }
        }
        val controller = begin(ColumnDragZone.Open, over = true)
        until("follow") { controller.step == DragStep.Follow }
        assertEquals("stuck at ${controller.step}", DragStep.Follow, controller.step)
        assertTrue(controller.pagePicture != null)
        rule.runOnUiThread {
            wide = 900.dp
            Snapshot.sendApplyNotifications()
        }
        until("dropped") { controller.pagePicture == null && controller.columnPicture == null }
        assertNull(controller.pagePicture)
        assertNull(controller.columnPicture)
        assertEquals(0f, controller.fingerVelocityPx, 0f)
        until("idle") { controller.step == DragStep.Idle }
        assertEquals(DragStep.Idle, controller.step)
        assertTrue(!shown)
        assertNull(controller.pagePicture)
        assertNull(controller.columnPicture)
    }

    private fun failedSnapshotLeavesIdle(shot: suspend (GraphicsLayer) -> Nothing?) {
        columnDragSnapshotForTest = shot
        var shown by mutableStateOf(false)
        var settled: Boolean? = null
        rule.setContent { column(shown = shown, onSettle = { settled = it; shown = it }) }
        val controller = begin(ColumnDragZone.Open, over = true)
        until("idle") { controller.step == DragStep.Idle }
        assertEquals(DragStep.Idle, controller.step)
        assertNull(controller.layoutShown)
        assertNull(controller.pagePicture)
        assertNull(controller.columnPicture)
        assertTrue(!shown)
        assertNull(settled)
        rule.runOnUiThread {
            controller.begin(ColumnDragZone.Open, over = true, widthPx = controller.widthPx)
            Snapshot.sendApplyNotifications()
        }
        assertEquals(DragStep.OpenPage, controller.step)
    }

    @androidx.compose.runtime.Composable
    private fun column(shown: Boolean, overlay: Boolean = true, onSettle: (Boolean) -> Unit) {
        PersonaTheme(reduceMotion = true) {
            Box(Modifier.size(600.dp, 800.dp).testTag("shot")) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize().background(Color(0xFF22AA44))) },
                    width = 320.dp, pageStart = 320.dp, shown = shown, overlay = overlay, drags = true,
                    onDragSettle = onSettle) {
                    Box(Modifier.fillMaxSize().background(Color(0xFF2244AA)))
                }
            }
        }
    }

    private fun begin(zone: ColumnDragZone, over: Boolean): ColumnDragController {
        rule.waitForIdle()
        val controller = columnDragControllerForTest ?: error("missing controller")
        val widthPx = with(rule.density) { 320.dp.toPx() }
        rule.mainClock.autoAdvance = false
        rule.runOnUiThread {
            controller.begin(zone, over = over, widthPx = widthPx)
            Snapshot.sendApplyNotifications()
        }
        return controller
    }

    private fun until(label: String, ready: () -> Boolean) {
        var frame = 0
        while (frame < 80 && !ready()) {
            rule.mainClock.advanceTimeByFrame()
            rule.onNodeWithTag("shot").captureToImage()
            ShadowLooper.idleMainLooper()
            frame++
        }
        assertTrue("$label stuck at ${columnDragControllerForTest?.step} after $frame", ready())
    }
}

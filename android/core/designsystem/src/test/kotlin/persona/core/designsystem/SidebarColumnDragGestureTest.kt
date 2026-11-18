package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipe
import androidx.compose.ui.test.swipeWithVelocity
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
 * The gesture beside real scrolling and row swipes. The system back edge is not made to wait
 * (`SplitColumnControl.swift:156`): [openEdgeYieldsWhenTheDetailCanGoBack] leaves that edge unconsumed.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w673dp-h951dp-mdpi")
class SidebarColumnDragGestureTest {
    @get:Rule val rule = createComposeRule()

    @Before fun hooks() { columnDragTestHooks = true }

    @After fun clearHooks() { columnDragTestHooks = false }

    @Test fun dragsOffIgnoresTheEdge() {
        var settled: Boolean? = null
        var shown by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize().background(Color(0xFF226644))) },
                    width = 320.dp, pageStart = 320.dp, shown = shown, overlay = true, drags = false,
                    onDragSettle = { settled = it; shown = it }) {
                    Box(Modifier.fillMaxSize().testTag("page").background(Color(0xFF88AACC)))
                }
            }
        }
        rule.onNodeWithTag("page").performTouchInput { swipe(Offset(8f, 200f), Offset(220f, 200f), durationMillis = 300) }
        rule.waitForIdle()
        assertNull(settled)
        assertTrue(!shown)
    }

    @Test fun openEdgeYieldsWhenTheDetailCanGoBack() {
        var settled: Boolean? = null
        var child = 0
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize()) }, width = 320.dp, pageStart = 320.dp,
                    shown = false, overlay = true, drags = true, detailAtRoot = false, onDragSettle = { settled = it }) {
                    Box(Modifier.fillMaxSize().testTag("page").pointerInput(Unit) {
                        detectHorizontalDragGestures { _, drag -> if (drag != 0f) child++ }
                    })
                }
            }
        }
        rule.onNodeWithTag("page").performTouchInput { swipe(Offset(8f, 240f), Offset(220f, 240f), durationMillis = 300) }
        rule.waitForIdle()
        assertTrue(child > 0)
        assertNull(settled)
    }

    @Test fun aVerticalSwipeInTheEdgeZoneStillScrolls() {
        val list = androidx.compose.foundation.lazy.LazyListState()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize()) }, width = 320.dp, pageStart = 320.dp,
                    shown = false, overlay = true, drags = true) {
                    LazyColumn(state = list, modifier = Modifier.fillMaxSize().testTag("page")) {
                        items(40) { Box(Modifier.height(48.dp).fillMaxSize()) }
                    }
                }
            }
        }
        rule.onNodeWithTag("page").performTouchInput { swipe(Offset(8f, 700f), Offset(8f, 200f), durationMillis = 200) }
        rule.waitForIdle()
        assertTrue(list.firstVisibleItemIndex > 0)
    }

    @Test fun aRowSwipeAwayFromTheColumnEdgeStillFires() {
        var pageSwiped = 0
        var columnSwiped = 0
        var shown by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                JSidebarColumn(sidebar = {
                    Box(Modifier.fillMaxSize().testTag("column").pointerInput(Unit) {
                        detectHorizontalDragGestures { _, drag -> if (drag != 0f) columnSwiped++ }
                    })
                }, width = 320.dp, pageStart = 320.dp, shown = shown, overlay = true, drags = true) {
                    Box(Modifier.fillMaxSize().testTag("page").pointerInput(Unit) {
                        detectHorizontalDragGestures { _, drag -> if (drag != 0f) pageSwiped++ }
                    })
                }
            }
        }
        // x=80 is outside the 22dp leading edge (`SplitColumnControl.swift:137`), so the row keeps the drag.
        rule.onNodeWithTag("page").performTouchInput {
            swipe(Offset(80f, 300f), Offset(240f, 300f), durationMillis = 200)
        }
        rule.waitForIdle()
        assertTrue(pageSwiped > 0)
        // Shown column: x=40 is not the trailing edge (`:146`). The page under the scrim is covered.
        rule.runOnIdle { shown = true }
        rule.waitForIdle()
        rule.onNodeWithTag("column").performTouchInput {
            swipe(Offset(40f, 300f), Offset(160f, 300f), durationMillis = 200)
        }
        rule.waitForIdle()
        assertTrue(columnSwiped > 0)
    }

    @Test fun theColumnEdgeStillScrollsVertically() {
        lateinit var list: androidx.compose.foundation.lazy.LazyListState
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                list = rememberLazyListState()
                JSidebarColumn(sidebar = {
                    LazyColumn(state = list, modifier = Modifier.fillMaxSize().testTag("list")) {
                        items(40) { Box(Modifier.height(48.dp).fillMaxSize().background(Color(0xFF226644))) }
                    }
                }, width = 320.dp, pageStart = 320.dp, shown = true, overlay = true, drags = true) {
                    Box(Modifier.fillMaxSize())
                }
            }
        }
        rule.onNodeWithTag("list").performTouchInput { swipe(Offset(310f, 700f), Offset(310f, 200f), durationMillis = 200) }
        rule.waitForIdle()
        assertTrue(list.firstVisibleItemIndex > 0)
    }

    @Test fun slowAndFastReleases() {
        var shown by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize().background(Color(0xFF226644))) },
                    width = 320.dp, pageStart = 320.dp, shown = shown, overlay = true, drags = true,
                    modifier = Modifier.testTag("root"), onDragSettle = { shown = it }) {
                    Box(Modifier.fillMaxSize().testTag("page").background(Color(0xFF88AACC)))
                }
            }
        }
        // Past half, slower than 500 dp/s: stays open (`:264`). 220 > 160.
        rule.onNodeWithTag("page").performTouchInput { swipe(Offset(8f, 180f), Offset(220f, 180f), durationMillis = 800) }
        flushUntil("root") { columnDragControllerForTest?.step == DragStep.Idle }
        assertTrue("open step=${columnDragControllerForTest?.step}", shown)
        // Slow, and the line ends at 150, short of half: stays closed. 310 is within 22dp of the edge (`:146`).
        rule.onNodeWithTag("root").performTouchInput { swipe(Offset(310f, 220f), Offset(140f, 220f), durationMillis = 800) }
        flushUntil("root") { columnDragControllerForTest?.step == DragStep.Idle }
        assertTrue("close step=${columnDragControllerForTest?.step}", !shown)
        // A flick faster than 500 dp/s opens even though the finger stops short of half (`:264`).
        rule.onNodeWithTag("page").performTouchInput {
            swipeWithVelocity(Offset(8f, 260f), Offset(70f, 260f), endVelocity = 2000f)
        }
        flushUntil("root") { columnDragControllerForTest?.step == DragStep.Idle }
        assertTrue("flick open step=${columnDragControllerForTest?.step} v=${columnDragControllerForTest?.fingerVelocityPx} dx=${columnDragControllerForTest?.dxPx}", shown)
        // A flick the other way closes even though the line is still past half.
        rule.onNodeWithTag("root").performTouchInput {
            swipeWithVelocity(Offset(310f, 300f), Offset(250f, 300f), endVelocity = 2000f)
        }
        flushUntil("root") { columnDragControllerForTest?.step == DragStep.Idle }
        assertTrue("flick close step=${columnDragControllerForTest?.step} v=${columnDragControllerForTest?.fingerVelocityPx} dx=${columnDragControllerForTest?.dxPx}", !shown)
    }

    @Test fun theScrimStillDismisses() {
        var shown by mutableStateOf(true)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize().testTag("column")) }, width = 320.dp,
                    pageStart = 320.dp, shown = shown, overlay = true, drags = true, onDismiss = { shown = false }) {
                    Box(Modifier.fillMaxSize().testTag("page"))
                }
            }
        }
        rule.onNodeWithContentDescription("Close").performClick()
        rule.waitForIdle()
        assertTrue(!shown)
    }

    @Test fun reduceMotionJumpsThenFades() {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize().background(Color(0xFF226644))) },
                    width = 320.dp, pageStart = 320.dp, shown = false, overlay = true, drags = true,
                    modifier = Modifier.testTag("root")) {
                    Box(Modifier.fillMaxSize().background(Color(0xFF88AACC)))
                }
            }
        }
        rule.waitForIdle()
        val controller = columnDragControllerForTest ?: error("missing controller")
        rule.runOnIdle { controller.begin(ColumnDragZone.Open, over = true, widthPx = 320f) }
        flushUntil("root") { controller.step == DragStep.Follow }
        assertEquals("stuck at ${controller.step} page=${controller.pageSeen} col=${controller.columnSeen}",
            DragStep.Follow, controller.step)
        rule.mainClock.autoAdvance = false
        rule.runOnIdle {
            controller.move(40f)
            controller.end(2000f)
            Snapshot.sendApplyNotifications()
        }
        var i = 0
        while (i < 80 && controller.step != DragStep.Fade && controller.step != DragStep.Idle) {
            rule.mainClock.advanceTimeByFrame()
            ShadowLooper.idleMainLooper()
            i++
        }
        assertTrue("release stuck at ${controller.step}", controller.step == DragStep.Fade || controller.step == DragStep.Idle)
        rule.mainClock.advanceTimeByFrame()
        ShadowLooper.idleMainLooper()
        assertEquals(320f, columnDragDrawnLineDp, 1f)
        var j = 0
        while (j < 40 && controller.step != DragStep.Idle) {
            rule.mainClock.advanceTimeByFrame()
            ShadowLooper.idleMainLooper()
            j++
        }
        assertEquals(DragStep.Idle, controller.step)
    }

    @Test fun aCancelEndsWithNoVelocity() {
        var shown by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize().background(Color(0xFF226644))) },
                    width = 320.dp, pageStart = 320.dp, shown = shown, overlay = true, drags = true,
                    modifier = Modifier.testTag("root"), onDragSettle = { shown = it }) {
                    Box(Modifier.fillMaxSize().testTag("page").background(Color(0xFF88AACC)))
                }
            }
        }
        // The same move as a flick (`:176`), but the system cancels the pointer (`.cancelled` passes 0, `:178`).
        rule.onNodeWithTag("page").performTouchInput {
            down(Offset(8f, 260f))
            moveTo(Offset(70f, 260f))
            cancel()
        }
        flushUntil("root") { columnDragControllerForTest?.step == DragStep.Idle }
        val controller = columnDragControllerForTest
        assertEquals(0f, controller?.fingerVelocityPx ?: Float.NaN)
        assertTrue("cancel must not flick open (step=${controller?.step})", !shown)
    }

    @Test fun aSecondFingerDoesNotEndTheDrag() {
        var shown by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize().background(Color(0xFF226644))) },
                    width = 320.dp, pageStart = 320.dp, shown = shown, overlay = true, drags = true,
                    modifier = Modifier.testTag("root"), onDragSettle = { shown = it }) {
                    Box(Modifier.fillMaxSize().background(Color(0xFF88AACC)))
                }
            }
        }
        // A pan does not end when another finger lands (`moved` has no pointer-count branch, `:171-179`).
        // The step under the slop shares the down's time. The next steps are 100ms apart, so the one that
        // passes the slop has a speed under a fling, and the line is still short of half: ending there stays shut.
        // The tracked finger then passes half while the other finger is down, and the column opens.
        rule.onNodeWithTag("root").performTouchInput {
            down(0, Offset(8f, 220f))
            moveTo(0, Offset(12f, 220f))
            moveTo(0, Offset(36f, 220f), delayMillis = 100)
            moveTo(0, Offset(60f, 220f), delayMillis = 100)
            down(1, Offset(40f, 500f))
            moveTo(0, Offset(120f, 220f), delayMillis = 100)
            moveTo(0, Offset(180f, 220f), delayMillis = 100)
            moveTo(0, Offset(240f, 220f), delayMillis = 100)
            up(0)
        }
        flushUntil("root") { columnDragControllerForTest?.step == DragStep.Idle }
        val controller = columnDragControllerForTest
        assertEquals(DragStep.Idle, controller?.step)
        assertTrue("second finger ended the drag early (dx=${controller?.dxPx})", shown)
    }

    @Test fun aGestureWhileTheLineIsSettlingDoesNotSwallowTheRow() {
        var drags = 0
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize()) }, width = 320.dp, pageStart = 320.dp,
                    shown = false, overlay = true, drags = true, modifier = Modifier.testTag("root")) {
                    Box(Modifier.fillMaxSize().testTag("page").pointerInput(Unit) {
                        detectHorizontalDragGestures { _, drag -> if (drag != 0f) drags++ }
                    })
                }
            }
        }
        rule.waitForIdle()
        val controller = columnDragControllerForTest ?: error("missing controller")
        // `gestureRecognizerShouldBegin` is false while a drag exists (`drag == nil`, `:131`).
        rule.runOnIdle { controller.step = DragStep.Follow }
        rule.onNodeWithTag("page").performTouchInput {
            swipe(Offset(8f, 300f), Offset(180f, 300f), durationMillis = 200)
        }
        rule.waitForIdle()
        assertTrue("the row kept the drag", drags > 0)
        assertEquals(DragStep.Follow, controller.step)
    }

    @Test fun noBlurBeforeApi31() {
        // This process is API 35. API 28 is covered by SidebarColumnDragTest, which passes the SDK in.
        assertTrue(columnDragBlurEffect(8f) != null)
        assertTrue(columnDragBlurEffect(0.4f) == null)
    }

    /** A picture is taken in draw. Capturing forces that draw, then the posted phase advance runs. */
    private fun flushUntil(tag: String, done: () -> Boolean) {
        var i = 0
        while (i < 16 && !done()) {
            rule.onNodeWithTag(tag, useUnmergedTree = true).captureToImage()
            rule.waitForIdle()
            ShadowLooper.idleMainLooper()
            i++
        }
    }
}

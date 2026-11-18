package persona.core.designsystem

import android.view.View
import android.webkit.WebView
import androidx.compose.foundation.ScrollState
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.semantics
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.ui.layout.Layout
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toPixelMap
import androidx.compose.ui.hapticfeedback.HapticFeedback
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performTouchInput
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
import persona.core.designsystem.icons.PhIcons
import kotlin.math.abs
import kotlin.math.min

/**
 * The drawer's finger on the JVM. Progress is the blue page's left edge in the picture: `graphicsLayer` translation
 * does not move layout bounds. The rules copy `DrawerPan` (`apple/Persona/Views/Assistant/AssistantSidebar.swift`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-mdpi")
class DrawerDragGestureTest {
    @get:Rule val rule = createComposeRule()

    private val shellWidth = 360.dp
    private val shellHeight = 640.dp
    private val page = Color.Blue
    private val sidebar = Color.Red

    @Test fun rightwardDragTracksTheFingerAndAVerticalDragDoesNot() {
        val vertical = ScrollState(0)
        show(reduce = true) {
            Column(Modifier.fillMaxSize().verticalScroll(vertical)) {
                Box(Modifier.fillMaxWidth().height(1600.dp))
            }
        }
        // Finger moving up scrolls the list. A downward drag at the top does not, and iOS would not take it either.
        drag(from = Offset(80f, 400f), by = Offset(8f, -180f))
        assertTrue("vertical scroll moved ${vertical.value}", vertical.value > 20)
        assertEquals(0f, edge().toFloat(), 3f)

        val drawer = drawerPx()
        drag(from = Offset(80f, 240f), by = Offset(drawer / 2f, 6f), hold = true)
        frame()
        val tracked = edge()
        assertEquals("edge=$tracked drawer=$drawer", drawer / 2f, tracked.toFloat(), 4f)
        val held = edge()
        rule.mainClock.advanceTimeBy(200)
        assertEquals(held.toFloat(), edge().toFloat(), 2f)
    }

    @Test fun horizontalScrollerWithRoomKeepsTheDrag() {
        val scroll = ScrollState(0)
        show(reduce = true) {
            Box(Modifier.horizontalScroll(scroll)) {
                Box(Modifier.width(900.dp).height(120.dp).background(Color.Yellow))
            }
        }
        assertTrue("scroller max ${scroll.maxValue}", scroll.maxValue > 200)
        // Room back toward the start. iOS refuses (`sidewaysScroller`, AssistantSidebar.swift:556-559, line 526).
        rule.runOnIdle { scroll.dispatchRawDelta(200f - scroll.value) }
        val before = scroll.value
        drag(from = Offset(120f, 40f), by = Offset(90f, 0f))
        assertTrue("scroll $before -> ${scroll.value}", scroll.value < before - 10)
        assertEquals(0f, edge().toFloat(), 4f)
    }

    @Test fun horizontalScrollerAtItsStartIsRecorded() {
        val scroll = ScrollState(0)
        show(reduce = true) {
            Box(Modifier.horizontalScroll(scroll)) {
                Box(Modifier.width(900.dp).height(120.dp).background(Color.Yellow))
            }
        }
        assertEquals(0, scroll.value)
        drag(from = Offset(120f, 40f), by = Offset(90f, 0f), hold = true)
        frame()
        // No room toward the start (`AssistantSidebar.swift:558`): the drawer takes the drag. The scroller stays put.
        assertTrue("at-start edge=${edge()} scroll=${scroll.value}", edge() > 40)
        assertEquals(0, scroll.value)
    }

    @Test fun fastDiagonalOnAListTakesTheDrawer() {
        // Every message has pointerInput (MessageSelection.kt:174 and :240). A fast diagonal whose first sample
        // is past both slops (dx=30, dy=20) used to be blamed on that child because the list also consumed it.
        // The list waits (`shouldBeRequiredToFailBy`, AssistantSidebar.swift:540-544), so the drawer takes it.
        show(reduce = true) {
            LazyColumn(Modifier.fillMaxSize()) {
                items(40) { index ->
                    Box(Modifier.fillMaxWidth().height(72.dp).pointerInput(index) {
                        awaitEachGesture {
                            val down = awaitFirstDown(requireUnconsumed = false)
                            while (true) {
                                val event = awaitPointerEvent()
                                val change = event.changes.firstOrNull { it.id == down.id } ?: break
                                if (!change.pressed) break
                            }
                        }
                    })
                }
            }
        }
        rule.onNodeWithTag("shell").performTouchInput {
            down(Offset(80f, 200f))
            moveBy(Offset(30f, 20f))
        }
        frame()
        assertTrue("diagonal edge ${edge()}", edge() > 12)
    }

    @Test fun stillAndroidViewOpensTheDrawer() {
        // A link card's WebView that cannot scroll sideways is not a sideways scroller (AssistantSidebar.swift:551-558).
        show(reduce = true) {
            AndroidView(factory = { StillView(it) }, modifier = Modifier.fillMaxWidth().height(220.dp))
        }
        drag(from = Offset(80f, 80f), by = Offset(160f, 0f), hold = true)
        frame()
        assertTrue("still view edge ${edge()}", edge() > 40)
    }

    @Test fun androidViewWithRoomKeepsTheDrag() {
        lateinit var view: RoomView
        show(reduce = true) {
            AndroidView(
                factory = { RoomView(it).also { created -> view = created } },
                modifier = Modifier.fillMaxWidth().height(220.dp),
            )
        }
        drag(from = Offset(80f, 80f), by = Offset(140f, 0f))
        assertEquals("room edge ${edge()}", 0f, edge().toFloat(), 4f)
        rule.runOnIdle { view.room = false }
        drag(from = Offset(80f, 120f), by = Offset(140f, 0f), hold = true)
        frame()
        assertTrue("no room edge ${edge()}", edge() > 40)
    }

    @Test fun pooledHorizontalScrollerDoesNotKeepTheBlankSpot() {
        // A LazyColumn drops a scrolled-off row from its child list, so that row cannot keep a stale hit.
        // A reuse-pool node that stays attached is not placed. `covers()` still used its last coordinates.
        // Place a sideways scroller at the top, give it room, then stop placing it. The blank spot is where it was.
        val scroll = ScrollState(0)
        var placeWide by mutableStateOf(true)
        var ready by mutableStateOf(false)
        show(reduce = true) {
            LaunchedEffect(Unit) {
                scroll.scrollTo(180)
                placeWide = false
                ready = true
            }
            Layout(
                content = {
                    Box(Modifier.fillMaxWidth().height(160.dp).horizontalScroll(scroll)) {
                        Box(Modifier.width(1200.dp).height(160.dp).background(Color.Yellow))
                    }
                },
                measurePolicy = { measurables, constraints ->
                    val wide = measurables.first().measure(constraints.copy(minWidth = 0, minHeight = 0))
                    layout(constraints.maxWidth, constraints.maxHeight) {
                        if (placeWide) wide.place(0, 0)
                    }
                },
            )
        }
        var spins = 0
        while (!ready && spins < 40) {
            rule.mainClock.advanceTimeByFrame()
            spins++
        }
        rule.mainClock.advanceTimeByFrame()
        assertTrue("not ready scroll=${scroll.value}", ready)
        assertTrue("scroller has no room ${scroll.value}", scroll.value > 100)
        drag(from = Offset(120f, 40f), by = Offset(140f, 0f), hold = true)
        frame()
        assertTrue("blank edge ${edge()} scroll=${scroll.value}", edge() > 40)
    }

    @Test fun openScrimOverAWebViewCloses() {
        var opened by mutableStateOf(true)
        show(reduce = true, opened = { opened }, onDismiss = { opened = false }) {
            AndroidView(
                factory = {
                    WebView(it).apply { setBackgroundColor(android.graphics.Color.BLUE) }
                },
                modifier = Modifier.fillMaxSize(),
            )
        }
        val openEdge = edge()
        assertTrue("open edge $openEdge", openEdge > 200)
        drag(from = Offset(openEdge + 30f, 300f), by = Offset(-160f, 0f))
        rule.mainClock.advanceTimeBy(400)
        assertEquals("scrim edge ${edge()}", 0f, edge().toFloat(), 8f)
        assertTrue(!opened)
    }

    @Test fun sliderLikeChildKeepsARightwardDrag() {
        var pulls = 0
        show(reduce = true) { ConsumingRow(onPull = { pulls++ }) }
        drag(from = Offset(80f, 240f), by = Offset(160f, 0f))
        assertEquals(0f, edge().toFloat(), 4f)
        assertTrue("child saw $pulls moves", pulls > 0)
    }

    @Test fun sliderLikeChildKeepsTheOpenDrawer() {
        var opened by mutableStateOf(true)
        var pulls = 0
        // The open page's scrim covers the page, so the child has to live in the drawer. A leftward take would close.
        show(
            reduce = true,
            opened = { opened },
            onDismiss = { opened = false },
            drawer = { ConsumingRow(onPull = { pulls++ }) },
        ) { Box(Modifier.fillMaxSize()) }
        rule.mainClock.advanceTimeBy(50)
        val openEdge = edge()
        assertTrue("open edge $openEdge", openEdge > 200)
        drag(from = Offset(80f, 240f), by = Offset(-90f, 0f))
        assertEquals(openEdge.toFloat(), edge().toFloat(), 8f)
        assertTrue(opened)
        assertTrue("child saw $pulls moves", pulls > 0)
    }

    @Test fun jSliderKeepsARightwardDrag() {
        var value by mutableStateOf(0.4f)
        show(reduce = true) {
            Box(Modifier.fillMaxWidth().padding(top = 48.dp)) {
                JSlider(value, onValueChange = { value = it })
            }
        }
        val before = value
        drag(from = Offset(180f, 72f), by = Offset(90f, 0f))
        assertEquals(0f, edge().toFloat(), 4f)
        assertTrue("slider $before -> $value", value > before + 0.02f)
    }

    @Test fun cancelClosesWithNoVelocity() {
        var opened by mutableStateOf(false)
        show(reduce = true, opened = { opened }, onOpen = { opened = true }) { Box(Modifier.fillMaxSize()) }
        val flick = drawerPx() * 0.7f
        rule.onNodeWithTag("shell").performTouchInput {
            down(Offset(40f, 220f))
            val steps = 6
            repeat(steps) { moveBy(Offset(flick / steps, 0f), delayMillis = 8) }
            cancel()
        }
        rule.mainClock.advanceTimeBy(400)
        assertEquals("cancel edge ${edge()}", 0f, edge().toFloat(), 4f)
        assertTrue(!opened)
    }

    @Test fun backDuringADragStaysClosed() {
        var opened by mutableStateOf(true)
        show(reduce = true, opened = { opened }, onOpen = { opened = true }, onDismiss = { opened = false }) {
            Box(Modifier.fillMaxSize())
        }
        rule.mainClock.advanceTimeBy(50)
        assertTrue("open ${edge()}", edge() > 200)
        // A short drag would settle open on release. Back must win before that release.
        drag(from = Offset(edge() + 24f, 280f), by = Offset(36f, 0f), hold = true)
        frame()
        assertTrue("held ${edge()}", edge() > 200)
        opened = false
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        rule.mainClock.advanceTimeBy(400)
        assertEquals("back edge ${edge()}", 0f, edge().toFloat(), 6f)
        rule.onNodeWithTag("shell").performTouchInput { up() }
        rule.mainClock.advanceTimeBy(400)
        assertEquals("release edge ${edge()}", 0f, edge().toFloat(), 6f)
        assertTrue(!opened)
    }

    @Test fun disablingTheDragDoesNotDismiss() {
        var opened by mutableStateOf(true)
        var enabled by mutableStateOf(true)
        var dismissals = 0
        show(
            reduce = true,
            dragEnabledOf = { enabled },
            opened = { opened },
            onDismiss = { dismissals++; opened = false },
        ) { Box(Modifier.fillMaxSize()) }
        rule.mainClock.advanceTimeBy(50)
        val openEdge = edge()
        drag(from = Offset(openEdge + 20f, 260f), by = Offset(-48f, 0f), hold = true)
        frame()
        enabled = false
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        rule.mainClock.advanceTimeBy(80)
        assertEquals(0, dismissals)
        assertTrue(opened)
        assertTrue("still open ${edge()}", edge() > openEdge - 40)
    }

    @Test fun aDragDoesNotRecomposeEveryFrame() {
        var compositions = 0
        show(reduce = true) {
            compositions++
            Box(Modifier.fillMaxSize())
        }
        rule.mainClock.advanceTimeBy(32)
        val before = compositions
        drag(from = Offset(40f, 220f), by = Offset(drawerPx() / 2f, 0f), hold = true, delayMillis = 96)
        frame()
        frame()
        val during = compositions - before
        assertTrue("recomposed $during times while the finger moved", during <= 3)
    }

    @Test fun swipeToReplyKeepsALeftwardDrag() {
        var replied = false
        show(reduce = true) {
            Box(Modifier.fillMaxWidth().height(88.dp).background(Color.Green).swipeToReply("Reply") { replied = true })
        }
        drag(from = Offset(200f, 40f), by = Offset(-140f, 0f))
        assertTrue(replied)
        assertEquals(0f, edge().toFloat(), 3f)
    }

    @Test fun swipeRowKeepsTheOpenDrawer() {
        var opened by mutableStateOf(true)
        var archived = false
        show(
            reduce = true,
            opened = { opened },
            onDismiss = { opened = false },
            drawer = {
                // The real sidebar is a vertical list (`AssistantScreen.kt` puts `JSwipeActions` in it).
                // The list waits (`shouldBeRequiredToFailBy`, AssistantSidebar.swift:540-544). The row's mark
                // still keeps a leftward drag (line 500).
                LazyColumn(Modifier.fillMaxSize()) {
                    items(8) {
                        JSwipeActions(end = SwipeAction("Done", PhIcons.Check, Status.Unread, { archived = true })) {
                            Box(Modifier.fillMaxWidth().height(72.dp).background(sidebar))
                        }
                    }
                }
            },
        ) { Box(Modifier.fillMaxSize()) }
        rule.mainClock.advanceTimeBy(50)
        val openEdge = edge()
        assertTrue("open edge $openEdge", openEdge > 200)
        drag(from = Offset(240f, 36f), by = Offset(-200f, 0f))
        // The row's own settle runs after the finger lifts. The drawer must not have taken the drag.
        rule.mainClock.advanceTimeBy(500)
        assertEquals(openEdge.toFloat(), edge().toFloat(), 4f)
        assertTrue(opened)
        assertTrue("the row archived", archived)
    }

    @Test fun swipeRowWhileClosedStillOpensTheDrawer() {
        // The closed branch does not check the mark (AssistantSidebar.swift:524-526). A rightward drag opens.
        show(reduce = true) {
            LazyColumn(Modifier.fillMaxSize()) {
                items(4) {
                    JSwipeActions(end = SwipeAction("Done", PhIcons.Check, Status.Unread, {})) {
                        Box(Modifier.fillMaxWidth().height(72.dp).background(sidebar))
                    }
                }
            }
        }
        drag(from = Offset(40f, 36f), by = Offset(140f, 0f), hold = true)
        frame()
        assertTrue("closed row edge ${edge()}", edge() > 40)
    }

    @Test fun composerFieldOpensTheDrawer() {
        // A collapsed cursor is not a selection. iOS's closed drawer branch does not check typing
        // (AssistantSidebar.swift:524-526); the typing refusal is the pager (line 513).
        show(reduce = true) {
            BasicTextField(
                value = "Saturday",
                onValueChange = {},
                modifier = Modifier.fillMaxWidth().height(64.dp),
            )
        }
        drag(from = Offset(48f, 24f), by = Offset(140f, 0f), hold = true)
        frame()
        assertTrue("composer edge ${edge()}", edge() > 40)
    }

    @Test fun selectedTextKeepsTheDrag() {
        show(reduce = true) {
            BasicTextField(
                value = TextFieldValue("Saturday plans", TextRange(0, 8)),
                onValueChange = {},
                modifier = Modifier.fillMaxWidth().height(64.dp),
            )
        }
        drag(from = Offset(48f, 24f), by = Offset(140f, 0f))
        assertEquals("selection edge ${edge()}", 0f, edge().toFloat(), 4f)
    }

    @Test fun progressIndicatorDoesNotBlockTheDrawer() {
        // A spinner and a labelled progress range carry ProgressBarRangeInfo. A slider is the control
        // that sets its own value (SemanticsActions.SetProgress). The range on its own must not block.
        show(reduce = true) {
            Box(Modifier.fillMaxSize().semantics { progressBarRangeInfo = ProgressBarRangeInfo(0.4f, 0f..1f) })
        }
        drag(from = Offset(80f, 240f), by = Offset(140f, 0f), hold = true)
        frame()
        assertTrue("progress edge ${edge()}", edge() > 40)
    }

    @Test fun spinnerDoesNotBlockTheDrawer() {
        show(reduce = true) {
            CircularProgressIndicator(Modifier.size(160.dp))
        }
        drag(from = Offset(40f, 40f), by = Offset(140f, 0f), hold = true)
        frame()
        assertTrue("spinner edge ${edge()}", edge() > 40)
    }

    @Test fun longPressThenDragDoesNotOpenTheDrawer() {
        show(reduce = true) { Box(Modifier.fillMaxSize()) }
        rule.onNodeWithTag("shell").performTouchInput {
            down(Offset(80f, 240f))
            advanceEventTime(viewConfiguration.longPressTimeoutMillis)
            moveBy(Offset(160f, 0f))
            up()
        }
        frame()
        assertEquals("long-press edge ${edge()}", 0f, edge().toFloat(), 4f)
        drag(from = Offset(80f, 300f), by = Offset(140f, 0f), hold = true)
        frame()
        assertTrue("a quick drag still opens, edge ${edge()}", edge() > 40)
    }

    @Test fun rtlHorizontalScrollerAtTheLeftKeepsTheDrag() {
        // A rightward drag in RTL increases ScrollState (measured 270 to 540). Value 0 still has room,
        // so the scroller keeps it. `horizontalScroll` does not fold layout direction into reverseScrolling.
        val scroll = ScrollState(0)
        var opened by mutableStateOf(false)
        show(
            reduce = true,
            layoutDirection = LayoutDirection.Rtl,
            opened = { opened },
            onOpen = { opened = true },
        ) {
            Box(Modifier.horizontalScroll(scroll)) {
                Box(Modifier.width(900.dp).height(120.dp).background(Color.Yellow))
            }
        }
        assertEquals(0, scroll.value)
        drag(from = Offset(120f, 40f), by = Offset(90f, 0f))
        rule.mainClock.advanceTimeBy(80)
        assertTrue(!opened)
        assertTrue("the scroller moved ${scroll.value}", scroll.value != 0)
    }

    @Test fun rtlHorizontalScrollerAtItsStartOpensTheDrawer() {
        // The high end is the start in RTL: a rightward drag there has no room, so the drawer takes it.
        val scroll = ScrollState(0)
        var opened by mutableStateOf(false)
        var ready by mutableStateOf(false)
        show(
            reduce = true,
            layoutDirection = LayoutDirection.Rtl,
            opened = { opened },
            onOpen = { opened = true },
        ) {
            LaunchedEffect(scroll.maxValue) {
                if (scroll.maxValue > 0) {
                    scroll.scrollTo(scroll.maxValue)
                    ready = true
                }
            }
            Box(Modifier.horizontalScroll(scroll)) {
                Box(Modifier.width(900.dp).height(120.dp).background(Color.Yellow))
            }
        }
        var spins = 0
        while (!ready && spins < 40) {
            rule.mainClock.advanceTimeByFrame()
            spins++
        }
        assertTrue("not at the rtl start ${scroll.value}/${scroll.maxValue}", ready)
        val atEnd = scroll.value
        drag(from = Offset(120f, 40f), by = Offset(180f, 0f))
        rule.mainClock.advanceTimeBy(80)
        assertTrue("rtl start did not open, scroll ${scroll.value}", opened)
        assertEquals(atEnd, scroll.value)
    }

    @Test fun buttonCloseUnderReduceMotionSnaps() {
        var opened by mutableStateOf(true)
        show(reduce = true, opened = { opened }, onDismiss = { opened = false }) { Box(Modifier.fillMaxSize()) }
        rule.mainClock.advanceTimeBy(50)
        assertTrue("open ${edge()}", edge() > 200)
        opened = false
        Snapshot.sendApplyNotifications()
        // A few frames, not the 0.32 s spring. A snap is at 0; the spring would still be most of the way open.
        repeat(6) { rule.mainClock.advanceTimeByFrame() }
        assertEquals("button edge ${edge()}", 0f, edge().toFloat(), 2f)
    }

    @Test fun unfoldingSnapsTheOpenDrawer() {
        var opened by mutableStateOf(true)
        var enabled by mutableStateOf(true)
        show(
            style = AppStyle.Glass,
            reduce = false,
            dragEnabledOf = { enabled },
            opened = { opened },
            onDismiss = { opened = false },
        ) { Box(Modifier.fillMaxSize()) }
        assertTrue("open ${edge()}", edge() > 200)
        enabled = false
        opened = false
        Snapshot.sendApplyNotifications()
        repeat(6) { rule.mainClock.advanceTimeByFrame() }
        assertEquals("unfold edge ${edge()}", 0f, edge().toFloat(), 2f)
    }

    @Test fun leftwardDragOnTheOpenPageCloses() {
        var opened by mutableStateOf(true)
        show(reduce = true, opened = { opened }, onDismiss = { opened = false }) { Box(Modifier.fillMaxSize()) }
        rule.mainClock.advanceTimeBy(50)
        val openEdge = edge()
        drag(from = Offset(openEdge + 30f, 300f), by = Offset(-160f, 0f))
        rule.mainClock.advanceTimeBy(400)
        assertEquals(0f, edge().toFloat(), 4f)
        assertTrue(!opened)
    }

    @Test fun pushedPageDoesNotDrag() {
        show(reduce = true, dragEnabled = false) { Box(Modifier.fillMaxSize()) }
        drag(from = Offset(80f, 200f), by = Offset(180f, 0f))
        assertEquals(0f, edge().toFloat(), 3f)
    }

    @Test fun leftEdgeOpensAtRootAndTrailingInsetStaysWithBack() {
        // Left edge at a root opens the drawer (#9803, `SwipeBack.swift:11-16`); the trailing inset still refuses.
        lateinit var host: View
        var opened by mutableStateOf(false)
        show(
            reduce = true,
            opened = { opened },
            onOpen = { opened = true },
            onDismiss = { opened = false },
            onView = { host = it },
        ) { Box(Modifier.fillMaxSize()) }
        val insets = WindowInsetsCompat.Builder()
            .setInsets(WindowInsetsCompat.Type.systemGestures(), Insets.of(48, 0, 48, 0))
            .setInsets(WindowInsetsCompat.Type.mandatorySystemGestures(), Insets.of(48, 0, 48, 0))
            .build()
        rule.runOnIdle { ViewCompat.dispatchApplyWindowInsets(host, insets) }
        rule.mainClock.advanceTimeBy(50)
        // Inside the centred 200 dp band (shell 640 tall → mid ≈ 320).
        drag(from = Offset(10f, 320f), by = Offset(160f, 0f))
        rule.mainClock.advanceTimeBy(400)
        assertTrue("left edge at root, edge ${edge()}", edge() > 200)
        opened = false
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(400)
        assertEquals(0f, edge().toFloat(), 4f)
        val widthPx = with(rule.density) { shellWidth.toPx() }
        drag(from = Offset(widthPx - 40f, 280f), by = Offset(80f, 0f))
        assertEquals(0f, edge().toFloat(), 3f)
    }

    @Test fun leftInsetOutsideTheBandIsRefused() {
        // Android only grants the excluded band; a left-inset down outside it must stay with Back.
        lateinit var host: View
        show(reduce = true, onView = { host = it }) { Box(Modifier.fillMaxSize()) }
        val insets = WindowInsetsCompat.Builder()
            .setInsets(WindowInsetsCompat.Type.systemGestures(), Insets.of(48, 0, 48, 0))
            .setInsets(WindowInsetsCompat.Type.mandatorySystemGestures(), Insets.of(48, 0, 48, 0))
            .build()
        rule.runOnIdle { ViewCompat.dispatchApplyWindowInsets(host, insets) }
        rule.mainClock.advanceTimeBy(50)
        drag(from = Offset(10f, 20f), by = Offset(160f, 0f))
        assertEquals(0f, edge().toFloat(), 3f)
        drag(from = Offset(10f, 320f), by = Offset(160f, 0f))
        rule.mainClock.advanceTimeBy(400)
        assertTrue("inside band, edge ${edge()}", edge() > 200)
    }

    @Test fun glassCarriesTheFingerVelocity() {
        val (slow, fast) = pairedTravel(AppStyle.Glass)
        // Same clock on both: the spring that kept the finger's speed is further along.
        assertTrue("glass slow $slow fast $fast", fast.second > slow.second + 6)
    }

    @Test fun briefIgnoresFingerVelocity() {
        val (slow, fast) = pairedTravel(AppStyle.Brief)
        // Brief's tween ignores the finger's speed, so the two releases are in the same place.
        assertTrue("brief slow $slow fast $fast", abs(fast.second - slow.second) < 14)
    }

    @Test fun reducedMotionCarriesTheFingerVelocity() {
        // `Theme.Motion.drawer` (`Theme.swift:799-801`) does not read Reduce Motion. A fast flick is further
        // along than a slow one. An instant tween would leave both already open, in the same place.
        val (slow, fast) = pairedTravel(AppStyle.Glass, reduce = true)
        assertTrue("reduce slow $slow fast $fast", fast.second > slow.second + 6)
    }

    @Test fun fingerTakesOverMidSpring() {
        var opened by mutableStateOf(false)
        show(style = AppStyle.Glass, reduce = false, opened = { opened }, onOpen = { opened = true }) {
            Box(Modifier.fillMaxSize())
        }
        drag(from = Offset(60f, 220f), by = Offset(40f, 0f), delayMillis = 5)
        rule.mainClock.advanceTimeBy(40)
        assertTrue("mid ${edge()}", edge() > 30)
        drag(from = Offset(70f, 300f), by = Offset(24f, 0f), hold = true)
        frame()
        val held = edge()
        rule.mainClock.advanceTimeBy(280)
        assertEquals(held.toFloat(), edge().toFloat(), 2f)
    }

    @Test fun hapticOnlyWhenAClosedDragSettlesOpen() {
        val recorded = RecordingHaptics()
        var opened by mutableStateOf(false)
        show(
            reduce = true,
            opened = { opened },
            onOpen = { opened = true },
            onDismiss = { opened = false },
            haptics = recorded,
        ) { Box(Modifier.fillMaxSize()) }
        drag(from = Offset(60f, 220f), by = Offset(40f, 0f), delayMillis = 5)
        rule.mainClock.advanceTimeBy(32)
        assertEquals(listOf(HapticFeedbackType.ContextClick), recorded.types)
        recorded.types.clear()
        opened = false
        rule.mainClock.advanceTimeBy(400)
        assertTrue(recorded.types.isEmpty())
        // Slow, and short of half: projection closes, and a drag that does not open plays nothing (line 110).
        drag(from = Offset(60f, 260f), by = Offset(70f, 0f), delayMillis = 400)
        rule.mainClock.advanceTimeBy(400)
        assertEquals(0f, edge().toFloat(), 4f)
        assertTrue(recorded.types.isEmpty())
    }

    /** Slow settle, then a flick, each from about 0.7 of the width, sampled 40 ms later. One composition. */
    private fun pairedTravel(style: AppStyle, reduce: Boolean = false): Pair<Pair<Int, Int>, Pair<Int, Int>> {
        var opened by mutableStateOf(false)
        show(
            style = style,
            reduce = reduce,
            opened = { opened },
            onOpen = { opened = true },
            onDismiss = { opened = false },
        ) { Box(Modifier.fillMaxSize()) }
        val slow = travel(fast = false)
        opened = false
        rule.mainClock.advanceTimeBy(800)
        assertEquals("did not reclose from $slow", 0f, edge().toFloat(), 6f)
        return slow to travel(fast = true)
    }

    /** Where the page is 16 ms after release, and again 64 ms after release. */
    private fun travel(fast: Boolean): Pair<Int, Int> {
        val distance = drawerPx() * 0.7f
        // About 200 px in 120 ms: faster than 300 dp/s, slow enough for the 0.32 s spring to stay on screen.
        if (fast) drag(from = Offset(40f, 200f), by = Offset(distance, 0f), delayMillis = 120)
        else drag(from = Offset(40f, 200f), by = Offset(distance, 0f), delayMillis = 16, settleMillis = 400)
        frame()
        val start = edge()
        rule.mainClock.advanceTimeBy(48)
        return start to edge()
    }

    private fun frame() {
        rule.mainClock.advanceTimeBy(16)
    }

    private fun show(
        style: AppStyle = AppStyle.Glass,
        reduce: Boolean = true,
        dragEnabled: Boolean = true,
        dragEnabledOf: (() -> Boolean)? = null,
        opened: () -> Boolean = { false },
        onOpen: () -> Unit = {},
        onDismiss: () -> Unit = {},
        haptics: HapticFeedback? = null,
        layoutDirection: LayoutDirection = LayoutDirection.Ltr,
        onView: (View) -> Unit = {},
        drawer: @Composable () -> Unit = { Box(Modifier.fillMaxSize().background(sidebar)) },
        content: @Composable () -> Unit,
    ) {
        rule.mainClock.autoAdvance = false
        rule.setContent {
            onView(LocalView.current)
            val shell: @Composable () -> Unit = {
                CompositionLocalProvider(LocalLayoutDirection provides layoutDirection) {
                PersonaTheme(darkTheme = false, reduceMotion = reduce, style = style) {
                    JShellDrawer(
                        opened(),
                        onDismiss = onDismiss,
                        modifier = Modifier.testTag("shell").width(shellWidth).height(shellHeight),
                        dragEnabled = dragEnabledOf?.invoke() ?: dragEnabled,
                        onOpen = onOpen,
                        drawer = drawer,
                    ) {
                        Box(Modifier.fillMaxSize().background(page)) { content() }
                    }
                }
                }
            }
            if (haptics == null) shell()
            else CompositionLocalProvider(LocalHapticFeedback provides haptics) { shell() }
        }
        rule.mainClock.advanceTimeBy(100)
    }

    private fun drag(
        from: Offset,
        by: Offset,
        hold: Boolean = false,
        delayMillis: Long = 16,
        settleMillis: Long = 0,
    ) {
        rule.onNodeWithTag("shell").performTouchInput {
            down(from)
            // Lsq2 needs three samples. One moveBy plus the down is not a flick.
            val steps = 6
            val piece = by / steps.toFloat()
            val each = (delayMillis / steps).coerceAtLeast(1)
            repeat(steps) { moveBy(piece, delayMillis = each) }
            if (settleMillis > 0) moveBy(Offset(1f, 0f), delayMillis = settleMillis)
            if (!hold) up()
        }
    }

    private fun drawerPx(): Float {
        val width = with(rule.density) { shellWidth.toPx() }
        val cap = with(rule.density) { HomeTokens.drawerMaximum.toPx() }
        return min(width * HomeTokens.drawerFraction, cap)
    }

    /** Left edge of the blue page at mid-height, in px. The test window is mdpi, so px and dp match. */
    private fun edge(): Int {
        val map = rule.onNodeWithTag("shell").captureToImage().toPixelMap()
        val y = map.height / 2
        for (x in 0 until map.width) {
            val color = map[x, y]
            if (color.blue > color.red + 0.15f && color.alpha > 0.5f) return x
        }
        return -1
    }
}

/** A slider or a text selection: it consumes horizontal moves and does not scroll. */
@Composable
private fun ConsumingRow(onPull: () -> Unit) {
    Box(Modifier.fillMaxSize().pointerInput(Unit) {
        awaitEachGesture {
            val down = awaitFirstDown(requireUnconsumed = false)
            while (true) {
                val event = awaitPointerEvent()
                val change = event.changes.firstOrNull { it.id == down.id } ?: break
                if (!change.pressed) break
                if (change.position.x != change.previousPosition.x) {
                    onPull()
                    change.consume()
                }
            }
        }
    })
}

/** An Android view with no sideways room. `canScrollHorizontally` stays false. */
private class StillView(context: android.content.Context) : View(context)

/** Room toward the start while [room] is set (`View.canScrollHorizontally(-1)`, AssistantSidebar.swift:558). */
private class RoomView(context: android.content.Context) : View(context) {
    var room: Boolean = true
    override fun canScrollHorizontally(direction: Int): Boolean = room && direction < 0
}

private class RecordingHaptics : HapticFeedback {
    val types = mutableListOf<HapticFeedbackType>()
    override fun performHapticFeedback(hapticFeedbackType: HapticFeedbackType) {
        types += hapticFeedbackType
    }
}

package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.ui.Alignment
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.unit.Dp
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
import kotlin.math.abs
import kotlin.math.min

/**
 * Resting pixels against the drawer's look before the finger-tracking change, and the three poses the issue asks
 * for. The oracle is that previous drawing, copied here so the compare does not depend on a second checkout.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w480dp-h1100dp-mdpi")
class DrawerDragShotsTest {
    @get:Rule val rule = createComposeRule()

    private val outDir = File("build/drawer-drag-shots").also { it.mkdirs() }
    private val phoneWidth = 411.dp
    private val phoneHeight = 891.dp
    private val foldedHeight = 900.dp

    @Test fun restingClosedAndOpenMatchThePreviousDrawer() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var opened by mutableStateOf(false)
        var showOracle by mutableStateOf(true)
        // One slot. Stacking the two drawers puts the spot shadow at two screen positions, and Android's
        // light then paints the rounded corner a gray level apart even when the drawing is the same.
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(360.dp).height(400.dp)) {
                    if (showOracle) {
                        RestingDrawer(opened, Modifier.fillMaxSize())
                    } else {
                        JShellDrawer(
                            opened,
                            onDismiss = {},
                            modifier = Modifier.fillMaxSize(),
                            dragEnabled = true,
                            drawer = { Swatch(Color.Red) },
                        ) { Swatch(Color.Blue) }
                    }
                }
            }
        }
        val report = File(outDir, "pixel-compare.txt")
        report.writeText("style\ttheme\tpose\tpixels\n")
        for (current in AppStyle.entries) {
            for (isDark in listOf(false, true)) {
                for (isOpen in listOf(false, true)) {
                    rule.runOnIdle {
                        style = current
                        dark = isDark
                        opened = isOpen
                        showOracle = true
                    }
                    rule.waitForIdle()
                    val oracle = pixels("shot")
                    rule.runOnIdle { showOracle = false }
                    rule.waitForIdle()
                    val subject = pixels("shot")
                    val same = oracle.sameAs(subject)
                    val pose = if (isOpen) "open" else "closed"
                    val theme = if (isDark) "dark" else "light"
                    if (!same) {
                        File(outDir, "oracle-$current-$theme-$pose.png").outputStream().use {
                            oracle.compress(Bitmap.CompressFormat.PNG, 100, it)
                        }
                        File(outDir, "subject-$current-$theme-$pose.png").outputStream().use {
                            subject.compress(Bitmap.CompressFormat.PNG, 100, it)
                        }
                    }
                    val diff = if (same) "0" else firstDiff(oracle, subject)
                    report.appendText("$current\t$theme\t$pose\t$diff\n")
                    assertTrue("$current $theme $pose $diff", same)
                }
            }
        }
    }

    @Test fun glassPosesPhoneAndFolded() {
        rule.mainClock.autoAdvance = false
        var dark by mutableStateOf(false)
        var width by mutableStateOf(phoneWidth)
        var height by mutableStateOf(phoneHeight)
        var opened by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = AppStyle.Glass) {
                JShellDrawer(
                    opened,
                    onDismiss = { opened = false },
                    modifier = Modifier.testTag("shell").width(width).height(height),
                    dragEnabled = true,
                    onOpen = { opened = true },
                    drawer = { ShotSidebar() },
                ) { ShotChat() }
            }
        }
        val cases = listOf(
            Pose(false, phoneWidth, phoneHeight, "phone-411"),
            Pose(false, phoneWidth, foldedHeight, "folded-411x900"),
            Pose(true, phoneWidth, phoneHeight, "phone-411"),
            Pose(true, phoneWidth, foldedHeight, "folded-411x900"),
        )
        for (pose in cases) {
            // autoAdvance is off, so a state write sits until a frame is requested (PageTransitionsTest).
            rule.runOnIdle {
                dark = pose.dark
                width = pose.width
                height = pose.height
                opened = false
            }
            Snapshot.sendApplyNotifications()
            repeat(2) { rule.mainClock.advanceTimeByFrame() }
            // The drawer spring is 0.32 s even with Reduce Motion (Theme.swift:799-801). Let a previous pose rest.
            rule.mainClock.advanceTimeBy(400)
            val theme = if (pose.dark) "dark" else "light"
            val closed = capture()
            write("glass-$theme-${pose.label}-closed", closed)
            val drawer = drawerPx(pose.width)
            // The marker sits on the left of the chat, below the page corner, so a rightward drag slides it.
            rule.onNodeWithTag("shell").performTouchInput {
                down(Offset(48f, 420f))
                moveBy(Offset(drawer / 2f, 0f))
                moveBy(Offset(1f, 0f), delayMillis = 16)
            }
            rule.mainClock.advanceTimeBy(16)
            val midBitmap = capture()
            write("glass-$theme-${pose.label}-mid", midBitmap)
            val mid = pageShift(closed, midBitmap)
            assertEquals("${pose.label} mid", drawer / 2f, mid.toFloat(), 16f)
            // Exactly half with no velocity closes (`SwipeRelease.goes` is strict). Stay in this gesture and go past half.
            rule.onNodeWithTag("shell").performTouchInput {
                moveBy(Offset(drawer * 0.25f, 0f), delayMillis = 32)
                up()
            }
            rule.mainClock.advanceTimeBy(400)
            val openBitmap = capture()
            write("glass-$theme-${pose.label}-open", openBitmap)
            val open = pageShift(closed, openBitmap)
            assertTrue("${pose.label} open shift $open drawer $drawer", open > drawer - 16)
        }
    }

    @Test fun glassSettleIsMidSpringOnTheClock() {
        rule.mainClock.autoAdvance = false
        var dark by mutableStateOf(false)
        var width by mutableStateOf(phoneWidth)
        var height by mutableStateOf(phoneHeight)
        var opened by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = false, style = AppStyle.Glass) {
                JShellDrawer(
                    opened,
                    onDismiss = { opened = false },
                    modifier = Modifier.testTag("shell").width(width).height(height),
                    dragEnabled = true,
                    onOpen = { opened = true },
                    drawer = { ShotSidebar() },
                ) { ShotChat() }
            }
        }
        val cases = listOf(
            Pose(false, phoneWidth, phoneHeight, "phone-411"),
            Pose(false, phoneWidth, foldedHeight, "folded-411x900"),
            Pose(true, phoneWidth, phoneHeight, "phone-411"),
            Pose(true, phoneWidth, foldedHeight, "folded-411x900"),
        )
        for (pose in cases) {
            val theme = if (pose.dark) "dark" else "light"
            rule.runOnIdle {
                dark = pose.dark
                width = pose.width
                height = pose.height
                opened = false
            }
            Snapshot.sendApplyNotifications()
            repeat(2) { rule.mainClock.advanceTimeByFrame() }
            // The close is the 0.32 s drawer spring (Theme.swift:799-801). 400 ms lets it rest before the next size.
            rule.mainClock.advanceTimeBy(400)
            val closed = capture()
            val drawer = drawerPx(pose.width)
            val resting = pageShift(closed, closed)
            assertEquals("${pose.label} not closed", 0f, resting.toFloat(), 4f)
            // Past half, slow enough that the critically damped spring does not leave the bitmap.
            // A ~10000 dp/s flick's share sends Compose past 1; iOS does not clamp either, but the shot must stay on screen.
            val flick = drawer * 0.55f
            rule.onNodeWithTag("shell").performTouchInput {
                down(Offset(48f, 420f))
                val steps = 8
                repeat(steps) { moveBy(Offset(flick / steps, 0f), delayMillis = 18) }
                up()
            }
            rule.mainClock.advanceTimeBy(48)
            val midBitmap = capture()
            write("glass-$theme-${pose.label}-settling", midBitmap)
            val mid = pageShift(closed, midBitmap)
            rule.mainClock.advanceTimeBy(700)
            val end = pageShift(closed, capture())
            assertTrue(
                "${pose.label} settling mid=$mid end=$end drawer=$drawer",
                mid > drawer * 0.55f && mid < end - 8 && end > drawer - 16,
            )
        }
    }

    /** The drawer as it drew before finger tracking: same layers, no drag, no lift, hairline only while out. */
    @Composable
    private fun RestingDrawer(opened: Boolean, modifier: Modifier) {
        val progress by animateFloatAsState(
            if (opened) 1f else 0f,
            drawerSpec(PersonaTheme.style, PersonaTheme.reduceMotion),
            label = "oracle-drawer",
        )
        val palette = PersonaTheme.palette
        val density = LocalDensity.current
        val shape = RoundedCornerShape(HomeTokens.drawerPageRadius)
        val close = stringResource(R.string.designsystem_close)
        val background = if (PersonaTheme.style == AppStyle.Glass) {
            if (PersonaTheme.darkTheme) HomeTokens.drawerDark else HomeTokens.drawerLight
        } else palette.surfaceOpaque
        BoxWithConstraints(modifier.fillMaxSize().background(background)) {
            val drawerWidth = (maxWidth * HomeTokens.drawerFraction).coerceAtMost(HomeTokens.drawerMaximum)
            HardwareShortcutHost(Modifier.fillMaxSize()) {
                if (opened || progress > 0f) {
                    Box(Modifier.width(drawerWidth).fillMaxHeight().statusBarsPadding().navigationBarsPadding()) {
                        Swatch(Color.Red)
                    }
                }
                Box(Modifier.fillMaxSize().graphicsLayer {
                    translationX = with(density) { drawerWidth.toPx() } * progress
                    this.shape = shape
                    clip = progress > 0f
                    shadowElevation = with(density) { HomeTokens.drawerShadow.toPx() } * progress
                    ambientShadowColor = Color.Black.copy(alpha = HomeTokens.drawerShadowAlpha)
                    spotShadowColor = Color.Black.copy(alpha = HomeTokens.drawerShadowAlpha)
                }.then(if (progress > 0f) Modifier.border(Tokens.Space.Standard.xxs / 2, palette.line, shape) else Modifier)) {
                    Box(Modifier.fillMaxSize().then(if (opened)
                        Modifier.focusProperties { canFocus = false }.clearAndSetSemantics {} else Modifier)) { Swatch(Color.Blue) }
                    if (opened || progress > 0f) Box(Modifier.fillMaxSize().background(palette.scrim.copy(alpha = palette.scrim.alpha * progress))
                        .clickable(enabled = opened, role = Role.Button, onClick = {}).semantics { contentDescription = close })
                }
            }
        }
    }

    @Composable
    private fun Swatch(color: Color) {
        Box(Modifier.fillMaxSize().background(color))
    }

    /** A sidebar list of chats, the same rows the Assistant sidebar draws. */
    @Composable
    private fun ShotSidebar() {
        Column(
            Modifier.fillMaxSize().padding(Tokens.Space.m),
            verticalArrangement = Arrangement.spacedBy(Tokens.Space.s),
        ) {
            JSidebarNewChat("New chat", onClick = {})
            JSidebarHistoryRow("Trip notes", Status.Idle, selected = true, onClick = {}, onMenu = {}, menuLabel = "More")
            JSidebarHistoryRow("Grocery list", Status.Unread, selected = false, onClick = {}, onMenu = {}, menuLabel = "More")
            JSidebarHistoryRow(
                "Weekend plan",
                Status.NeedsYou,
                selected = false,
                onClick = {},
                onMenu = {},
                menuLabel = "More",
                hint = "Pack the bags",
            )
        }
    }

    /**
     * A short chat. The first bubble is the marker [pageShift] follows: it sits on the left, below the drawer's
     * 55 dp page corner, so it stays on screen when the page has slid by the drawer width.
     */
    @Composable
    private fun ShotChat() {
        val palette = PersonaTheme.palette
        val type = PersonaTheme.type
        Column(
            Modifier.fillMaxSize().background(palette.background).padding(Tokens.Space.l),
            verticalArrangement = Arrangement.spacedBy(Tokens.Space.m),
        ) {
            Spacer(Modifier.height(Tokens.Space.xl * 3))
            Text("Saturday", style = type.caption, color = palette.ink2)
            Text(
                "What is left for the trip?",
                style = type.body,
                color = palette.userBubbleText ?: palette.ink,
                modifier = Modifier.background(palette.userBubble, RoundedCornerShape(Tokens.Radius.bubble))
                    .padding(Tokens.Space.s),
            )
            Text("Pack the bags and send the list before noon.", style = type.body, color = palette.ink)
            Text("The train is at four.", style = type.callout, color = palette.ink2, modifier = Modifier.align(Alignment.End))
        }
    }

    private fun pixels(tag: String): Bitmap = rule.onNodeWithTag(tag).captureToImage().asAndroidBitmap()
        .copy(Bitmap.Config.ARGB_8888, false)

    private fun firstDiff(a: Bitmap, b: Bitmap): String {
        if (a.width != b.width || a.height != b.height) return "size ${a.width}x${a.height} vs ${b.width}x${b.height}"
        var count = 0
        var first = ""
        for (y in 0 until a.height) {
            for (x in 0 until a.width) {
                val av = a.getPixel(x, y)
                val bv = b.getPixel(x, y)
                if (av != bv) {
                    count++
                    if (count == 1) first = "x=$x y=$y $av vs $bv"
                }
            }
        }
        return if (count == 0) "0" else "$first count=$count"
    }

    private fun capture(): Bitmap = rule.onNodeWithTag("shell").captureToImage().asAndroidBitmap()
        .copy(Bitmap.Config.ARGB_8888, false)

    private fun write(name: String, bitmap: Bitmap) {
        File(outDir, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private fun drawerPx(width: Dp): Float {
        val px = with(rule.density) { width.toPx() }
        val cap = with(rule.density) { HomeTokens.drawerMaximum.toPx() }
        return min(px * HomeTokens.drawerFraction, cap)
    }

    /**
     * How far the chat page has moved, in px. The closed frame is the template: the interior of the left-hand
     * bubble (it starts near y=127). A patch below the bubble is page-colored on both sides of the slide, so it
     * matches the unmoved page. The scrim tints the bubble, and the same patch is still the closest match.
     */
    private fun pageShift(closed: Bitmap, now: Bitmap): Int {
        val templateX = 20
        val templateW = 24
        val y0 = 136
        val y1 = min(154, closed.height)
        var bestX = templateX
        var best = Long.MAX_VALUE
        val maxX = min(closed.width, now.width) - templateW
        var x = 0
        while (x <= maxX) {
            var sad = 0L
            var y = y0
            while (y < y1) {
                var dx = 0
                while (dx < templateW) {
                    val a = closed.getPixel(templateX + dx, y)
                    val b = now.getPixel(x + dx, y)
                    sad += abs((a shr 16 and 0xFF) - (b shr 16 and 0xFF))
                    sad += abs((a shr 8 and 0xFF) - (b shr 8 and 0xFF))
                    // `and` is infix and loses to subtraction, so the blue term needs its own parentheses.
                    sad += abs((a and 0xFF) - (b and 0xFF))
                    dx += 2
                }
                y += 2
            }
            if (sad < best) {
                best = sad
                bestX = x
            }
            // Step one pixel. A two-pixel step misses the bubble's glyphs and locks onto a worse match.
            x += 1
        }
        return bestX - templateX
    }
}

private data class Pose(val dark: Boolean, val width: Dp, val height: Dp, val label: String)

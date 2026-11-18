package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Composer
import androidx.compose.runtime.CompositionTracer
import androidx.compose.runtime.InternalComposeTracingApi
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import kotlin.math.abs

/**
 * [JSpinner] reads its angle only while drawing: a turn recomposes nothing, where reading it while composing recomposed the
 * spinner every frame. It looks and turns as it did then: [ComposedSpinner] keeps that drawing, and the two are caught side by
 * side, frame by frame, in every style and with motion reduced.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
@OptIn(InternalComposeTracingApi::class)
class JSpinnerFrameTest {
    @get:Rule val rule = createComposeRule()

    @After fun stopTracing() = Composer.setTracer(null)

    @Test fun aTurnRecomposesNothing() {
        var compositions = 0
        var composedSpinner = 0
        Composer.setTracer(object : CompositionTracer {
            override fun traceEventStart(key: Int, dirty1: Int, dirty2: Int, info: String) {
                if (info.startsWith("persona.core.designsystem.JSpinner ")) compositions++
                if (info.startsWith("persona.core.designsystem.ComposedSpinner ")) composedSpinner++
            }
            override fun traceEventEnd() {}
            override fun isTraceInProgress() = true
        })
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = false, style = AppStyle.Glass) {
                Box(Modifier.testTag("now").background(PersonaTheme.palette.background).padding(8.dp)) { Row { JSpinner(); ComposedSpinner() } }
            }
        }
        rule.mainClock.advanceTimeByFrame()
        val composed = compositions
        val composedBefore = composedSpinner
        assertTrue("the trace saw JSpinner compose", composed >= 1)
        val before = shot("now")
        // A second of frames; one turn is 1.1 s, so the angle at the end is not the angle at the start.
        repeat(60) { rule.mainClock.advanceTimeByFrame() }
        assertFalse("the spinner turned", before.sameAs(shot("now")))
        assertEquals("JSpinner composed again while it turned", composed, compositions)
        // The trace does see a spinner that reads its angle while composing: it composes with nearly every frame.
        assertTrue("ComposedSpinner composed ${composedSpinner - composedBefore} times", composedSpinner - composedBefore >= 50)
    }

    @Test fun looksAndTurnsAsBefore() {
        var case by mutableStateOf(Case(AppStyle.Glass, dark = false, reduced = false))
        rule.mainClock.autoAdvance = false
        rule.setContent {
            // A fresh pair for each case, both starting on the same frame.
            key(case) {
                PersonaTheme(darkTheme = case.dark, reduceMotion = case.reduced, style = case.style) {
                    Row(Modifier.background(PersonaTheme.palette.background).padding(8.dp)) {
                        Box(Modifier.testTag("now")) { Spinners { m, s -> JSpinner(m, stroke = s) } }
                        Box(Modifier.testTag("before")) { Spinners { m, s -> ComposedSpinner(m, stroke = s) } }
                    }
                }
            }
        }
        val worst = mutableMapOf<String, Int>()
        for (style in AppStyle.entries) for (dark in listOf(false, true)) for (reduced in listOf(false, true)) {
            case = Case(style, dark, reduced)
            rule.waitForIdle()
            rule.mainClock.advanceTimeByFrame()
            var elapsed = 0L
            // Through a whole turn, at times that fall between frames as well as on them. (Past it the two transitions, each
            // its own, may start their second turn a fraction of a degree apart.)
            for (at in listOf(0L, 96L, 275L, 413L, 550L, 707L, 825L, 1013L, 1100L)) {
                rule.mainClock.advanceTimeBy(at - elapsed)
                elapsed = at
                val now = shot("now")
                val before = shot("before")
                worst["$case@$at"] = differing(now, before)
            }
        }
        // The same arc at the same angle, pixel for pixel (the page behind the two may round a channel one step apart).
        assertEquals(emptyMap<String, Int>(), worst.filterValues { it > 0 })
    }

    private data class Case(val style: AppStyle, val dark: Boolean, val reduced: Boolean)

    /** The callers' sizes: the default, a row's small one with a thin line, a page's large one. */
    @Composable private fun Spinners(spinner: @Composable (Modifier, Dp) -> Unit) = Row {
        spinner(Modifier, 2.2.dp)
        spinner(Modifier.padding(4.dp).size(14.dp), 1.8.dp)
        spinner(Modifier.padding(4.dp).size(28.dp), 2.2.dp)
    }

    private fun shot(tag: String): Bitmap = rule.onNodeWithTag(tag).captureToImage().asAndroidBitmap()

    /** Pixels whose channels differ by more than [Tolerance] between [a] and [b], which are the same size. */
    private fun differing(a: Bitmap, b: Bitmap): Int {
        assertEquals(a.width to a.height, b.width to b.height)
        var count = 0
        for (y in 0 until a.height) for (x in 0 until a.width) {
            val p = a.getPixel(x, y)
            val q = b.getPixel(x, y)
            if ((0..24 step 8).any { abs((p shr it and 0xff) - (q shr it and 0xff)) > Tolerance }) count++
        }
        return count
    }
}

private const val Tolerance = 1

/** [JSpinner] as it was when it read its angle while composing: the arc drawn from it. */
@Composable
private fun ComposedSpinner(
    modifier: Modifier = Modifier,
    color: Color = if (PersonaTheme.style == AppStyle.Brief) PersonaTheme.palette.ink else PersonaTheme.palette.accent,
    stroke: Dp = 2.2.dp,
) {
    val turning = !PersonaTheme.reduceMotion
    val angle = if (turning) rememberInfiniteTransition(label = "spin").animateFloat(0f, 360f, infiniteRepeatable(tween(1100, easing = LinearEasing)), label = "angle").value else 0f
    Canvas(modifier.size(20.dp)) {
        val width = stroke.toPx()
        val inset = width / 2
        val arcSize = Size(size.width - width, size.height - width)
        drawArc(color.copy(alpha = 0.2f), 0f, 360f, false, Offset(inset, inset), arcSize, style = Stroke(width))
        drawArc(color, angle - 90f, 100f, false, Offset(inset, inset), arcSize, style = Stroke(width, cap = StrokeCap.Round))
    }
}

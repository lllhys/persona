package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertHeightIsEqualTo
import androidx.compose.ui.test.assertTopPositionInRootIsEqualTo
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import dev.chrisbanes.haze.HazeState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * The top overlay stays at full effect strength through its 80 dp of chrome; only the next 28 dp fade into the content.
 * Native Robolectric exercises the actual Liquid and Solid branches in light and dark, isolating the wash and its
 * mask. It does not render the device blur itself. The height assertion also guards the local coordinates supplied to
 * Haze. Native, 1 px per dp.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-mdpi")
class ScrollEdgeTest {
    @get:Rule val rule = createComposeRule()

    @Test fun topOverlayIsWashedToItsBoundaryThenFadesFor28Dp() {
        rule.setContent {
            Column {
                for (glass in listOf(GlassLevel.Liquid, GlassLevel.Solid)) {
                    for (dark in listOf(false, true)) {
                        PersonaTheme(darkTheme = dark, reduceMotion = true, style = AppStyle.Glass) {
                            CompositionLocalProvider(
                                LocalGlassLevel provides glass,
                                LocalGlassSource provides remember { HazeState() },
                            ) {
                                Box(Modifier.testTag("shot glass=$glass dark=$dark").size(120.dp, 160.dp).background(Content)) {
                                    ScrollEdge(
                                        Modifier.align(Alignment.TopCenter).testTag("edge glass=$glass dark=$dark"),
                                        height = 80.dp,
                                        top = true,
                                        light = Ambience.Calm,
                                    )
                                }
                            }
                        }
                    }
                }
            }
        }
        rule.waitForIdle()

        for (glass in listOf(GlassLevel.Liquid, GlassLevel.Solid)) {
            for (dark in listOf(false, true)) {
                val tag = "glass=$glass dark=$dark"
                rule.onNodeWithTag("edge $tag").assertHeightIsEqualTo(108.dp)
                val bitmap = rule.onNodeWithTag("shot $tag").captureToImage().asAndroidBitmap()
                // The old edge was almost transparent at 79 dp. Full-strength wash must still cover that content boundary.
                val atBoundary = bitmap.getPixel(10, 79)
                val nearFadeEnd = bitmap.getPixel(10, 107)
                if (dark) {
                    assertTrue("$tag: dark wash still covers the overlay boundary", android.graphics.Color.blue(atBoundary) < 160)
                    assertTrue("$tag: the extra 28 dp return to the content colour", android.graphics.Color.blue(nearFadeEnd) > 215)
                } else {
                    assertTrue("$tag: light wash still covers the overlay boundary", android.graphics.Color.red(atBoundary) > 100)
                    assertTrue("$tag: the extra 28 dp return to the content colour", android.graphics.Color.red(nearFadeEnd) < 40)
                }
            }
        }
    }

    @Test fun pageGradientsStayWithinBothBarsAtEveryGlassLevel() {
        val levels = listOf(GlassLevel.Liquid, GlassLevel.Frosted, GlassLevel.Solid)
        val level = mutableStateOf(levels.first())
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(
                    LocalGlassLevel provides level.value,
                    LocalChromeInsets provides ChromeInsets(handlesNavigationBar = true),
                ) {
                    Box(Modifier.size(120.dp, 320.dp).testTag("page")) {
                        JPage("chat", scrollEdgeStyle = ScrollEdgeStyle.Gradient,
                            topBar = { Spacer(Modifier.fillMaxWidth().height(80.dp)) },
                            bottomBar = { Spacer(Modifier.fillMaxWidth().height(80.dp)) },
                        ) { Box(Modifier.fillMaxSize().background(Content)) }
                    }
                }
            }
        }

        val samples = listOf(60, 72, 80, 239, 248, 259)
        val pixels = levels.map { glass ->
            rule.runOnIdle { level.value = glass }
            val bitmap = rule.onNodeWithTag("page").captureToImage().asAndroidBitmap()
            samples.map { bitmap.getPixel(10, it) }
        }
        val (top, topFade, topContent, bottomContent, bottomFade) = pixels.first()
        assertTrue(android.graphics.Color.red(top) > android.graphics.Color.red(topFade))
        assertTrue(android.graphics.Color.red(topFade) > 0)
        assertEquals(android.graphics.Color.BLUE, topContent)
        assertEquals(android.graphics.Color.BLUE, bottomContent)
        assertTrue(android.graphics.Color.red(pixels.first().last()) > android.graphics.Color.red(bottomFade))
        assertTrue(android.graphics.Color.red(bottomFade) > 0)
        assertEquals(pixels[0], pixels[1])
        assertEquals(pixels[0], pixels[2])
    }

    @Test fun topFadeFollowsAnExtraBarWithoutMovingContentPadding() {
        val extraBar = mutableStateOf(false)
        val base = ScrollEdgeStyle.Gradient
        val overlapping = base.copy(overlap = 8.dp)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Box(Modifier.size(120.dp, 240.dp).testTag("page")) {
                    JPage("chat", scrollEdgeStyle = base,
                        topScrollEdgeStyle = if (extraBar.value) overlapping else base,
                        topBar = {
                            Column {
                                Spacer(Modifier.fillMaxWidth().height(80.dp))
                                if (extraBar.value) Spacer(Modifier.fillMaxWidth().height(32.dp))
                            }
                        },
                    ) { padding ->
                        Box(Modifier.fillMaxSize().background(Content)) {
                            Box(Modifier.padding(top = padding.calculateTopPadding())) {
                                Spacer(Modifier.size(1.dp).testTag("content-start"))
                            }
                        }
                    }
                }
            }
        }

        rule.onNodeWithTag("content-start").assertTopPositionInRootIsEqualTo(80.dp)
        assertEquals(android.graphics.Color.BLUE, rule.onNodeWithTag("page").captureToImage().asAndroidBitmap().getPixel(10, 82))

        rule.runOnIdle { extraBar.value = true }
        rule.onNodeWithTag("content-start").assertTopPositionInRootIsEqualTo(112.dp)
        val bitmap = rule.onNodeWithTag("page").captureToImage().asAndroidBitmap()
        assertTrue(android.graphics.Color.red(bitmap.getPixel(10, 114)) > 0)
        assertEquals(android.graphics.Color.BLUE, bitmap.getPixel(10, 122))
    }

    /**
     * The blurred top edge reaches 28 dp below the bar, over whatever a page keeps still there (the to-dos view toggles,
     * #6159). A page that says nothing is under the bar leaves that band clear, and gets the edge back once content
     * scrolls under it; a page that says nothing keeps it.
     */
    @Test fun blurredTopEdgeDropsWhileNothingIsUnderTheBar() {
        val underTop = mutableStateOf<Boolean?>(false)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalChromeInsets provides ChromeInsets(handlesNavigationBar = true)) {
                    Box(Modifier.size(120.dp, 320.dp).testTag("page")) {
                        val under = underTop.value
                        JPage("todos",
                            topBar = { Spacer(Modifier.fillMaxWidth().height(80.dp)) },
                            underTop = remember(under == null) { if (under == null) null else { { underTop.value == true } } },
                        ) { Box(Modifier.fillMaxSize().background(Content)) }
                    }
                }
            }
        }

        // y = 40 under the bar, 82 just below it, 100 well inside the 28 dp the blurred edge reaches past the bar
        val samples = listOf(40, 82, 100)
        fun pixels() = rule.onNodeWithTag("page").captureToImage().asAndroidBitmap().let { b -> samples.map { b.getPixel(10, it) } }

        assertEquals(samples.map { android.graphics.Color.BLUE }, pixels())

        rule.runOnIdle { underTop.value = true }
        rule.waitForIdle()
        val scrolled = pixels()
        samples.zip(scrolled).forEach { (y, pixel) -> assertTrue("y=$y is washed once content is under the bar", android.graphics.Color.red(pixel) > 0) }

        rule.runOnIdle { underTop.value = null }
        rule.waitForIdle()
        assertEquals("the default keeps the edge on", scrolled, pixels())
    }

    private companion object {
        val Content = Color.Blue
    }
}

package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-mdpi")
class ComposerDensityTest {
    @get:Rule val rule = createComposeRule()

    @After fun reset() = LayoutDensity.apply(LayoutDensity.Default)

    @Test fun homeGlassFrameUsesBubbleCornersInCompactOnly() = assertBubbleCornerScale(home = true)

    @Test fun fallbackGlassFrameUsesBubbleCornersInCompactOnly() = assertBubbleCornerScale(home = false)

    @Test fun writesHomeGlassFramesForInspection() {
        var compact by mutableStateOf(false)
        var dark by mutableStateOf(false)
        var fontScale by mutableStateOf(1f)
        rule.setContent {
            LayoutDensity.apply(if (compact) LayoutDensity.Compact else LayoutDensity.Standard)
            CompositionLocalProvider(LocalHomeChrome provides HomeChrome(emptyList(), "chat", {}, {})) {
                CompositionLocalProvider(LocalDensity provides Density(LocalDensity.current.density, fontScale = fontScale)) {
                    PersonaTheme(darkTheme = dark, style = AppStyle.Glass, reduceMotion = true) {
                        Column(Modifier.testTag("shot").width(360.dp).background(PersonaTheme.palette.background)) {
                            ComposerFrame { Text("Message") }
                        }
                    }
                }
            }
        }
        val out = File(System.getProperty("persona.shots") ?: "build/composer-density-shots").apply { mkdirs() }
        for (isCompact in listOf(false, true)) for (isDark in listOf(false, true)) for (scale in listOf(1f, 2f)) {
            compact = isCompact
            dark = isDark
            fontScale = scale
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            val name = "composer-${if (isCompact) "compact" else "standard"}-${if (isDark) "dark" else "light"}${if (scale == 1f) "" else "-large-text"}.png"
            File(out, name).outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    private fun assertBubbleCornerScale(home: Boolean) {
        var compact by mutableStateOf(false)
        rule.setContent {
            LayoutDensity.apply(if (compact) LayoutDensity.Compact else LayoutDensity.Standard)
            Column(Modifier.background(Backdrop)) {
                CompositionLocalProvider(
                    LocalHomeChrome provides if (home) HomeChrome(emptyList(), "chat", {}, {}) else null,
                ) {
                    PersonaTheme(style = AppStyle.Glass, reduceMotion = true) {
                        ComposerFrame(Modifier.testTag("composer").width(320.dp)) { Text("Message") }
                    }
                }
            }
        }
        val standard = cornerPixel()

        compact = true
        rule.waitForIdle()
        assertNotEquals("the Compact frame covers the probe that Standard leaves outside its corner", standard, cornerPixel())

        compact = false
        rule.waitForIdle()
        assertEquals("returning to Standard restores the original corner", standard, cornerPixel())
    }

    private fun cornerPixel(): Int {
        val bitmap = rule.onNodeWithTag("composer").captureToImage().asAndroidBitmap()
        val x = with(rule.density) { (Tokens.Space.m + 4.dp).toPx() }.toInt()
        val y = with(rule.density) { (Tokens.Space.s + 4.dp).toPx() }.toInt()
        return bitmap.getPixel(x, y)
    }

    private companion object {
        val Backdrop = Color(0xFFFF00FF)
    }
}

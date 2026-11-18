package persona.feature.chat

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaPalette
import persona.core.designsystem.PersonaTheme
import persona.core.domain.diff.WordSpan

/**
 * What an added or removed row is drawn on: an edit's rows use the style's diff tints over their own background, in every
 * style and both appearances; a Codex patch's rows stay plain, as they were. Read from the rendered pixels, beside an
 * unchanged row of the same diff.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
class DiffTintTest {
    @get:Rule val rule = createComposeRule()

    private val edit = toolDiff(Item("call", kind = "tool_use", toolName = "Edit",
        toolInput = buildJsonObject { put("old_string", "keep\nremoved row"); put("new_string", "keep\nadded row") }))!!
    private val patch = patchDiff("@@ -1,2 +1,2 @@\n keep\n-removed row\n+added row", null)

    private var palette: PersonaPalette? = null
    private var style by mutableStateOf(AppStyle.Glass)
    private var dark by mutableStateOf(false)

    /** One composition (a rule takes one setContent); the style and appearance move under it. */
    private fun show(diff: ToolDiff) = rule.setContent {
        PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
            palette = PersonaTheme.palette
            DiffView(diff, "call")
        }
    }

    private fun each(check: (AppStyle, Boolean) -> Unit) {
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            check(s, d)
        }
    }

    /** A pixel in the middle of these short rows' code, past the last glyph and away from the clipped rounded edge. */
    private fun rowColour(text: String): Color {
        val image = rule.onNodeWithText(text).captureToImage()
        return Color(image.asAndroidBitmap().getPixel(image.width / 2, image.height / 2))
    }

    private fun isClose(expected: Color, actual: Color, tolerance: Int) =
        listOf(expected.alpha to actual.alpha, expected.red to actual.red, expected.green to actual.green, expected.blue to actual.blue)
            .all { (a, b) -> Math.abs(a * 255 - b * 255) <= tolerance }

    private fun assertClose(message: String, expected: Color, actual: Color, tolerance: Int = 3) =
        assertTrue("$message: expected ${expected.toArgb().toUInt().toString(16)} got ${actual.toArgb().toUInt().toString(16)}", isClose(expected, actual, tolerance))

    /** Whether any pixel along the middle of the row's code is [expected]: the changed words' own tint, on blank characters so no glyph is in the way. */
    private fun rowHas(text: String, expected: Color): Boolean {
        val image = rule.onNodeWithText(text).captureToImage()
        val bitmap = image.asAndroidBitmap()
        return (0 until image.width).any { x -> isClose(expected, Color(bitmap.getPixel(x, image.height / 2)), 3) }
    }

    /** One added and one removed line whose changed words are the blanks between two letters, so their tint shows without any glyph over it. */
    private val gap = "        "
    private fun withWords(foldsContext: Boolean) = ToolDiff(listOf(
        DiffLine("keep", ' ', 1, 1),
        DiffLine("a${gap}b", '-', 2, null, listOf(WordSpan(1, 1 + gap.length))),
        DiffLine("c${gap}d", '+', null, 2, listOf(WordSpan(1, 1 + gap.length))),
    ), foldsContext)

    @Test fun anEditsRowsUseTheStylesDiffTintsInEveryStyleAndAppearance() {
        show(edit)
        each { s, d ->
            val p = palette!!
            val plain = rowColour("keep")
            assertClose("$s dark=$d added", p.diffAddFill.compositeOver(plain), rowColour("added row"))
            assertClose("$s dark=$d removed", p.diffDelFill.compositeOver(plain), rowColour("removed row"))
        }
    }

    /** The changed words' own tint (the emphasis token) is drawn over the row's fill, in every style and appearance. */
    @Test fun changedWordsAreDrawnOnTheStylesEmphasisOverTheLineTint() {
        show(withWords(foldsContext = true))
        each { s, d ->
            val p = palette!!
            val plain = rowColour("keep")
            assertTrue("$s dark=$d added words", rowHas("c${gap}d", p.diffAddEmphasis.compositeOver(p.diffAddFill.compositeOver(plain))))
            assertTrue("$s dark=$d removed words", rowHas("a${gap}b", p.diffDelEmphasis.compositeOver(p.diffDelFill.compositeOver(plain))))
        }
    }

    /** A patch's rows carry no word tint even if a line had marks: the same lines as a patch have no emphasis pixel. */
    @Test fun aCodexPatchDrawsNoWordTint() {
        show(withWords(foldsContext = false))
        each { s, d ->
            val p = palette!!
            val plain = rowColour("keep")
            assertTrue("$s dark=$d added", !rowHas("c${gap}d", p.diffAddEmphasis.compositeOver(p.diffAddFill.compositeOver(plain))))
            assertTrue("$s dark=$d removed", !rowHas("a${gap}b", p.diffDelEmphasis.compositeOver(p.diffDelFill.compositeOver(plain))))
        }
    }

    @Test fun aCodexPatchsRowsKeepNoTint() {
        show(patch)
        each { s, d ->
            val plain = rowColour("keep")
            assertClose("$s dark=$d added", plain, rowColour("added row"), tolerance = 1)
            assertClose("$s dark=$d removed", plain, rowColour("removed row"), tolerance = 1)
        }
    }
}

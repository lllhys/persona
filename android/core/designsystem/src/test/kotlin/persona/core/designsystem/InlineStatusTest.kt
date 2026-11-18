package persona.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** The shared inline status stays a three-line preview unless its caller explicitly removes that cap. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w320dp-h800dp-xhdpi")
class InlineStatusTest {
    @get:Rule val rule = createComposeRule()

    private val longText = "Waiting for the procurement team to confirm the revised delivery date and pricing · " +
        "look again Jan 5 09:00: ask whether Friday works for everyone before confirming the revised quote"
    private val shortText = "Waiting"

    private fun textLayouts(text: String): List<TextLayoutResult> {
        val nodes = rule.onAllNodesWithText(text, useUnmergedTree = true).fetchSemanticsNodes()
        assertEquals("default and unlimited instances of $text", 2, nodes.size)
        return nodes.map { node ->
            val layouts = mutableListOf<TextLayoutResult>()
            assertTrue(node.config[SemanticsActions.GetTextLayoutResult].action!!(layouts))
            layouts.single()
        }
    }

    @Test fun defaultCapsAtThreeLinesAndExplicitUnlimitedShowsWholeSentence() {
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = true) {
                Column(Modifier.width(320.dp)) {
                    JInlineStatus(longText)
                    JInlineStatus(longText, maxLines = Int.MAX_VALUE)
                    JInlineStatus(shortText)
                    JInlineStatus(shortText, maxLines = Int.MAX_VALUE)
                }
            }
        }
        val (default, unlimited) = textLayouts(longText)
        assertEquals("the default keeps exactly three lines", 3, default.lineCount)
        assertTrue("the default ellipsizes its last line", default.isLineEllipsized(default.lineCount - 1))

        assertTrue("the explicit unlimited value lays out more than three lines", unlimited.lineCount > 3)
        assertFalse("the explicit unlimited value does not clip", unlimited.hasVisualOverflow)
        for (line in 0 until unlimited.lineCount) {
            assertFalse("line $line is not ellipsized", unlimited.isLineEllipsized(line))
        }
        val lastGlyph = unlimited.getBoundingBox(longText.lastIndex)
        assertTrue("the final glyph is inside the unlimited layout",
            lastGlyph.width > 0f && lastGlyph.left >= 0f && lastGlyph.right <= unlimited.size.width + 1f &&
                lastGlyph.top >= 0f && lastGlyph.bottom <= unlimited.size.height + 1f)

        for (layout in textLayouts(shortText)) {
            assertEquals("a short sentence is one line with either limit", 1, layout.lineCount)
        }
    }
}

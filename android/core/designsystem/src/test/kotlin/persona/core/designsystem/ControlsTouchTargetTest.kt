package persona.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.SemanticsNodeInteraction
import androidx.compose.ui.test.assertHeightIsEqualTo
import androidx.compose.ui.test.assertWidthIsEqualTo
import androidx.compose.ui.test.click
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/** Compact controls keep their drawn sizes while the platform reserves a 48dp touch target around them. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class ControlsTouchTargetTest {
    @get:Rule val rule = createComposeRule()

    @Test fun smallAndMediumButtonsKeepTheirVisualHeights() {
        rule.setContent {
            Column {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    Row {
                        for (size in listOf(JButtonSize.Small, JButtonSize.Medium)) {
                            JButton(size.name, {}, Modifier.testTag("$style-$size"), size = size)
                        }
                    }
                }
            }
        }
        for (style in AppStyle.entries) for (size in listOf(JButtonSize.Small, JButtonSize.Medium)) {
            val button = rule.onNodeWithTag("$style-$size")
            val visualHeight = button.fetchSemanticsNode().boundsInRoot.height
            val minimum = with(rule.density) { Tokens.MinTouch.android.toPx() }
            assertTrue("$style $size grew to its touch height: $visualHeight", visualHeight < minimum)
            button.assertMinimumTouchTarget()
        }
    }

    @Test fun searchClearKeepsItsGlyphButtonSize() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                JSearchField("query", {}, "Search")
            }
        }
        val clear = rule.onNodeWithContentDescription("Clear search")
        clear.assertWidthIsEqualTo(24.dp).assertHeightIsEqualTo(24.dp)
        clear.assertMinimumTouchTarget()
    }

    @Test fun switchesKeepTheirTrackSizesInEveryStyle() {
        rule.setContent {
            Column {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    JSwitch(false, {}, Modifier.testTag("$style"))
                }
            }
        }
        for (style in AppStyle.entries) {
            val (width, height) = when (style) {
                AppStyle.Glass -> 50.dp to 30.dp
                AppStyle.Brief -> 48.dp to 28.dp
                AppStyle.Board -> 46.dp to 26.dp
            }
            val switch = rule.onNodeWithTag("$style")
            switch.assertWidthIsEqualTo(width).assertHeightIsEqualTo(height)
            switch.assertMinimumTouchTarget()
        }
    }

    @Test fun clickableChipsKeepTheirVisualHeightAndReceiveOutsideEdgeTaps() {
        var clicks = 0
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                JChip("Open menu", Modifier.testTag("chip"), onClick = { clicks++ })
            }
        }
        val chip = rule.onNodeWithTag("chip")
        chip.assertHeightIsEqualTo(JChipTokens.height).assertMinimumTouchTarget()
        val visual = chip.fetchSemanticsNode().boundsInRoot
        val touch = chip.fetchSemanticsNode().touchBoundsInRoot
        val outsideEdge = Offset(visual.center.x, visual.bottom + with(rule.density) { 5.dp.toPx() })
        assertTrue("The test tap must be outside the visual chip", outsideEdge.y > visual.bottom)
        assertTrue("The test tap must be inside the touch target", touch.contains(outsideEdge))
        rule.onRoot().performTouchInput { click(outsideEdge) }
        rule.runOnIdle { assertEquals(1, clicks) }
    }

    private fun SemanticsNodeInteraction.assertMinimumTouchTarget() {
        val bounds = fetchSemanticsNode().touchBoundsInRoot
        val minimum = with(rule.density) { Tokens.MinTouch.android.toPx() }
        assertTrue("Touch width ${bounds.width} is below $minimum", bounds.width >= minimum)
        assertTrue("Touch height ${bounds.height} is below $minimum", bounds.height >= minimum)
    }
}

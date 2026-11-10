package persona.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.ui.test.SemanticsNodeInteraction
import androidx.compose.ui.test.assertHeightIsEqualTo
import androidx.compose.ui.test.assertWidthIsEqualTo
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/** Actions inside clickable Agents rows keep their drawn sizes and receive independent 48dp targets. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp")
class AgentsTouchTargetTest {
    @get:Rule val rule = createComposeRule()

    @Test fun headingTabAndSourceActionsDoNotTriggerTheirRows() {
        var headingParent = 0
        var headingAction = 0
        var tabParent = 0
        var tabToggle = 0
        var rowParent = 0
        var sourceAction = 0
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Column {
                    AgentTreeHeader("Computer", folded = false, onToggle = { headingParent++ }, first = true,
                        onMonitor = { headingAction++ })
                    AgentTabRow("#1", "Tab", Status.Working, agents = 3, folded = false,
                        onToggle = { tabToggle++ }, onClick = { tabParent++ })
                    AgentRow("Session", Status.Working, onClick = { rowParent++ }, preview = "Latest words",
                        source = RowSource("Lark group", SourceKind.Feishu, onClick = { sourceAction++ }))
                }
            }
        }

        val heading = rule.onNodeWithContentDescription("Machine monitor")
        heading.assertWidthIsEqualTo(40.dp).assertHeightIsEqualTo(40.dp).assertMinimumTouchTarget()
        heading.performClick()
        rule.runOnIdle {
            assertEquals(1, headingAction)
            assertEquals(0, headingParent)
        }

        val tab = rule.onNodeWithContentDescription("3 agents")
        tab.assertHeightIsEqualTo(40.dp).assertMinimumTouchTarget()
        tab.performClick()
        rule.runOnIdle {
            assertEquals(1, tabToggle)
            assertEquals(0, tabParent)
        }

        val source = rule.onNodeWithText("Lark group", useUnmergedTree = true)
        source.assertMinimumTouchTarget()
        source.performClick()
        rule.runOnIdle {
            assertEquals(1, sourceAction)
            assertEquals(0, rowParent)
        }
    }

    private fun SemanticsNodeInteraction.assertMinimumTouchTarget() {
        val bounds = fetchSemanticsNode().touchBoundsInRoot
        val minimum = with(rule.density) { Tokens.MinTouch.android.toPx() }
        assertTrue("Touch width ${bounds.width} is below $minimum", bounds.width >= minimum)
        assertTrue("Touch height ${bounds.height} is below $minimum", bounds.height >= minimum)
    }
}

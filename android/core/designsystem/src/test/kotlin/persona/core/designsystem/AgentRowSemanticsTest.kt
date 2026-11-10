package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * A compact session row has no state line and its mark is colour only, so TalkBack hears the state from the row's
 * state description (#3902); a detailed row already says it in its text and does not say it twice.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class AgentRowSemanticsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun compactRowSaysItsState() {
        rule.setContent {
            Column {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    AgentRow("补齐网络层的单元测试", Status.NeedsYou, onClick = {}, Modifier.testTag("$style"), time = "2 分钟前",
                        statusText = "需要你", compact = true)
                }
            }
        }
        for (style in AppStyle.entries) rule.onNodeWithTag("$style")
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "需要你"))
    }

    @Test fun detailedRowSaysItInItsText() {
        rule.setContent {
            Column {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    AgentRow("补齐网络层的单元测试", Status.NeedsYou, onClick = {}, Modifier.testTag("$style"), statusText = "需要你")
                }
            }
        }
        for (style in AppStyle.entries) rule.onNodeWithTag("$style")
            .assert(SemanticsMatcher.keyNotDefined(SemanticsProperties.StateDescription))
    }

    /** A compact tab row says its lead agent's state; its fold toggle keeps only its own. */
    @Test fun compactTabRowSaysItsLeadsState() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                AgentTabRow("#2", "补齐网络层的单元测试", Status.NeedsYou, agents = 3, folded = true, onToggle = {}, onClick = {},
                    Modifier.testTag("tab"), statusText = "需要你", compact = true)
            }
        }
        rule.onNodeWithTag("tab").assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "需要你"))
    }

    /** A replacement Workflow is visible at once, ellipsizes on its own line, and leaves the facts drawn below it. */
    @Test fun detailedRowKeepsFactsBelowALongWorkflow() {
        val workflow = "Background workflow with a very long name that consumes the default row width · Verify 2/2"
        val facts = "Read only   Queued 2   Draft   Background 3"
        var note by mutableStateOf("Background workflow · Scan 1/2")
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = false, style = AppStyle.Glass) {
                Column(Modifier.testTag("metadata-shot").width(280.dp).background(PersonaTheme.palette.background)) {
                    AgentRow("Workflow", Status.Working, onClick = {}, statusText = "Working", preview = "Latest words",
                        note = RowNote(note, WorkflowIcon),
                        facts = listOf(RowFact("Read only", caution = true), RowFact("Queued 2", caution = true),
                            RowFact("Draft", caution = true), RowFact("Background 3")))
                }
            }
        }
        rule.mainClock.advanceTimeByFrame()
        note = workflow
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()

        fun renderedPixels(text: String): Int {
            val shot = rule.onNodeWithTag("metadata-shot")
            val origin = shot.fetchSemanticsNode().boundsInRoot.topLeft
            val bounds = rule.onNodeWithText(text, useUnmergedTree = true).fetchSemanticsNode().boundsInRoot
            val bitmap = shot.captureToImage().asAndroidBitmap()
            val background = bitmap.getPixel(0, 0)
            val left = (bounds.left - origin.x).toInt().coerceIn(0, bitmap.width)
            val right = (bounds.right - origin.x).toInt().coerceIn(left, bitmap.width)
            val top = (bounds.top - origin.y).toInt().coerceIn(0, bitmap.height)
            val bottom = (bounds.bottom - origin.y).toInt().coerceIn(top, bitmap.height)
            var drawn = 0
            for (y in top until bottom) for (x in left until right) {
                if (bitmap.getPixel(x, y) != background) drawn++
            }
            return drawn
        }

        assertTrue("the replacement Workflow must not start transparent", renderedPixels(workflow) > 20)
        val noteNode = rule.onNodeWithText(workflow, useUnmergedTree = true).fetchSemanticsNode()
        val layouts = mutableListOf<TextLayoutResult>()
        noteNode.config[SemanticsActions.GetTextLayoutResult].action!!(layouts)
        assertTrue("the long Workflow ellipsizes in its own bounds", layouts.single().hasVisualOverflow)
        val factsNode = rule.onNodeWithText(facts, useUnmergedTree = true).fetchSemanticsNode()
        assertTrue("critical facts need a second line below the Workflow", factsNode.boundsInRoot.top >= noteNode.boundsInRoot.bottom)
        assertTrue("critical facts must be rendered, not only present in semantics", renderedPixels(facts) > 20)
    }
}

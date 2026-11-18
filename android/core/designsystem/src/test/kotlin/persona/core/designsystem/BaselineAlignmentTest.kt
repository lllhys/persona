package persona.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.Text
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.AlignmentLine
import androidx.compose.ui.layout.FirstBaseline
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.sp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/** The list markers and tool arguments that sit on a text baseline keep their place, and survive a baseline that cannot be read. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class BaselineAlignmentTest {
    @get:Rule val rule = createComposeRule()

    @Test fun aSmallerMarkerSitsOnTheSameBaselineAsComposesOwnAlignment() {
        rule.setContent {
            Column {
                Row {
                    Text("1.", Modifier.alignBy(FirstBaseline).testTag("own-marker"), fontSize = 12.sp)
                    Text("Convert the photo", Modifier.alignBy(FirstBaseline).testTag("own-text"), fontSize = 24.sp)
                }
                Row {
                    Text("1.", firstBaselineAligned().testTag("marker"), fontSize = 12.sp)
                    Text("Convert the photo", firstBaselineAligned().testTag("text"), fontSize = 24.sp)
                }
            }
        }
        val own = rule.onNodeWithTag("own-marker").getUnclippedBoundsInRoot()
        val ours = rule.onNodeWithTag("marker").getUnclippedBoundsInRoot()
        val ownText = rule.onNodeWithTag("own-text").getUnclippedBoundsInRoot()
        val oursText = rule.onNodeWithTag("text").getUnclippedBoundsInRoot()
        // The second row starts under the first: compare each marker's offset from its own row's text.
        assertEquals(own.top - ownText.top, ours.top - oursText.top)
        assertEquals(ownText.bottom - ownText.top, oursText.bottom - oursText.top)
    }

    @Test fun theCapturedLookaheadFailureIsLeftToTheRowsOwnAlignment() {
        assertEquals(AlignmentLine.Unspecified, readBaseline { throw missingLookaheadBaseline() })
    }

    @Test fun aBaselineThatCanBeReadIsReturnedAsItIs() {
        assertEquals(42, readBaseline { 42 })
        assertEquals(AlignmentLine.Unspecified, readBaseline { AlignmentLine.Unspecified })
    }

    @Test fun aNullPointerFailureFromSomewhereElseIsNotHidden() {
        assertThrows(NullPointerException::class.java) { readBaseline { throw NullPointerException("another failure") } }
    }

    @Test fun otherFailuresAreNotHidden() {
        assertThrows(IllegalStateException::class.java) { readBaseline { error("a real failure") } }
    }
}

/** The top of the tablet capture, without relying on line numbers that a Compose patch can move. */
private fun missingLookaheadBaseline() = NullPointerException().apply {
    stackTrace = arrayOf(
        StackTraceElement("androidx.compose.ui.node.LookaheadDelegate", "getAlignmentLinesOwner", "LookaheadDelegate.kt", 607),
        StackTraceElement("androidx.compose.ui.node.InnerNodeCoordinator\$LookaheadDelegateImpl", "calculateAlignmentLine", "InnerNodeCoordinator.kt", 90),
        StackTraceElement("androidx.compose.ui.node.InnerNodeCoordinator", "calculateAlignmentLine", "InnerNodeCoordinator.kt", 170),
        StackTraceElement("androidx.compose.ui.node.LookaheadCapablePlaceable", "get", "LookaheadDelegate.kt", 113),
    )
}

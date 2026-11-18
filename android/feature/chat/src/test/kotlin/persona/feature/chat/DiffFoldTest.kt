package persona.feature.chat

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens

/**
 * A long edit's unchanged lines fold into one row each until Show All: the words, the state across a new window or a new
 * diff, the largest font, and the three languages. The fixture is 40 lines with one changed, so the fold above it is 16
 * lines and the one below 17.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
class DiffFoldTest {
    @get:Rule val rule = createComposeRule()

    private val old = (1..40).joinToString("\n") { "line $it" }

    private fun fixture(change: String = "line twenty"): ToolDiff = toolDiff(Item("call", kind = "tool_use", toolName = "Edit",
        toolInput = buildJsonObject { put("old_string", old); put("new_string", old.replace("line 20\n", "$change\n")) }))!!

    private fun show(fontScale: Float = 1f, dark: Boolean = false, content: @androidx.compose.runtime.Composable () -> Unit) = rule.setContent {
        val density = LocalDensity.current
        CompositionLocalProvider(LocalDensity provides Density(density.density, fontScale)) {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = AppStyle.Glass) {
                Column(Modifier.width(400.dp).padding(Tokens.Space.l)) { content() }
            }
        }
    }

    @Test fun longContextIsFoldedUntilShowAll() {
        val shown = fixture()
        show { DiffView(shown, "call") }
        rule.onNodeWithText("… 16 unchanged lines").assertExists()
        rule.onNodeWithText("Show all (41 lines)").performClick()
        rule.onAllNodesWithText("… 16 unchanged lines").assertCountEquals(0)
        rule.onNodeWithText("Collapse").performClick()
        rule.onNodeWithText("… 16 unchanged lines").assertExists()
    }

    @Test fun theSignIsReadAsAddedOrRemoved() {
        val shown = fixture()
        show { DiffView(shown, "call") }
        rule.onAllNodesWithContentDescription("Added").assertCountEquals(1)
        rule.onAllNodesWithContentDescription("Removed").assertCountEquals(1)
        // the sign itself is still there for the eye
        rule.onNodeWithText("+").assertExists()
    }

    @Test fun showAllSurvivesANewWindowAndRecreation() {
        val shown = fixture()
        val restoration = StateRestorationTester(rule)
        restoration.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) { DiffView(shown, "call") }
        }
        rule.onNodeWithText("Show all (41 lines)").performClick()
        restoration.emulateSavedInstanceStateRestore()
        rule.onAllNodesWithText("… 16 unchanged lines").assertCountEquals(0)
        rule.onNodeWithText("Collapse").assertExists()
    }

    @Test fun aGrowingDiffKeepsItsOpenState() {
        var shown by mutableStateOf(fixture("line twenty"))
        show { DiffView(shown, "call") }
        rule.onNodeWithText("Show all (41 lines)").performClick()
        shown = fixture("line twenty, then more")
        rule.waitForIdle()
        rule.onAllNodesWithText("… 16 unchanged lines").assertCountEquals(0)
        rule.onNodeWithText("Collapse").assertExists()
    }

    @Test fun aShortEditHasNoFoldAndNoShowAll() {
        val short = toolDiff(Item("call", kind = "tool_use", toolName = "Edit",
            toolInput = buildJsonObject { put("old_string", "a\nb\nc"); put("new_string", "a\nx\nc") }))!!
        show { DiffView(short, "call") }
        rule.onAllNodesWithText("Collapse").assertCountEquals(0)
        rule.onAllNodesWithText("unchanged", substring = true).assertCountEquals(0)
    }

    @Test fun theLargestFontKeepsTheFoldAndItsButton() {
        val shown = fixture()
        show(fontScale = 2f, dark = true) { DiffView(shown, "call") }
        rule.onNodeWithText("… 16 unchanged lines").assertExists()
        rule.onNodeWithText("Show all (41 lines)").assertExists()
    }

    @Config(qualifiers = "zh-w400dp-h1200dp-xhdpi")
    @Test fun chineseReadsTheFoldAndTheSigns() {
        val shown = fixture()
        show { DiffView(shown, "call") }
        rule.onNodeWithText("… 16 行没变").assertExists()
        rule.onAllNodesWithContentDescription("新增").assertCountEquals(1)
        rule.onAllNodesWithContentDescription("删除").assertCountEquals(1)
        rule.onNodeWithText("展开全部（41 行）").assertExists()
    }

    @Config(qualifiers = "ja-w400dp-h1200dp-xhdpi")
    @Test fun japaneseReadsTheFoldAndTheSigns() {
        val shown = fixture()
        show { DiffView(shown, "call") }
        rule.onNodeWithText("… 16行は変更なし").assertExists()
        rule.onAllNodesWithContentDescription("追加").assertCountEquals(1)
        rule.onAllNodesWithContentDescription("削除").assertCountEquals(1)
        rule.onNodeWithText("すべて表示（41行）").assertExists()
    }
}

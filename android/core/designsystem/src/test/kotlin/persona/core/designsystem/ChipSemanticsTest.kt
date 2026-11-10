package persona.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/** Toggle chips retain checkbox semantics; expansion entries are buttons with an expanded / collapsed state. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class ChipSemanticsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun toggleChipsKeepTheirExistingRoleAndSelection() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Column {
                    JChip("Filter", Modifier.testTag("toggle"), selected = true, onClick = {})
                    JChip("Open menu", Modifier.testTag("action"), selected = true, onClick = {}, onLongClick = {}, role = Role.Button)
                }
            }
        }
        rule.onNodeWithTag("toggle")
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Checkbox)).assertIsSelected()
        rule.onNodeWithTag("action")
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Button))
            .assert(SemanticsMatcher.keyNotDefined(SemanticsProperties.Selected))
    }

    @Test fun trayEntryIsAnExpandableButtonWithAChevron() {
        var expanded by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Column {
                    AssistantTrayEntry("Pending", 2, AssistantTrayTone.Pending, expanded,
                        if (expanded) "Expanded" else "Collapsed", { expanded = !expanded }, Modifier.testTag("tray"))
                    JChip("Pending", Modifier.testTag("plain"), selected = expanded, onClick = {}, count = 2,
                        dot = assistantTrayColor(AssistantTrayTone.Pending), role = Role.Button)
                }
            }
        }
        val tray = rule.onNodeWithTag("tray")
        tray.assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Button))
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Collapsed"))
            .assert(SemanticsMatcher.keyNotDefined(SemanticsProperties.Selected))
        assertChevronWidth()
        tray.performClick()
        tray.assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Expanded"))
            .assert(SemanticsMatcher.keyNotDefined(SemanticsProperties.Selected))
        assertChevronWidth()
    }

    private fun assertChevronWidth() {
        val trayWidth = rule.onNodeWithTag("tray").fetchSemanticsNode().boundsInRoot.width
        val plainWidth = rule.onNodeWithTag("plain").fetchSemanticsNode().boundsInRoot.width
        val chevronWidth = with(rule.density) { (JChipTokens.iconSize + JChipTokens.iconGap).toPx() }
        assertEquals("The trailing chevron reserves its icon and gap", chevronWidth, trayWidth - plainWidth, 0.5f)
    }
}

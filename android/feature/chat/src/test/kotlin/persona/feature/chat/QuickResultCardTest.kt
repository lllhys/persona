package persona.feature.chat

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.time.Instant
import java.util.Locale
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.designsystem.LocalizedFormats
import persona.core.designsystem.PersonaTheme
import persona.core.domain.tasks.QuickResult
import persona.core.domain.tasks.QuickResultUndo

/** A quick result shows what was added, as on iOS, without inventing a source device (#6864), with its Undo (#9340). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class QuickResultCardTest {
    @get:Rule val rule = createComposeRule()

    private val result = QuickResult(reminder = false, callId = "call-1", itemId = 42, title = "Dentist",
        time = "2026-11-13T15:30:00Z", place = "Personal")

    @Test fun showsResultDetailsWithoutInventingADeviceSource() {
        val time = LocalizedFormats.format(Instant.parse(result.time!!), LocalizedFormats.MONTH_DAY_TIME, Locale.ENGLISH)
        rule.setContent { PersonaTheme(reduceMotion = true) { QuickResultCard(result) } }

        rule.onNodeWithText("Added to calendar").assertExists()
        rule.onNodeWithText("$time · ${result.title} · ${result.place}").assertExists()
        rule.onAllNodes(hasText("iPhone", substring = true), useUnmergedTree = true).assertCountEquals(0)
    }

    @Test fun undoShowsOnlyWhileTheEventIsOnThisPhone() {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                androidx.compose.foundation.layout.Column {
                    // Offered: the event is this phone's, nothing undone yet.
                    QuickResultCard(result, onUndo = {})
                    // A card without an event id has nothing to take back.
                    QuickResultCard(result.copy(itemId = null))
                    // Already taken back: the words say so and the button is gone.
                    QuickResultCard(result, QuickResultUndo.Undone, onUndo = {})
                }
            }
        }

        rule.onNodeWithText("Undo").assertExists()
        rule.onAllNodesWithText("Undo").assertCountEquals(1)
        rule.onNodeWithText("Undone").assertExists()
        rule.onAllNodesWithText("Added to calendar").assertCountEquals(2)
    }

    @Test fun undoRunsOnceAndTakesNoSecondTapWhileItRuns() {
        var taps = 0
        rule.setContent { PersonaTheme(reduceMotion = true) { QuickResultCard(result, onUndo = { taps++ }) } }

        rule.onNodeWithText("Undo").assertIsEnabled().performClick()
        assertEquals(1, taps)
    }

    @Test fun anUndoingCardTakesNoTap() {
        rule.setContent { PersonaTheme(reduceMotion = true) { QuickResultCard(result, QuickResultUndo.Undoing, onUndo = {}) } }

        rule.onNodeWithText("Undo").assertIsNotEnabled()
    }
}

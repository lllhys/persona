package persona.feature.chat

import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.SessionKey
import persona.core.designsystem.PersonaTheme

/**
 * The main conversation's `/clear` in its composer (#5100, the web's #5095): offered in the "/" list and sent alone it
 * clears through [LocalMainChatClear] instead of going to Persona, with no confirmation; a failure puts `/clear` back;
 * anything more is a message. Its + sheet offers no Agent quick messages, while an Agent's sheet keeps them. Without the
 * local on an Assistant chat (a hub without `assistant_main_clear`) no command is offered and sending is unchanged; Agent
 * composers leave [ChatComposer]'s `assistantChat` flag false.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class MainChatClearTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val state = ChatComposerState()
    private val clears = mutableListOf<() -> Unit>()
    private var sent = 0

    private fun mount(clear: MainChatClear? = MainChatClear { onFailed -> clears += onFailed }, working: Boolean = false, assistant: Boolean = true) {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalMainChatClear provides clear) {
                    ChatComposer(SessionKey("mac", "main"), state, working = working, sending = false, canClear = false, service = null,
                        onSend = { sent++ }, onCommand = {}, onNotice = {}, assistantChat = assistant)
                }
            }
        }
    }

    private fun type(text: String) = rule.runOnIdle { state.value = TextFieldValue(text, TextRange(text.length)) }

    @Test fun clearSentAloneClearsWithoutAskingAndNeverGoesToPersona() {
        mount()
        type("/clear")
        rule.onNodeWithContentDescription("Send").performClick()
        rule.runOnIdle {
            assertEquals(1, clears.size)
            assertEquals(0, sent)
            assertEquals("", state.value.text)
        }
        // No confirmation, unlike a pane's /clear.
        rule.onAllNodesWithText("Clear context?").assertCountEquals(0)
    }

    @Test fun aFailedClearPutsTheCommandBack() {
        mount()
        type("/clear")
        rule.onNodeWithContentDescription("Send").performClick()
        rule.runOnIdle { clears.single().invoke() }
        rule.runOnIdle { assertEquals("/clear", state.value.text) }
    }

    @Test fun theSlashListOffersItWhileTheConversationWorksToo() {
        mount(working = true)
        type("/cl")
        rule.onNodeWithText("Clear the chat and start over").assertExists()
        rule.onNodeWithText("/clear").performClick()
        rule.runOnIdle {
            assertEquals(1, clears.size)
            assertEquals("", state.value.text)
        }
        rule.onAllNodesWithText("Clear context?").assertCountEquals(0)
    }

    @Test fun thePlusSheetOffersOnlyTheMainClear() {
        mount()
        rule.onNodeWithContentDescription("Add attachments and quick messages").performClick()
        for (title in listOf("Continue", "Compact context /compact", "View context /context", "Commit and push", "Report progress"))
            rule.onNodeWithText(title).assertDoesNotExist()
        rule.onNodeWithText("Clear context /clear").performClick()
        rule.runOnIdle {
            assertEquals(1, clears.size)
            assertEquals(0, sent)
        }
    }

    @Test fun anAgentPlusSheetKeepsItsQuickMessages() {
        mount(clear = null, assistant = false)
        rule.onNodeWithContentDescription("Add attachments and quick messages").performClick()
        for (title in listOf("Continue", "Compact context /compact", "View context /context", "Commit and push", "Report progress"))
            rule.onNodeWithText(title).assertExists()
    }

    @Test fun moreThanTheWordIsAMessage() {
        mount()
        type("/clear the table please")
        rule.onNodeWithContentDescription("Send").performClick()
        rule.runOnIdle {
            assertTrue(clears.isEmpty())
            assertEquals(1, sent)
        }
    }

    @Test fun withoutItTheMainConversationHasNoClear() {
        mount(clear = null)
        type("/")
        rule.onAllNodesWithText("/clear").assertCountEquals(0)
        rule.onAllNodesWithText("/compact").assertCountEquals(0)
        rule.onAllNodesWithText("/continue").assertCountEquals(0)
        type("/cl")
        rule.onAllNodesWithText("/clear").assertCountEquals(0)
        type("/clear")
        rule.onNodeWithContentDescription("Send").performClick()
        rule.runOnIdle {
            assertTrue(clears.isEmpty())
            assertEquals(1, sent)
        }
    }

    @Test fun onlyTheWordAloneIsTheCommand() {
        assertTrue(isMainClearCommand(" /Clear ", attachments = 0, extraContent = false))
        assertFalse(isMainClearCommand("/clear", attachments = 1, extraContent = false))
        assertFalse(isMainClearCommand("/clear", attachments = 0, extraContent = true))
        assertFalse(isMainClearCommand("/clear now", attachments = 0, extraContent = false))
    }
}

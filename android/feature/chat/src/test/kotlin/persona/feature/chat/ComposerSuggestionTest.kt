package persona.feature.chat

import android.content.Context
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performSemanticsAction
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.JsonCodec
import persona.core.contracts.Session
import persona.core.designsystem.PersonaTheme

/** The suggestion chip's merged semantics node: risky text can only fill the composer for review. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class ComposerSuggestionTest {
    @get:Rule val rule = createComposeRule()
    private val resources = ApplicationProvider.getApplicationContext<Context>().resources
    private val text = "Review the release plan"
    private val sendLabel get() = resources.getString(R.string.chat_suggestion_send)
    private val fillLabel get() = resources.getString(R.string.chat_suggestion_fill)

    private fun session(risky: Boolean? = null): Session = JsonCodec.decodeFromString(buildJsonObject {
        put("session_id", "s")
        put("status", "idle")
        put("suggestion", "  $text  ")
        risky?.let { put("suggestion_risky", it) }
    }.toString())

    private fun mount(session: Session, onSend: ((String) -> Unit)?): ChatComposerState {
        val composer = ChatComposerState()
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                ComposerSuggestion(session, composer, onSend)
            }
        }
        return composer
    }

    @Test fun riskySuggestionHasNoDirectSendAndTapOnlyFills() {
        val sent = mutableListOf<String>()
        val composer = mount(session(risky = true), sent::add)
        val chip = rule.onNodeWithText(text)
        val semantics = chip.fetchSemanticsNode().config
        assertNull("a risky suggestion has no send accessibility action",
            semantics.getOrNull(SemanticsActions.CustomActions)?.firstOrNull { it.label == sendLabel })
        assertNull("a risky suggestion has no long-press menu", semantics.getOrNull(SemanticsActions.OnLongClick))

        chip.performClick()
        assertEquals(text, composer.value.text)
        assertEquals(1, composer.focusRequest)
        assertTrue(sent.isEmpty())
        rule.onNodeWithText(sendLabel).assertDoesNotExist()
    }

    @Test fun safeSuggestionKeepsItsSendActionAndLongPressMenu() {
        val sent = mutableListOf<String>()
        mount(session(), sent::add)
        val chip = rule.onNodeWithText(text)
        val semantics = chip.fetchSemanticsNode().config
        val send = semantics.getOrNull(SemanticsActions.CustomActions)?.firstOrNull { it.label == sendLabel }
        assertNotNull("a safe suggestion keeps its send accessibility action", send)
        assertNotNull("a safe suggestion keeps its long-press menu", semantics.getOrNull(SemanticsActions.OnLongClick))
        assertTrue(send!!.action())
        assertEquals(listOf(text), sent)

        chip.performSemanticsAction(SemanticsActions.OnLongClick)
        rule.onNodeWithText(sendLabel).assertExists()
        rule.onNodeWithText(fillLabel).assertExists()
    }

    @Test fun noSenderHasTheSameFillOnlySemantics() {
        val composer = mount(session(risky = false), onSend = null)
        val chip = rule.onNodeWithText(text)
        val semantics = chip.fetchSemanticsNode().config
        assertNull(semantics.getOrNull(SemanticsActions.CustomActions)?.firstOrNull { it.label == sendLabel })
        assertNull(semantics.getOrNull(SemanticsActions.OnLongClick))
        chip.performClick()
        assertEquals(text, composer.value.text)
        assertEquals(1, composer.focusRequest)
    }
}

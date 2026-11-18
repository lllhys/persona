package persona.feature.chat

import android.content.Context
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme

/** Agent and assistant conversations use the real page's one suggestion chip and its fill-only risky behavior. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class ComposerSuggestionConversationTest {
    @get:Rule val rule = createComposeRule()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val computer = MentionChatHarness(scope)
    private val text = "Review the suggested next step"
    private val sendLabel get() = ApplicationProvider.getApplicationContext<Context>().resources.getString(R.string.chat_suggestion_send)

    @After fun close() { scope.cancel() }

    private fun assertSharedRiskyChip(key: SessionKey, assistant: Boolean) {
        val rows = computer.sessionRows.value
        val row = rows.firstOrNull { it.key == key } ?: Session(key.machineId, key.sessionId, title = "Planning", assistant = assistant)
        val risky = row.copy(status = "idle", state = "idle", suggestion = text, suggestionRisky = true)
        computer.sessionRows.value = if (row in rows) rows.map { if (it.key == key) risky else it } else rows + risky
        val composer = ChatComposerState()
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Box(Modifier.fillMaxSize()) { computer.Page(key, composer) }
            }
        }
        rule.waitUntil(20_000) { rule.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty() }
        val chip = rule.onNodeWithText(text)
        val semantics = chip.fetchSemanticsNode().config
        assertNull(semantics.getOrNull(SemanticsActions.CustomActions)?.firstOrNull { it.label == sendLabel })
        assertNull(semantics.getOrNull(SemanticsActions.OnLongClick))
        chip.performClick()
        assertEquals(text, composer.value.text)
        assertEquals(1, composer.focusRequest)
        assertTrue(computer.sent.isEmpty())
    }

    @Test fun agentConversationUsesTheFillOnlyChip() = assertSharedRiskyChip(computer.agent, assistant = false)

    @Test fun mainPersonaConversationUsesTheSameChip() = assertSharedRiskyChip(computer.persona, assistant = true)

    @Test fun ordinaryAssistantConversationUsesTheSameChip() =
        assertSharedRiskyChip(SessionKey("macbook", "ordinary"), assistant = true)
}

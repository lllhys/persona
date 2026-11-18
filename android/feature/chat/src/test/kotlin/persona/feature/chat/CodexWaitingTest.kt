package persona.feature.chat

import android.content.Context
import android.content.res.Configuration
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.*
import persona.core.domain.answer.rowLine
import java.time.Instant
import java.util.Locale

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class CodexWaitingTest {
    private val waiting = JsonCodec.decodeFromString<Session>("""{"machine_id":"computer","session_id":"waiting",
        "agent":"codex","status":"idle","pane_id":"pane","awaiting_first_message":true}""")
    private val thread = waiting.copy(sessionId = "thread", awaitingFirstMessage = false, status = "working")

    @Test fun followsOnlyALiveCodexThreadInTheSameComputersPane() {
        assertEquals(thread, codexThreadOf(listOf(waiting, thread), waiting))
        assertEquals(thread, codexThreadOf(listOf(thread), waiting))
        for (wrong in listOf(
            thread.copy(machineId = "elsewhere"), thread.copy(paneId = "another"), thread.copy(agent = "claude"),
            thread.copy(status = "offline", state = "offline"), thread.copy(state = "offline"), thread.copy(awaitingFirstMessage = true),
            thread.copy(sessionId = waiting.sessionId),
        )) assertNull(wrong.toString(), codexThreadOf(listOf(wrong), waiting))
        assertNull(codexThreadOf(listOf(thread), waiting.copy(paneId = null)))
        assertNull(codexThreadOf(listOf(thread), thread))
        assertFalse(JsonCodec.decodeFromString<Session>("""{"session_id":"older"}""").awaitingFirstMessage)
    }

    @Test fun waitingIsIdleAndItsSecondLineExplainsWhatItWaitsFor() {
        assertEquals(AgentState.Idle, waiting.agentState)
        assertEquals(0, waiting.pendingInteractions)
        assertEquals(CoreText.Key.CodexWaiting, waiting.copy(lastText = "Old text").rowLine(null, Instant.EPOCH)?.key)
        assertEquals(CoreText.Key.CodexWaiting, waiting.rowLine(null, Instant.EPOCH, muted = true)?.key)
        assertNull(waiting.copy(awaitingFirstMessage = false).rowLine(null, Instant.EPOCH))
        assertNull(waiting.copy(status = "offline", state = "offline").rowLine(null, Instant.EPOCH))
    }

    @Test fun waitingNoteAndPlaceholderUseAllThreeLanguagesAndYieldToViewOnly() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        for ((language, fragment, placeholder) in listOf(
            Triple("en", "Codex is open on Studio", "First message to Codex"),
            Triple("zh", "Codex 已在「Studio」上打开", "给 Codex 的第一条消息"),
            Triple("ja", "Codexは「Studio」で開いています", "Codex への最初のメッセージ"),
        )) {
            val configuration = Configuration(context.resources.configuration).apply { setLocale(Locale.forLanguageTag(language)) }
            val res = context.createConfigurationContext(configuration).resources
            assertTrue(emptyChatHint(res, waiting, "Studio", readOnly = false).contains(fragment))
            assertEquals(placeholder, agentPlaceholder(res, waiting))
            assertEquals(res.getString(R.string.chat_screen_empty_read_only), emptyChatHint(res, waiting, "Studio", readOnly = true))
            assertEquals(res.getString(R.string.chat_screen_empty_hint), emptyChatHint(res, thread, "Studio", readOnly = false))
            assertFalse(emptyChatHint(res, waiting, null, readOnly = false).contains("null"))
        }
    }

    @Test fun firstMessageRefusalsHaveActionableWords() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        for (language in listOf("en", "zh", "ja")) {
            val config = Configuration(context.resources.configuration).apply { setLocale(Locale.forLanguageTag(language)) }
            val res = context.createConfigurationContext(config).resources
            for ((code, id) in listOf("codex_not_ready" to R.string.chat_delivery_codex_not_ready,
                "codex_first_text" to R.string.chat_delivery_codex_first_text,
                "input_draft" to R.string.chat_delivery_input_draft, "dialog_open" to R.string.chat_delivery_dialog_open)) {
                assertEquals(persona.core.designsystem.UiText.Res(id), refusalText(code))
                assertTrue(res.getString(id).isNotBlank())
                assertFalse(res.getString(id).contains(code))
            }
        }
    }
}

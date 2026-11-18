package persona.feature.chat

import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import persona.core.contracts.Item
import persona.core.contracts.ModelRefusal
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.designsystem.LocalizedFormats
import persona.core.designsystem.PersonaTheme
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.LineItem
import persona.core.domain.lines.chatLines
import java.time.Instant
import java.time.ZoneId

/**
 * The node's own model recovery on the phone (#7824): a switch names its cause, a configured model that answered a probe
 * says the next message switches back, and when every model failed the turn's last error card keeps Retry with the cause,
 * the recovery time and, folded, the models tried. A runtime-written record (no recovery fields) reads as before.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class ModelRecoveryTest {
    @get:Rule val rule = createComposeRule()

    private val resources get() = RuntimeEnvironment.getApplication().resources
    private val zone = ZoneId.of("Asia/Shanghai")

    // A runtime-written record: Claude Code's own fallback, with the runtime's own category.
    private val runtime = ModelRefusal("model_refusal_fallback", "primary-model", "fallback-model", "synthetic-policy", "session",
        "Upstream refused the request")
    // The node's projection of its own switch (decorateModelRecovery).
    private val switched = ModelRefusal("model_refusal_fallback", "primary-model", "fallback-model", "quota_exhausted", "session",
        "the account's quota was exhausted", recoveryReset = "2026-11-15T15:59:00Z", recoveryTried = "primary-model → fallback-model")
    // Every model of the chain failed: no model answers, so no fallback model.
    private val exhausted = ModelRefusal("model_refusal_fallback", "primary-model", null, "safeguards_refusal", "session",
        "the model's safety classifier refused the request", recoveryReset = "2026-11-15T15:59:00Z", recoveryExhausted = true,
        recoveryTried = "primary-model → fallback-a → fallback-b")

    private fun session(f: ModelRefusal?) = Session(machineId = "box", sessionId = "main", agent = "claude", status = "idle",
        assistant = true, modelFallback = f)

    // A refusal the upstream's safeguards flagged: a 400 that, on its own, needs someone to act.
    private val refusedError = "API Error: 400 {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\"," +
        "\"message\":\"Output blocked: safeguards flagged this request\"}} request_id: req_011CUabcdEFgh"
    private fun lines(vararg texts: Pair<String, String>) =
        chatLines(texts.mapIndexed { i, (role, text) -> LineItem(Item("i$i#0", i + 1, role = role, kind = "text", text = text)) }).lines

    @Test fun anExhaustedChainKeepsRetryOnTheTurnsLastError() {
        val lines = lines("user" to "go on", "assistant" to refusedError)
        val card = lines.last() as ChatLine.ApiError
        assertTrue("the error alone needs someone to act", card.info.needsAction)
        assertNull("no Retry for it without the marker", retryLine(lines, working = false, canSend = true))
        assertEquals(card.id, retryLine(lines, working = false, canSend = true, exhausted = true))
        // still only while the conversation is idle and the page can send
        assertNull(retryLine(lines, working = true, canSend = true, exhausted = true))
        assertNull(retryLine(lines, working = false, canSend = false, exhausted = true))
        // The marker goes on the newest line, the turn's last error, and only when the chain is exhausted.
        assertEquals(ExhaustedRecovery(card.id, exhausted), exhaustedRecovery(lines, exhausted))
        assertNull(exhaustedRecovery(lines, switched))
        assertNull(exhaustedRecovery(lines, runtime))
        assertNull(exhaustedRecovery(lines, null))
        assertNull(exhaustedRecovery(lines("assistant" to refusedError, "user" to "something else"), exhausted))
    }

    @Test fun theCauseAndTheRecoveryTimeInThePhonesZone() {
        val locale = resources.configuration.locales[0]
        val at = Instant.parse("2026-11-15T15:59:00Z")
        val sameDay = Instant.parse("2026-11-15T10:00:00Z") // 18:00 in the zone: the reset is later today, so the clock alone
        val lines = recoveryExhaustedLines(exhausted, resources, sameDay, zone)
        assertEquals(listOf("Cause: the model refused",
            "Available again at " + LocalizedFormats.format(at, LocalizedFormats.TIME, locale, zone)), lines)
        assertTrue("23:59 in the zone, not 15:59 UTC: ${lines[1]}", lines[1].contains("11:59"))
        val dayBefore = Instant.parse("2026-11-14T10:00:00Z")
        assertEquals("Available again at " + LocalizedFormats.format(at, LocalizedFormats.MONTH_DAY_TIME, locale, zone),
            recoveryResetText(exhausted.recoveryReset, resources, dayBefore, zone))
        // Each of the node's five reasons in plain words; quota exhaustion is told apart from the rest.
        assertEquals(listOf("the model refused", "quota exhausted", "rate limited", "service error", "model unavailable"),
            listOf("safeguards_refusal", "quota_exhausted", "rate_limited", "server_error", "model_unavailable")
                .map { resources.getString(recoveryCause(it)!!) })
        // No time named: the card offers Retry with the cause alone.
        assertEquals(listOf("Cause: quota exhausted"),
            recoveryExhaustedLines(exhausted.copy(category = "quota_exhausted", recoveryReset = null), resources, sameDay, zone))
    }

    @Test fun theExhaustedCardShowsRetryCauseAndTimeWithTheModelsTriedFolded() {
        val lines = lines("user" to "go on", "assistant" to refusedError)
        val card = lines.last() as ChatLine.ApiError
        val marker = exhausted.copy(recoveryReset = null)
        var retried = 0
        val actions = ChatRowActions(SessionKey("box", "main"), fullText = { "" }, openPath = null, photos = null,
            pictures = { emptyList() }, openImages = { _, _ -> }, menu = null,
            retryLineId = retryLine(lines, working = false, canSend = true, exhausted = true), onRetry = { retried++ },
            exhausted = exhaustedRecovery(lines, marker))
        rule.setContent { PersonaTheme(reduceMotion = true) { CompositionLocalProvider(LocalChatRowActions provides actions) { ApiErrorLine(card) } } }
        rule.onNodeWithText("Cause: the model refused").assertExists()
        rule.onNodeWithText("Models tried", substring = true).assertDoesNotExist()
        rule.onNodeWithText("Retry").performClick()
        assertEquals(1, retried)
        rule.onNodeWithText("400", substring = true).performClick()
        rule.waitForIdle()
        rule.onNodeWithText("Models tried: primary-model → fallback-a → fallback-b").assertExists()
    }

    @Test fun anOrdinaryErrorCardReadsAsBefore() {
        val lines = lines("user" to "go on", "assistant" to refusedError)
        val card = lines.last() as ChatLine.ApiError
        val actions = ChatRowActions(SessionKey("box", "main"), fullText = { "" }, openPath = null, photos = null,
            pictures = { emptyList() }, openImages = { _, _ -> }, menu = null,
            retryLineId = retryLine(lines, working = false, canSend = true), onRetry = {},
            exhausted = exhaustedRecovery(lines, runtime))
        rule.setContent { PersonaTheme(reduceMotion = true) { CompositionLocalProvider(LocalChatRowActions provides actions) { ApiErrorLine(card) } } }
        rule.onNodeWithText("Retry").assertDoesNotExist()
        rule.onNodeWithText("Cause:", substring = true).assertDoesNotExist()
        rule.onNodeWithText("Available again", substring = true).assertDoesNotExist()
    }

    @Test fun aSwitchNamesItsCause() {
        assertEquals(FallbackView("primary-model", "fallback-model", "", false, "", "quota_exhausted"), fallbackView(session(switched)))
        rule.setContent { PersonaTheme(reduceMotion = true) { ModelFallbackCard(session(switched), actions = null) } }
        rule.onNodeWithText("Switched to fallback-model (quota exhausted)").assertExists()
        rule.onNodeWithText("Fallback model: fallback-model").assertDoesNotExist()
        rule.onNodeWithText("is available again", substring = true).assertDoesNotExist()
    }

    @Test fun aConfiguredModelThatAnsweredAgainSaysTheNextMessageSwitchesBack() {
        val returning = switched.copy(recoveryReturning = true)
        assertEquals(true, fallbackView(session(returning))?.returning)
        rule.setContent { PersonaTheme(reduceMotion = true) { ModelFallbackCard(session(returning), actions = null) } }
        rule.onNodeWithText("primary-model is available again; your next message switches back").assertExists()
        rule.onNodeWithText("Switched to fallback-model (quota exhausted)").assertExists()
    }

    @Test fun aRuntimeWrittenRecordReadsAsBefore() {
        assertEquals(FallbackView("primary-model", "fallback-model", "", false, ""), fallbackView(session(runtime)))
        assertNull(recoveryCause(runtime.category))
        rule.setContent { PersonaTheme(reduceMotion = true) { ModelFallbackCard(session(runtime), actions = null) } }
        rule.onNodeWithText("Fallback model: fallback-model").assertExists()
        rule.onNodeWithText("Switched to", substring = true).assertDoesNotExist()
        rule.onNodeWithText("is available again", substring = true).assertDoesNotExist()
        // an exhausted chain has no model that answers: no fallback card, the failure card says it
        assertNull(fallbackView(session(exhausted)))
    }

    @Config(qualifiers = "ja")
    @Test fun inJapanese() {
        rule.setContent { PersonaTheme(reduceMotion = true) { ModelFallbackCard(session(switched.copy(recoveryReturning = true)), actions = null) } }
        rule.onNodeWithText("fallback-model に切り替えました（利用枠を使い切りました）").assertExists()
        rule.onNodeWithText("primary-model が再び利用可能になりました。次のメッセージで元に戻ります").assertExists()
        assertEquals("原因：モデルが拒否しました", recoveryExhaustedLines(exhausted.copy(recoveryReset = null), resources).single())
    }

    @Config(qualifiers = "zh")
    @Test fun inChinese() {
        val returning = switched.copy(recoveryReturning = true)
        rule.setContent { PersonaTheme(reduceMotion = true) { ModelFallbackCard(session(returning), actions = null) } }
        rule.onNodeWithText("已改用 fallback-model（额度已用尽）").assertExists()
        rule.onNodeWithText("primary-model 已恢复，下一条消息会切回").assertExists()
        assertEquals("原因：模型拒绝了请求", recoveryExhaustedLines(exhausted.copy(recoveryReset = null), resources).single())
    }
}

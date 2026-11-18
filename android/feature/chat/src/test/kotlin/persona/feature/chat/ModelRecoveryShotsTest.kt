package persona.feature.chat

import android.content.res.Configuration
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.contracts.ModelRefusal
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.designsystem.*
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.LineItem
import persona.core.domain.lines.chatLines
import java.io.File

/**
 * The node's own recovery (#7824) as drawn: a switch with its cause, the same switch once the configured model answered
 * again, and an exhausted chain's failure card with its cause, recovery time and Retry, in every style, light and dark.
 * PNGs in `build/model-recovery-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
class ModelRecoveryShotsTest {
    @get:Rule val rule = createComposeRule()

    private val switched = ModelRefusal("model_refusal_fallback", "primary-model", "fallback-model", "quota_exhausted", "session",
        "the account's quota was exhausted", recoveryTried = "primary-model → fallback-model")
    private val returning = switched.copy(recoveryReturning = true)
    private val exhausted = ModelRefusal("model_refusal_fallback", "primary-model", null, "quota_exhausted", "session",
        "the account's quota was exhausted", recoveryReset = "2026-11-17T04:03:00Z", recoveryExhausted = true,
        recoveryTried = "primary-model → fallback-a → fallback-b")

    private fun session(id: String, f: ModelRefusal) = Session(machineId = "box", sessionId = id, agent = "claude", status = "idle",
        assistant = true, modelFallback = f)

    private val lines = chatLines(listOf(
        LineItem(Item("u#0", 1, role = "user", kind = "text", text = "go on")),
        LineItem(Item("e#0", 2, role = "assistant", kind = "text", ts = "2026-11-15T08:01:05Z",
            text = "API Error: Request rejected (429) · upstream account quota usage limit has been reached; " +
                "an account becomes available again at 04:03 UTC (in 2d3h)")),
    )).lines
    private val card = lines.last() as ChatLine.ApiError
    private val actions = ChatRowActions(SessionKey("box", "main"), fullText = { "" }, openPath = null, photos = null,
        pictures = { emptyList() }, openImages = { _, _ -> }, menu = null,
        retryLineId = retryLine(lines, working = false, canSend = true, exhausted = true), onRetry = {},
        exhausted = exhaustedRecovery(lines, exhausted))

    @Test fun everyCaseInEveryStyleAndAppearance() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            val configuration = Configuration(LocalConfiguration.current).apply {
                uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                    if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
            }
            CompositionLocalProvider(LocalConfiguration provides configuration) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l),
                        verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        ModelFallbackCard(session("s1", switched), actions = null)
                        ModelFallbackCard(session("s2", returning), actions = null)
                        CompositionLocalProvider(LocalChatRowActions provides actions) { ApiErrorLine(card) }
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            assertEquals(2, rule.onAllNodesWithText("Switched to fallback-model (quota exhausted)").fetchSemanticsNodes().size)
            assertEquals(1, rule.onAllNodesWithText("primary-model is available again; your next message switches back").fetchSemanticsNodes().size)
            assertEquals(1, rule.onAllNodesWithText("Cause: quota exhausted").fetchSemanticsNodes().size)
            assertEquals(1, rule.onAllNodesWithText("Available again at", substring = true).fetchSemanticsNodes().size)
            assertEquals(1, rule.onAllNodesWithText("Retry").fetchSemanticsNodes().size)
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            val out = File(System.getProperty("persona.shots") ?: "build/model-recovery-shots").apply { mkdirs() }
            File(out, "model-recovery-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)
            }
        }
    }
}

package persona.feature.chat

import android.content.Context
import android.graphics.Bitmap
import android.provider.Settings
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.junit.After
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.SessionKey
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * The @ list in an Agent's chat and in Persona's conversation, and the + menu (#4339 C16), on the real page over a fake
 * computer: "@" typed (the files its tools used, its folder's entries, the sessions), a folder opened (`@app/`), and the
 * + menu with its Mention item. Every style, light and dark. PNGs in `build/chat-mention-shots/` (or
 * `-Dpersona.shots=<dir>`). The same test on `main` draws the before: no entries, no Mention in the menu.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class ChatMentionShotsTest {
    @get:Rule val rule = createComposeRule()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val computer = MentionChatHarness(scope)
    @After fun close() { scope.cancel() }

    private fun out() = File(System.getProperty("persona.shots") ?: "build/chat-mention-shots").also { it.mkdirs() }
    private fun name(style: AppStyle, dark: Boolean) = "${style.name.lowercase()}-${if (dark) "dark" else "light"}"
    private fun save(bitmap: Bitmap, name: String) = File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }

    private fun settle() {
        rule.waitForIdle()
        // The folder read waits a moment (120 ms) before it asks; give it that and the frames after it.
        rule.mainClock.advanceTimeBy(600)
        rule.waitForIdle()
    }

    private fun scenes(key: SessionKey, style: AppStyle, dark: Boolean, scenes: Map<String, String>, menu: Boolean) {
        Settings.Global.putFloat(ApplicationProvider.getApplicationContext<Context>().contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 0f)
        val composer = ChatComposerState()
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("page")) { computer.Page(key, composer) }
            }
        }
        val ready = if (key == computer.agent) "Found it" else "The web update"
        rule.waitUntil(20_000) { rule.onAllNodesWithText(ready, substring = true).fetchSemanticsNodes().isNotEmpty() }
        val prefix = if (key == computer.agent) "agent" else "persona"
        for ((scene, text) in scenes) {
            rule.runOnUiThread { composer.value = TextFieldValue(text, TextRange(text.length)) }
            settle()
            save(rule.onNodeWithTag("page").captureToImage().asAndroidBitmap(), "$prefix-$scene-${name(style, dark)}")
        }
        if (menu) {
            rule.runOnUiThread { composer.value = TextFieldValue() }
            rule.waitForIdle()
            rule.onNodeWithContentDescription("Add attachments and quick messages").performClick()
            rule.waitForIdle()
            save(rule.onNode(isDialog()).captureToImage().asAndroidBitmap(), "$prefix-menu-${name(style, dark)}")
        }
    }

    private fun agent(style: AppStyle, dark: Boolean) =
        scenes(computer.agent, style, dark, mapOf("at" to "Look at @", "folder" to "Look at @app/"), menu = true)
    private fun persona(style: AppStyle, dark: Boolean) = scenes(computer.persona, style, dark, mapOf("at" to "@"), menu = false)

    @Test fun agentGlassLight() = agent(AppStyle.Glass, false)
    @Test fun agentGlassDark() = agent(AppStyle.Glass, true)
    @Test fun agentBriefLight() = agent(AppStyle.Brief, false)
    @Test fun agentBriefDark() = agent(AppStyle.Brief, true)
    @Test fun agentBoardLight() = agent(AppStyle.Board, false)
    @Test fun agentBoardDark() = agent(AppStyle.Board, true)
    @Test fun personaGlassLight() = persona(AppStyle.Glass, false)
    @Test fun personaGlassDark() = persona(AppStyle.Glass, true)
    @Test fun personaBriefLight() = persona(AppStyle.Brief, false)
    @Test fun personaBriefDark() = persona(AppStyle.Brief, true)
    @Test fun personaBoardLight() = persona(AppStyle.Board, false)
    @Test fun personaBoardDark() = persona(AppStyle.Board, true)
}

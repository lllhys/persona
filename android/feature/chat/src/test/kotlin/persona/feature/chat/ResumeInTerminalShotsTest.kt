package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.SessionCapabilities
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * Resume in Terminal on the Agent chat's ⋯ sheet (#5565): offered for a stopped Agent, and disabled with why when the
 * computer enables no terminal engine. Every style, light and dark. PNGs in `build/resume-in-terminal-shots/`
 * (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class ResumeInTerminalShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/resume-in-terminal-shots").also { it.mkdirs() }

    private val stoppedCaps = SessionCapabilities(read = true, terminal = true)
    private val offeredSession = Session(
        machineId = "macbook",
        sessionId = "5e000000-0000-4000-8000-000000005521",
        title = "整理发布说明",
        cwd = "/Users/me/code/weather",
        status = "offline",
        agent = "claude",
        agentSurface = "cli",
        capabilities = stoppedCaps,
        readOnlyReason = "stopped",
        paneId = "p1",
    )
    private val noEngineSession = offeredSession.copy(
        machineId = "mini",
        sessionId = "5e000000-0000-4000-8000-000000005523",
        title = "备份照片库",
        cwd = "/Users/me/Pictures",
    )
    private val withEngine = Machine(id = "macbook", name = "MacBook", online = true, engineHosts = listOf("herdr"))
    private val noEngine = Machine(id = "mini", name = "Mac mini", online = true, engineHosts = emptyList())

    private fun save(bitmap: Bitmap, name: String) {
        File(out(), "$name.png").outputStream().use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
    }

    @Test fun offeredAndNoEngineEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var noEngineVariant by mutableStateOf(false)
        var round by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val scope = rememberCoroutineScope()
                val resources = androidx.compose.ui.platform.LocalResources.current
                val chrome = remember { ChatChromeState(resources, null, scope, {}, {}) }
                // Remount each look so the sheet does not keep a prior theme's first frame (ChatTerminalShotsTest).
                key(round) {
                    val session = if (noEngineVariant) noEngineSession else offeredSession
                    val machine = if (noEngineVariant) noEngine else withEngine
                    Box(
                        Modifier
                            .testTag("shot")
                            .width(400.dp)
                            .height(860.dp)
                            .fillMaxWidth()
                            .background(PersonaTheme.palette.background),
                    ) {
                        ChatHeader(
                            session, machine, listOf(session), chrome, null, false,
                            onBack = {}, onOpenSession = {}, onConnectionDetails = {}, onOverlay = {},
                            onTerminal = {}, onResumeInTerminal = {},
                        )
                    }
                }
            }
        }
        for (variant in listOf(false, true)) for (s in AppStyle.entries) for (d in listOf(false, true)) {
            rule.runOnIdle {
                noEngineVariant = variant
                style = s
                dark = d
                round++
            }
            rule.waitForIdle()
            rule.onNodeWithContentDescription("More actions").performClick()
            rule.waitForIdle()
            repeat(4) { rule.mainClock.advanceTimeByFrame(); rule.waitForIdle() }
            rule.onNodeWithText("Resume in Terminal").assertExists()
            if (variant) rule.onNodeWithText("No terminal engine on Mac mini").assertExists()
            val name = "resume-${if (variant) "no-engine" else "offered"}-${s.name.lowercase()}-${if (d) "dark" else "light"}"
            save(rule.onNode(isDialog()).captureToImage().asAndroidBitmap(), name)
        }
    }
}

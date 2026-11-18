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
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertHasClickAction
import androidx.compose.ui.test.assertHasNoClickAction
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.Flow
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.SessionCapabilities
import persona.core.contracts.api.ChatAction
import persona.core.contracts.api.ChatActionResult
import persona.core.contracts.api.ChatActions
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.domain.sort.SessionRenamer
import java.io.File

/**
 * The Agent chat's ⋯ sheet for an agent runtime's session (#6294, a Paseo row): no terminal, yet Rename and Close agent,
 * since its node says it can; then Close agent's confirmation (the session ends, no tab or pane). The rename dialog's
 * note is `ChatChromeTest`'s: a dialog with a text field never lets Compose idle under Robolectric. Every style, light
 * and dark. PNGs in `build/runtime-session-actions-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1000dp-xhdpi")
class RuntimeSessionActionsShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/runtime-session-actions-shots").also { it.mkdirs() }

    private val session = Session(
        machineId = "macbook",
        sessionId = "paseo-agent-1",
        title = "Draft the release notes",
        cwd = "/Users/me/code/weather",
        status = "idle",
        state = "idle",
        agent = "claude",
        sourceKind = "runtime",
        spaceKind = "paseo",
        spaceName = "weather",
        // A Paseo row names its workspace here: not a terminal tab.
        slotName = "release-notes",
        capabilities = SessionCapabilities(read = true, send = true, interrupt = true, approve = true, close = true, rename = true, model = true),
    )
    private val machine = Machine(id = "macbook", name = "MacBook", online = true)

    private val actions = object : ChatActions {
        override suspend fun isMuted(session: Session) = false
        override suspend fun execute(session: Session, action: ChatAction) = ChatActionResult.Done
        override suspend fun verify(session: Session, action: ChatAction) = ChatActionResult.Done
    }
    private val renamer = object : SessionRenamer {
        override fun project(source: Flow<List<Session>>) = source
        override suspend fun rename(session: Session, title: String?, tabName: String?) = true
    }

    private fun save(bitmap: Bitmap, name: String) {
        File(out(), "$name.png").outputStream().use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
    }

    private fun settle() {
        rule.waitForIdle()
        repeat(4) { rule.mainClock.advanceTimeByFrame(); rule.waitForIdle() }
    }

    private fun openMenu() {
        rule.onNodeWithContentDescription("More actions").performClick()
        settle()
    }

    @Test fun menuAndCloseEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var round by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val scope = rememberCoroutineScope()
                val resources = androidx.compose.ui.platform.LocalResources.current
                val chrome = remember { ChatChromeState(resources, actions, scope, {}, {}, renamer) }
                // Remount each look so the sheet does not keep a prior theme's first frame (ChatTerminalShotsTest).
                key(round) {
                    Box(Modifier.testTag("shot").width(400.dp).height(960.dp).fillMaxWidth().background(PersonaTheme.palette.background)) {
                        ChatHeader(session, machine, listOf(session), chrome, null, false,
                            onBack = {}, onOpenSession = {}, onConnectionDetails = {}, onOverlay = {})
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            rule.runOnIdle { style = s; dark = d; round++ }
            settle()
            val look = "${s.name.lowercase()}-${if (d) "dark" else "light"}"

            openMenu()
            rule.onNodeWithText("Rename").assertHasClickAction()
            rule.onNodeWithText("Close agent").assertHasClickAction()
            // Idle: nothing to interrupt. No terminal: no Terminal and no resume command.
            rule.onNodeWithText("Interrupt").assertHasNoClickAction()
            rule.onAllNodesWithText("Terminal").assertCountEquals(0)
            rule.onAllNodesWithText("Copy resume command").assertCountEquals(0)
            save(rule.onNode(isDialog()).captureToImage().asAndroidBitmap(), "runtime-actions-$look")

            rule.onNodeWithText("Close agent").performClick()
            settle()
            rule.onNodeWithText("This ends the session on MacBook. Work in progress stops.", substring = true).assertExists()
            rule.onAllNodesWithText("terminal", substring = true, ignoreCase = true).assertCountEquals(0)
            save(rule.onNode(isDialog()).captureToImage().asAndroidBitmap(), "runtime-close-$look")
            rule.onNodeWithText("Cancel").performClick()
            settle()
        }
    }
}

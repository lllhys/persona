package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
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
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.contracts.SessionCapabilities
import persona.core.contracts.api.*
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * Brief and Board's "Mode" sheet for an agent whose model the node switches itself (#8143, a Paseo provider other than
 * Claude Code and Codex): its launcher's models (the one it runs checked) and the efforts that model takes, no Claude
 * Code permission modes; a pick goes as `POST …/modes {model}`. Every style, light and dark, as the sheet the composer's
 * Mode button opens. PNGs in `build/paseo-mode-sheet-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1000dp-xhdpi")
class PaseoModeSheetShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/paseo-mode-sheet-shots").also { it.mkdirs() }

    private val grok = Session(machineId = "macbook", sessionId = "paseo-agent-2", title = "Sort the photo albums", cwd = "/Users/me/code/albums",
        status = "idle", state = "idle", agent = "paseo", agentLabelHint = "Grok", launcher = "grok", model = "grok-5", effort = "low",
        sourceKind = "runtime", spaceKind = "paseo",
        capabilities = SessionCapabilities(read = true, send = true, interrupt = true, approve = true, close = true, rename = true, modes = false, model = true))

    private val sent = mutableListOf<ChatAction>()
    private val actions = object : ChatActions {
        override suspend fun isMuted(session: Session) = false
        override suspend fun execute(session: Session, action: ChatAction): ChatActionResult = ChatActionResult.Done.also { sent += action }
        override suspend fun verify(session: Session, action: ChatAction) = ChatActionResult.Done
    }
    private val asked = mutableListOf<String>()
    private val creation = object : AgentCreationService {
        override val scope = "test"
        override suspend fun launchers(machineId: String) = listOf(AgentLauncher("grok", agentType = "paseo"))
        override suspend fun directories(machineId: String, path: String) = AgentDirectories(path)
        override suspend fun start(request: AgentStartRequest, operationId: String) = AgentStartResult.Unknown
        override suspend fun operation(operationId: String) = AgentStartResult.Unknown
        override suspend fun launcherModels(machineId: String, launcher: String, refresh: Boolean): LauncherModels {
            asked += "$machineId/$launcher"
            return LauncherModels("paseo", listOf(
                AgentModel("grok-5", "Grok 5", description = "The latest model", isDefault = true, efforts = listOf("low", "high"), defaultEffort = "high"),
                AgentModel("grok-5-fast", "Grok 5 Fast")))
        }
    }

    private fun settle() {
        rule.waitForIdle()
        repeat(4) { rule.mainClock.advanceTimeByFrame(); rule.waitForIdle() }
    }

    private fun inSheet(text: String) = hasText(text) and hasAnyAncestor(isDialog())

    @Test fun theSheetListsTheProvidersModelsAndTheEffortsOfTheOneItRuns() {
        var style by mutableStateOf(AppStyle.Brief)
        var dark by mutableStateOf(false)
        var round by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val scope = rememberCoroutineScope()
                val resources = androidx.compose.ui.platform.LocalResources.current
                val chrome = remember { ChatChromeState(resources, actions, scope, {}, {}) }
                key(round) {
                    Box(Modifier.width(400.dp).height(960.dp).background(PersonaTheme.palette.background)) {
                        ChatModeBar(grok, chrome, online = true, creation = { creation })
                    }
                }
            }
        }
        assertTrue(SessionModes.listsModels(grok))
        assertFalse(SessionModes.takesModes(grok))
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            rule.runOnIdle { style = s; dark = d; round++ }
            settle()
            val look = "${s.name.lowercase()}-${if (d) "dark" else "light"}"
            rule.onNodeWithContentDescription("Mode").performClick()
            rule.waitUntil(5_000) { rule.onAllNodes(inSheet("Grok 5 Fast"), useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }
            settle()
            // Its models, the one it runs said in its own words; the efforts that one takes, the one it recommends said so;
            // neither Claude Code's permission modes nor its effort descriptions.
            for (text in listOf("Model", "Grok 5", "The latest model", "Thinking effort", "Low", "High", "Recommended")) {
                assertTrue("$text in $look", rule.onAllNodes(inSheet(text), useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
            }
            rule.onAllNodesWithText("File permissions").assertCountEquals(0)
            rule.onAllNodesWithText("Very high").assertCountEquals(0)
            rule.onAllNodesWithText("The default for most tasks").assertCountEquals(0)
            File(out(), "paseo-mode-sheet-$look.png").outputStream().use {
                assertTrue(rule.onNode(isDialog()).captureToImage().asAndroidBitmap().compress(Bitmap.CompressFormat.PNG, 100, it))
            }
            rule.onNode(inSheet("Grok 5 Fast"), useUnmergedTree = true).performClick()
            settle()
        }
        // Each opening reads the list again; each pick switched the model of the session's own launcher.
        assertEquals(List(6) { "macbook/grok" }, asked)
        rule.waitUntil(5_000) { sent.size == 6 }
        assertEquals(List(6) { ChatAction.Modes(model = "grok-5-fast") }, sent)
    }

    /**
     * Claude Code keeps its sheet: in a terminal its permission modes, its efforts with their words and /model's aliases,
     * no list read; on Paseo (no pane, the node switches the model) the same modes and efforts, and its launcher's models.
     */
    @Test fun claudeCodeKeepsItsModesAndEffortsWithTheModelsWhereTheNodeSwitchesThem() {
        val terminal = Session(machineId = "macbook", sessionId = "claude-1", status = "idle", state = "idle", agent = "claude", paneId = "w1:p1",
            launcher = "claude", model = "claude-opus-5-5", effort = "high", permissionMode = "default")
        val paseoClaude = terminal.copy(sessionId = "paseo-agent-3", paneId = null, launcher = "my-claude", sourceKind = "runtime", spaceKind = "paseo",
            capabilities = SessionCapabilities(read = true, send = true, interrupt = true, approve = true, modes = true, model = true))
        var shown by mutableStateOf(terminal)
        rule.setContent {
            PersonaTheme(reduceMotion = true, style = AppStyle.Brief) {
                val scope = rememberCoroutineScope()
                val resources = androidx.compose.ui.platform.LocalResources.current
                val chrome = remember { ChatChromeState(resources, actions, scope, {}, {}) }
                key(shown.sessionId) { ChatModeBar(shown, chrome, online = true, onPickModel = {}, creation = { creation }) }
            }
        }
        assertFalse(SessionModes.listsModels(terminal))
        assertTrue(SessionModes.listsModels(paseoClaude) && SessionModes.takesModes(paseoClaude))
        for (session in listOf(terminal, paseoClaude)) {
            rule.runOnIdle { shown = session }
            settle()
            rule.onNodeWithContentDescription("Mode").performClick()
            if (session == paseoClaude) rule.waitUntil(5_000) { rule.onAllNodes(inSheet("Grok 5 Fast"), useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }
            settle()
            for (text in listOf("File permissions", "Thinking effort", "The default for most tasks", "Model")) {
                assertTrue("$text for ${session.sessionId}", rule.onAllNodes(inSheet(text), useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
            }
            // /model's aliases in a terminal only; the launcher's list only where the node switches the model.
            assertEquals(session == terminal, rule.onAllNodes(inSheet("Opus"), useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
            assertEquals(session == paseoClaude, rule.onAllNodes(inSheet("Grok 5 Fast"), useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        }
        assertEquals(listOf("macbook/my-claude"), asked)
    }
}

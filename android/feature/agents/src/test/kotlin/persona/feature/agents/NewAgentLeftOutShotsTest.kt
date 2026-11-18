package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.runtime.*
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.lifecycle.viewModelScope
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File

/**
 * New Agent on a computer where Claude Code and Codex are signed in (#8109, the web's New Agent panel from #8122): the
 * Persona-managed providers their own-service rule leaves out there come after the launchers of the type they run, off,
 * with the hub's reason, and How to fix opens the provider's page in Settings; nothing of them is picked. On a computer
 * of another group of hubs only the words. Every style, light and dark. PNGs in `build/new-agent-left-out-shots/` (or
 * `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1400dp-xhdpi")
class NewAgentLeftOutShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val reason = "Studio already has its own Claude Code service, and DeepSeek is set to skip such computers. Turn on " +
        "“Also use on computers with their own sign-in” in its Devices, or pick another provider."
    private val launchers = listOf(AgentLauncher("claude", displayName = "Claude Code"), AgentLauncher("work_gw", model = "glm-5"),
        AgentLauncher("codex", agentType = "codex", promptOptional = true))
    private val unavailable = listOf(
        UnavailableLauncher("deepseek", "DeepSeek", "launcher", "claude", code = "launcher_own_service", reason = reason),
        UnavailableLauncher("claude", kind = "launcher", agent = "claude", code = "launcher_own_service", reason = "not this one"),
        UnavailableLauncher("cx-gw", kind = "launcher", agent = "codex", code = "launcher_own_service", reason = "Codex is signed in on Studio."),
    )
    private fun service() = object : AgentCreationService {
        override val scope = "hub/pairing"
        override suspend fun launchers(machineId: String) = launchers
        override suspend fun launcherList(machineId: String) = AssistantLaunchers(launchers, unavailable = unavailable)
        override suspend fun directories(machineId: String, path: String) = AgentDirectories("/code/project")
        override suspend fun start(request: AgentStartRequest, operationId: String) = AgentStartResult.Unknown
        override suspend fun operation(operationId: String) = AgentStartResult.Unknown
    }
    private fun store(machine: String) = object : AgentCreationStore {
        override suspend fun lastMachine(scope: String) = machine
        override suspend fun defaults(scope: String, machine: String) = AgentDefaults(cwd = "/code/project", launcher = "claude")
        override suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults) = Unit
        override suspend fun pending(scope: String): AgentStartOperation? = null
        override suspend fun pending(scope: String, value: AgentStartOperation?) = Unit
    }

    @Test fun aProviderLeftOutHereSaysWhyAndHowToFixOpensItsPage() {
        val computers = listOf(Machine("computer", "Studio", online = true))
        val model = NewAgentViewModel(service(), store("computer"), MutableStateFlow(computers))
        val opened = mutableListOf<String>()
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                NewAgentPage(model, computers, online = true, onBack = {}, onOpenProvider = { opened += it })
            }
        }
        try {
            rule.waitUntil(10_000) { model.state.value.launchers.isNotEmpty() && !model.state.value.loading }
            val res = rule.activity.resources
            val fix = res.getString(R.string.agents_launcher_help_how)
            val out = File(System.getProperty("persona.shots") ?: "build/new-agent-left-out-shots").also { it.mkdirs() }
            for (s in AppStyle.entries) for (d in listOf(false, true)) {
                rule.runOnIdle { style = s; dark = d }
                rule.waitForIdle()
                rule.onNode(hasScrollToNodeAction()).performScrollToNode(hasText("DeepSeek"))
                rule.onNodeWithText("DeepSeek").assertIsDisplayed()
                rule.onNodeWithText(reason, substring = true).assertIsDisplayed()
                // one the computer lists already stays as listed; another type's is under that type
                rule.onAllNodesWithText("not this one", substring = true).assertCountEquals(0)
                rule.onAllNodesWithText("cx-gw").assertCountEquals(0)
                rule.onAllNodesWithTag("newAgent.leftOut").assertCountEquals(1)
                rule.onNodeWithText(fix).assertIsDisplayed()
                rule.onRoot().captureToImage().asAndroidBitmap().let { bitmap ->
                    out.resolve("new-agent-left-out-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                        bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it)
                    }
                }
            }
            rule.onNodeWithText(fix).performClick()
            rule.waitForIdle()
            assertEquals(listOf("deepseek"), opened)
            // nothing of it is picked: the launcher that runs stays
            assertEquals("claude", model.state.value.selection.launcher)
            // the Codex type lists the Codex one, with its own words
            rule.runOnIdle { style = AppStyle.Glass; dark = false; model.selection(model.state.value.selection.copy(launcher = "codex")) }
            rule.waitForIdle()
            rule.onNode(hasScrollToNodeAction()).performScrollToNode(hasText("cx-gw"))
            rule.onNodeWithText("Codex is signed in on Studio.", substring = true).assertIsDisplayed()
            rule.onAllNodesWithText("DeepSeek").assertCountEquals(0)
            rule.onAllNodesWithTag("newAgent.leftOut").assertCountEquals(1)
            rule.onRoot().captureToImage().asAndroidBitmap().let { bitmap ->
                out.resolve("new-agent-left-out-codex-glass-light.png").outputStream().use { bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
            }
        } finally { model.viewModelScope.cancel() }
    }

    /** The largest text: the hub's words wrap in full beside How to fix, light and dark. */
    @Test @Config(sdk = [35], qualifiers = "en-w400dp-h2400dp-xhdpi", fontScale = 2f)
    fun theLargestTextShowsTheWholeReason() {
        val computers = listOf(Machine("computer", "Studio", online = true))
        val model = NewAgentViewModel(service(), store("computer"), MutableStateFlow(computers))
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = dark, reduceMotion = true) {
                NewAgentPage(model, computers, online = true, onBack = {}, onOpenProvider = {})
            }
        }
        try {
            rule.waitUntil(10_000) { model.state.value.launchers.isNotEmpty() && !model.state.value.loading }
            val out = File(System.getProperty("persona.shots") ?: "build/new-agent-left-out-shots").also { it.mkdirs() }
            for (d in listOf(false, true)) {
                rule.runOnIdle { dark = d }
                rule.waitForIdle()
                rule.onNode(hasScrollToNodeAction()).performScrollToNode(hasTestTag("newAgent.leftOut"))
                rule.onNodeWithText(reason, substring = true).assertIsDisplayed()
                rule.onNodeWithText(rule.activity.getString(R.string.agents_launcher_help_how)).assertIsDisplayed()
                rule.onRoot().captureToImage().asAndroidBitmap().let { bitmap ->
                    out.resolve("new-agent-left-out-glass-${if (d) "dark" else "light"}-large.png").outputStream().use {
                        bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it)
                    }
                }
            }
        } finally { model.viewModelScope.cancel() }
    }

    /** A computer of another group of hubs: Settings is the primary group's, so the row says why and offers no page. */
    @Test fun anotherGroupsComputerGetsTheWordsOnly() {
        val computers = listOf(Machine("computer~g0a1b2c3d", "Studio", online = true))
        val model = NewAgentViewModel(service(), store("computer~g0a1b2c3d"), MutableStateFlow(computers))
        val opened = mutableListOf<String>()
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = true) {
                NewAgentPage(model, computers, online = true, onBack = {}, onOpenProvider = { opened += it })
            }
        }
        try {
            rule.waitUntil(10_000) { model.state.value.launchers.isNotEmpty() && !model.state.value.loading }
            rule.onNode(hasScrollToNodeAction()).performScrollToNode(hasText("DeepSeek"))
            rule.onNodeWithText(reason, substring = true).assertIsDisplayed()
            rule.onAllNodesWithText(rule.activity.getString(R.string.agents_launcher_help_how)).assertCountEquals(0)
            rule.onNodeWithText("DeepSeek").performClick()
            rule.waitForIdle()
            assertTrue(opened.isEmpty())
            assertEquals("claude", model.state.value.selection.launcher)
        } finally { model.viewModelScope.cancel() }
    }
}

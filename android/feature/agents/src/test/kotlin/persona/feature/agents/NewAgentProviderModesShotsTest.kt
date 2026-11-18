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
import org.junit.Assert.assertFalse
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
 * New Agent on Paseo's Grok (#8261): the launcher by its own name, a Permission mode field with its own approval ("Follow
 * launcher") and what it says, Allow everything with its warning, and only the efforts its model lists; none of Claude
 * Code's modes, nor the one remembered for them. Every style, light and dark. PNGs in `build/new-agent-modes-shots/`
 * (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1600dp-xhdpi")
class NewAgentProviderModesShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val computers = listOf(Machine("computer", "Studio", online = true, engine = "paseo"))
    private val launchers = listOf(AgentLauncher("claude"),
        AgentLauncher("grok", model = "Grok 5", agentType = PaseoAgent, displayName = "Grok", permissionModes = listOf("default", "bypassPermissions")))
    private val service = object : AgentCreationService {
        override val scope = "hub/pairing"
        override suspend fun launchers(machineId: String) = launchers
        override suspend fun directories(machineId: String, path: String) = AgentDirectories("/code/project")
        override suspend fun start(request: AgentStartRequest, operationId: String) = AgentStartResult.Unknown
        override suspend fun operation(operationId: String) = AgentStartResult.Unknown
        override suspend fun launcherModels(machineId: String, launcher: String, refresh: Boolean) = if (launcher != "grok") null
            else LauncherModels(PaseoAgent, listOf(AgentModel("grok-5", "Grok 5", isDefault = true, efforts = listOf("low", "high"), defaultEffort = "high")))
    }
    // Claude Code's choice remembered on this computer (Plan only): never Grok's.
    private val store = object : AgentCreationStore {
        override suspend fun lastMachine(scope: String) = "computer"
        override suspend fun defaults(scope: String, machine: String) = AgentDefaults(cwd = "/code/project", launcher = "grok", mode = "plan")
        override suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults) = Unit
        override suspend fun pending(scope: String): AgentStartOperation? = null
        override suspend fun pending(scope: String, value: AgentStartOperation?) = Unit
    }

    @Test fun grokOffersItsOwnApprovalOrAllowEverythingAndItsModelsEfforts() {
        val model = NewAgentViewModel(service, store, MutableStateFlow(computers))
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                NewAgentPage(model, computers, online = true, onBack = {})
            }
        }
        try {
            rule.waitUntil(10_000) { model.state.value.launchers.isNotEmpty() && !model.state.value.loading && model.state.value.modelList != null }
            assertEquals(listOf("default", "bypassPermissions"), model.state.value.providerModes)
            val res = rule.activity.resources
            val follow = res.getString(R.string.agents_mode_follow)
            val hint = res.getString(R.string.agents_new_provider_mode_hint)
            val warning = res.getString(R.string.agents_new_bypass_warning)
            val out = File(System.getProperty("persona.shots") ?: "build/new-agent-modes-shots").also { it.mkdirs() }
            for (s in AppStyle.entries) for (d in listOf(false, true)) for (bypass in listOf(false, true)) {
                // Its own approval and effort; then Allow everything, with an effort its model lists picked.
                rule.runOnIdle {
                    style = s; dark = d
                    model.providerMode(if (bypass) "bypassPermissions" else "")
                    model.providerEffort(if (bypass) "high" else "")
                }
                rule.waitForIdle()
                rule.onNode(hasScrollToNodeAction()).performScrollToNode(hasText(res.getString(R.string.agents_new_effort)))
                rule.onNodeWithText("Grok").assertExists()
                rule.onNodeWithText(if (bypass) warning else hint).assertIsDisplayed()
                if (bypass) {
                    rule.onNodeWithText(res.getString(R.string.agents_mode_bypass)).assertIsDisplayed()
                    rule.onNodeWithText(res.getString(R.string.agents_effort_high)).assertIsDisplayed()
                } else rule.onAllNodesWithText(follow).assertCountEquals(2) // the mode and the effort, each its own
                // Claude Code's remembered Plan only is not this start's.
                rule.onAllNodesWithText(res.getString(R.string.agents_mode_plan)).assertCountEquals(0)
                rule.onRoot().captureToImage().asAndroidBitmap().let { bitmap ->
                    val name = "new-agent-modes-${s.name.lowercase()}-${if (d) "dark" else "light"}${if (bypass) "-allow-everything" else ""}.png"
                    out.resolve(name).outputStream().use { bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
                }
            }
            assertFalse(model.state.value.claudeModes)
        } finally { model.viewModelScope.cancel() }
    }
}

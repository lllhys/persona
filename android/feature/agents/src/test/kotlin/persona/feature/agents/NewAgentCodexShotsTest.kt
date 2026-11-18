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
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class NewAgentCodexShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val computers = listOf(Machine("computer", "Studio", online = true))
    private val service = object : AgentCreationService {
        override val scope = "hub/pairing"
        override suspend fun launchers(machineId: String) = listOf(
            AgentLauncher("codex", agentType = "codex", promptOptional = true),
            AgentLauncher("older", agentType = "codex"))
        override suspend fun directories(machineId: String, path: String) = AgentDirectories("/code/project")
        override suspend fun start(request: AgentStartRequest, operationId: String) = AgentStartResult.Unknown
        override suspend fun operation(operationId: String) = AgentStartResult.Unknown
    }
    private val store = object : AgentCreationStore {
        override suspend fun lastMachine(scope: String) = "computer"
        override suspend fun defaults(scope: String, machine: String) = AgentDefaults()
        override suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults) = Unit
        override suspend fun pending(scope: String): AgentStartOperation? = null
        override suspend fun pending(scope: String, value: AgentStartOperation?) = Unit
    }

    @Test fun optionalAndRequiredFirstMessageInEveryStyleLightAndDark() {
        val model = NewAgentViewModel(service, store, MutableStateFlow(computers))
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                NewAgentPage(model, computers, online = true, onBack = {})
            }
        }
        try {
            rule.waitUntil(10_000) { model.state.value.launchers.isNotEmpty() && !model.state.value.loading }
            val out = File(System.getProperty("persona.shots") ?: "build/codex-waiting-shots").also { it.mkdirs() }
            for (s in AppStyle.entries) for (d in listOf(false, true)) for (optional in listOf(true, false)) {
                rule.runOnIdle {
                    style = s; dark = d
                    model.selection(model.state.value.selection.copy(launcher = if (optional) "codex" else "older"))
                }
                rule.waitForIdle()
                val label = rule.activity.getString(if (optional) R.string.agents_new_first_message_codex_optional else R.string.agents_new_first_message_codex)
                rule.onNode(hasScrollToNodeAction()).performScrollToNode(hasText(label))
                rule.onNodeWithText(label).assertIsDisplayed()
                val start = rule.onNodeWithText(rule.activity.getString(R.string.agents_new_start))
                if (optional) start.assertIsEnabled() else start.assertIsNotEnabled()
                rule.onRoot().captureToImage().asAndroidBitmap().let { bitmap ->
                    out.resolve("new-${if (optional) "optional" else "required"}-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                        bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it)
                    }
                }
            }
        } finally { model.viewModelScope.cancel() }
    }
}

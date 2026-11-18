package persona.feature.terminal

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.NewPaneTab
import persona.core.contracts.api.PaneHost
import persona.core.contracts.api.PanesResponse
import persona.core.contracts.api.SessionsRepository
import persona.core.contracts.api.TerminalPanes
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import persona.core.navigation.TerminalRoute
import java.io.File

/** Both empty states in every style, appearance and font scale, plus the New Terminal click through the real screen. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class TerminalPanesShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private class Panes : TerminalPanes {
        override val available = MutableStateFlow(true)
        var response = PanesResponse(listOf(PaneHost("herdr", stream = true, control = true, newTab = true)))
        var newSpace = true
        override suspend fun panes(machineId: String) = response
        override fun opensNewSpaces(machineId: String) = newSpace
        override suspend fun newTab(machineId: String, workspaceId: String, kind: String?, cwd: String?): NewPaneTab {
            assertEquals("", workspaceId)
            assertEquals("herdr", kind)
            assertEquals("~", cwd)
            return NewPaneTab("w1:p4", "pane:w1:p4", "w1:t4")
        }
    }

    private class Sessions : SessionsRepository {
        override val machines = MutableStateFlow(listOf(Machine("desk", "Workstation", online = true)))
        override val sessions = MutableStateFlow(emptyList<Session>())
        override suspend fun refresh() = Unit
    }

    private class Routes : Navigator {
        val opened = mutableListOf<NavKey>()
        override fun navigate(key: NavKey) { opened += key }
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }

    @Test fun emptyStatesAndNewTerminal() {
        val panes = Panes()
        val model = TerminalPanesViewModel(panes, Sessions())
        val navigator = Routes()
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var fontScale by mutableStateOf(1f)
        rule.setContent {
            val density = LocalDensity.current
            CompositionLocalProvider(LocalDensity provides Density(density.density, fontScale)) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Box(Modifier.testTag("shot").width(400.dp).height(900.dp).background(PersonaTheme.palette.background)) {
                        TerminalPanesScreen(model, navigator)
                    }
                }
            }
        }
        val output = File(System.getProperty("persona.shots") ?: "build/terminal-panes-shots").also { it.mkdirs() }
        for ((name, response, text) in listOf(
            Triple("new-terminal", PanesResponse(listOf(PaneHost("herdr", stream = true, control = true, newTab = true))),
                rule.activity.getString(R.string.terminal_panes_no_panes)),
            Triple("agent-needed", PanesResponse(listOf(PaneHost("herdr", stream = true, control = true))),
                rule.activity.getString(R.string.terminal_panes_no_panes_agent)),
        )) {
            panes.response = response
            model.refresh()
            rule.waitForIdle()
            rule.onNodeWithText(text).assertExists()
            for (nextStyle in AppStyle.entries) for (nextDark in listOf(false, true)) for (scale in listOf(1f, 2f)) {
                style = nextStyle
                dark = nextDark
                fontScale = scale
                rule.waitForIdle()
                val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                val suffix = "${nextStyle.name.lowercase()}-${if (nextDark) "dark" else "light"}-${if (scale == 1f) "default" else "large"}"
                File(output, "terminal-panes-$name-$suffix.png").outputStream().use {
                    check(image.compress(Bitmap.CompressFormat.PNG, 100, it))
                }
            }
            if (name == "new-terminal") {
                rule.onNodeWithText(rule.activity.getString(R.string.terminal_panes_new_terminal)).performClick()
                rule.waitForIdle()
                assertEquals(listOf(TerminalRoute("desk", "pane:w1:p4", rule.activity.getString(R.string.terminal_panes_untitled))), navigator.opened)
            } else {
                rule.onNodeWithText(rule.activity.getString(R.string.terminal_panes_new_terminal)).assertDoesNotExist()
            }
        }
        // A hub without pane_new_space keeps the Agent-start hint even though the host can open workspace tabs.
        panes.newSpace = false
        panes.response = PanesResponse(listOf(PaneHost("herdr", stream = true, control = true, newTab = true)))
        model.refresh()
        rule.waitForIdle()
        rule.onNodeWithText(rule.activity.getString(R.string.terminal_panes_no_panes_agent)).assertExists()
        rule.onNodeWithText(rule.activity.getString(R.string.terminal_panes_new_terminal)).assertDoesNotExist()
    }
}

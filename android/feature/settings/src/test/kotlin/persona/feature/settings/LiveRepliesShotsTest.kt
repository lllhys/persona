package persona.feature.settings

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.AgentLauncher
import persona.core.contracts.api.ClaudeSetting
import persona.core.contracts.api.HerdrState
import persona.core.contracts.api.LauncherQuotaAnswer
import persona.core.contracts.api.MachineManagement
import persona.core.contracts.api.SessionsRepository
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import java.io.File

/**
 * Native JVM renders of the Live Replies section inside [ClaudeSettingsPage] across Glass, Brief and Board,
 * light and dark; written to `build/live-replies-shots/` to inspect.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class LiveRepliesShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val studio = Machine("hub", name = "Studio", kind = "local", online = true)
    private val laptop = Machine("mac", name = "Laptop", kind = "ssh", online = true)

    private class FakeMachines : MachineManagement {
        var liveOn: Boolean? = true
        var failRead = false
        var failWrite = false

        override suspend fun claudeSettings(machineId: String) = listOf(
            ClaudeSetting("verbose", "Verbose output", type = "bool", value = kotlinx.serialization.json.JsonPrimitive(true)),
        )
        override suspend fun setClaudeSetting(machineId: String, key: String, value: kotlinx.serialization.json.JsonElement) = claudeSettings(machineId)
        override suspend fun codexSettings(machineId: String) = emptyList<ClaudeSetting>()
        override suspend fun setCodexSetting(machineId: String, key: String, value: kotlinx.serialization.json.JsonElement) = emptyList<ClaudeSetting>()
        override suspend fun claudeLiveReplies(machineId: String): Boolean? {
            if (failRead) throw HubFailure(HubError.Timeout, "timed out")
            return liveOn
        }
        override suspend fun setClaudeLiveReplies(machineId: String, on: Boolean): Boolean {
            if (failWrite) throw HubFailure(HubError.Server, "offline", 503)
            liveOn = on
            return on
        }
        override suspend fun launchers(machineId: String) = emptyList<AgentLauncher>()
        override suspend fun launcherQuota(machineId: String, name: String, refresh: Boolean) = LauncherQuotaAnswer()
        override suspend fun reconnect(machineId: String) = Unit
        override suspend fun deploy(machineId: String) = Unit
        override suspend fun herdr(machineId: String): HerdrState = error("unused")
        override suspend fun startHerdr(machineId: String): HerdrState = error("unused")
        override suspend fun setEngine(machineId: String, engine: String) = Machine(machineId)
    }

    private fun capture(name: String): Bitmap {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val out = File("build/live-replies-shots").also { it.mkdirs() }
        File(out, "$name.png").outputStream().use {
            assertTrue("could not write $name", bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
        }
        return bitmap
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h1400dp-xhdpi")
    @Test fun englishMatrix() = matrix("en", toggleTitle = "Stream Replies as Claude Writes", offSubtitle = "Replies show up once Claude saves them")

    @Config(sdk = [35], qualifiers = "zh-w400dp-h1400dp-xhdpi")
    @Test fun simplifiedChineseMatrix() = matrix("zh", toggleTitle = "边写边发回复", offSubtitle = "Claude 存下回复后才显示")

    private fun matrix(language: String, toggleTitle: String, offSubtitle: String) {
        val api = FakeMachines()
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow(listOf(studio, laptop))
            override val sessions = MutableStateFlow(emptyList<Session>())
            override suspend fun refresh() {}
        }
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var run by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                androidx.compose.runtime.key(run) {
                    Box(Modifier.testTag("shot").width(400.dp).height(1100.dp).background(PersonaTheme.palette.background)) {
                        ClaudeSettingsPage(sessions, api, onBack = {})
                    }
                }
            }
        }
        for (s in AppStyle.entries) {
            val lightAndDark = mutableListOf<Bitmap>()
            for (d in listOf(false, true)) {
                style = s
                dark = d
                api.liveOn = true
                api.failRead = false
                api.failWrite = false
                run++
                rule.waitUntil(5_000) { rule.onAllNodesWithTag("live-replies").fetchSemanticsNodes().isNotEmpty() }
                rule.onNodeWithText(toggleTitle).assertExists()
                rule.onNodeWithTag("live-replies").assertIsOn()
                lightAndDark += capture("$language-${s.key}-${if (d) "dark" else "light"}-on")
            }
            assertFalse("${s.key} light and dark should differ", lightAndDark[0].sameAs(lightAndDark[1]))
        }
        // Also capture off state and load-error Retry state in Glass light.
        style = AppStyle.Glass
        dark = false
        rule.onNodeWithTag("live-replies").performClick()
        rule.waitForIdle()
        rule.onNodeWithTag("live-replies").assertIsOff()
        rule.onNodeWithText(offSubtitle).assertExists()
        capture("$language-glass-light-off")

        api.failRead = true
        run++
        rule.waitForIdle()
        capture("$language-glass-light-error")

        // A change that fails on both computers: one line each, named, under the switch.
        api.failRead = false
        api.failWrite = true
        api.liveOn = true
        run++
        rule.waitUntil(5_000) { rule.onAllNodesWithTag("live-replies").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithTag("live-replies").performClick()
        rule.waitForIdle()
        rule.onNodeWithTag("live-replies").assertIsOn()
        capture("$language-glass-light-save-error")
    }
}

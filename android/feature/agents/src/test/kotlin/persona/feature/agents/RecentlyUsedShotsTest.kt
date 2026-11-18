package persona.feature.agents

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.designsystem.AppStyle
import persona.core.designsystem.HomeChrome
import persona.core.designsystem.LocalHomeChrome
import persona.core.designsystem.PersonaTheme
import persona.core.domain.sort.AgentGrouping
import java.io.File

/**
 * Recently Used (#9199): one flat latest-first list, in Glass / Brief / Board, light and dark.
 * PNGs in `build/recently-used-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1000dp-xhdpi")
class RecentlyUsedShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun at(sec: Long) = "2026-01-01T00:00:${sec.toString().padStart(2, '0')}Z"

    private val sessions = listOf(
        Session("mac", "needs", title = "Approve the migration", cwd = "/code/app", state = "needs_you",
            agent = "claude", lastText = "Run the migration on staging?", updatedAt = at(10)),
        Session("mac", "working", title = "Fix the login bug", cwd = "/code/app", state = "working",
            agent = "claude", lastText = "Updating the retry logic.", updatedAt = at(40)),
        Session("mac", "seen", title = "Review the refactor", cwd = "/code/api", state = "idle",
            agent = "codex", lastText = "Two places swallow errors.", updatedAt = at(30)),
        Session("mac", "history", title = "Yesterday's build", cwd = "/code/app", state = "offline", status = "offline",
            agent = "claude", lastText = "Build finished overnight.", updatedAt = at(50)),
    )
    private val machines = listOf(Machine("mac", "MacBook Pro", online = true, icon = "laptop"))

    @Test fun everyStyle() {
        val model = agentsModel(sessions, machines)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalHomeChrome provides HomeChrome(emptyList(), "Agents", onSelect = {}, leading = {})) {
                    AgentsScreen(model, NoNavigation, AgentsMetadata())
                }
            }
        }
        rule.waitUntil(5_000) { model.page.value.loaded }
        rule.runOnIdle { model.grouping(AgentGrouping.Recent) }
        val out = File(System.getProperty("persona.shots") ?: "build/recently-used-shots").also { it.mkdirs() }
        fun save(name: String, s: AppStyle, d: Boolean) {
            rule.waitForIdle()
            val bitmap = rule.onRoot().captureToImage().asAndroidBitmap()
            File(out, "$name-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            rule.runOnIdle { style = s; dark = d }
            rule.waitForIdle()
            save("recently-used", s, d)
            rule.onNodeWithText("Recently Used").assertIsDisplayed()
            rule.onNodeWithText("Yesterday's build").assertIsDisplayed()
            rule.onNodeWithText("Fix the login bug").assertIsDisplayed()
        }
    }
}

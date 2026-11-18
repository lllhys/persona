package persona.feature.agents

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
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
 * By App (#9199): desktop apps, terminal engines and Other, in Glass / Brief / Board, light and dark.
 * PNGs in `build/by-app-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1000dp-xhdpi")
class ByAppShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun at(sec: Long) = "2026-01-01T00:00:${sec.toString().padStart(2, '0')}Z"

    private val sessions = listOf(
        Session("mac", "claude-desk", title = "Ship the release", cwd = "/Users/me/code/app", state = "working",
            agent = "claude", agentSurface = "desktop", lastText = "Preparing the changelog.", updatedAt = at(50)),
        Session("mac", "codex-desk", title = "Review the PR", cwd = "/Users/me/code/billing", state = "idle",
            agent = "codex", agentSurface = "desktop", spaceKind = "codex_app", spaceId = "codex-project:/Users/me/code/billing",
            spaceName = "billing", lastText = "Looks good to merge.", updatedAt = at(40)),
        Session("mac", "herdr-1", title = "Fix the login bug", cwd = "/Users/me/code/app", state = "working",
            agent = "claude", agentSurface = "cli", spaceKind = "herdr", spaceId = "app", spaceName = "app",
            paneId = "app:p1", lastText = "Updating the retry logic.", updatedAt = at(30)),
        Session("mac", "term", title = "Scratch notes", cwd = "/Users/me", state = "idle",
            agent = "claude", agentSurface = "cli", lastText = "Notes in the home folder.", updatedAt = at(20)),
        Session("mac", "offline", title = "Yesterday's thread", cwd = "/Users/me/code/app", state = "offline", status = "offline",
            agent = "claude", agentSurface = "desktop", lastText = "Wrapped up for the day.", updatedAt = at(10)),
    )
    private val machines = listOf(Machine("mac", "MacBook Pro", online = true, icon = "laptop"))

    /** Long project name + two machines: title ellipsizes; machine caption shows; badge/chevron stay trailing. */
    private val longProjectSessions = listOf(
        Session("mac", "long-a", title = "Ship on Mac", cwd = "/Users/me/code/persona-android-groupings-acceptance-suite",
            state = "working", agent = "claude", agentSurface = "desktop", lastText = "On the Mac checkout.", updatedAt = at(50)),
        Session("box", "long-b", title = "Ship on Box", cwd = "/Users/me/code/persona-android-groupings-acceptance-suite",
            state = "idle", agent = "claude", agentSurface = "desktop", lastText = "On the Box checkout.", updatedAt = at(40)),
    )
    private val twoMachines = listOf(
        Machine("mac", "MacBook Pro", online = true, icon = "laptop"),
        Machine("box", "Example computer", online = true, icon = "desktop"),
    )

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
        rule.runOnIdle { model.grouping(AgentGrouping.App) }
        val out = File(System.getProperty("persona.shots") ?: "build/by-app-shots").also { it.mkdirs() }
        fun save(name: String, s: AppStyle, d: Boolean) {
            rule.waitForIdle()
            val bitmap = rule.onRoot().captureToImage().asAndroidBitmap()
            File(out, "$name-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            rule.runOnIdle { style = s; dark = d }
            rule.waitForIdle()
            save("by-app", s, d)
            rule.onNodeWithText("Claude desktop app").assertIsDisplayed()
            rule.onNodeWithText("Codex App").assertIsDisplayed()
            rule.onNodeWithText("herdr").assertIsDisplayed()
            rule.onNodeWithText("Other").assertIsDisplayed()
        }
    }

    @Test fun longProjectTwoMachinesGlassLight() {
        val model = agentsModel(longProjectSessions, twoMachines)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalHomeChrome provides HomeChrome(emptyList(), "Agents", onSelect = {}, leading = {})) {
                    AgentsScreen(model, NoNavigation, AgentsMetadata())
                }
            }
        }
        rule.waitUntil(5_000) { model.page.value.loaded }
        rule.runOnIdle { model.grouping(AgentGrouping.App) }
        rule.waitForIdle()
        rule.onNodeWithText("Claude desktop app").assertIsDisplayed()
        // Project rows merge semantics (RepoTreeLine); machine captions live in contentDescription.
        // Two machines → two project rows with the same long folder name.
        rule.onNode(hasContentDescription("MacBook Pro", substring = true)).assertIsDisplayed()
        rule.onNode(hasContentDescription("Example computer", substring = true)).assertIsDisplayed()
        rule.onAllNodes(hasContentDescription("persona-android-groupings-acceptance-suite", substring = true))
            .assertCountEquals(2)
        val out = File(System.getProperty("persona.shots") ?: "build/by-app-shots").also { it.mkdirs() }
        val bitmap = rule.onRoot().captureToImage().asAndroidBitmap()
        File(out, "by-app-long-project-glass-light.png").outputStream()
            .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    /** Very long machine display name: ellipsizes; must not run under the project count badge. */
    @Test fun longMachineNameGlassLight() {
        val longName = "Example computer with an unusually long display name for the By App project row"
        val model = agentsModel(
            listOf(
                Session("mac", "a", title = "Ship on Mac", cwd = "/Users/me/code/app", state = "working",
                    agent = "claude", agentSurface = "desktop", lastText = "On Mac.", updatedAt = at(50)),
                Session("box", "b", title = "Ship on Box", cwd = "/Users/me/code/app", state = "idle",
                    agent = "claude", agentSurface = "desktop", lastText = "On Box.", updatedAt = at(40)),
            ),
            listOf(
                Machine("mac", "MacBook Pro", online = true, icon = "laptop"),
                Machine("box", longName, online = true, icon = "desktop"),
            ),
        )
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalHomeChrome provides HomeChrome(emptyList(), "Agents", onSelect = {}, leading = {})) {
                    AgentsScreen(model, NoNavigation, AgentsMetadata())
                }
            }
        }
        rule.waitUntil(5_000) { model.page.value.loaded }
        rule.runOnIdle { model.grouping(AgentGrouping.App) }
        rule.waitForIdle()
        rule.onNodeWithText("Claude desktop app").assertIsDisplayed()
        rule.onNode(hasContentDescription(longName, substring = true)).assertIsDisplayed()
        // Full name is spoken on the row; on screen the Text ellipsizes (clearAndSetSemantics hides the child's text).
        rule.onAllNodes(hasText(longName, substring = false)).assertCountEquals(0)
        val out = File(System.getProperty("persona.shots") ?: "build/by-app-shots").also { it.mkdirs() }
        val bitmap = rule.onRoot().captureToImage().asAndroidBitmap()
        File(out, "by-app-long-machine-glass-light.png").outputStream()
            .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}

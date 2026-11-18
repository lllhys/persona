package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onRoot
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.AgentWorkspace
import persona.core.designsystem.AppStyle
import persona.core.designsystem.HomeChrome
import persona.core.designsystem.LocalHomeChrome
import persona.core.designsystem.PersonaTheme
import java.io.File

/** The real Agents page, with its catalog load and source pin indicator, in every Android style and appearance. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h1000dp")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class PaseoWorkspacePinsShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun workspace(id: String, title: String, pin: String? = null, main: Boolean = false) =
        AgentWorkspace(machineId = "computer", source = "paseo", sourceId = id, title = title,
            cwd = "/code/project/$id", projectId = "project", projectName = "Travel planning", projectPath = "/code/project",
            isMain = main, branch = "main", workspaceKind = "git", pinnedAt = pin, online = true, available = true)
    private val pinned = workspace("review", "Review draft", "2026-11-16T10:00:00Z")
    private val empty = workspace("empty", "Next trip", "2026-11-16T11:00:00Z")
    private val main = workspace("main", "Itinerary", main = true)
    private val sessions = listOf(
        Session(machineId = "computer", sessionId = "draft", title = "Review the weekend plan", cwd = pinned.cwd,
            workspaceId = pinned.sourceId, sourceKind = "runtime", sourceInstanceId = "paseo", state = "offline", status = "offline"),
        Session(machineId = "computer", sessionId = "plan", title = "Prepare the itinerary", cwd = main.cwd,
            workspaceId = main.sourceId, sourceKind = "runtime", sourceInstanceId = "paseo", state = "working", status = "working"),
    )

    @Test fun glassLight() = shoot(AppStyle.Glass, false)
    @Test fun glassDark() = shoot(AppStyle.Glass, true)
    @Test fun briefLight() = shoot(AppStyle.Brief, false)
    @Test fun briefDark() = shoot(AppStyle.Brief, true)
    @Test fun boardLight() = shoot(AppStyle.Board, false)
    @Test fun boardDark() = shoot(AppStyle.Board, true)

    private fun shoot(style: AppStyle, dark: Boolean) {
        val service = PaseoCatalogTestService().apply { rows = listOf(main, pinned, empty) }
        val model = agentsModel(sessions, listOf(Machine("computer", "Computer", online = true, engine = "paseo")))
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                CompositionLocalProvider(LocalAgentCreationService provides service,
                    LocalHomeChrome provides HomeChrome(emptyList(), "Agents", onSelect = {}, leading = {})) {
                    AgentsScreen(model, NoNavigation, AgentsMetadata())
                }
            }
        }
        rule.waitUntil(5_000) { model.page.value.list.groups.count { it.worktree?.pinned == true } == 2 }
        rule.waitForIdle()
        val groups = model.page.value.list.groups.filter { it.worktree != null }
        assertEquals(listOf(empty.sourceId, pinned.sourceId, main.sourceId), groups.map { it.id.substringAfterLast('/') })
        assertEquals(listOf("draft"), groups.single { it.id.endsWith("review") }.visibleRows.map { it.session.sessionId })
        assertFalse(groups.first().folded)
        val directory = File(System.getProperty("persona.shots") ?: "build/paseo-pin-shots").also { it.mkdirs() }
        rule.onRoot().captureToImage().asAndroidBitmap().let { bitmap ->
            directory.resolve("paseo-pins-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream().use {
                assertTrue(bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it))
            }
        }
    }
}

package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onRoot
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withTimeout
import kotlin.time.Duration.Companion.milliseconds
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.domain.sort.AgentGrouping
import persona.core.domain.sort.AgentSession
import java.io.File

/** Native host Compose evidence: the opened Unread row stays visible after Seen, and folds again after leaving. */
@RunWith(androidx.test.ext.junit.runners.AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class AgentsHeldInListShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    @Test fun threeStylesLightAndDark() = runTest(timeout = 120_000.milliseconds) {
        val visible = MutableStateFlow<SessionKey?>(SessionKey("machine", "summary"))
        val sessions = MutableStateFlow(listOf(
            session("summary", "Unread report", "unread"),
            session("working", "Fix the login bug", "working"),
            session("seen", "Older refactor", "idle"),
        ))
        val repository = object : persona.core.contracts.api.SessionsRepository {
            override val sessions = sessions
            override val machines = MutableStateFlow(listOf(Machine("machine", "Example computer", online = true)))
            override suspend fun refresh() = Unit
        }
        val dependencies = agentsDependencies(emptyList(), emptyList())
        val model = AgentsViewModel(repository, dependencies.interactions(), dependencies.connection(),
            dependencies.sender(), dependencies.preferences(), visibleChat = visible,
            heldChat = MutableStateFlow(SessionKey("machine", "summary")))
        val modelReference = model
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                AgentsScreen(modelReference, NoNavigation, AgentsMetadata())
            }
        }
        withTimeout(10_000) { model.page.first { it.loaded } }
        model.grouping(AgentGrouping.State)
        model.range(AgentRange.All)
        awaitRows(model, "state/unread", "machine/summary")
        sessions.value = sessions.value.map { if (it.sessionId == "summary") it.copy(state = "idle") else it }
        awaitRows(model, "state/unread", "machine/summary")

        val out = File(System.getProperty("persona.shots") ?: "build/held-in-list-shots").also { it.mkdirs() }
        for (styleOption in AppStyle.entries) for (darkOption in listOf(false, true)) {
            rule.runOnIdle { style = styleOption; dark = darkOption }
            rule.waitForIdle()
            capture(rule, out, "${styleOption.name.lowercase()}-${if (darkOption) "dark" else "light"}-held")
        }
        visible.value = null
        awaitRows(model, "state/idle", "machine/seen", "machine/summary", visibleRows = false)
        awaitRows(model, "state/working", "machine/working")
        for (styleOption in AppStyle.entries) for (darkOption in listOf(false, true)) {
            rule.runOnIdle { style = styleOption; dark = darkOption }
            rule.waitForIdle()
            capture(rule, out, "${styleOption.name.lowercase()}-${if (darkOption) "dark" else "light"}-left")
        }
        model.viewModelScope.cancel()
    }

    private fun session(id: String, title: String, state: String) = Session("machine", id, title = title,
        cwd = "/home/user/project", spaceId = "space", spaceName = "Example project", state = state,
        agent = "codex", lastText = "Example summary", updatedAt = when (id) {
            "summary" -> "2026-01-01T00:00:20Z"
            "working" -> "2026-01-01T00:00:10Z"
            else -> "2026-01-01T00:00:30Z"
        })

    private suspend fun awaitRows(model: AgentsViewModel, section: String, vararg ids: String, visibleRows: Boolean = true) {
        withTimeout(10_000) {
            model.page.first {
                it.list.groups.singleOrNull { group -> group.id == section }?.let { group ->
                group.rows.map(AgentSession::id) == ids.toList() &&
                    (if (visibleRows) group.visibleRows.map(AgentSession::id) == ids.toList() else group.visibleRows.isEmpty())
            } == true
            }
        }
        rule.waitForIdle()
    }
}

private fun capture(rule: androidx.compose.ui.test.junit4.AndroidComposeTestRule<*, *>, out: File, name: String) {
    val bitmap = rule.onRoot().captureToImage().asAndroidBitmap()
    out.resolve("$name.png").outputStream().use {
        check(bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it)) { "Cannot write $name" }
    }
}

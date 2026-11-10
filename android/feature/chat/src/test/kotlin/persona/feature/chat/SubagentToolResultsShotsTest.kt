package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Density
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import coil3.ImageLoader
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.TaskNote
import persona.core.designsystem.*
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.WorkItem
import java.io.File

/** Real Compose/native Robolectric component captures, not emulator screenshots. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
class SubagentToolResultsShotsTest {
    @get:Rule val rule = createComposeRule()
    private val timeline = ChatTimeline()
    private var style by mutableStateOf(AppStyle.Glass)
    private var dark by mutableStateOf(false)
    private var fontScale by mutableStateOf(1f)
    private var expanded by mutableStateOf(emptySet<String>())
    private var items by mutableStateOf(fixture())
    private val read = mutableListOf<Item>()

    private fun use(seq: Int, id: String, name: String, title: String) = Item("use-$id", seq, role = "assistant",
        kind = "tool_use", toolUseId = id, toolName = name,
        toolInput = buildJsonObject { put("description", title); put("prompt", "Check the component fixtures") })

    private fun result(seq: Int, call: String, status: String, text: String? = null, full: Int? = null, preview: String? = null) =
        Item("report-$seq", seq, role = "user", kind = "tool_result", toolUseId = call, text = text, fullSize = full,
            task = TaskNote(kind = "agent", taskId = "agent-$call", toolUseId = call, status = status,
                title = "Component checks", detail = preview))

    private fun fixture() = listOf(
        Item("u1", 1, role = "user", kind = "text", text = "Check the completed and running agents"),
        use(2, "done", "Task", "Verify completed task"),
        result(3, "done", "running", "Async agent launched successfully.\nagentId: agent-done"),
        result(4, "done", "running", "Earlier SendMessage report\nThe first checks passed."),
        Item("foreground#1", 4, role = "user", kind = "tool_result", toolUseId = "done",
            text = "Foreground Task report\nAll checks passed.\nagentId: agent-done"),
        result(5, "done", "completed", "Completed SendMessage report\nAll component checks passed.\nagentId: agent-done", 8192, "Short compatibility preview"),
        result(6, "done", "completed"), // Pointer notification: terminal status, no duplicate report body.
        use(7, "live", "Agent", "Verify live task"),
        result(8, "live", "running", "Async agent launched successfully.\nagentId: agent-live"),
        result(9, "live", "running", "Interim SendMessage report\nThe agent is still checking the remaining cases.", preview = "Stale preview"),
    )

    private fun frame() {
        Snapshot.sendApplyNotifications()
        repeat(2) { rule.mainClock.advanceTimeByFrame() }
    }

    @Test fun reportsStayUnderTheirCallsAndManualExpansionSurvivesUpdates() {
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                val context = LocalContext.current
                val loader = remember { ImageLoader.Builder(context).build() }
                val snapshot = timeline.lines(items, live = true, open = expanded)
                CompositionLocalProvider(LocalChatTaskIndex provides snapshot.tasks,
                    LocalDensity provides Density(LocalDensity.current.density, fontScale)) {
                    Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                        snapshot.lines.forEach { line ->
                            ChatLineContent(line, SessionKey("computer", "parent"), loader, null,
                                onToggle = { id -> expanded = if (id in expanded) expanded - id else expanded + id },
                                onFold = {}, folding = emptySet(), foldErrors = emptyMap(), onFullText = { read += it },
                                workRunning = true, lastWorkStep = line is ChatLine.Step && line.step.id == "use-live")
                        }
                    }
                }
            }
        }
        frame()
        // The entire work process starts closed, even while the latest Agent has already sent a result.
        rule.onNodeWithText("Verify completed task").assertDoesNotExist()
        rule.onNodeWithText("Interim SendMessage report").assertDoesNotExist()
        rule.onNodeWithText("Verify live task", substring = true).performClick()
        frame()
        rule.onNodeWithText("Verify completed task").assertIsDisplayed()
        rule.onNodeWithText("Verify live task").assertIsDisplayed()
        rule.onNodeWithText("Earlier SendMessage report").assertDoesNotExist()
        rule.onNodeWithText("Completed SendMessage report").assertDoesNotExist()
        assertLifecycle()
        val collapsed = timeline.lines(items, true, emptySet()).lines
        assertEquals(setOf("w-u1"), searchTarget(collapsed, "report-4")?.expand)
        assertEquals("w-u1", taskRecordTarget(collapsed, "report-5")?.expandGroup)
        captureStyles("tools-collapsed") // Every capture has a completed folded Task and a running Agent/work bar.
        fontScale = 2f
        captureStyles("tools-collapsed-large-type")
        fontScale = 1f
        frame()

        rule.onNodeWithText("Verify completed task").performClick()
        frame()
        rule.onNodeWithText("Earlier SendMessage report").assertIsDisplayed()
        rule.onNodeWithText("Completed SendMessage report").assertIsDisplayed()
        rule.onNodeWithText("Foreground Task report\nAll checks passed.\nagentId: agent-done").assertIsDisplayed()
        rule.onNodeWithText("Async agent launched successfully.", substring = true).assertDoesNotExist()
        rule.onNodeWithText("Short compatibility preview").assertDoesNotExist()
        rule.onNodeWithText("Completed SendMessage report\nAll component checks passed.\nagentId: agent-done").assertDoesNotExist()
        rule.onNodeWithText("Completed SendMessage report").performClick()
        frame()
        rule.onNodeWithText("Completed SendMessage report\nAll component checks passed.\nagentId: agent-done").assertIsDisplayed()
        rule.onNodeWithText("Full output").performClick()
        assertEquals(listOf("report-5"), read.map { it.id })
        assertEquals(8192, read.single().fullSize)
        // Re-fold the completed Task before capturing another report, retaining the live work bar beside it.
        rule.onNodeWithText("Verify completed task").performClick()
        rule.onNodeWithText("Verify live task").performClick()
        frame()
        rule.onNodeWithText("Interim SendMessage report").performClick()
        frame()
        rule.onNodeWithText("Interim SendMessage report\nThe agent is still checking the remaining cases.").assertIsDisplayed()
        rule.onNodeWithText("Stale preview").assertDoesNotExist()
        captureStyles("interim-expanded")

        // New SendMessage and notification items do not finish the call or close what the reader opened.
        items = items + result(10, "live", "running", "Incremental report\nOne case remains.")
        frame()
        assertLifecycle()
        rule.onNodeWithText("Interim SendMessage report\nThe agent is still checking the remaining cases.").assertIsDisplayed()
        rule.onNodeWithText("Incremental report\nOne case remains.").assertDoesNotExist()
        items = items + result(11, "live", "waiting")
        frame()
        assertTrue(calls().last().running)
        items = items + result(12, "live", "completed", "Terminal notification report\nThe final case passed.")
        frame()
        assertFalse(calls().last().running)
        rule.onNodeWithText("Terminal notification report").assertIsDisplayed()
        rule.onNodeWithText("Terminal notification report\nThe final case passed.").assertDoesNotExist()
        rule.onNodeWithText("Terminal notification report").performClick()
        frame()
        rule.onNodeWithText("Terminal notification report\nThe final case passed.").assertIsDisplayed()
        rule.onNodeWithText("Terminal notification report").performClick()
        frame()
        rule.onNodeWithText("Terminal notification report\nThe final case passed.").assertDoesNotExist()
        items = items + result(13, "live", "failed", "Failed SendMessage report\nThe check returned an error.")
        frame()
        assertTrue(calls().last().failed)
        rule.onNodeWithText("Failed SendMessage report\nThe check returned an error.").assertDoesNotExist()
        rule.onNodeWithText("Verify live task").performClick()
        frame()
        rule.onNodeWithText("Failed SendMessage report").assertDoesNotExist()
        items = items + result(14, "live", "stopped")
        frame()
        assertFalse(calls().last().running)
        // No child handback becomes a user/peer feed card.
        assertEquals(listOf("u1"), timeline.lines(items, true, expanded).lines.filterIsInstance<ChatLine.Row>().map { it.id })
    }

    private fun calls() = timeline.lines(items, true, emptySet()).lines.filterIsInstance<ChatLine.Work>()
        .flatMap { it.steps }.filterIsInstance<WorkItem.Tool>().map { it.call }

    private fun assertLifecycle() {
        assertFalse(calls().first().running)
        assertTrue(calls().last().running)
        assertEquals(5, calls().first().results.size)
    }

    private fun captureStyles(scene: String) {
        val directory = File("build/subagent-tool-results-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) {
            val colors = mutableListOf<Int>()
            for (d in listOf(false, true)) {
                style = s; dark = d; frame()
                rule.onNodeWithText("Completed").assertIsDisplayed()
                rule.onNodeWithText("Working").assertIsDisplayed()
                val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                File(directory, "$scene-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                    assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
                }
                colors += bitmap.getPixel(2, 2)
            }
            assertNotEquals("$s dark capture must differ from light", colors[0], colors[1])
        }
    }
}

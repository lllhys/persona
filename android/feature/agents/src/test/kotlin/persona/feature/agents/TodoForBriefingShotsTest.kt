package persona.feature.agents

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Todo
import persona.core.contracts.TodoAsk
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File
import java.time.Instant

/**
 * A decision held for the next briefing (#4930, docs/assistant/todos.md "Decisions that can wait") on its To-dos row: one
 * quiet line while the hub says so, none for a decision that was pushed, for a hub from before the field, or once the
 * to-do is finished. Every style, light and dark; native JVM captures in `build/reports/tests/todo-for-briefing-shots/` (or
 * `-Dpersona.shots=<dir>`), not device screenshots.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class TodoForBriefingShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/reports/tests/todo-for-briefing-shots").also { it.mkdirs() }

    private val now = Instant.parse("2026-11-10T09:30:00Z")
    private fun ago(minutes: Long) = now.minusSeconds(minutes * 60).toString()
    private fun ask(forBriefing: Boolean) = TodoAsk("decide", "Needs your decision", "Rename it", ago(20),
        question = "Rename the importer module?", forBriefing = forBriefing)

    private val todos = listOf(
        Todo("held", title = "Rename the importer module", createdAt = ago(90), updatedAt = ago(20), origin = "main", ask = ask(true)),
        Todo("pushed", title = "Merge the importer fix", createdAt = ago(80), updatedAt = ago(15), origin = "main", ask = ask(false)),
        // a hub from before #4930 sends no `for_briefing`
        Todo("plain", title = "Rotate the API key", createdAt = ago(70), origin = "main"),
        Todo("finished", title = "Sort the crash reports", createdAt = ago(600), doneAt = ago(30), doneBy = "user", ask = ask(true)),
    )

    @Test fun onlyAnOpenDecisionHeldForTheBriefingSaysSo() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(900.dp).background(PersonaTheme.palette.background)) {
                    TodosScreen(todos, emptyList(), null, onBack = {}, onOpenTodo = {}, now = now)
                }
            }
        }
        for (s in AppStyle.entries) {
            var light: Bitmap? = null
            for (d in listOf(false, true)) {
                style = s; dark = d
                Snapshot.sendApplyNotifications()
                rule.waitForIdle()
                rule.onNodeWithText("Rename the importer module", useUnmergedTree = true).assertIsDisplayed()
                rule.onNodeWithTag("todo-for-briefing-held", useUnmergedTree = true).assertIsDisplayed()
                rule.onAllNodesWithText("Decision comes with the next briefing", useUnmergedTree = true).assertCountEquals(1)
                for (id in listOf("pushed", "plain", "finished")) {
                    rule.onAllNodesWithTag("todo-for-briefing-$id", useUnmergedTree = true).assertCountEquals(0)
                }
                val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                File(out(), "todo-for-briefing-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                    .use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
                if (!d) light = bitmap.copy(Bitmap.Config.ARGB_8888, false)
                else assertFalse("Light and dark must differ for $s", light!!.sameAs(bitmap))
            }
        }
    }
}

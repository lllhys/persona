package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.getBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import coil3.ImageLoader
import kotlinx.serialization.json.Json
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.contracts.Machine
import persona.core.contracts.SessionKey
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.ToolCall
import persona.core.domain.lines.WorkItem
import java.io.File

/**
 * Tool lines in iOS's words (#4339 C1 and C18, iOS `ToolPresentation`): Claude Code's newer tools by their verbs with what
 * the call is about under them ("2 findings", a wake-up's delay), a computer tool by the Mac's name, Persona's own tools
 * with a subject picked under their words on Glass; and an opened computer tool in words with iOS's Raw input toggle. Every
 * style, light and dark, and Glass in Chinese and Japanese. PNGs in `build/tool-words-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1000dp-xhdpi")
class ToolWordsShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/tool-words-shots").also { it.mkdirs() }

    private fun step(id: String, name: String, input: String, output: String? = null) = WorkItem.Tool(ToolCall(
        Item(id, kind = "tool_use", role = "assistant", toolName = name, toolInput = Json.parseToJsonElement(input), toolUseId = id).asLineItem(),
        output?.let { Item("$id-r", kind = "tool_result", role = "user", text = it, toolUseId = id).asLineItem() }, emptyList()))

    /** The tool lines of a turn, as the work line lists them open. */
    private val lines = listOf(
        step("t1", "SendMessage", """{"to":"researcher","summary":"assign task 1","message":"start on task #1"}""", "sent"),
        step("t2", "ScheduleWakeup", """{"delaySeconds":300}""", "scheduled"),
        step("t3", "ReportFindings", """{"findings":[{"file":"a.kt","summary":"Crashes on nil"},{"file":"b.kt","summary":"Leaks a stream"}]}""", "ok"),
        step("t4", "LSP", """{"operation":"findReferences","filePath":"/w/app/Export.kt","line":120}""", "3 references"),
        step("t5", "CronCreate", """{"cron":"7 * * * *","prompt":"check CI\nthen report"}""", "created"),
        step("t6", "TaskUpdate", """{"taskId":"3","status":"completed"}""", "updated"),
        step("t7", "mcp__persona__kb_search", """{"query":"release notes"}""", "2 notes"),
        step("t8", "mcp__persona__computer_run", """{"machine":"macbook","steps":[{"click":{"name":"All Clear"}},{"click":{"name":"6"}},{"click":{"name":"Multiply"}}]}""", "done"),
        step("t9", "mcp__cua-driver__run_steps", """{"steps":[{"click":{"name":"All Clear"}},{"click":{"name":"6"}},{"click":{"name":"Multiply"}}]}""", "Clicked 3 elements"),
    )

    private val names = { glass: Boolean -> ToolNames(listOf(Machine("macbook", "MacBook", online = true)), "mac", glass = glass) }

    private fun show(run: List<WorkItem.Tool>, style: () -> AppStyle, dark: () -> Boolean) {
        rule.setContent {
            PersonaTheme(darkTheme = dark(), reduceMotion = true, style = style()) {
                val context = LocalContext.current
                val loader = remember { ImageLoader.Builder(context).build() }
                DisposableEffect(loader) { onDispose { loader.shutdown() } }
                CompositionLocalProvider(LocalToolNames provides names(style() == AppStyle.Glass)) {
                    Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                        run.forEachIndexed { i, step ->
                            ChatLineContent(ChatLine.Step("w", step, i == 0), SessionKey("mac", "s1"), loader, recordedDuration = null,
                                onToggle = {}, onFold = {}, folding = emptySet(), foldErrors = emptyMap(), onFullText = {},
                                lastWorkStep = i == run.lastIndex)
                        }
                    }
                }
            }
        }
    }

    private fun shoot(name: String) {
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun theToolLinesReadInIOSsWords() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        show(lines, { style }, { dark })
        // Claude Code's newer tools: iOS's verb, what the call is about under it.
        for ((words, about) in listOf(
            "Message agent" to "researcher · assign task 1", "Schedule wake-up" to "5 min", "Report findings" to "2 findings",
            "Find references" to "Export.kt:120", "Schedule task" to "7 * * * * · check CI", "Complete to-do" to "#3",
        )) {
            rule.onNodeWithText(words, useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText(about, useUnmergedTree = true).assertIsDisplayed()
        }
        // The Mac as the user knows it, with the steps; an agent's own Cua Driver call as another server's tool.
        rule.onNodeWithText("MacBook · Click AC → 6 → ×", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("Cua Driver · Run steps", useUnmergedTree = true).assertIsDisplayed()
        rule.onAllNodesWithText("macbook", substring = true, useUnmergedTree = true).assertCountEquals(0)
        // Persona's own tool: its words, and on Glass what the call is about under them.
        rule.onNodeWithText("Search knowledge base", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("release notes", useUnmergedTree = true).assertIsDisplayed()
        val corners = mutableMapOf<AppStyle, MutableList<Int>>()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            Snapshot.sendApplyNotifications()
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            corners.getOrPut(s) { mutableListOf() } += bitmap.getPixel(2, 2)
            File(out(), "tool-lines-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        corners.values.forEach { assertNotEquals("dark capture must differ", it[0], it[1]) }
        // Brief and Board keep a Persona tool's words alone (the subject under them is Glass's).
        style = AppStyle.Brief
        Snapshot.sendApplyNotifications()
        rule.waitForIdle()
        rule.onAllNodesWithText("release notes", useUnmergedTree = true).assertCountEquals(0)
    }

    @Config(qualifiers = "zh-w400dp-h1000dp-xhdpi")
    @Test fun theToolLinesReadInChinese() {
        show(lines, { AppStyle.Glass }, { false })
        for (text in listOf("给 Agent 发消息", "安排下次唤醒", "5 分", "报告审查结果", "2 条发现", "找引用", "设定期任务", "完成待办", "查知识库", "MacBook · 点 AC → 6 → ×")) {
            rule.onNodeWithText(text, useUnmergedTree = true).assertIsDisplayed()
        }
        shoot("tool-lines-glass-zh")
    }

    @Config(qualifiers = "ja-w400dp-h1000dp-xhdpi")
    @Test fun theToolLinesReadInJapanese() {
        show(lines, { AppStyle.Glass }, { false })
        for (text in listOf("エージェントにメッセージ", "次回の起動を予約", "5分", "レビュー結果を報告", "指摘2件", "参照を検索", "定期タスクを設定", "ToDoを完了", "MacBook · クリック AC → 6 → ×")) {
            rule.onNodeWithText(text, useUnmergedTree = true).assertIsDisplayed()
        }
        shoot("tool-lines-glass-ja")
    }

    // An opened computer tool on Glass (iOS `ComputerToolDetail`): what it did as a line of steps in words, the driver's
    // answer and the JSON under iOS's Raw input toggle. Brief and Board keep the input as code.
    @Test fun anOpenedComputerToolSaysItsStepsAndKeepsItsJSONUnderRawInput() {
        val run = listOf(
            step("a1", "Read", """{"file_path":"/w/app/Export.kt"}""", "1\tfun export() {}"),
            lines.last(),
            step("a2", "Bash", """{"command":"./gradlew :export:test"}""", "BUILD SUCCESSFUL"),
        )
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        show(run, { style }, { dark })
        rule.onNodeWithText("Cua Driver · Run steps", useUnmergedTree = true).assertIsDisplayed().performClick()
        rule.waitForIdle()
        // The steps in words, led by the mark of how it went; the JSON waits behind Raw input.
        rule.onNodeWithTag("computer-steps", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("Click AC → 6 → ×", useUnmergedTree = true).assertIsDisplayed()
        rule.onAllNodesWithTag("computer-detail", useUnmergedTree = true).assertCountEquals(1)
        rule.onNodeWithText("Raw input", useUnmergedTree = true).assertIsDisplayed()
        rule.onAllNodesWithText("\"steps\"", substring = true, useUnmergedTree = true).assertCountEquals(0)
        // Right of the rail, under the step's words (#5025), like every opened step.
        val left = rule.onNodeWithTag("computer-detail", useUnmergedTree = true).getBoundsInRoot().left
        assertTrue("details at $left cross the rail", left >= Tokens.Space.l + Tokens.Space.m + Tokens.Size.iconXS)
        shoot("computer-detail-glass-light")
        dark = true
        Snapshot.sendApplyNotifications()
        rule.waitForIdle()
        shoot("computer-detail-glass-dark")
        dark = false
        Snapshot.sendApplyNotifications()
        rule.waitForIdle()
        // Raw input opens the JSON and what the driver answered, and hides again.
        rule.onNodeWithTag("tool-raw", useUnmergedTree = true).performClick()
        rule.waitForIdle()
        rule.onNodeWithText("Hide raw input", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("Clicked 3 elements", substring = true, useUnmergedTree = true).assertIsDisplayed()
        shoot("computer-raw-glass-light")
        dark = true
        Snapshot.sendApplyNotifications()
        rule.waitForIdle()
        shoot("computer-raw-glass-dark")
        dark = false
        Snapshot.sendApplyNotifications()
        rule.waitForIdle()
        rule.onNodeWithTag("tool-raw", useUnmergedTree = true).performClick()
        rule.waitForIdle()
        rule.onNodeWithText("Raw input", useUnmergedTree = true).assertIsDisplayed()
        // Brief keeps the input as code, with the reader's Raw input beside the output.
        style = AppStyle.Brief
        Snapshot.sendApplyNotifications()
        rule.waitForIdle()
        rule.onAllNodesWithTag("computer-detail", useUnmergedTree = true).assertCountEquals(0)
        rule.onAllNodesWithText("Raw input", useUnmergedTree = true).assertCountEquals(1)
    }
}

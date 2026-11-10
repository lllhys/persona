package persona.feature.agents

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.BackgroundTask
import persona.core.contracts.Session
import persona.core.contracts.Workflow
import persona.core.designsystem.*
import persona.core.domain.sort.AgentSession
import java.io.File
import java.time.Instant

/**
 * An agent whose turn ended while Workflows run (#2935): the row's workflow line (the newest, then "N more waiting") and its
 * facts on a separate metadata line. An older hub's row that only has a workflow in its background, and a plain row. Every style,
 * light and dark; PNGs in `build/workflow-row-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h700dp-xhdpi")
class AgentWorkflowRowShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/workflow-row-shots").also { it.mkdirs() }

    private val now = Instant.parse("2026-11-05T10:14:30Z")
    private val audit = Workflow(taskId = "t1", runId = "wf_1", name = "parity-audit", status = "running", running = true,
        phases = listOf("Audit", "Verify"), phase = "Verify", phaseIndex = 2, agentsStarted = 185, agentsDone = 169, agentsRunning = 16,
        startedAt = "2026-11-05T10:00:00Z")
    private fun row(id: String, title: String, words: String, background: List<BackgroundTask>? = null, workflows: List<Workflow>? = null) =
        AgentSession(Session(machineId = "server", sessionId = id, title = title, cwd = "/home/me/code/persona", status = "idle", state = "working",
            lastText = words, updatedAt = "2026-11-05T10:12:00Z", background = background, workflows = workflows), stateSince = "2026-11-05T10:00:00Z")

    private val rows = listOf(
        row("w", "对齐三端的 workflow 显示", "开了 parity-audit，跑完再汇报。",
            background = listOf(BackgroundTask("t1", "workflow", runId = "wf_1"), BackgroundTask("t2", "workflow", runId = "wf_2"),
                BackgroundTask("b1", "shell", "npm test")),
            workflows = listOf(audit, audit.copy(taskId = "t2", runId = "wf_2", name = "docs-sweep"), audit.copy(taskId = "t0", status = "killed", running = false))),
        row("o", "旧 hub 上的 Agent", "workflow 起来了。", background = listOf(BackgroundTask("t1", "workflow"))),
        row("p", "没有后台的 Agent", "改完了。"),
    )

    @Test fun drawsTheWorkflowLineInEveryStyleAndAppearance() {
        assertEquals(3, rows[0].session.backgroundCount) // npm test + two running workflows, not their background entries again
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    rows.forEachIndexed { index, row ->
                        SlotRow(row, "工作站", AgentRowMetadata(), now, false, false, false, depth = 0,
                            position = GroupPosition(index == 0, index == rows.lastIndex), located = false, onOpen = {}, menuItems = { emptyList() })
                    }
                }
            }
        }
        for (nextStyle in AppStyle.entries) for (nextDark in listOf(false, true)) {
            rule.runOnIdle { style = nextStyle; dark = nextDark }
            rule.onNodeWithText("后台 workflow「parity-audit」 · Verify 2/2 · 169/185+ · 14 分 · 等 1 个").assertIsDisplayed()
            rule.onNodeWithText("后台运行中").assertIsDisplayed()
            rule.onAllNodesWithText("后台 3", substring = true).fetchSemanticsNodes().let { assertEquals(1, it.size) }
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            val name = "workflow-row-${nextStyle.name.lowercase()}-${if (nextDark) "dark" else "light"}"
            File(out(), "$name.png").outputStream().use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        }
    }
}

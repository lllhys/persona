package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.FeedItem
import persona.core.contracts.Item
import persona.core.contracts.Workflow
import persona.core.contracts.WorkflowAgent
import persona.core.contracts.WorkflowPhaseStats
import persona.core.designsystem.AppStyle
import persona.core.designsystem.JWorkflowCallCard
import persona.core.designsystem.MessageContextGroup
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.designsystem.WorkGroup
import persona.core.domain.MessageContext
import persona.core.domain.lines.SubagentStatus
import persona.core.domain.lines.ToolCall
import java.io.File
import java.time.Instant

/**
 * What opens in a chat fades over the space it already takes (#9574). At 140 ms the row is as tall as it is two
 * seconds later; a workflow phase that closes is already folded. Frames go to `build/unfolding-shots/`.
 * Phone is 400 dp, foldable 840 dp. Brief and Board are included for the work group because that row follows iOS
 * on every style.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w900dp-h2200dp-xhdpi")
class UnfoldingShotsTest {
    @get:Rule val rule = createComposeRule()

    private enum class Scene { Work, Context, Workflow, Answered, Sent, Subagent }

    private data class Case(val scene: Scene, val style: AppStyle, val dark: Boolean, val fold: Boolean)

    private var style by mutableStateOf(AppStyle.Glass)
    private var dark by mutableStateOf(false)
    private var width by mutableStateOf(400.dp)
    private var scene by mutableStateOf(Scene.Work)
    private var expanded by mutableStateOf(false)
    private var generation by mutableStateOf(0)

    private val out = File("build/unfolding-shots").also { it.mkdirs() }
    private val now = Instant.now()

    private val cases = buildList {
        for (scene in Scene.entries) for (dark in listOf(false, true)) for (fold in listOf(false, true)) {
            add(Case(scene, AppStyle.Glass, dark, fold))
        }
        add(Case(Scene.Work, AppStyle.Brief, dark = false, fold = false))
        add(Case(Scene.Work, AppStyle.Board, dark = false, fold = false))
    }

    @Test fun openedRowsKeepTheirHeight() {
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = false) {
                Column(
                    Modifier.testTag("shot").width(width).background(PersonaTheme.palette.background).padding(Tokens.Space.m),
                ) {
                    key(generation) { body() }
                }
            }
        }
        val log = File(out, "heights.txt").also { it.writeText("") }
        cases.forEach { case ->
            show(case)
            if (case.scene == Scene.Workflow) workflow(case, log) else open(case, log)
        }
    }

    @Composable
    private fun body() {
        when (scene) {
            Scene.Work -> WorkGroup("Did 3 steps", expanded, { expanded = !expanded }) {
                Text("Opened work stays at full height from the first frame, so a height animation would clip this last line.")
            }
            Scene.Context -> MessageContextGroup(
                listOf(MessageContext("doc", "Crash log", null,
                    "Line 140 was null.\nThe session id never arrived.\nThe opened row is its full height while it fades.")),
                "ctx", {},
            )
            Scene.Workflow -> JWorkflowCallCard(run())
            Scene.Answered -> AnsweredLine(answered(), "Release check", null)
            Scene.Sent -> SentLine(sent(), "Inbox", null)
            Scene.Subagent -> SubagentCard(
                call(), live = false, assistant = false,
                statusOverride = SubagentStatus.Completed,
                metaOverride = SubagentCardText.Meta("Claude Code", "Haiku", "1.2k tokens", "4 tool uses", "2 min"),
            )
        }
    }

    private fun show(case: Case) {
        rule.runOnIdle {
            style = case.style
            dark = case.dark
            width = if (case.fold) 840.dp else 400.dp
            scene = case.scene
            expanded = false
            generation++
        }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        rule.mainClock.advanceTimeByFrame()
    }

    private fun open(case: Case, log: File) {
        val closed = height()
        save(case, "closed")
        if (case.scene == Scene.Work) {
            rule.runOnIdle { expanded = true }
        } else {
            rule.onNodeWithText(label(case.scene), substring = true).performClick()
        }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        rule.mainClock.advanceTimeBy(140)
        val mid = height()
        val faded = if (sample(case)) shot() else null
        save(case, "mid")
        rule.mainClock.advanceTimeBy(2_000)
        val settled = height()
        val solid = if (faded != null) shot() else null
        save(case, "open")
        log.appendText("${name(case)} closed=$closed mid=$mid open=$settled\n")
        assertEquals("${name(case)} height animated $closed -> $mid -> $settled", settled, mid)
        assertTrue("${name(case)} did not open $closed -> $settled", settled > closed)
        if (faded != null && solid != null) assertTrue("${name(case)} fade is invisible", differs(faded, solid))
    }

    private fun workflow(case: Case, log: File) {
        rule.onNodeWithTag("workflow-agent:v5", useUnmergedTree = true).assertExists()
        val open = height()
        save(case, "open")
        rule.onNodeWithText("Verify").performClick()
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        rule.mainClock.advanceTimeBy(140)
        assertEquals(0, rule.onAllNodesWithTag("workflow-agent:v5", useUnmergedTree = true).fetchSemanticsNodes().size)
        val mid = height()
        save(case, "mid")
        rule.mainClock.advanceTimeBy(2_000)
        val folded = height()
        save(case, "folded")
        log.appendText("${name(case)} open=$open mid=$mid folded=$folded\n")
        assertEquals("${name(case)} close animated $open -> $mid -> $folded", mid, folded)
        assertTrue("$folded < $open", folded < open)
        rule.onNodeWithText("Verify").performClick()
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        val reopened = height()
        rule.mainClock.advanceTimeBy(140)
        val fading = if (sample(case)) shot() else null
        assertEquals("reopen is full at once", open, reopened)
        assertEquals(open, height())
        save(case, "reopen-mid")
        rule.onNodeWithTag("workflow-agent:v5", useUnmergedTree = true).assertExists()
        rule.mainClock.advanceTimeBy(2_000)
        assertEquals(open, height())
        if (fading != null) assertTrue("${name(case)} phase fade is invisible", differs(fading, shot()))
    }

    private fun sample(case: Case) = case.style == AppStyle.Glass && !case.dark && !case.fold

    private fun label(scene: Scene) = when (scene) {
        Scene.Context -> "Crash log"
        Scene.Answered -> "Release check"
        Scene.Sent -> "Sent to"
        Scene.Subagent -> "Read the crash log"
        else -> error(scene)
    }

    private fun height(): Int = rule.onNodeWithTag("shot").fetchSemanticsNode().size.height

    private fun shot(): Bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()

    /** True when two frames are not the same picture. Samples a grid so a fade anywhere in the row counts. */
    private fun differs(first: Bitmap, second: Bitmap): Boolean {
        if (first.width != second.width || first.height != second.height) return true
        var y = 0
        while (y < first.height) {
            var x = 0
            while (x < first.width) {
                if (first.getPixel(x, y) != second.getPixel(x, y)) return true
                x += 8
            }
            y += 8
        }
        return false
    }

    private fun save(case: Case, frame: String) {
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out, "${name(case)}-$frame.png").outputStream().use {
            assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
        }
    }

    private fun name(case: Case): String {
        val tone = if (case.dark) "dark" else "light"
        val size = if (case.fold) "fold" else "phone"
        return "${case.scene.name.lowercase()}-${case.style.name.lowercase()}-$tone-$size"
    }

    private fun answered() = FeedItem("a1", kind = "answered", body = buildJsonObject {
        put("answer", "Shipped")
        put("items", buildJsonArray {
            add(buildJsonObject {
                put("question", "What changed?")
                put("answer", "The opened answer is its full height on the first frame, and only the opacity moves.")
            })
        })
    })

    private fun sent() = FeedItem("s1", kind = "routed", body = buildJsonObject {
        put("text", "Please check the release notes.\nThe opened message keeps its full height.\nCopy stays on screen while the text fades in.")
    })

    private fun call(): ToolCall {
        val use = Item("c1", seq = 1, role = "assistant", kind = "tool_use", toolUseId = "a1", toolName = "Agent",
            toolInput = buildJsonObject {
                put("description", "Read the crash log")
                put("prompt", "Read the latest crash log and say which line failed, then the cause, then what to change.")
                put("subagent_type", "general-purpose")
            })
        val result = Item("r1", seq = 2, role = "user", kind = "tool_result", toolUseId = "a1",
            text = "Line 140 was null.\nThe session id never arrived.\nPass it through before the card opens.\nThe detail is full height while it fades.")
        return ToolCall(use.asLineItem(), result.asLineItem(), emptyList())
    }

    private fun agent(id: String, phase: String, running: Boolean) = WorkflowAgent(
        id, "lead@$id", "${phase.lowercase()}:$id", phase, running = running,
        status = if (running) "running" else "done",
        startedAt = now.minusSeconds(300).toString(),
        endedAt = if (running) null else now.minusSeconds(120).toString(),
    )

    private fun run() = Workflow(
        taskId = "w1", runId = "wf_1", name = "widget-review", status = "running", running = true,
        phases = listOf("Scan", "Verify", "Report"), phase = "Verify",
        phaseStats = listOf(
            WorkflowPhaseStats("Scan", 4, 4),
            WorkflowPhaseStats("Verify", 5, 2, running = 3),
            WorkflowPhaseStats("Report", 0, 0),
        ),
        agentsStarted = 185, agentsDone = 100, agentsRunning = 4,
        startedAt = now.minusSeconds(540).toString(),
        agents = (1..4).map { agent("s$it", "Scan", false) } + (1..5).map { agent("v$it", "Verify", it > 2) },
    )
}

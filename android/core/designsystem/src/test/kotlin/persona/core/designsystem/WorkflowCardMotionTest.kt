package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Workflow
import persona.core.contracts.WorkflowAgent
import persona.core.contracts.WorkflowPhaseStats
import java.io.File
import java.time.Instant

/**
 * The workflow card moves only when a value changes (#3435, as the web's #3434): nothing on the first draw, a count
 * counts to its new value. A phase opens and closes as the chat's unfolding (#9574): full height on the first frame,
 * gone at once when it closes (#3005). With reduced motion every new value shows at once. Frames of one change
 * (before, mid-way, after) go to `build/workflow-motion-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1400dp-xhdpi")
class WorkflowCardMotionTest {
    @get:Rule val rule = createComposeRule()

    private val now = Instant.now()

    private fun agent(id: String, phase: String, running: Boolean) = WorkflowAgent(id, "lead@$id", "${phase.lowercase()}:$id", phase,
        running = running, status = if (running) "running" else "done", startedAt = now.minusSeconds(300).toString(),
        endedAt = if (running) null else now.minusSeconds(120).toString())

    /** In Verify, two of five back; [done] agents done of [started] started in the run's head. */
    private fun run(done: Int = 100, started: Int = 185, phase: String = "Verify", report: Boolean = false) = Workflow(
        taskId = "w1", runId = "wf_1", name = "widget-review", status = "running", running = true,
        phases = listOf("Scan", "Verify", "Report"), phase = phase,
        phaseStats = listOf(WorkflowPhaseStats("Scan", 4, 4), WorkflowPhaseStats("Verify", 5, if (report) 5 else 2, running = if (report) 0 else 3),
            WorkflowPhaseStats("Report", if (report) 1 else 0, 0, running = if (report) 1 else 0)),
        agentsStarted = started, agentsDone = done, agentsRunning = 4, startedAt = now.minusSeconds(540).toString(),
        agents = (1..4).map { agent("s$it", "Scan", false) } + (1..5).map { agent("v$it", "Verify", !report && it > 2) } +
            (if (report) listOf(agent("r1", "Report", true)) else emptyList()))

    private fun counts(): String = rule.onNodeWithTag("workflow-counts", useUnmergedTree = true).fetchSemanticsNode().config[SemanticsProperties.Text].joinToString("") { it.text }

    private fun draw(reduce: Boolean, first: Workflow, style: AppStyle = AppStyle.Glass, dark: Boolean = false): (Workflow) -> Unit {
        var workflow by mutableStateOf(first)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = reduce) {
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    JWorkflowCallCard(workflow)
                }
            }
        }
        return { next ->
            rule.runOnIdle { workflow = next }
            Snapshot.sendApplyNotifications()
        }
    }

    @Test fun theFirstDrawShowsEveryValueAsItIs() {
        rule.mainClock.autoAdvance = false
        draw(reduce = false, first = run())
        rule.mainClock.advanceTimeByFrame()
        assertEquals("100/185+", counts())
        // the open phase is open at once, not opening: as tall on the first frame as two seconds later
        val first = rule.onNodeWithTag("workflow-phase:Verify").fetchSemanticsNode().size.height
        rule.onNodeWithText("verify:v5").assertExists()
        rule.mainClock.advanceTimeBy(2_000)
        assertEquals(first, rule.onNodeWithTag("workflow-phase:Verify").fetchSemanticsNode().size.height)
        assertEquals("100/185+", counts())
    }

    @Test fun aChangedCountCountsThroughToTheNewOne() {
        rule.mainClock.autoAdvance = false
        val set = draw(reduce = false, first = run())
        rule.mainClock.advanceTimeByFrame()
        set(run(done = 169))
        val frames = (0 until 60).map { rule.mainClock.advanceTimeByFrame(); counts() }
        assertTrue(frames.toString(), frames.all { Regex("""\d+/185\+""").matches(it) })
        val done = frames.map { it.substringBefore('/').toInt() }
        assertTrue(frames.toString(), done.zipWithNext().all { (a, b) -> b >= a }) // up, never back
        assertTrue(frames.toString(), done.any { it in 101..168 })
        assertEquals("169/185+", frames.last())
    }

    @Test fun withReducedMotionNewValuesShowAtOnce() {
        rule.mainClock.autoAdvance = false
        val set = draw(reduce = true, first = run())
        rule.mainClock.advanceTimeByFrame()
        set(run(done = 169))
        val frames = (0 until 10).map { rule.mainClock.advanceTimeByFrame(); counts() }
        assertTrue(frames.toString(), frames.all { it == "100/185+" || it == "169/185+" })
        assertEquals("169/185+", frames.last())
        // a phase folds at once
        rule.onNodeWithText("Verify").performClick()
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        rule.mainClock.advanceTimeByFrame()
        assertEquals(0, rule.onAllNodesWithTag("workflow-agent:v5").fetchSemanticsNodes().size)
    }

    @Test fun aPhaseClosesAtOnceAndOpensAtFullHeight() {
        rule.mainClock.autoAdvance = false
        draw(reduce = false, first = run())
        rule.mainClock.advanceTimeByFrame()
        val full = rule.onNodeWithTag("workflow-phase:Verify").fetchSemanticsNode().size.height
        rule.onNodeWithText("Verify").performClick()
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        // Closed on the next frame: the agents are gone, and a later frame is no shorter.
        assertEquals(0, rule.onAllNodesWithTag("workflow-agent:v5").fetchSemanticsNodes().size)
        val folded = rule.onNodeWithTag("workflow-phase:Verify").fetchSemanticsNode().size.height
        assertTrue("$folded < $full", folded < full)
        rule.mainClock.advanceTimeBy(140)
        assertEquals(folded, rule.onNodeWithTag("workflow-phase:Verify").fetchSemanticsNode().size.height)
        rule.onNodeWithText("Verify").performClick()
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        assertEquals(1, rule.onAllNodesWithTag("workflow-agent:v5").fetchSemanticsNodes().size)
        val reopened = rule.onNodeWithTag("workflow-phase:Verify").fetchSemanticsNode().size.height
        rule.mainClock.advanceTimeBy(2_000)
        assertEquals(full, reopened)
        assertEquals(reopened, rule.onNodeWithTag("workflow-phase:Verify").fetchSemanticsNode().size.height)
    }

    @Test fun framesOfAChangeLight() = frames(dark = false)

    @Test fun framesOfAChangeDark() = frames(dark = true)

    /** Frames of a run moving on (Verify all back, Report lit with its first agent): before, mid-way, after. */
    private fun frames(dark: Boolean) {
        val dir = File(System.getProperty("persona.shots") ?: "build/workflow-motion-shots").also { it.mkdirs() }
        val mode = if (dark) "dark" else "light"
        fun frame(name: String) {
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(dir, "workflow-change-$name-glass-$mode.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        rule.mainClock.autoAdvance = false
        val set = draw(reduce = false, first = run(done = 150, started = 185), dark = dark)
        rule.mainClock.advanceTimeByFrame()
        frame("before")
        set(run(done = 169, started = 190, phase = "Report", report = true))
        rule.mainClock.advanceTimeBy(200)
        frame("mid")
        assertTrue(counts(), counts() != "169/190+" && counts() != "150/185+")
        rule.mainClock.advanceTimeBy(2_000)
        frame("after")
        assertEquals("169/190+", counts())
        rule.onNodeWithText("report:r1").assertExists()
    }
}

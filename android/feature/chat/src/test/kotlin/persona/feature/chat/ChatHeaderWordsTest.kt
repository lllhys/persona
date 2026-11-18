package persona.feature.chat

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.BackgroundTask
import persona.core.contracts.LoopInfo
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.Workflow
import persona.core.contracts.api.ChatSnapshot
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Status
import persona.core.designsystem.StatusLineState
import persona.core.domain.sort.AgentSession
import java.time.Instant

/**
 * The first words of an Agent chat's header line (#4339, iOS `ChatView.stateWords`), in the app's three languages and in every
 * style: "Working 22 min" while a turn runs, the state's name otherwise, and "Working (Background)" or "Waiting in the
 * background" once the turn ended and a workflow or background work keeps the agent going.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h1200dp")
class ChatHeaderWordsTest {
    @get:Rule val rule = createComposeRule()

    private val machine = Machine(id = "box", name = "MacBook", online = true)
    private val base = Session("box", "s", title = "Refactor the sync layer", agent = "claude", paneId = "p1",
        cwd = "/Users/me/src/persona", spaceName = "persona", slotName = "review")
    private val workflow = Workflow(taskId = "t1", runId = "wf_1", name = "parity-audit", status = "running", running = true)
    private val command = BackgroundTask(id = "b1", kind = "bash", description = "npm run build")
    private val now = Instant.now()

    /** A scene: one chat's session and the start of its newest turn, drawn under a tag of its own. */
    private class Scene(val tag: String, val session: Session, val turnStart: Instant?)

    private val scenes = listOf(
        Scene("run", base.copy(status = "working", state = "working"), now.minusSeconds(22 * 60 + 10)),
        Scene("hour", base.copy(sessionId = "s2", status = "working", state = "working"), now.minusSeconds(65 * 60 + 5)),
        Scene("fresh", base.copy(sessionId = "s3", status = "working", state = "working"), now.minusSeconds(20)),
        // the turn ended, a workflow runs: the hub counts it as working
        Scene("flow", base.copy(sessionId = "s4", status = "idle", state = "working", backgroundWorking = true,
            workflows = listOf(workflow), background = listOf(command.copy(kind = "workflow", runId = "wf_1"))), now.minusSeconds(40 * 60)),
        // the turn ended, all it runs only waits
        Scene("wait", base.copy(sessionId = "s5", status = "idle", state = "working", backgroundWorking = true,
            backgroundWaiting = true, background = listOf(command)), now.minusSeconds(40 * 60)),
        // the turn ended, a busy background command runs: the state's own name, whatever the turn's age
        Scene("busy", base.copy(sessionId = "s6", status = "idle", state = "working", backgroundWorking = true,
            background = listOf(command)), now.minusSeconds(40 * 60)),
        // seen: its dot says it, not words
        Scene("seen", base.copy(sessionId = "s7", status = "idle", state = "idle"), now.minusSeconds(40 * 60)),
        // seen, with a /loop that has not fired yet or background work the hub does not count as working: iOS says nothing in words
        // (the Agents list's "Waiting" group is the list's, and the loop has its pill and chips); not the status line's own "Waiting"
        Scene("loop", base.copy(sessionId = "s8", status = "idle", state = "idle", loop = LoopInfo(kind = "loop", prompt = "check the build")),
            now.minusSeconds(40 * 60)),
        Scene("tasks", base.copy(sessionId = "s9", status = "idle", state = "idle", background = listOf(command)), now.minusSeconds(40 * 60)),
        // finished and not seen yet, with a loop: its own word
        Scene("unread", base.copy(sessionId = "s10", status = "idle", state = "unread", loop = LoopInfo(kind = "loop", prompt = "check the build")),
            now.minusSeconds(40 * 60)),
    )

    private var style by mutableStateOf(AppStyle.Glass)

    private fun compose() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                val scope = rememberCoroutineScope()
                val resources = androidx.compose.ui.platform.LocalResources.current
                val chrome = remember { ChatChromeState(resources, null, scope, {}, {}) }
                Column(Modifier.width(400.dp)) {
                    for (scene in scenes) androidx.compose.foundation.layout.Box(Modifier.testTag(scene.tag)) {
                        // The status line the real chat builds, so the header reads what it reads in the app.
                        val status = chatStatusState(resources, ChatSnapshot(items = emptyList(), loading = false), scene.session, machine,
                            emptyList(), emptyList(), answering = false)
                        ChatHeader(scene.session, machine, scenes.map { it.session }, chrome, null, false, onBack = {}, onOpenSession = {},
                            onConnectionDetails = {}, onOverlay = {}, statusState = status, turnStart = scene.turnStart)
                    }
                }
            }
        }
    }

    private fun says(tag: String, words: String) = rule.onNode(hasText(words) and hasAnyAncestor(hasTestTag(tag))).assertExists()
    private fun doesNotSay(tag: String, words: String) = rule.onNode(hasText(words) and hasAnyAncestor(hasTestTag(tag))).assertDoesNotExist()

    @Test fun english() {
        compose()
        says("run", "Working 22 min")
        says("hour", "Working 1 hr 5 min")
        says("fresh", "Working")
        says("flow", "Working (Background)")
        says("wait", "Waiting in the background")
        says("busy", "Working")
        doesNotSay("busy", "Waiting")
        // Seen says nothing in words: the place leads the line, with a loop or background work too (iOS `stateWords`)
        doesNotSay("seen", "Seen")
        for (tag in listOf("loop", "tasks")) { doesNotSay(tag, "Waiting"); doesNotSay(tag, "Seen") }
        says("unread", "Unread")
        doesNotSay("unread", "Waiting")
    }

    @Config(qualifiers = "zh-w400dp-h1200dp")
    @Test fun simplifiedChinese() {
        compose()
        says("run", "工作中 22 分")
        says("hour", "工作中 1 小时 5 分")
        says("fresh", "工作中")
        says("flow", "工作中（后台）")
        says("wait", "后台等待中")
        says("busy", "工作中")
        says("unread", "完成未看")
        for (tag in listOf("loop", "tasks")) doesNotSay(tag, "待唤醒")
    }

    @Config(qualifiers = "ja-w400dp-h1200dp")
    @Test fun japanese() {
        compose()
        says("run", "作業中 22分")
        says("hour", "作業中 1時間5分")
        says("fresh", "作業中")
        says("flow", "作業中（バックグラウンド）")
        says("wait", "バックグラウンドで待機中")
        says("busy", "作業中")
        says("unread", "未確認")
        for (tag in listOf("loop", "tasks")) doesNotSay(tag, "待機中")
    }

    /** The words are shared behaviour: Brief and Board read the same ones as Glass. */
    @Test fun everyStyleSaysTheSameWords() {
        compose()
        for (shown in listOf(AppStyle.Brief, AppStyle.Board)) {
            rule.runOnIdle { style = shown }
            says("run", "Working 22 min")
            says("flow", "Working (Background)")
            says("wait", "Waiting in the background")
            says("busy", "Working")
        }
    }

    @Test fun theHeaderSaysTheSessionsStateNotTheStatusLinesWaiting() {
        val loop = base.copy(status = "idle", state = "idle", loop = LoopInfo(kind = "loop"))
        val kept = { s: Session, state: StatusLineState -> headerStatus(state, s) }
        val waiting = chatStatusState(androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>().resources,
            ChatSnapshot(items = emptyList(), loading = false), loop, machine, emptyList(), emptyList(), answering = false)
        // the status line says Waiting for a loop (a kind of its own), the header says Seen
        assertEquals(persona.core.designsystem.StatusLineKind.Waiting, persona.core.designsystem.resolveStatusLine(waiting)?.kind)
        assertEquals(Status.Idle, kept(loop, waiting))
        assertEquals(Status.Unread, kept(loop.copy(state = "unread"), waiting))
        // a chat the hub counts as working (its turn ended, background commands go on) keeps the status line's Waiting for its dot;
        // its words are `stateLabel`'s ("Working", "Working (Background)", "Waiting in the background")
        assertEquals(Status.Waiting, kept(loop.copy(state = "working", backgroundWorking = true, background = listOf(command)), waiting))
        // the states the status line really knows stay: an ended chat, an offline computer, a request
        val ended = StatusLineState(ended = true, waiting = true)
        assertEquals(Status.Offline, kept(loop, ended))
        assertEquals(Status.NeedsYou, kept(loop, StatusLineState(pendingRequests = 1, waiting = true)))
        assertEquals(Status.Working, kept(loop, StatusLineState(working = true, waiting = true)))
    }

    @Test fun minutesCountFromTheTurnThatRunsNowAndOnlyWhileItRuns() {
        val agent = { s: Session -> AgentSession(s) }
        val working = base.copy(status = "working", state = "working")
        val start = now.minusSeconds(600)
        assertEquals(start, workedSince(agent(working), start))
        // no turn in the transcript yet: the state's name alone, as iOS (`store.blocks.last` is no turn)
        assertNull(workedSince(agent(working), null))
        // the agent's turn ended (the hub counts its background commands as working): no clock
        assertNull(workedSince(agent(working.copy(status = "idle", backgroundWorking = true, background = listOf(command))), start))
        // a workflow or a wait keeps it going: "Working (Background)" and "Waiting in the background" lead instead
        assertNull(workedSince(agent(working.copy(workflows = listOf(workflow))), start))
        assertNull(workedSince(agent(working.copy(backgroundWaiting = true)), start))
        // it asks you, is seen, or is not running: not working
        for (state in listOf("needs_you", "unread", "idle", "offline", "unknown")) assertNull(workedSince(agent(working.copy(state = state)), start))
        // blocked on a request still has a live turn, but its state is not working
        assertNull(workedSince(agent(working.copy(status = "blocked", state = "needs_you")), start))
        assertNull(workedSince(null, start))
    }

    @Test fun onlyTheTwoBackgroundStatesRunInTheBackground() {
        assertTrue(AgentSession(base.copy(state = "working", workflows = listOf(workflow))).runsInBackground)
        assertTrue(AgentSession(base.copy(state = "working", backgroundWaiting = true)).runsInBackground)
        assertFalse(AgentSession(base.copy(state = "working", backgroundWorking = true, background = listOf(command))).runsInBackground)
        assertFalse(AgentSession(base.copy(state = "idle", backgroundWaiting = true)).runsInBackground)
        // the assistant's own conversation never runs background work in this sense
        assertFalse(AgentSession(base.copy(state = "working", assistant = true, workflows = listOf(workflow))).runsInBackground)
    }
}

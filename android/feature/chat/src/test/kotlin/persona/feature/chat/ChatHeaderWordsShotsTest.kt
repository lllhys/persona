package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.BackgroundTask
import persona.core.contracts.LoopInfo
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.Workflow
import persona.core.contracts.api.ChatSnapshot
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File
import java.time.Instant

/**
 * The first words of an Agent chat's header line (#4339, iOS `ChatView.stateWords`) and the folded slim bar's dot, in every
 * style, light and dark: a turn that has run 22 minutes and one over an hour, then a turn that ended while a workflow runs
 * ("Working (Background)"), while all the background work only waits ("Waiting in the background", the clock for a dot on
 * Glass) and while a busy background command runs ("Working"); then a seen chat with a loop or background work, which says
 * nothing in words (iOS). PNGs in `build/header-words-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class ChatHeaderWordsShotsTest {
    @get:Rule val rule = createComposeRule()

    private val machine = Machine(id = "box", name = "MacBook", online = true)
    private val base = Session("box", "s", title = "Refactor the sync layer", agent = "claude", paneId = "p1",
        cwd = "/Users/me/src/persona", spaceName = "persona", slotName = "review")
    private val workflow = Workflow(taskId = "t1", runId = "wf_1", name = "parity-audit", status = "running", running = true)
    private val command = BackgroundTask(id = "b1", kind = "bash", description = "npm run build")
    private val now = Instant.now()

    /** A chat's session and when its newest turn began. */
    private class Scene(val session: Session, val turnStart: Instant?)

    // Both turns began a while ago: the header counts from the turn's start ([Scene.turnStart]); `state_since` is what it counted
    // from before, kept equal here so the same scenes draw the same minutes on both sides of a before / after.
    private val running = now.minusSeconds(22 * 60 + 10)
    private val long = now.minusSeconds(65 * 60 + 5)
    private val scenes = listOf(
        Scene(base.copy(status = "working", state = "working", stateSince = running.toString()), running),
        Scene(base.copy(sessionId = "s2", title = "Migrate the database", status = "working", state = "working", stateSince = long.toString()), long),
        Scene(base.copy(sessionId = "s3", title = "Audit the parity checks", status = "idle", state = "working", backgroundWorking = true,
            workflows = listOf(workflow), background = listOf(command.copy(kind = "workflow", runId = "wf_1"))), null),
        Scene(base.copy(sessionId = "s4", title = "Watch the deploy", status = "idle", state = "working", backgroundWorking = true,
            backgroundWaiting = true, background = listOf(command)), null),
        Scene(base.copy(sessionId = "s5", title = "Build the release", status = "idle", state = "working", backgroundWorking = true,
            background = listOf(command)), null),
        // seen, with a /loop that has not fired yet, or with background work the hub does not count as working: iOS says nothing in
        // words (it was the status line's "Waiting")
        Scene(base.copy(sessionId = "s6", title = "Check the build every hour", status = "idle", state = "idle",
            loop = LoopInfo(kind = "loop", prompt = "check the build")), null),
        Scene(base.copy(sessionId = "s7", title = "Serve the docs", status = "idle", state = "idle", background = listOf(command)), null),
    )

    @Test fun everyStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val scope = rememberCoroutineScope()
                val resources = androidx.compose.ui.platform.LocalResources.current
                val chrome = remember { ChatChromeState(resources, null, scope, {}, {}) }
                val all = scenes.map { it.session }
                Column(Modifier.width(400.dp).background(PersonaTheme.palette.background)) {
                Column(Modifier.testTag("headers").width(400.dp)) {
                    for (scene in scenes) {
                        // The status line the real chat builds, so the header reads what it reads in the app.
                        val status = chatStatusState(resources, ChatSnapshot(items = emptyList(), loading = false), scene.session, machine,
                            emptyList(), emptyList(), answering = false)
                        ChatHeader(scene.session, machine, all, chrome, null, false, onBack = {}, onOpenSession = {}, onConnectionDetails = {},
                            onOverlay = {}, statusState = status, turnStart = scene.turnStart)
                    }
                }
                Column(Modifier.testTag("slim").width(400.dp)) {
                    for (scene in scenes) {
                        val status = chatStatusState(resources, ChatSnapshot(items = emptyList(), loading = false), scene.session, machine,
                            emptyList(), emptyList(), answering = false)
                        ChatSlimHeader(scene.session, status, null, onBack = {}, onOpen = {})
                    }
                }
                }
            }
        }
        val out = File(System.getProperty("persona.shots") ?: "build/header-words-shots").also { it.mkdirs() }
        val seen = mutableMapOf<String, Bitmap>()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            rule.runOnIdle { style = s; dark = d }
            rule.waitForIdle()
            for (part in listOf("headers", "slim")) {
                val bitmap = rule.onNodeWithTag(part).captureToImage().asAndroidBitmap()
                val name = "header-words-${if (part == "slim") "slim-" else ""}${s.name.lowercase()}-${if (d) "dark" else "light"}"
                File(out, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
                seen[name] = bitmap
            }
        }
        // Dark is drawn differently from light.
        assertFalse(seen.getValue("header-words-glass-light").sameAs(seen.getValue("header-words-glass-dark")))
    }
}

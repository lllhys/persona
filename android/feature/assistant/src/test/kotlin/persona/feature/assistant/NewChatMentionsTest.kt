package persona.feature.assistant

import androidx.activity.ComponentActivity
import androidx.compose.material3.Text
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator

/**
 * @ on New chat's page (#4339 C16, iOS `StarterComposer`): the agents' sessions, a typed path's folder on the
 * assistant's computer, and a picked session going in front of the first message as a `<persona-context>` tag.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class NewChatMentionsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val main = SessionKey("mac", "main")
    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
    private val sessions = MutableStateFlow(listOf(
        Session(machineId = "mac", sessionId = "s1", title = "Fix the login page", cwd = "/w/app", updatedAt = "2026-11-12T09:00:00Z"),
        Session(machineId = "studio", sessionId = "s2", title = "Photo sync", cwd = "/w/photos", agent = "codex",
            updatedAt = "2026-11-12T10:00:00Z", lastText = "Synced 120 photos."),
        Session(machineId = "mac", sessionId = "main", title = "Main chat", assistant = true, updatedAt = "2026-11-12T11:00:00Z"),
    ))

    @Before fun setup() = AssistantManagerChoice.choose("hub", null, "assistant")
    @After fun cleanup() = AssistantManagerChoice.reset()

    private class Hub(main: SessionKey) : AssistantRepository, MessageSender, MainConversationSender {
        override val assistant = MutableStateFlow(AssistantSnapshot(mainSession = main))
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        val sent = mutableListOf<Pair<SessionKey, String>>()
        override suspend fun refresh() = Unit
        override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) = Unit
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) { sent += session to text }
        suspend fun start() = SessionKey("mac", "new")
    }

    private class Machine : SessionFiles {
        val asked = mutableListOf<String>()
        override suspend fun directory(machineId: String, cwd: String, path: String): SessionDirectory {
            asked += "$machineId $path"
            return SessionDirectory(path, when (path) {
                "~" -> listOf(SessionFileEntry("notes.md"), SessionFileEntry("code", dir = true), SessionFileEntry(".zshrc"))
                "~/code" -> listOf(SessionFileEntry("weather", dir = true), SessionFileEntry("README.md"))
                else -> emptyList()
            })
        }
        override suspend fun file(machineId: String, cwd: String, path: String) = throw UnsupportedOperationException()
        override suspend fun status(machineId: String, cwd: String) = throw UnsupportedOperationException()
        override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean) = throw UnsupportedOperationException()
        override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int) = throw UnsupportedOperationException()
        override suspend fun show(machineId: String, cwd: String, hash: String) = throw UnsupportedOperationException()
    }

    private fun mount(machine: Machine = Machine(), needs: List<NeedsYouLine> = emptyList(), still: Boolean = true): Hub {
        val hub = Hub(main)
        rule.setContent {
            PersonaTheme(reduceMotion = still, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalManagerRoster provides ManagerRoster(listOf(
                    HubManager("assistant", "Helper", online = true, isDefault = true),
                ), "assistant")) {
                    AssistantScreen(navigator, hub, hub, hub::start, composerService = null, mainConversationSender = hub,
                        chatContent = { Text("chat:${it.sessionId}") }, mentionSessions = sessions, mentionFiles = machine,
                        shellNeedsYou = needs, onOpenNeed = {}, onOpenInbox = {})
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithContentDescription("New chat").performClick()
        rule.waitForIdle()
        return hub
    }
    private fun field() = rule.onNode(hasSetTextAction())
    private fun type(text: String) { field().performTextReplacement(text); rule.waitForIdle() }

    @Test fun atListsTheAgentsSessionsAndAPickGoesInFrontOfTheFirstMessage() {
        val hub = mount()
        type("@")
        // Other agents' sessions, newest first, from every computer; not Persona's own conversations.
        rule.onNodeWithText("Photo sync").assertIsDisplayed()
        rule.onNodeWithText("Fix the login page").assertIsDisplayed()
        rule.onAllNodesWithText("Main chat").assertCountEquals(0)
        assertTrue(rule.onNodeWithText("Photo sync").fetchSemanticsNode().boundsInRoot.top <
            rule.onNodeWithText("Fix the login page").fetchSemanticsNode().boundsInRoot.top)
        type("@login")
        rule.onAllNodesWithText("Photo sync").assertCountEquals(0)
        type("@photo")
        rule.onNodeWithText("Photo sync").performClick()
        rule.waitForIdle()
        // The @word goes; the session waits over the composer as a quote card.
        assertEquals("", field().fetchSemanticsNode().config[androidx.compose.ui.semantics.SemanticsProperties.EditableText].text)
        rule.onNodeWithText("Quoting session Photo sync").assertIsDisplayed()
        type("Compare with this")
        rule.onNodeWithContentDescription("Send").performClick()
        rule.waitUntil(5_000) { hub.sent.isNotEmpty() }
        val (target, text) = hub.sent.single()
        assertEquals(SessionKey("mac", "new"), target)
        assertTrue(text, text.startsWith("<persona-context kind=\"context\" title=\"Photo sync\" url=\"persona://session/studio/s2\">\n"))
        assertTrue(text, text.endsWith("</persona-context>\n\nCompare with this"))
    }

    @Test fun aTypedHomePathListsThatFolderOnTheAssistantsComputerAndAFolderKeepsBrowsing() {
        val machine = Machine()
        mount(machine)
        type("@~/")
        rule.waitUntil(5_000) { rule.onAllNodesWithText("code/").fetchSemanticsNodes().isNotEmpty() }
        assertEquals(listOf("mac ~"), machine.asked)
        rule.onNodeWithText("notes.md").assertIsDisplayed()
        rule.onAllNodesWithText(".zshrc").assertCountEquals(0)
        rule.onAllNodesWithText("Photo sync").assertCountEquals(0)
        rule.onNodeWithText("code/").performClick()
        rule.waitUntil(5_000) { rule.onAllNodesWithText("weather/").fetchSemanticsNodes().isNotEmpty() }
        assertEquals("@~/code/", field().fetchSemanticsNode().config[androidx.compose.ui.semantics.SemanticsProperties.EditableText].text)
        assertEquals(listOf("mac ~", "mac ~/code"), machine.asked)
        rule.onNodeWithText("README.md").performClick()
        rule.waitForIdle()
        assertEquals("@~/code/README.md ", field().fetchSemanticsNode().config[androidx.compose.ui.semantics.SemanticsProperties.EditableText].text)
        type("@/nowhere/")
        rule.waitUntil(5_000) { rule.onAllNodesWithText("Nothing matches in this folder").fetchSemanticsNodes().isNotEmpty() }
    }

    /** Glass's motion, with the clock under the test's hand: iOS's `withAnimation(Theme.Motion.standard)` around `ShellNeedsYou`. */
    @Test fun whatNeedsYouFoldsAwayInTheStateMotionAndFoldsBack() {
        mount(needs = listOf(NeedsYouLine.Agent(SessionKey("mac", "agent"), "r1", "plan", null, who = "weather-app", what = "Plan ready")), still = false)
        rule.onNodeWithText("Plan ready", substring = true).assertIsDisplayed()
        val full = rule.onNodeWithTag("shell-needs-aside").fetchSemanticsNode().size.height
        rule.mainClock.autoAdvance = false
        fun height() = rule.onNodeWithTag("shell-needs-aside").fetchSemanticsNode().size.height
        rule.onNode(hasSetTextAction()).performClick()
        field().performTextReplacement("@")
        rule.mainClock.advanceTimeBy(60)
        // Half way: still there, folding.
        rule.onNodeWithText("Plan ready", substring = true).assertExists()
        assertTrue("folding, $full to ${height()}", height() in 1 until full)
        rule.mainClock.advanceTimeBy(1_000)
        rule.onAllNodesWithText("Plan ready", substring = true).assertCountEquals(0)
        rule.onAllNodesWithTag("shell-needs-aside").assertCountEquals(0)
        // The @ typed away: back the same way.
        field().performTextReplacement("Hello")
        rule.mainClock.advanceTimeBy(60)
        assertTrue("unfolding, ${height()} of $full", height() in 1 until full)
        rule.mainClock.advanceTimeBy(1_000)
        rule.onNodeWithText("Plan ready", substring = true).assertIsDisplayed()
        assertEquals(full, height())
    }

    @Test fun theDockKeepsThePagesBackdropWhileWhatNeedsYouIsAside() {
        mount(needs = listOf(NeedsYouLine.Agent(SessionKey("mac", "agent"), "r1", "plan", null, who = "weather-app", what = "Plan ready")))
        rule.onAllNodesWithTag("needs-backdrop").assertCountEquals(1)
        type("@")
        rule.onAllNodesWithText("Plan ready", substring = true).assertCountEquals(0)
        rule.onAllNodesWithTag("needs-backdrop").assertCountEquals(1)
        type("")
        rule.onAllNodesWithTag("needs-backdrop").assertCountEquals(1)
    }

    @Test fun whatNeedsYouStepsAsideWhileTheListIsOpen() {
        mount(needs = listOf(NeedsYouLine.Agent(SessionKey("mac", "agent"), "r1", "plan", null, who = "weather-app", what = "Plan ready")))
        rule.onNodeWithText("Plan ready", substring = true).assertIsDisplayed()
        type("@")
        rule.onNodeWithText("Photo sync").assertIsDisplayed()
        rule.onAllNodesWithText("Plan ready", substring = true).assertCountEquals(0)
        type("Hello")
        rule.onNodeWithText("Plan ready", substring = true).assertIsDisplayed()
    }
}

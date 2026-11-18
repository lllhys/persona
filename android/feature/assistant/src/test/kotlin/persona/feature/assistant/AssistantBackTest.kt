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
import org.junit.Assert.*
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
import persona.feature.chat.LocalComposerFocusAsk

/**
 * There is no home page (#4339 C11, as iOS): Back from a conversation lands on the main one and then leaves the app, New
 * chat is the only blank page and its first message starts a new conversation, and Ask Persona goes to the main one.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class AssistantBackTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val main = SessionKey("mac", "main")
    private val side = SessionKey("mac", "side")
    private val other = SessionKey("mac", "other")
    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }

    @Before fun setup() = AssistantManagerChoice.choose("hub", null, "assistant")
    @After fun cleanup() = AssistantManagerChoice.reset()

    @Test fun backGoesToTheMainConversationOrWhereNewChatWasOpened() {
        val page = NewChatStart(from = side)
        // From another conversation: the main one; without a main conversation, the page that starts it.
        assertEquals(AssistantBack.Main, assistantBack(side, main, null, mainQueue = false))
        assertEquals(AssistantBack.Main, assistantBack(side, null, null, mainQueue = false))
        // From New chat's page: where it was opened; opened on the main one or nowhere, the main one.
        assertEquals(AssistantBack.Open(side), assistantBack(null, main, page, mainQueue = false))
        assertEquals(AssistantBack.Main, assistantBack(null, main, NewChatStart(from = main), mainQueue = false))
        assertEquals(AssistantBack.Main, assistantBack(null, main, NewChatStart(from = null), mainQueue = false))
        // The health space's page too.
        assertEquals(AssistantBack.Open(side), assistantBack(null, main, HealthEntry.newChat(side, health = true), mainQueue = false))
        // The main conversation, its messages on their way, and the page that starts it: the shell's Back.
        assertNull(assistantBack(main, main, null, mainQueue = false))
        assertNull(assistantBack(null, null, null, mainQueue = true))
        assertNull(assistantBack(null, main, null, mainQueue = true))
        assertNull(assistantBack(null, null, null, mainQueue = false))
    }

    private inner class Hub : AssistantRepository, MessageSender, MainConversationSender {
        override val assistant = MutableStateFlow(AssistantSnapshot(mainSession = main, conversations = listOf(
            Session(machineId = "mac", sessionId = "main", title = "Main", assistant = true),
            Session(machineId = "mac", sessionId = "side", title = "Trip plan", assistant = true),
            Session(machineId = "mac", sessionId = "other", title = "Weekly report", assistant = true),
        )))
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        val starts = mutableListOf<SessionKey>()
        val targets = mutableListOf<SessionKey>()
        val toMain = mutableListOf<String>()
        override suspend fun refresh() = Unit
        override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) { toMain += text }
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) { targets += session }
        suspend fun start() = SessionKey("mac", "new-${starts.size + 1}").also { starts += it }
    }

    private fun mount(opened: SessionKey = side, focus: ComposerFocusRequests = ComposerFocusRequests()): Hub {
        val hub = Hub()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalManagerRoster provides ManagerRoster(listOf(
                    HubManager("assistant", "Helper", online = true, isDefault = true),
                ), "assistant")) {
                    AssistantScreen(navigator, hub, hub, hub::start, composerService = null, mainConversationSender = hub,
                        requestedSession = opened, composerFocus = focus,
                        chatContent = {
                            val asked = LocalComposerFocusAsk.current?.session?.sessionId?.let { " (cursor)" }.orEmpty()
                            Text("chat:${it.sessionId}$asked")
                        })
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("chat:${opened.sessionId}").assertExists()
        return hub
    }
    private fun back() { rule.runOnIdle { rule.activity.onBackPressedDispatcher.onBackPressed() }; rule.waitForIdle() }
    private fun backIsTheShells() = rule.runOnIdle { assertFalse(rule.activity.onBackPressedDispatcher.hasEnabledCallbacks()) }
    private fun newChat() = rule.onNodeWithContentDescription("New chat")
    private fun send(text: String) {
        rule.onNode(hasSetTextAction()).performTextReplacement(text)
        rule.onNodeWithContentDescription("Send").performClick()
        rule.waitForIdle()
    }

    @Test fun backFromAConversationLandsOnTheMainOneAndThenLeavesTheApp() {
        mount()
        back()
        rule.onNodeWithText("chat:main").assertExists()
        rule.onAllNodesWithText("Start a new side chat", substring = true).assertCountEquals(0)
        rule.onAllNodesWithText("This is your main chat", substring = true).assertCountEquals(0)
        backIsTheShells()
    }

    @Test fun newChatIsTheOnlyBlankPageAndItsFirstMessageStartsANewConversation() {
        val hub = mount()
        newChat().performClick()
        rule.waitForIdle()
        rule.onAllNodesWithText("chat:", substring = true).assertCountEquals(0)
        // On the blank page itself New chat is off (iOS), and its first message goes nowhere near the main conversation.
        newChat().assertIsNotEnabled()
        rule.onAllNodesWithText("Start a new side chat", substring = true).assertCountEquals(1)
        rule.onAllNodesWithText("This is your main chat", substring = true).assertCountEquals(0)
        // Back undoes it: the conversation it was opened on, then the main one.
        back()
        rule.onNodeWithText("chat:side").assertExists()
        assertEquals(emptyList<SessionKey>(), hub.starts)
        newChat().performClick()
        rule.waitForIdle()
        send("Book a table for Friday")
        rule.waitUntil(5_000) { hub.targets.isNotEmpty() }
        assertEquals(listOf(SessionKey("mac", "new-1")), hub.starts)
        assertEquals(listOf(SessionKey("mac", "new-1")), hub.targets)
        assertEquals(emptyList<String>(), hub.toMain)
        rule.onNodeWithText("chat:new-1").assertExists()
        newChat().assertIsEnabled()
        back()
        rule.onNodeWithText("chat:main").assertExists()
        backIsTheShells()
    }

    @Test fun theMenuGoesBackToTheMainConversationInPlaceOfHome() {
        mount(opened = other)
        rule.onNodeWithText("Persona").performClick()
        rule.onAllNodesWithText("Back to home").assertCountEquals(0)
        rule.onNodeWithText("Back to main chat").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("chat:main").assertExists()
        rule.onNodeWithText("Persona").performClick()
        rule.onNodeWithText("Knowledge").assertExists()
        rule.onAllNodesWithText("Back to main chat").assertCountEquals(0)
    }

    @Test fun askPersonaPutsTheCursorInTheMainConversation() {
        val focus = ComposerFocusRequests()
        mount(focus = focus)
        rule.runOnIdle { focus.request() }
        rule.waitForIdle()
        rule.onNodeWithText("chat:main (cursor)").assertExists()
        rule.onAllNodesWithText("chat:side", substring = true).assertCountEquals(0)
    }
}

package persona.feature.assistant

import java.io.IOException
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
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator

/** New chat opens a blank page; its conversation starts with the first message there, never in the main one. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class NewConversationStartTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val main = SessionKey("mac", "main")
    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }

    @Before fun setup() = AssistantManagerChoice.choose("hub", null, "assistant")
    @After fun cleanup() = AssistantManagerChoice.reset()

    private inner class Hub : AssistantRepository, MessageSender, MainConversationSender {
        override val assistant = MutableStateFlow(AssistantSnapshot(mainSession = main))
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        val starts = mutableListOf<SessionKey>()
        val targets = mutableListOf<SessionKey>()
        val toMain = mutableListOf<String>()
        var failNext = false
        override suspend fun refresh() = Unit
        override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) { toMain += text }
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
            targets += session
            if (failNext) { failNext = false; throw IOException("offline") }
        }
        suspend fun start() = SessionKey("mac", "new-${starts.size + 1}").also { starts += it }
    }

    private fun mount(): Hub {
        val hub = Hub()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalManagerRoster provides ManagerRoster(listOf(
                    HubManager("assistant", "Helper", online = true, isDefault = true), HubManager("work", "Work", online = true),
                ), "assistant")) {
                    AssistantScreen(navigator, hub, hub, hub::start, composerService = null, mainConversationSender = hub,
                        chatContent = { Text("chat:${it.sessionId}") })
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("chat:main").assertExists()
        return hub
    }
    private fun newChat() { rule.onNodeWithContentDescription("New chat").performClick(); rule.waitForIdle() }
    private fun send(text: String) {
        rule.onNode(hasSetTextAction()).performTextReplacement(text)
        rule.onNodeWithContentDescription("Send").performClick()
        rule.waitForIdle()
    }
    private fun note() = rule.onAllNodesWithText("What you send goes to the main chat", substring = true)

    @Test fun newChatStartsItsConversationWithTheFirstMessageAndBackLeavesNothing() {
        val hub = mount()
        newChat()
        assertEquals("New chat alone starts nothing on the hub", emptyList<SessionKey>(), hub.starts)
        rule.onAllNodesWithText("chat:main").assertCountEquals(0)
        note().assertCountEquals(0)

        rule.activity.onBackPressedDispatcher.onBackPressed()
        rule.waitForIdle()
        rule.onNodeWithText("chat:main").assertExists()
        assertEquals(emptyList<SessionKey>(), hub.starts)

        newChat()
        hub.failNext = true
        send("first synthetic")
        rule.onAllNodesWithText("chat:new-1").assertCountEquals(0)
        send("first synthetic")
        rule.waitUntil(5_000) { hub.targets.size == 2 }
        assertEquals("one conversation, kept for the retry", listOf(SessionKey("mac", "new-1")), hub.starts)
        assertEquals(listOf(SessionKey("mac", "new-1"), SessionKey("mac", "new-1")), hub.targets)
        assertEquals("nothing goes to the main chat", emptyList<String>(), hub.toMain)
        rule.onNodeWithText("chat:new-1").assertExists()
    }

    @Test fun switchingManagerLeavesTheNewChatPage() {
        val hub = mount()
        newChat()
        note().assertCountEquals(0)
        AssistantManagerChoice.pick("work")
        rule.waitForIdle()
        note().assertCountEquals(1)
        AssistantManagerChoice.pick("assistant")
        rule.waitForIdle()
        rule.onNodeWithText("chat:main").assertExists()
        assertEquals(emptyList<SessionKey>(), hub.starts)
    }
}

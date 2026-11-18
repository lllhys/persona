package persona.feature.assistant

import android.content.res.Configuration
import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.After
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File

/**
 * What needs the user over New chat's composer stepping aside for the @ list, frame by frame on Glass with the clock in the
 * test's hand (#4339 C17, iOS `ShellHome`'s `withAnimation(Theme.Motion.standard)`): "@" typed, then 0, 40, 80, 120, 200 and
 * 400 ms later. On `main` the lines are gone at the first frame. PNGs in `build/new-chat-motion-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class NewChatMentionsMotionShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val main = SessionKey("mac", "main")
    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
    private val sessions = MutableStateFlow(listOf(
        Session(machineId = "mbp", sessionId = "s1", title = "Fix the login page", cwd = "/Users/me/code/weather-app", updatedAt = "2026-11-12T09:40:00Z"),
        Session(machineId = "studio", sessionId = "s2", title = "Photo sync", cwd = "/Users/me/code/photos", agent = "codex", updatedAt = "2026-11-12T10:05:00Z"),
        Session(machineId = "mbp", sessionId = "s3", title = "Release notes for 2.4", cwd = "/Users/me/code/site", updatedAt = "2026-11-11T17:20:00Z"),
        Session(machineId = "mac", sessionId = "main", title = "Main chat", assistant = true, updatedAt = "2026-11-12T11:00:00Z"),
    ))

    @Before fun setup() = AssistantManagerChoice.choose("hub", null, "assistant")
    @After fun cleanup() = AssistantManagerChoice.reset()

    private class Hub(main: SessionKey) : AssistantRepository, MessageSender, MainConversationSender {
        override val assistant = MutableStateFlow(AssistantSnapshot(mainSession = main, conversations = listOf(
            Session(machineId = "mac", sessionId = "main", title = "Main chat", assistant = true),
        )))
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        override suspend fun refresh() = Unit
        override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) = Unit
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
        suspend fun start() = SessionKey("mac", "new")
    }

    private fun frames(dark: Boolean) {
        val hub = Hub(main)
        val needs = NeedsYou(listOf(
            NeedsYouLine.Agent(SessionKey("mbp", "agent"), "r1", "plan", null, who = "weather-app", what = "Plan ready"),
            NeedsYouLine.Inbox("i1", lark = false, itemKind = "mail", who = "Alex", what = "Re: Q4 budget"),
        ))
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = false, style = AppStyle.Glass) {
                val configuration = Configuration(LocalConfiguration.current).apply {
                    uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                        if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
                }
                val chrome = HomeChrome(listOf(NavItem("Persona", "Assistant", PhIcons.Sparkle), NavItem("Agents", "Agents", PhIcons.Terminal)),
                    "Persona", {}, { JPersonaButton(PersonaState.Idle, "Chat history", {}) })
                CompositionLocalProvider(LocalConfiguration provides configuration, LocalManagerRoster provides ManagerRoster(listOf(
                    HubManager("assistant", "Helper", online = true, isDefault = true),
                ), "assistant"), LocalHomeChrome provides chrome, LocalAssistantSidebarHost provides AssistantSidebarHost()) {
                    Box(Modifier.testTag("shot").width(400.dp).height(900.dp).background(PersonaTheme.palette.background)) {
                        AssistantScreen(navigator, hub, hub, hub::start, composerService = null, mainConversationSender = hub,
                            requestedSession = main, needsYou = needs, shellNeedsYou = needs.lines, onOpenNeed = {}, onOpenInbox = {},
                            machines = listOf(Machine("mbp", name = "MacBook Pro", online = true)),
                            phoneToolsAvailable = true, chatContent = { Box(Modifier.fillMaxSize()) },
                            mentionSessions = sessions, mentionFiles = null)
                    }
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithContentDescription("New chat").performClick()
        rule.waitForIdle()
        rule.onNode(hasSetTextAction()).performClick()
        rule.waitForIdle()
        val out = File(System.getProperty("persona.shots") ?: "build/new-chat-motion-shots").also { it.mkdirs() }
        fun capture(name: String) {
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        val tag = if (dark) "dark" else "light"
        capture("motion-$tag-0-before")
        rule.mainClock.autoAdvance = false
        rule.onNode(hasSetTextAction()).performTextReplacement("@")
        var at = 0
        for (ms in listOf(0, 40, 80, 120, 200, 400)) {
            rule.mainClock.advanceTimeBy((ms - at).toLong().coerceAtLeast(16))
            at = ms
            capture("motion-$tag-${ms.toString().padStart(3, '0')}ms")
        }
    }

    @Test fun light() = frames(dark = false)
    @Test fun dark() = frames(dark = true)
}

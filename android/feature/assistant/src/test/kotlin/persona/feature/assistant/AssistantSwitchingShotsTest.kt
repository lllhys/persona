package persona.feature.assistant

import android.content.res.Configuration
import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
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
import java.io.File
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.After
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator

/** Real top-bar clicks and screen composer, with independently owned conversation histories/streams. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class AssistantSwitchingShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    @After fun reset() = AssistantManagerChoice.reset()

    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
    private var dark by mutableStateOf(false)
    private var style by mutableStateOf(AppStyle.Default)
    private var hubOnline by mutableStateOf(true)
    private var roster by mutableStateOf(ManagerRoster(listOf(
        HubManager("assistant", "助手", online = true, isDefault = true, builtin = true),
        HubManager("work", "工作", online = true),
    ), "assistant"))

    private class Hub : AssistantConversations, MessageSender {
        override val assistant = MutableStateFlow(snapshot(null))
        override val pinned = MutableStateFlow<Set<SessionKey>>(emptySet())
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        val streams = mapOf(
            "assistant-main" to MutableStateFlow("助手运行流：第 1 段"),
            "work-main" to MutableStateFlow("工作运行流：第 1 段"),
        )
        val sent = mutableListOf<Pair<SessionKey, String>>()
        var sendGate: CompletableDeferred<Unit>? = null
        var lookupGate: CompletableDeferred<Unit>? = null
        val lookups = mutableListOf<String>()
        var autoEmit = true
        fun show(manager: String?) { assistant.value = snapshot(manager) }
        override suspend fun refresh() = Unit
        override suspend fun mainConversationOf(manager: String): SessionKey {
            lookups += manager
            lookupGate?.await()
            return key(manager)
        }
        override suspend fun startConversation() = key(AssistantManagerChoice.current.value.manager)
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
            sent += session to text
            sendGate?.await()
        }
        companion object {
            fun key(manager: String?) = SessionKey("shared-pro", "${manager ?: "assistant"}-main")
            fun snapshot(manager: String?): AssistantSnapshot {
                val main = key(manager)
                return AssistantSnapshot(main, listOf(Session(machineId = main.machineId,
                    sessionId = main.sessionId, title = if (manager == null) "助手独立主会话" else "工作独立主会话",
                    assistant = true, manager = manager)), manager)
            }
        }
    }

    private fun mount(hub: Hub, embeddedChat: Boolean = true) {
        AssistantManagerChoice.choose("hub", null, "assistant")
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val configuration = Configuration(LocalConfiguration.current).apply {
                    uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                        if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
                }
                LaunchedEffect(hub) {
                    AssistantManagerChoice.current.collect { if (hub.autoEmit) hub.show(it.manager) }
                }
                CompositionLocalProvider(LocalManagerRoster provides roster, LocalConfiguration provides configuration) {
                    Box(Modifier.testTag("switch-shot").width(400.dp).height(900.dp).background(PersonaTheme.palette.background)) {
                        AssistantScreen(navigator, hub, hub, hub::startConversation, composerService = null, hubOnline = hubOnline,
                            assistantChatContent = if (embeddedChat) ({ key, options ->
                                val stream by hub.streams.getValue(key.sessionId).collectAsState()
                                Column(Modifier.fillMaxSize().padding(top = 150.dp, start = 24.dp, end = 24.dp)) {
                                    val name = if (key.sessionId == "assistant-main") "助手" else "工作"
                                    Text("${name}独立主会话", color = PersonaTheme.palette.ink)
                                    Spacer(Modifier.height(20.dp))
                                    Text("会话：${key.machineId}/${key.sessionId}", color = PersonaTheme.palette.ink2)
                                    Spacer(Modifier.height(24.dp))
                                    Text("${name}历史：之前的消息已保留", color = PersonaTheme.palette.ink)
                                    Spacer(Modifier.height(24.dp))
                                    Text(stream, color = PersonaTheme.palette.ink)
                                    options.readOnlyNote?.let { Text(it, color = PersonaTheme.palette.ink2) }
                                }
                            }) else null)
                    }
                }
            }
        }
        rule.waitForIdle()
    }
    // The page draws no manager chips any more: the sidebar's switcher calls AssistantManagerChoice.pick(id), so this does the same.
    private fun pick(name: String) {
        AssistantManagerChoice.pick(mapOf("助手" to "assistant", "工作" to "work").getValue(name))
        rule.waitForIdle()
    }
    private fun capture(name: String) {
        rule.waitForIdle()
        val directory = File("build/assistant-switch-shots").also { check(it.isDirectory || it.mkdirs()) }
        val bitmap = rule.onNodeWithTag("switch-shot").captureToImage().asAndroidBitmap()
        File(directory, "$name.png").outputStream().use {
            check(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
        }
    }
    private fun history(name: String) = rule.onNodeWithText("${name}历史：之前的消息已保留")

    @Test fun switchingRestoresIndependentHistoriesAndRunningStreamsInLightAndDark() {
        val hub = Hub()
        mount(hub)
        for (appStyle in AppStyle.entries) for (night in listOf(false, true)) {
            rule.runOnIdle { style = appStyle; dark = night; hub.show(null) }
            pick("助手")
            history("助手").assertIsDisplayed()
            history("工作").assertDoesNotExist()
            val mode = (if (appStyle == AppStyle.Default) "" else "${appStyle.key}-") + if (night) "dark" else "light"
            capture("$mode-a-main-history")
            pick("工作")
            history("工作").assertIsDisplayed()
            history("助手").assertDoesNotExist()
            rule.onNodeWithText("会话：shared-pro/work-main").assertIsDisplayed()
            capture("$mode-b-main-history")
            rule.runOnIdle { hub.streams.getValue("assistant-main").value = "助手运行流：切走后继续至第 2 段" }
            pick("助手")
            history("助手").assertIsDisplayed()
            rule.onNodeWithText("助手运行流：切走后继续至第 2 段").assertIsDisplayed()
            capture("$mode-a-restored-live-stream")
            rule.runOnIdle { hub.streams.getValue("work-main").value = "工作运行流：切走后继续至第 3 段" }
            pick("工作")
            rule.onNodeWithText("工作运行流：切走后继续至第 3 段").assertIsDisplayed()
            pick("助手")
            assertTrue(hub.sent.isEmpty())
        }
    }

    @Test fun delayedSnapshotsAndRepeatedFastSwitchesNeverDisplayTheOtherManagersHistory() {
        val hub = Hub()
        mount(hub)
        rule.runOnIdle { hub.autoEmit = false }
        pick("工作")
        history("助手").assertDoesNotExist()
        rule.runOnIdle { hub.show("work") }
        history("工作").assertIsDisplayed()
        pick("助手")
        history("工作").assertDoesNotExist()
        rule.runOnIdle { hub.show(null) }
        history("助手").assertIsDisplayed()
        rule.runOnIdle { hub.show("work") }
        history("工作").assertDoesNotExist()
        history("助手").assertIsDisplayed()
        capture("light-late-b-response-stays-on-a")
        repeat(6) {
            pick("工作")
            history("助手").assertDoesNotExist()
            rule.runOnIdle { hub.show("work") }
            history("工作").assertIsDisplayed()
            pick("助手")
            history("工作").assertDoesNotExist()
            rule.runOnIdle { hub.show(null) }
            history("助手").assertIsDisplayed()
        }
        assertNull(AssistantManagerChoice.current.value.manager)
        assertTrue(hub.sent.isEmpty())
        capture("light-repeated-switches-return-a")
    }

    @Test fun sendingThenSwitchingKeepsTheOriginalTargetAndLateSuccessCannotNavigateBack() {
        val hub = Hub().apply { sendGate = CompletableDeferred() }
        mount(hub, embeddedChat = false)
        rule.onNode(hasSetTextAction()).performTextInput("发给助手独立会话")
        rule.onNodeWithContentDescription("发送").performClick()
        rule.waitForIdle()
        assertEquals(listOf(Hub.key(null) to "发给助手独立会话"), hub.sent)
        pick("工作")
        assertEquals("work", AssistantManagerChoice.current.value.manager)
        rule.onNode(hasSetTextAction()).performTextInput("工作助手保留草稿")
        capture("light-send-a-in-flight-switch-b")
        rule.runOnIdle { hub.sendGate!!.complete(Unit) }
        rule.waitForIdle()
        assertEquals("work", AssistantManagerChoice.current.value.manager)
        rule.onNodeWithText("工作助手保留草稿").assertExists()
        assertEquals(listOf(Hub.key(null)), hub.sent.map { it.first })
        capture("light-late-a-send-success-keeps-b")
    }

    @Test fun switchingWhileWorkMainIsStillLoadingSendsToWorkAndKeepsTheCurrentAssistantDraft() {
        val hub = Hub().apply { lookupGate = CompletableDeferred() }
        mount(hub, embeddedChat = false)
        rule.runOnIdle { hub.autoEmit = false }
        pick("工作")
        rule.onNode(hasSetTextAction()).performTextInput("主会话加载中发给工作")
        rule.onNodeWithContentDescription("发送").performClick()
        rule.waitForIdle()
        assertEquals(listOf("work"), hub.lookups)
        assertTrue(hub.sent.isEmpty())
        pick("助手")
        rule.runOnIdle { hub.show(null) }
        rule.onNode(hasSetTextAction()).performTextInput("助手当前草稿")
        capture("light-work-main-loading-return-a")
        rule.runOnIdle { hub.lookupGate!!.complete(Unit) }
        rule.waitForIdle()
        assertEquals(listOf(Hub.key("work") to "主会话加载中发给工作"), hub.sent)
        assertNull(AssistantManagerChoice.current.value.manager)
        rule.onNodeWithText("助手当前草稿").assertExists()
        capture("light-late-work-send-keeps-a-draft")
    }

    @Test fun disconnectedHubOverridesTheCachedOnlineRosterAndDisablesSending() {
        val hub = Hub()
        mount(hub, embeddedChat = false)
        pick("工作")
        rule.onNode(hasSetTextAction()).performTextInput("Hub 断线保留的工作草稿")
        rule.runOnIdle { hubOnline = false }
        rule.waitForIdle()
        assertTrue(roster.managers.all { it.online })
        rule.onNodeWithContentDescription("发送").assertIsNotEnabled()
        rule.onAllNodes(hasSetTextAction()).assertCountEquals(0)
        rule.onNodeWithText("工作离线，能看历史，暂时不能发消息。").assertIsDisplayed()
        assertTrue(hub.sent.isEmpty())
        capture("light-hub-disconnected-cached-roster-online")
        rule.runOnIdle { dark = true }
        capture("dark-hub-disconnected-cached-roster-online")
        rule.runOnIdle { hubOnline = true }
        rule.waitForIdle()
        rule.onNodeWithContentDescription("发送").assertIsEnabled()
        rule.onNode(hasSetTextAction()).assertExists()
        assertTrue(hub.sent.isEmpty())
    }

    @Test fun changingDefaultCannotOpenThePreviousDefaultsMainWhileResponseIsDelayed() {
        val hub = Hub()
        mount(hub)
        history("助手").assertIsDisplayed()
        rule.runOnIdle {
            hub.autoEmit = false
            roster = roster.copy(defaultId = "work", managers = roster.managers.map { it.copy(isDefault = it.id == "work") })
            AssistantManagerChoice.choose("hub", null, "work")
        }
        rule.waitForIdle()
        history("助手").assertDoesNotExist()
        history("工作").assertDoesNotExist()
        capture("light-default-changed-old-main-hidden")
        rule.runOnIdle { hub.assistant.value = Hub.snapshot("work").copy(manager = null) }
        rule.waitForIdle()
        history("工作").assertIsDisplayed()
        history("助手").assertDoesNotExist()
        capture("light-new-default-work-main")
    }

    @Test fun sendWhileNewDefaultMainLoadsCapturesItsConcreteOwner() {
        val hub = Hub().apply { lookupGate = CompletableDeferred() }
        mount(hub, embeddedChat = false)
        rule.runOnIdle {
            hub.autoEmit = false
            roster = roster.copy(defaultId = "work", managers = roster.managers.map { it.copy(isDefault = it.id == "work") })
            AssistantManagerChoice.choose("hub", null, "work")
        }
        rule.waitForIdle()
        rule.onNode(hasSetTextAction()).performTextInput("发给新默认工作助手")
        rule.onNodeWithContentDescription("发送").performClick()
        rule.waitForIdle()
        assertEquals(listOf("work"), hub.lookups)
        rule.runOnIdle { AssistantManagerChoice.choose("hub", null, "assistant") }
        rule.waitForIdle()
        rule.runOnIdle { hub.lookupGate!!.complete(Unit) }
        rule.waitForIdle()
        assertEquals(listOf(Hub.key("work") to "发给新默认工作助手"), hub.sent)
        assertEquals("assistant", AssistantManagerChoice.current.value.shown)
    }

    @Test fun firstSendOnNewDefaultOpensItsResolvedMainInsteadOfTheDefaultOutbox() {
        val hub = Hub()
        mount(hub)
        rule.runOnIdle {
            hub.autoEmit = false
            roster = roster.copy(defaultId = "work", managers = roster.managers.map { it.copy(isDefault = it.id == "work") })
            AssistantManagerChoice.choose("hub", null, "work")
            hub.assistant.value = AssistantSnapshot(managerId = "work")
        }
        rule.waitForIdle()
        rule.onNode(hasSetTextAction()).performTextInput("新默认的首条消息")
        rule.onNodeWithContentDescription("发送").performClick()
        rule.waitForIdle()
        assertEquals(listOf("work"), hub.lookups)
        assertEquals(listOf(Hub.key("work") to "新默认的首条消息"), hub.sent)
        history("工作").assertIsDisplayed()
        history("助手").assertDoesNotExist()
    }

    @Test fun offlineWorkManagerDisablesTheRealComposerAndDoesNotSend() {
        val hub = Hub()
        mount(hub, embeddedChat = false)
        pick("工作")
        rule.onNode(hasSetTextAction()).performTextInput("离线时不能发出的工作消息")
        rule.runOnIdle {
            roster = roster.copy(managers = roster.managers.map {
                if (it.id == "work") it.copy(online = false, lastSeen = "2026-11-07T12:00:00Z") else it
            })
        }
        rule.waitForIdle()
        rule.onNodeWithContentDescription("发送").assertIsNotEnabled()
        rule.onAllNodes(hasSetTextAction()).assertCountEquals(0)
        assertTrue(hub.sent.isEmpty())
        for (appStyle in AppStyle.entries) for (night in listOf(false, true)) {
            rule.runOnIdle { style = appStyle; dark = night }
            rule.onNodeWithContentDescription("发送").assertIsNotEnabled()
            val mode = (if (appStyle == AppStyle.Default) "" else "${appStyle.key}-") + if (night) "dark" else "light"
            capture("$mode-offline-work-composer-disabled")
        }
    }
}

package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.ServerEvent
import persona.core.contracts.Session
import persona.core.contracts.api.*
import persona.core.navigation.SettingsPage
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.onAllNodesWithText
import persona.core.designsystem.*
import java.io.File

/**
 * Settings › Managers drawn on the JVM with Robolectric's native graphics (no emulator), in every style, light and dark: the
 * settings row among its neighbours, the list (the hub's own default, one on a Mac, one offline, one installing), a
 * manager's page (another computer's, the hub's own with Delete off, one installing), the install code sheet with the
 * hub's commands and an expired one without, and a hub from before the roster. Each is checked to draw something and
 * written as a PNG to `build/managers-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h2100dp-xhdpi")
class ManagersShotsTest {
    @get:Rule val rule = createComposeRule()

    private val loaded = ManagersState(roster = ManagerRoster(ManagersSample, "assistant"))
    // 工作 on a build that reads no manager.json of its own: the hub marks it needs_update
    private val outdated = ManagersState(roster = ManagerRoster(ManagersSample.map { if (it.id == "m-3fa21c") it.copy(needsUpdate = true) else it }, "assistant"))
    private val commands = ManagerCommands(
        service = "curl -fsSL http://<hub 地址>/i/mi_0123456789abcdef0123456789abcdef | sh",
        docker = "docker run -d --name persona-m-c1e9f0 --restart unless-stopped -e PERSONA_INSTALL=http://<hub 地址>/i/mi_0123456789abcdef0123456789abcdef ghcr.io/example/personad:latest manager",
    )
    private val fresh = ManagerCodeSheet("m-c1e9f0", "测试", ManagerCode("mi_0123456789abcdef0123456789abcdef", "2026-11-05T06:09:41Z", commands = commands))
    private val expired = ManagerCodeSheet("m-c1e9f0", "测试", ManagerCode("mi_fedcba9876543210fedcba9876543210", "2026-11-05T05:59:00Z"))

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/managers-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private enum class Shot(val tall: Boolean = true) { Settings, List, Detail, Builtin, Installing, Code, Expired, Unsupported, Push, PushOffline, Outdated, OutdatedDetail }

    @Test fun drawsTheManagersPagesInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var shot by mutableStateOf(Shot.Settings)
        val services = settingsServices(FakeManagers())
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val palette = PersonaTheme.palette
                Box(Modifier.testTag("shot").width(400.dp).then(if (shot.tall) Modifier.height(900.dp) else Modifier).background(palette.background)) {
                    when (shot) {
                        // The real settings list (docs/lessons/android.md: not hand-drawn rows), its Connection section on top.
                        Shot.Settings -> SettingsScreen(services, onOpen = {})
                        Shot.List -> ManagersContent(loaded, ManagersNow, {}, {}, {}, {}, selected = { it == "m-3fa21c" })
                        Shot.Detail -> ManagerContent(loaded, "m-3fa21c", ManagersNow, {}, {}, {}, {}, {}, {})
                        Shot.Builtin -> ManagerContent(loaded, "assistant", ManagersNow, {}, {}, {}, {}, {}, {})
                        Shot.Installing -> ManagerContent(loaded, "m-c1e9f0", ManagersNow, {}, {}, {}, {}, {}, {})
                        // The sheet's body on the sheet's own surface (JSheet draws it over a scrim in the app).
                        Shot.Code, Shot.Expired -> Column(Modifier.fillMaxWidth().background(if (style == AppStyle.Brief) palette.background else palette.surfaceOpaque)
                            .padding(horizontal = pagePadding, vertical = Tokens.Space.l)) {
                            val sheet = if (shot == Shot.Code) fresh else expired
                            androidx.compose.material3.Text(sheet.name, style = PersonaTheme.type.title, color = palette.ink)
                            androidx.compose.foundation.layout.Spacer(Modifier.height(Tokens.Space.m))
                            ManagerCodeBody(sheet, ManagersNow, busy = false, failure = null, onNewCode = {}, onDone = {})
                        }
                        Shot.Unsupported -> ManagersContent(ManagersState(unsupported = true), ManagersNow, {}, {}, {}, {})
                        // 推送通知: on for one on a Mac, off (muted) for the offline one, whose last change was refused.
                        Shot.Push -> ManagerContent(loaded, "m-3fa21c", ManagersNow, {}, {}, {}, {}, {}, {},
                            push = ManagerPushState(muted = setOf("m-8b04d2")), onPush = {})
                        Shot.PushOffline -> ManagerContent(loaded, "m-8b04d2", ManagersNow, {}, {}, {}, {}, {}, {},
                            push = ManagerPushState(muted = setOf("m-8b04d2"), failure = ResText.of(R.string.settings_managers_push_failed, "网络断开了")), onPush = {})
                        Shot.Outdated -> ManagersContent(outdated, ManagersNow, {}, {}, {}, {})
                        Shot.OutdatedDetail -> ManagerContent(outdated, "m-3fa21c", ManagersNow, {}, {}, {}, {}, {}, {},
                            push = ManagerPushState(muted = emptySet()), onPush = {})
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            val suffix = "${s.name.lowercase()}-${if (d) "dark" else "light"}"
            for (next in Shot.entries) {
                shot = next
                capture("managers-${next.name.lowercase()}-$suffix")
            }
        }
    }

    /** The list reads the hub, opens a manager, and reads the list again on the hub's push. */
    @Test fun theListFollowsThePushes() {
        val pushes = MutableSharedFlow<Unit>(extraBufferCapacity = 4)
        val fake = FakeManagers()
        val live = object : HubManagers by fake { override val changes = pushes }
        var opened: String? = null
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Box(Modifier.width(400.dp).height(1500.dp)) { ManagersPage(live, onBack = {}, onManager = { opened = it }, ticking = false) }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("工作").assertIsDisplayed()
        rule.onNodeWithText("安装中 · 码", substring = true).assertExists()
        rule.onNodeWithText("工作").performClick()
        assertEquals("m-3fa21c", opened)
        // Another device renamed one: the push reads the list again.
        fake.list = fake.list.map { if (it.id == "m-8b04d2") it.copy(name = "书房") else it }.toMutableList()
        val reads = fake.reads
        pushes.tryEmit(Unit)
        rule.waitForIdle()
        assertTrue(fake.reads > reads)
        rule.onNodeWithText("书房").assertIsDisplayed()
        rule.onNode(hasText("新建助手") and hasClickAction()).assertIsDisplayed()
    }

    /**
     * Install elsewhere warns first, then shows the new code with the hub's commands; Done closes it. (The name dialogs are
     * not driven here: a focused text field in a dialog never lets Robolectric's compose go idle. The model's test
     * covers making one.)
     */
    @Test fun movingAManagerWarnsThenShowsTheCode() {
        val fake = FakeManagers().apply { commands = this@ManagersShotsTest.commands }
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Box(Modifier.width(400.dp).height(1500.dp)) { ManagerPage(fake, "m-3fa21c", onBack = {}, ticking = false) }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("MacBook · macOS · 服务").assertIsDisplayed()
        rule.onNode(hasText("换个地方装") and hasClickAction()).performClick()
        rule.waitForIdle()
        rule.onNodeWithText("换个地方装会给一个新的安装码；用它装好后，原来那处的助手立即停止工作。").assertIsDisplayed()
        assertTrue(fake.calls.isEmpty())
        rule.onAllNodes(hasText("换个地方装") and hasClickAction()).fetchSemanticsNodes().let { assertEquals(2, it.size) }
        rule.onAllNodes(hasText("换个地方装") and hasClickAction())[1].performClick()
        rule.waitForIdle()
        assertEquals(listOf("code m-3fa21c"), fake.calls)
        rule.onNodeWithText("mi_fedcba9876543210fedcba9876543210").assertExists()
        rule.onNodeWithText("装成服务").assertExists()
        rule.onNodeWithText("用 Docker").assertExists()
        rule.onNode(hasText("完成") and hasClickAction()).performClick()
        rule.waitForIdle()
        assertTrue(rule.onAllNodes(hasText("mi_fedcba9876543210fedcba9876543210")).fetchSemanticsNodes().isEmpty())
    }

    /** A manager the hub marks needs_update says so in the list and on its page; the others don't. */
    @Test fun anOutdatedManagerSaysSo() {
        val fake = FakeManagers(ManagersSample.map { if (it.id == "m-3fa21c") it.copy(needsUpdate = true) else it })
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Box(Modifier.width(400.dp).height(1500.dp)) { ManagerPage(fake, "m-3fa21c", onBack = {}, ticking = false) }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("需要更新").assertIsDisplayed()
        rule.onNodeWithText("请在它所在的电脑上更新 personad", substring = true).assertIsDisplayed()
    }

    /** 推送通知 reads the device's prefs, and turning it off sends the list with this manager in it. */
    @Test fun thePushSwitchWritesTheMutedList() {
        val devices = FakeDevices()
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Box(Modifier.width(400.dp).height(1500.dp)) { ManagerPage(FakeManagers(), "m-3fa21c", onBack = {}, ticking = false, devices = devices) }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("关掉后，这台设备不再收到这个助手的推送").assertIsDisplayed()
        rule.onNode(hasText("推送通知") and hasClickAction()).performClick()
        rule.waitForIdle()
        assertEquals(listOf(listOf("m-3fa21c")), devices.saved)
    }

    /** The hub's own manager offers neither Install elsewhere nor Delete, and says why. */
    @Test fun theHubsOwnManagerCannotBeMovedOrDeleted() {
        val fake = FakeManagers()
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Box(Modifier.width(400.dp).height(1500.dp)) { ManagerPage(fake, "assistant", onBack = {}) }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("hub 自己运行的助手不能删除").assertIsDisplayed()
        rule.onNode(hasText("删除") and hasClickAction()).assertIsNotEnabled()
        assertTrue(rule.onAllNodes(hasText("换个地方装")).fetchSemanticsNodes().isEmpty())
        assertTrue(rule.onAllNodes(hasText("设为默认")).fetchSemanticsNodes().isEmpty())
    }

    /** Managers sits in Connection above Pairing, opens its page, and is left out where there is no hub (demo mode). */
    @Test fun theSettingsRowOpensTheManagers() {
        var opened: SettingsPage? = null
        var services by mutableStateOf(settingsServices(FakeManagers()))
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Box(Modifier.width(400.dp).height(2050.dp)) { SettingsScreen(services, onOpen = { opened = it }) }
            }
        }
        rule.waitForIdle()
        val row = rule.onNode(hasText("助手") and hasClickAction()).fetchSemanticsNode().boundsInRoot
        val pairing = rule.onNode(hasText("配对") and hasClickAction()).fetchSemanticsNode().boundsInRoot
        assertTrue(row.bottom <= pairing.top)
        rule.onNode(hasText("助手") and hasClickAction()).performClick()
        assertEquals(SettingsPage.Managers, opened)
        services = services.copy(demo = true)
        rule.waitForIdle()
        rule.onAllNodesWithText("助手").assertCountEquals(0)
    }

    private fun settingsServices(api: HubManagers) = SettingsServices(
        pairing = object : PairingService {
            override val isPaired = MutableStateFlow(true)
            override suspend fun pair(request: PairingRequest) = error("not used")
            override suspend fun unpair() = error("not used")
        },
        connection = object : HubConnection {
            override val state = MutableStateFlow(ConnectionState.Online)
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = error("not used")
            override fun send(message: JsonObject) = false
        },
        sessions = object : SessionsRepository {
            override val machines = MutableStateFlow(emptyList<Machine>())
            override val sessions = MutableStateFlow(emptyList<Session>())
            override suspend fun refresh() {}
        },
        updates = object : AppUpdates { override suspend fun check(): AppUpdate? = null },
        managers = api,
    )
}

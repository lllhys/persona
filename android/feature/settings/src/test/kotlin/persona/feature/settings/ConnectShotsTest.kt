package persona.feature.settings

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.DetectedEngine
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File
import java.lang.reflect.Proxy
import java.time.Instant

/**
 * The connection pages of Settings in each of the app's languages (docs/protocol/i18n.md): pairing (welcome, connect, manual
 * input, an address it can't use, confirm, a failed link), the hubs, the hub version, the candidate addresses and the engine step. Every style
 * in light, Glass in dark; written as PNGs to `build/i18n-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ConnectShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h1600dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", R.string.settings_pairing_manual)

    @Config(sdk = [35], qualifiers = "en-w400dp-h1600dp-xhdpi")
    @Test fun english() = shots("en", R.string.settings_pairing_manual)

    @Config(sdk = [35], qualifiers = "ja-w400dp-h1600dp-xhdpi")
    @Test fun japanese() = shots("ja", R.string.settings_pairing_manual)

    private enum class Shot { Welcome, Connect, Manual, ManualInvalid, Confirm, Failed, Hubs, HubUpdate, Endpoints, Engine }

    private val pairing = object : PairingService {
        override val isPaired = MutableStateFlow(false)
        override suspend fun pair(request: PairingRequest) = Unit
        override suspend fun unpair() = Unit
    }
    private val machine = Machine("m1", "工作站", online = true, engine = "herdr",
        engines = listOf(DetectedEngine("herdr", "herdr", true), DetectedEngine("orca", "Orca", false)))
    private val sessions = object : SessionsRepository {
        override val machines = MutableStateFlow(listOf(machine))
        override val sessions = MutableStateFlow(emptyList<Session>())
        override suspend fun refresh() = Unit
    }
    // Only drawn, never called: the pages hold on to it.
    private val management = Proxy.newProxyInstance(MachineManagement::class.java.classLoader, arrayOf(MachineManagement::class.java)) { _, _, _ ->
        throw UnsupportedOperationException()
    } as MachineManagement
    private val hubs = object : HubGroups {
        override val groups = MutableStateFlow(listOf(
            HubGroupInfo("home", "家里", null, selected = true, state = ConnectionState.Online, hubs = listOf(
                HubMemberInfo("h1", "家里的服务器", paired = true, connected = true, online = true),
                HubMemberInfo("h2", "NAS", paired = false, online = false),
            )),
            HubGroupInfo("office", "公司", "o", selected = false, state = ConnectionState.Connecting, hubs = listOf(
                HubMemberInfo("h3", "办公室", paired = true, online = true),
            )),
        ))
        override suspend fun select(groupId: String) = Unit
        override suspend fun rename(groupId: String, name: String) = Unit
        override suspend fun removeGroup(groupId: String) = Unit
        override suspend fun removeHub(groupId: String, hubId: String) = Unit
    }
    private val ago = { m: Long -> Instant.now().minusSeconds(m * 60).toString() }
    private val update = HubUpdateStatus(version = "1.42.0", build = 420, channel = "release", canUpdate = true, auto = true,
        lastCheck = ago(12), state = "waiting_idle",
        latest = HubRelease("1.43.0", 431, released = ago(180), newer = true, notes = "同步更快。", held = "这一版的代码不含 hub 部署时的提交"),
        busy = listOf(HubUpdateBusy("m1", "工作站", "s1", "重构登录流程"), HubUpdateBusy("m1", "工作站", null, "")))
    private val history = listOf(
        HubUpdateRecord("1.41.0", "1.42.0", 420, ended = ago(60 * 20), result = "updated", auto = true),
        HubUpdateRecord("1.41.0", "1.41.2", 412, ended = ago(60 * 30), result = "rolled_back"),
    )
    private val endpoints = object : ConnectionEndpoints {
        private val v6 = "https://[2001:db8::1]:18662"
        override val state = MutableStateFlow(ConnectionEndpointsState(
            entries = listOf(
                EndpointEntry(v6, "ipv6", EndpointSource.Hub, active = true),
                EndpointEntry("https://hub.example.com:18662", "name", EndpointSource.Manual),
                EndpointEntry("http://127.0.0.1:18660", "lan", EndpointSource.Pairing, enabled = false),
            ),
            activeUrl = v6, pinned = true, customOrder = true,
            tests = mapOf("https://hub.example.com:18662" to EndpointTestResult("https://hub.example.com:18662", false, persona.core.contracts.CoreText.plain("Timeout"), 5000, ago(3))),
        ))
        override fun check(address: String, replacing: String?) = EndpointCheck.Valid(address, "name")
        override suspend fun add(address: String) = address
        override suspend fun replace(url: String, address: String) = address
        override suspend fun remove(url: String) = Unit
        override suspend fun setEnabled(url: String, enabled: Boolean) = Unit
        override suspend fun reorder(urls: List<String>) = Unit
        override suspend fun resetOrder() = Unit
        override suspend fun test(url: String) = EndpointTestResult(url, true, persona.core.contracts.CoreText.plain("OK"), 40, ago(0))
    }
    private val link = "persona://pair?url=https%3A%2F%2Fhub.example.com%3A18662&token=example-token&hub=example-hub" +
        "&fp=${"A".repeat(43)}&alt=https%3A%2F%2F%5B2001%3Adb8%3A%3A1%5D%3A18662"

    private fun shots(language: String, manualLabel: Int) {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var shot by mutableStateOf(Shot.Welcome)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(1500.dp).background(PersonaTheme.palette.background)) {
                    key(shot, style, dark) {
                        when (shot) {
                            Shot.Welcome -> PairingScreen(pairing, sessions, onReady = {}, onDiagnostics = {})
                            Shot.Connect, Shot.Manual, Shot.ManualInvalid -> PairingScreen(pairing, sessions, onReady = {}, onDiagnostics = {}, skipWelcome = true)
                            Shot.Confirm -> PairingScreen(pairing, sessions, initialLink = link, onReady = {}, onDiagnostics = {})
                            Shot.Failed -> PairingScreen(pairing, sessions, initialLink = "persona://pair?token=x", onReady = {}, onDiagnostics = {})
                            Shot.Hubs -> HubsScreen(hubs, onAdd = {}, onBack = {})
                            Shot.HubUpdate -> HubUpdateContent(HubUpdateState(update, history), listOf(machine), {}, {}, null)
                            Shot.Endpoints -> {
                                val list = rememberLazyListState()
                                val scope = rememberCoroutineScope()
                                val snapshot = rememberEndpointsSnapshot(endpoints, emptyList(), list)!!
                                LazyColumn(Modifier.fillMaxWidth(), state = list, contentPadding = PaddingValues(Tokens.Space.l)) {
                                    editableCurrentConnection(snapshot, ConnectionDiagnostics(lastDisconnectReason = "Timeout"), ConnectionState.Online)
                                    editableCandidates(snapshot, scope)
                                }
                            }
                            Shot.Engine -> EngineSelectionScreen("m1", sessions, management, pairing = true, onDone = {})
                        }
                    }
                }
            }
        }
        val variants = AppStyle.entries.map { it to false } + (AppStyle.Glass to true)
        for ((s, d) in variants) for (next in Shot.entries) {
            style = s; dark = d; shot = next
            rule.waitForIdle()
            if (next == Shot.Manual || next == Shot.ManualInvalid) {
                rule.onNode(hasText(rule.activity.getString(manualLabel)) and hasClickAction()).performClick()
                rule.waitForIdle()
                rule.onNodeWithText(rule.activity.getString(R.string.settings_pairing_code_label)).assertExists()
            }
            if (next == Shot.ManualInvalid) {
                rule.onAllNodes(hasSetTextAction())[0].performTextInput("a b:99999")
                rule.onAllNodes(hasSetTextAction())[1].performTextInput("example")
                rule.onNode(hasText(rule.activity.getString(R.string.settings_pairing_next)) and hasClickAction()).performClick()
                rule.waitForIdle()
                rule.onNodeWithText(rule.activity.getString(R.string.settings_pairing_manual_invalid)).assertExists()
            }
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            var other = 0
            val background = bitmap.getPixel(1, 1)
            for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
            assertTrue("$next drew nothing", other > 100)
            val name = "connect-$language-${next.name.lowercase()}-${s.name.lowercase()}-${if (d) "dark" else "light"}.png"
            File(out(), name).outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}

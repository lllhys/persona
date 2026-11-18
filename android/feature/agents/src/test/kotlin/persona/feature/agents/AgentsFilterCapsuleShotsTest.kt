package persona.feature.agents

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.Density
import persona.core.designsystem.icons.PhIcons
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.domain.sort.AgentGrouping
import persona.core.domain.sort.AgentQuickFilter
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File

/** What the synthetic Agents page says in one language: the home bar's first tab, a computer and one session for each state. */
class FilterCapsuleWords(
    val language: String, val assistant: String, val computer: String,
    val unread: Pair<String, String>, val working: Pair<String, String>, val waiting: Pair<String, String>,
    val needsYou: Pair<String, String>, val larkGroup: Pair<String, String>, val terminal: Pair<String, String>,
)

/**
 * The Agents page's filter capsule (#9121, #7034): the production Agents page, not an HTML reconstruction; synthetic data
 * only. Every one of the seven filters has an agent, so the strip is as long as it gets. Each scene draws the strip at rest,
 * scrolled to its end, with Unread chosen and with the longest label chosen, in one style and appearance at one text size,
 * and then the grouping menu; a "few" scene draws a page of two states, whose three filters fit the strip. The same file
 * ran against `main` to draw the screenshots the change is compared with, so it uses only the page's own test tags.
 * PNGs go to `build/filter-capsule-shots/` (the module's build directory; `-Dpersona.shots=<dir>` does not reach the test JVM).
 */
abstract class FilterCapsuleScenes(private val words: FilterCapsuleWords) {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val sessions = listOf(
        Session("computer", "s1", title = words.unread.first, state = "unread", lastText = words.unread.second, updatedAt = "2026-11-13T12:00:00Z"),
        Session("computer", "s2", title = words.working.first, state = "working", lastText = words.working.second, updatedAt = "2026-11-13T12:00:00Z"),
        Session("computer", "s3", title = words.waiting.first, state = "working", lastText = words.waiting.second, backgroundWaiting = true,
            updatedAt = "2026-11-13T12:00:00Z"),
        Session("computer", "s4", title = words.needsYou.first, state = "needs_you", lastText = words.needsYou.second, updatedAt = "2026-11-13T12:00:00Z"),
        Session("computer", "s5", title = words.larkGroup.first, state = "unread", lastText = words.larkGroup.second, paneId = "botmux:abc",
            updatedAt = "2026-11-13T12:00:00Z"),
        Session("computer", "s6", title = words.terminal.first, state = "working", lastText = words.terminal.second, paneId = "w1:p1",
            updatedAt = "2026-11-13T12:00:00Z"),
    )
    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override val lastHello get() = Hello(serverVersion = "1.0.0", protocol = 1)
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }
    private class Requests : InteractionCenter {
        override val pending = MutableStateFlow<List<Interaction>>(emptyList())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }
    private class Sender : MessageSender {
        override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
    }
    private class Repository(rows: List<Session>, computer: String) : SessionsRepository {
        override val sessions = MutableStateFlow(rows)
        override val machines = MutableStateFlow(listOf(Machine("computer", computer, online = true)))
        override suspend fun refresh() = Unit
    }
    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
    private fun preferences(): AgentsPreferences {
        val store = object : DataStore<Preferences> {
            override val data = MutableStateFlow<Preferences>(emptyPreferences())
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences) = transform(data.value).also { data.value = it }
        }
        val hub = object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }
        return AgentsPreferences(store, hub)
    }
    private var scale = "default"
    private var textScale = 1f
    private fun write(image: Bitmap, name: String) {
        val directory = File(System.getProperty("persona.shots") ?: "build/filter-capsule-shots").also { it.mkdirs() }
        File(directory, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
    /** The whole page (at rest only) and the capsule's row with a margin around it, which is what the comparison is made of. */
    private fun save(state: String, style: AppStyle, dark: Boolean, popup: Boolean = false) {
        val root = rule.onNodeWithTag("shot")
        val image = root.captureToImage().asAndroidBitmap().copy(Bitmap.Config.ARGB_8888, true)
        if (popup) {
            val menu = rule.onNode(isPopup())
            val offset = menu.fetchSemanticsNode().positionOnScreen - root.fetchSemanticsNode().positionOnScreen
            Canvas(image).drawBitmap(menu.captureToImage().asAndroidBitmap(), offset.x, offset.y, null)
        }
        val name = "agents-$state-${style.name.lowercase()}-${if (dark) "dark" else "light"}-${words.language}-$scale"
        if (state == "rest" || state == "few" || popup) write(image, name)
        // The capsule is 42 dp tall and grows with the text size; the strip's own row can be shorter than it.
        val strip = rule.onNodeWithTag("agents-status-scroll").fetchSemanticsNode().boundsInRoot
        val density = rule.activity.resources.displayMetrics.density
        val middle = (strip.top + strip.bottom) / 2
        val half = maxOf((strip.bottom - strip.top) / 2, 21 * density * textScale)
        val top = (middle - half - 14 * density).toInt().coerceAtLeast(0)
        val bottom = (middle + half + 14 * density).toInt().coerceAtMost(image.height)
        write(Bitmap.createBitmap(image, 0, top, image.width, bottom - top), "$name-strip")
    }
    /** Lets what a gesture started finish: flings, the centering after a choice and the label timers of the older strip. */
    private fun settle() {
        rule.waitForIdle()
        rule.mainClock.advanceTimeBy(1_000)
        rule.waitForIdle()
    }
    private fun swipeStrip(left: Boolean) {
        repeat(5) {
            rule.onNodeWithTag("agents-status-scroll").performTouchInput { if (left) swipeLeft(durationMillis = 300) else swipeRight(durationMillis = 300) }
            settle()
        }
    }
    /** The Agents page over [rows], at a text size, with the search field pulled down above the strip; the strip is on screen. */
    private fun mount(style: AppStyle, dark: Boolean, fontScale: Float, rows: List<Session>): AgentsViewModel {
        val model = AgentsViewModel(Repository(rows, words.computer), Requests(), Connection(), Sender(), preferences())
        model.grouping(AgentGrouping.State)
        scale = if (fontScale > 1f) "large" else "default"
        textScale = fontScale
        rule.setContent {
            CompositionLocalProvider(LocalDensity provides Density(LocalDensity.current.density, fontScale)) {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                CompositionLocalProvider(LocalHomeChrome provides HomeChrome(listOf(
                    NavItem("assistant", words.assistant, PhIcons.ChatCircle), NavItem("agents", "Agents", PhIcons.Terminal)), "agents", {}, {})) {
                    Box(Modifier.width(400.dp).height(860.dp).background(PersonaTheme.palette.background).testTag("shot")) {
                        AgentsScreen(model, navigator, AgentsMetadata())
                    }
                }
            }
            }
        }
        rule.waitUntil(10_000) { rule.onAllNodesWithTag("agents-status-0").fetchSemanticsNodes().isNotEmpty() }
        rule.waitForIdle()
        rule.onNodeWithTag("shot").performTouchInput { swipeDown(durationMillis = 300) }
        rule.waitForIdle()
        return model
    }
    /** A page with two states only, so its three filters fit the strip: nothing to scroll to, nothing cut. */
    private fun few(style: AppStyle, dark: Boolean) {
        mount(style, dark, 1f, sessions.filter { it.state == "needs_you" || it.state == "working" && !it.backgroundWaiting }.take(2))
        rule.onNodeWithTag("agents-status-0").assertIsSelected()
        rule.onNodeWithTag("agents-status-2").assertExists()
        rule.onNodeWithTag("agents-status-3").assertDoesNotExist()
        save("few", style, dark)
    }
    private fun scene(style: AppStyle, dark: Boolean, fontScale: Float = 1f) {
        val model = mount(style, dark, fontScale, sessions)
        val grouping = rule.onNodeWithTag("agents-grouping")
        val fixed = grouping.fetchSemanticsNode().boundsInRoot
        rule.onNodeWithTag("agents-status-0").assertIsSelected()
        save("rest", style, dark)
        // To the end, by hand as a finger does it.
        swipeStrip(left = true)
        assertEquals("the grouping control stays where it is while the choices scroll", fixed, grouping.fetchSemanticsNode().boundsInRoot)
        save("end", style, dark)
        // Back to the start, then a filter in the middle: the strip carries it to the middle of the choices.
        swipeStrip(left = false)
        rule.onNodeWithTag("agents-status-scroll").performScrollToIndex(2)
        settle()
        rule.onNodeWithTag("agents-status-2").performClick()
        rule.waitUntil(5_000) { model.page.value.settings.quick == AgentQuickFilter.Unread }
        settle()
        rule.onNodeWithTag("agents-status-2").assertIsSelected()
        save("chosen", style, dark)
        // The longest label, chosen: shown whole where the strip has room for it, from its first word where it does not (200% text).
        rule.onNodeWithTag("agents-status-scroll").performScrollToIndex(4)
        settle()
        rule.onNodeWithTag("agents-status-4").performClick()
        rule.waitUntil(5_000) { model.page.value.settings.quick == AgentQuickFilter.BackgroundWaiting }
        settle()
        rule.onNodeWithTag("agents-status-4").assertIsSelected()
        save("long", style, dark)
        grouping.performClick()
        rule.waitForIdle()
        val menu = rule.onNode(isPopup())
        menu.assertExists()
        val byStatus = rule.activity.getString(R.string.agents_grouping_state)
        val byProject = rule.activity.getString(R.string.agents_grouping_project)
        listOf(byStatus, byProject, rule.activity.getString(R.string.agents_grouping_repo)).forEach {
            rule.onNode(hasText(it) and hasAnyAncestor(isPopup())).assertExists()
        }
        // The menu is the grouping alone: none of the filters is in it.
        listOf(words.working.first, words.unread.first).forEach { rule.onNode(hasText(it) and hasAnyAncestor(isPopup())).assertDoesNotExist() }
        save("menu", style, dark, popup = true)
        rule.onNode(hasText(byProject) and hasAnyAncestor(isPopup())).performClick()
        rule.waitUntil(5_000) { model.page.value.preferences.grouping == AgentGrouping.MachineSpace }
        rule.onNode(isPopup()).assertDoesNotExist()
        assertEquals(AgentQuickFilter.BackgroundWaiting, model.page.value.settings.quick)
    }
    @Test fun glassLight() = scene(AppStyle.Glass, false)
    @Test fun glassDark() = scene(AppStyle.Glass, true)
    @Test fun briefLight() = scene(AppStyle.Brief, false)
    @Test fun briefDark() = scene(AppStyle.Brief, true)
    @Test fun boardLight() = scene(AppStyle.Board, false)
    @Test fun boardDark() = scene(AppStyle.Board, true)
    @Test fun glassLargeLight() = scene(AppStyle.Glass, false, LARGEST)
    @Test fun glassLargeDark() = scene(AppStyle.Glass, true, LARGEST)
    @Test fun briefLargeLight() = scene(AppStyle.Brief, false, LARGEST)
    @Test fun briefLargeDark() = scene(AppStyle.Brief, true, LARGEST)
    @Test fun boardLargeLight() = scene(AppStyle.Board, false, LARGEST)
    @Test fun boardLargeDark() = scene(AppStyle.Board, true, LARGEST)
    @Test fun glassFewLight() = few(AppStyle.Glass, false)
    @Test fun glassFewDark() = few(AppStyle.Glass, true)
    @Test fun briefFewLight() = few(AppStyle.Brief, false)
    @Test fun briefFewDark() = few(AppStyle.Brief, true)
    @Test fun boardFewLight() = few(AppStyle.Board, false)
    @Test fun boardFewDark() = few(AppStyle.Board, true)

    private companion object {
        /** The largest text size Android's accessibility settings offer (200%). */
        const val LARGEST = 2f
    }
}

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h860dp-xhdpi")
class AgentsFilterCapsuleShotsTest : FilterCapsuleScenes(FilterCapsuleWords(
    "zh", "助手", "我的电脑",
    unread = "检查发布说明" to "发布说明已整理好。", working = "整理旅行资料" to "正在整理路线与住宿。",
    waiting = "等待后台任务" to "等待任务完成。", needsYou = "选择下一步" to "请确认下一步。",
    larkGroup = "群里问的发布时间" to "已经回复到群里了", terminal = "修复登录页" to "要跑测试吗？",
))

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h860dp-xhdpi")
class AgentsFilterCapsuleEnglishShotsTest : FilterCapsuleScenes(FilterCapsuleWords(
    "en", "Assistant", "My computer",
    unread = "Check the release notes" to "The release notes are ready.", working = "Plan the trip" to "Sorting out routes and stays.",
    waiting = "Wait for the nightly job" to "Waiting for the job to finish.", needsYou = "Choose the next step" to "Please confirm the next step.",
    larkGroup = "Release time asked in the group" to "Replied in the group.", terminal = "Fix the login page" to "Run the tests?",
))

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "ja-w400dp-h860dp-xhdpi")
class AgentsFilterCapsuleJapaneseShotsTest : FilterCapsuleScenes(FilterCapsuleWords(
    "ja", "アシスタント", "自分のコンピューター",
    unread = "リリースノートの確認" to "リリースノートを整理しました。", working = "旅行の計画" to "経路と宿を整理しています。",
    waiting = "夜間ジョブの待機" to "ジョブの完了を待っています。", needsYou = "次の手順の選択" to "次の手順を確認してください。",
    larkGroup = "グループで聞かれたリリース時刻" to "グループに返信しました。", terminal = "ログイン画面の修正" to "テストを実行しますか？",
))

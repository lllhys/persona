@file:OptIn(androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi::class)

package persona.android.ui

import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.material3.adaptive.currentWindowAdaptiveInfo
import androidx.compose.material3.adaptive.layout.calculatePaneScaffoldDirectiveWithTwoPanesOnMediumWidth
import androidx.compose.material3.adaptive.navigation3.ListDetailSceneStrategy
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.unit.dp
import persona.android.navigation.*
import persona.feature.assistant.AssistantSidebarContent

import android.app.Application
import android.content.ComponentName
import android.graphics.Bitmap
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.*
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.unit.Density
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.lifecycle.viewmodel.navigation3.rememberViewModelStoreNavEntryDecorator
import androidx.navigation3.runtime.*
import androidx.navigation3.ui.NavDisplay
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.After
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.android.navigation.AppNavigator
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.*
import persona.feature.agents.*
import persona.feature.chat.ChatScreen
import persona.feature.settings.AssistantSwitcher
import java.io.File

/** Regular-window columns with the real Assistant sidebar, Agents list, chat and Navigation 3 entries. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(application = Application::class, sdk = [35], qualifiers = "en-w673dp-h951dp-mdpi")
class FoldableWindowShotsTest {
    @get:Rule val rule = createEmptyComposeRule()
    private var animationScale: Float? = null
    @After fun resetAssistant() {
        AssistantManagerChoice.reset()
        animationScale?.let { scale ->
            Settings.Global.putFloat(ApplicationProvider.getApplicationContext<Application>().contentResolver,
                Settings.Global.ANIMATOR_DURATION_SCALE, scale)
        }
    }
    private val agent = Session("computer", "agent", title = "Plan a weekend trip", state = "unread",
        cwd = "/example/travel", updatedAt = "2026-11-12T09:00:00Z")
    private val route = ChatRoute(agent.machineId, agent.sessionId, ConversationKind.Agent)
    private val otherAgent = agent.copy(sessionId = "packing", title = "Review the packing list")
    private val repositories = object : SessionsRepository {
        override val machines = MutableStateFlow(listOf(Machine("computer", "Computer A", online = true)))
        override val sessions = MutableStateFlow(listOf(agent, otherAgent))
        override suspend fun refresh() = Unit
    }
    private val interactions = object : InteractionCenter {
        override val pending = MutableStateFlow(emptyList<Interaction>())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }
    private val sender = object : MessageSender {
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
    }
    private val chats = object : ChatRepository {
        override fun observe(session: SessionKey) = MutableStateFlow(ChatSnapshot(loading = false, items = listOf(
            Item("answer", 1, role = "assistant", kind = "text",
                text = if (session == otherAgent.key) "The packing list is ready." else "The trip outline is ready.", ts = "2026-11-12T09:00:00Z"),
        )))
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
    }
    private val connection = object : HubConnection {
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override val lastHello = Hello(serverVersion = "1.0", protocol = 1)
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
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

    @Test fun uprightInnerScreen() = windows("upright")
    @Test fun uprightOverlayPickMotionLight() = overlayPickMotion(dark = false)
    @Test fun uprightOverlayPickMotionDark() = overlayPickMotion(dark = true)
    @Test fun uprightOverlayPickMotionBriefLight() = overlayPickMotion(dark = false, style = AppStyle.Brief)
    @Test fun uprightOverlayPickMotionBriefDark() = overlayPickMotion(dark = true, style = AppStyle.Brief)
    @Test fun uprightOverlayPickMotionBoardLight() = overlayPickMotion(dark = false, style = AppStyle.Board)
    @Test fun uprightOverlayPickMotionBoardDark() = overlayPickMotion(dark = true, style = AppStyle.Board)
    @Test fun uprightOverlayPickIsInstantWithReducedMotion() = overlayPickMotion(dark = false, reduced = true)
    @Test fun uprightOverlayPickIsInstantWithSystemAnimationsOff() = overlayPickMotion(dark = true, systemOff = true)
    @Test fun uprightInnerScreenLarge() = windows("upright-large", large = true)
    @Config(qualifiers = "en-w951dp-h673dp-mdpi")
    @Test fun landscapeInnerScreen() = windows("landscape")
    @Config(qualifiers = "en-w411dp-h797dp-mdpi")
    @Test fun foldedOuterScreen() = windows("folded")
    @Config(qualifiers = "en-w1000dp-h800dp-mdpi")
    @Test fun wideWindow() = windows("wide")
    @Config(qualifiers = "en-w1000dp-h800dp-mdpi")
    @Test fun wideWindowWithSwitcher() = windows("wide-switcher", withSwitcher = true)
    @Config(qualifiers = "en-w1000dp-h800dp-mdpi")
    @Test fun wideWindowAtMaximumDivider() = windows("wide-max", dragMax = true)

    /**
     * iOS SplitNavigation.swift:125-146, 178-208: the sidebar replaces the detail immediately; only hideOverlay moves.
     * The real decorated Agents list and chat must retain that distinction on an upright open foldable (673 x 951dp).
     */
    private fun overlayPickMotion(dark: Boolean, style: AppStyle = AppStyle.Glass, reduced: Boolean = false, systemOff: Boolean = false) {
        val app = ApplicationProvider.getApplicationContext<Application>()
        Shadows.shadowOf(app.packageManager).addActivityIfNotPresent(ComponentName(app, ComponentActivity::class.java))
        animationScale = Settings.Global.getFloat(app.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f)
        Settings.Global.putFloat(app.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, if (systemOff) 0f else 1f)
        val prefs = preferences()
        val dependencies = object : AgentsDependencies {
            override fun sessions() = repositories
            override fun interactions() = this@FoldableWindowShotsTest.interactions
            override fun connection() = this@FoldableWindowShotsTest.connection
            override fun sender() = this@FoldableWindowShotsTest.sender
            override fun preferences() = prefs
            override fun machines(): MachineManagement? = null
        }
        val shown = mutableStateOf(false)
        val stack = mutableStateListOf<NavKey>(AgentsRoute, route)
        val navigator = AppNavigator(mapOf(AppTab.Agents to stack), { AppTab.Agents }, {}, chatTab = { AppTab.Agents })
        val listNavigator = object : Navigator by navigator {
            override fun navigate(key: NavKey) = pickAgentsList(navigator, stack, shown, key)
        }
        var listCompositions = 0
        rule.mainClock.autoAdvance = false
        ActivityScenario.launch(ComponentActivity::class.java).use { scenario ->
            scenario.onActivity { activity -> activity.setContent {
                // Leave PersonaTheme's default system-motion lookup in place for the system-off regression test.
                PersonaTheme(style = style, darkTheme = dark) {
                  CompositionLocalProvider(LocalReduceMotion provides (reduced || PersonaTheme.reduceMotion)) {
                    val overlay = rememberAgentsOverlayStrategy(true, shown)
                    val transitions = rememberPageTransitions()
                    val provider = entryProvider<NavKey> {
                        agentsEntries(navigator, dependencies, listNavigator = listNavigator)
                        entry<ChatRoute> { key ->
                            Box(Modifier.fillMaxSize().testTag("detail:${key.sessionId}")) {
                                ChatScreen(SessionKey(key.machineId, key.sessionId), chats, sender, interactions, repositories, onBack = navigator::back)
                            }
                        }
                    }
                    Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("shot")) {
                        NavDisplay(backStack = stack, onBack = navigator::back,
                            sceneStrategies = listOf(overlay),
                            entryDecorators = listOf(rememberSaveableStateHolderNavEntryDecorator(), rememberViewModelStoreNavEntryDecorator()),
                            transitionSpec = { transitions.push }, popTransitionSpec = { transitions.pop },
                            predictivePopTransitionSpec = { transitions.predictivePop },
                            entryProvider = { key ->
                                val entry = provider(key)
                                NavEntry(key, entry.contentKey, entry.metadata) {
                                    if (key == AgentsRoute) {
                                        SideEffect { listCompositions++ }
                                        Box(Modifier.fillMaxSize().testTag("motion-list")) { entry.Content() }
                                    } else entry.Content()
                                }
                            })
                    }
                  }
                }
            } }
            awaitText("The trip outline is ready.")
            rule.runOnIdle { shown.value = true }
            rule.mainClock.advanceTimeBy(1_000)
            awaitText(otherAgent.displayTitle)
            val before = capture("motion-before", style, dark, settle = false)
            rule.onNodeWithText(otherAgent.displayTitle, useUnmergedTree = true).performClick()
            Snapshot.sendApplyNotifications()
            repeat(2) { rule.mainClock.advanceTimeByFrame() }
            assertTrue("a pick closes the overlay on the tap frame", !shown.value)
            rule.onNodeWithTag("detail:${agent.sessionId}").assertDoesNotExist()
            rule.onNodeWithTag("detail:${otherAgent.sessionId}").assertExists()
            val detail = rule.onNodeWithTag("detail:${otherAgent.sessionId}").getUnclippedBoundsInRoot()
            assertEquals("replacement does not slide", 0f, detail.left.value, 0f)
            assertEquals(673f, (detail.right - detail.left).value, 0f)
            val compositionsAfterPick = listCompositions
            val instant = reduced || systemOff
            if (instant) {
                rule.onNodeWithTag("motion-list").assertDoesNotExist()
                capture("motion-reduced-first", style, dark, settle = false)
                rule.mainClock.advanceTimeBy(1_000)
                capture("motion-reduced-end", style, dark, settle = false)
                rule.onNodeWithTag("motion-list").assertDoesNotExist()
                // Chat data arrives asynchronously; pin navigation geometry, independently of the loading content.
                assertEquals("reduced motion is at its final position on the first frame", detail,
                    rule.onNodeWithTag("detail:${otherAgent.sessionId}").getUnclippedBoundsInRoot())
            } else {
                rule.mainClock.advanceTimeBy(80)
                val list = rule.onNodeWithTag("motion-list").getUnclippedBoundsInRoot()
                assertTrue("the list is sliding away: $list", list.left.value in -319f..-1f)
                val mid = capture("motion-mid", style, dark, settle = false)
                assertEquals("animation frames do not recompose the list", compositionsAfterPick, listCompositions)
                rule.mainClock.advanceTimeBy(1_000)
                rule.onNodeWithTag("motion-list").assertDoesNotExist()
                val end = capture("motion-end", style, dark, settle = false)
                assertTrue("mid-frame differs from start", !mid.sameAs(before))
                assertTrue("mid-frame differs from end", !mid.sameAs(end))
            }
        }
    }

    private fun windows(window: String, large: Boolean = false, dragMax: Boolean = false, withSwitcher: Boolean = false) {
        AssistantManagerChoice.choose("hub", null, "assistant")
        val app = ApplicationProvider.getApplicationContext<Application>()
        Shadows.shadowOf(app.packageManager).addActivityIfNotPresent(ComponentName(app, ComponentActivity::class.java))
        val prefs = preferences()
        val dividerStore = if (dragMax) PaneSplitStore(app) else null
        val dependencies = object : AgentsDependencies {
            override fun sessions() = repositories
            override fun interactions() = this@FoldableWindowShotsTest.interactions
            override fun connection() = this@FoldableWindowShotsTest.connection
            override fun sender() = this@FoldableWindowShotsTest.sender
            override fun preferences() = prefs
            override fun machines(): MachineManagement? = null
        }
        val main = Session("computer", "assistant", title = "Main conversation", assistant = true)
        val recent = main.copy(sessionId = "recent", title = "Weekend plans", updatedAt = "2026-11-12T09:00:00Z")
        val snapshot = AssistantSnapshot(mainSession = main.key, conversations = listOf(main, recent))
        val assistantRepository = object : AssistantRepository {
            override val assistant = MutableStateFlow(snapshot)
            override suspend fun refresh() = Unit
        }
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var selected by mutableStateOf(AppTab.Persona)
        val columnShown = mutableStateOf(true)
        val sidebarOverShown = mutableStateOf(false)
        val agentsOverShown = mutableStateOf(false)
        val drawerShown = mutableStateOf(false)
        val stacks = AppTab.entries.associateWith { mutableStateListOf<NavKey>(tabRoot(it)) }
        val navigator = AppNavigator(stacks, { selected }, { selected = it }, chatTab = { AppTab.Agents })
        val listNavigator = object : Navigator by navigator {
            override fun navigate(key: NavKey) { pickAgentsList(navigator, stacks.getValue(AppTab.Agents), agentsOverShown, key) }
        }
        val roster = if (withSwitcher) ManagerRoster(listOf(
            HubManager("assistant", "Persona", online = true, isDefault = true, builtin = true, host = "MacBook"),
            HubManager("m-2", "Work", online = true, host = "server", inboxNeedsYou = 1),
        ), "assistant") else null
        val managers = if (withSwitcher) object : HubManagers {
            override suspend fun roster() = roster!!
            override suspend fun create(name: String) = error("unused")
            override suspend fun newCode(id: String) = error("unused")
            override suspend fun rename(id: String, name: String) = error("unused")
            override suspend fun makeDefault(id: String) = error("unused")
            override suspend fun setHide(id: String, hide: Boolean) = error("unused")
            override suspend fun delete(id: String) = error("unused")
        } else null
        rule.mainClock.autoAdvance = false
        ActivityScenario.launch(ComponentActivity::class.java).use { scenario ->
            scenario.onActivity { activity -> activity.setContent {
              CompositionLocalProvider(
                  LocalDensity provides Density(LocalDensity.current.density, if (large) 2f else 1f),
                  LocalManagerRoster provides roster,
              ) {
                PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                    val scope = rememberCoroutineScope()
                    val width = with(LocalDensity.current) { LocalWindowInfo.current.containerSize.width.toDp() }
                    val height = with(LocalDensity.current) { LocalWindowInfo.current.containerSize.height.toDp() }
                    val regular = isRegularWindow(width.value, height.value)
                    val over = regular && columnPlacement(width.value, emptyList()) == ColumnPlacement.Over
                    val shown = if (over) sidebarOverShown else columnShown
                    val sidebar: @Composable (Boolean) -> Unit = { canFold ->
                        AssistantSidebarContent(snapshot, assistantRepository, main.key, scope, demo = !withSwitcher,
                            onOpen = { sidebarOverShown.value = false; drawerShown.value = false },
                            onAction = { sidebarOverShown.value = false; drawerShown.value = false },
                            onSettings = { sidebarOverShown.value = false; drawerShown.value = false },
                            onInbox = { sidebarOverShown.value = false; drawerShown.value = false },
                            onKnowledge = {}, onTodos = null, todoCount = 0, showTodos = false, inboxCount = 0,
                            settingsCount = 0, settingsDot = false, settingsBadge = null, pages = emptyList(), onPage = {}, onRetired = {},
                            switcher = if (withSwitcher) ({ head, trailing, fallback ->
                                AssistantSwitcher(managers, mapOf("m-2" to 1), hubOnline = true, onManage = {},
                                    headModifier = head, ticking = false, trailing = trailing, fallback = fallback)
                            }) else null,
                            fullWindowSearch = regular,
                            onFoldColumn = if (canFold) ({ columnShown.value = false }) else null)
                    }
                    val rootKeys = remember { listOf<NavKey>(AssistantRoute, AgentsRoute).map { NavEntry(it) {}.contentKey }.toSet() }
                    val assistantKey = remember { NavEntry<NavKey>(AssistantRoute) {}.contentKey }
                    val chrome = NavEntryDecorator<NavKey> { entry ->
                        val primary = entry.contentKey in rootKeys
                        val tabSplit = LocalAgentsTabSplit.current
                        val entrySplit = rememberEntrySplitDetail(tabSplit, entry.contentKey)
                        val page: @Composable () -> Unit = {
                            val content: @Composable () -> Unit = {
                                CompositionLocalProvider(LocalHomeChrome provides if (primary) HomeChrome(navigationItems(emptyMap()), selected.name,
                                    onSelect = { navigator.selectTab(AppTab.valueOf(it)) }, barHeight = remember { mutableIntStateOf(0) },
                                    leading = {
                                        val action = resolveHomeLeadingAction(
                                            assistantRoot = entry.contentKey == assistantKey,
                                            regular = regular,
                                            columnsOver = over,
                                            sidebarColumnShown = columnShown.value,
                                        )
                                        if (action == HomeLeadingAction.HiddenSpacer) JPersonaButtonRoom()
                                        else Box {
                                            JPersonaButton(PersonaState.Idle, "Sidebar", {
                                                when (action) {
                                                    HomeLeadingAction.OpenDrawer -> drawerShown.value = true
                                                    HomeLeadingAction.ToggleOverlay -> sidebarOverShown.value = !sidebarOverShown.value
                                                    HomeLeadingAction.ShowColumn -> columnShown.value = true
                                                    HomeLeadingAction.HiddenSpacer -> Unit
                                                }
                                            })
                                            if (withSwitcher) JStatusDot(Status.NeedsYou, Modifier.align(androidx.compose.ui.Alignment.TopEnd).padding(Tokens.Space.xs), breathes = false)
                                        }
                                    }) else null) { entry.Content() }
                            }
                            if (tabSplit != null) CompositionLocalProvider(LocalSplitDetail provides entrySplit, content = content) else content()
                        }
                        if (entry.contentKey == assistantKey) {
                            val split = sidebarSplit(width.value, emptyList())
                            val canFold = showsSidebarColumnFoldButton(regular = regular, columnsOver = over)
                            JSidebarColumn(if (regular) ({ sidebar(canFold) }) else null, width = (if (over) PaneSplits.DefaultLeft else split.sidebar).dp,
                                pageStart = split.page.dp, shown = shown.value, overlay = over,
                                onDismiss = { sidebarOverShown.value = false }) { page() }
                        } else page()
                    }
                    val adaptive = currentWindowAdaptiveInfo()
                    val directive = calculatePaneScaffoldDirectiveWithTwoPanesOnMediumWidth(adaptive)
                    val stack = stacks.getValue(selected)
                    val listDetail = rememberShellListDetail(selected, directive, width, emptyList(), detailOf(stack) != null,
                        store = dividerStore, collapsible = canCollapseList(stack))
                    val agentsTabSplit = rememberAgentsTabSplit(
                        tab = selected,
                        stack = stack,
                        twoPanes = directive.maxHorizontalPartitions > 1 && !over,
                        listHidden = listDetail.listHidden,
                        onShowList = listDetail::expand,
                    )
                    val overlay = rememberAgentsOverlayStrategy(over && selected == AppTab.Agents, agentsOverShown) {
                        CompositionLocalProvider(LocalHomeChrome provides HomeChrome(navigationItems(emptyMap()), selected.name,
                            onSelect = { navigator.selectTab(AppTab.valueOf(it)) }, leading = {
                                JPersonaButton(PersonaState.Idle, "Sidebar", { drawerShown.value = true })
                            })) {
                            AgentsEmptyPage { agentsOverShown.value = true }
                        }
                    }
                    val pageTransitions = rememberPageTransitions()
                    Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("shot")) {
                        JShellDrawer(drawerShown.value, onDismiss = { drawerShown.value = false }, drawer = { sidebar(false) }) {
                            CompositionLocalProvider(LocalAgentsTabSplit provides agentsTabSplit) {
                                NavDisplay(backStack = stack, onBack = navigator::back,
                                    entryDecorators = listOf(rememberSaveableStateHolderNavEntryDecorator(), rememberViewModelStoreNavEntryDecorator(), chrome),
                                    sceneStrategies = listOf(overlay, listDetail.strategy),
                                transitionSpec = { pageTransitions.push },
                                popTransitionSpec = { pageTransitions.pop },
                                predictivePopTransitionSpec = { pageTransitions.predictivePop },
                                entryProvider = { key ->
                                    val provider = entryProvider<NavKey> {
                                        entry<AssistantRoute> {
                                            JPage("Assistant") { padding ->
                                                Box(Modifier.fillMaxSize().padding(padding).testTag("assistant-page")) {
                                                    ChatScreen(main.key, chats, sender, interactions, repositories, onBack = {}, embedded = true,
                                                        showTopBar = false, renderStyle = persona.feature.chat.ChatRenderStyle.Assistant, historySession = main)
                                                }
                                            }
                                        }
                                        agentsEntries(navigator, dependencies, listNavigator = listNavigator)
                                        entry<ChatRoute> { route ->
                                            Box(Modifier.fillMaxSize().testTag("agent-page")) {
                                                ChatScreen(SessionKey(route.machineId, route.sessionId), chats, sender, interactions, repositories, onBack = navigator::back)
                                            }
                                        }
                                    }
                                    val entry = provider(key)
                                    val metadata = when (key) {
                                        AgentsRoute -> ListDetailSceneStrategy.listPane(detailPlaceholder = { PageBackdrop() })
                                        is ChatRoute -> ListDetailSceneStrategy.detailPane()
                                        else -> emptyMap()
                                    }
                                    NavEntry(key, entry.contentKey, entry.metadata + metadata) { entry.Content() }
                                })
                            }
                        }
                    }
                }
              }
            } }
            for (s in AppStyle.entries) {
                var light: Bitmap? = null
                for (d in listOf(false, true)) {
                    rule.runOnIdle { style = s; dark = d; selected = AppTab.Persona; sidebarOverShown.value = false; columnShown.value = true }
                    frames()
                    val page = rule.onNodeWithTag("assistant-page").getBoundsInRoot()
                    if (window.startsWith("upright")) {
                        capture("$window-assistant-away", s, d)
                        rule.onAllNodesWithContentDescription("Hide sidebar").assertCountEquals(0)
                        assertEquals(0f, page.left.value, 0.01f)
                        rule.onNodeWithContentDescription("Sidebar").performClick()
                        frames()
                        assertTrue(sidebarOverShown.value)
                        rule.onAllNodesWithContentDescription("Hide sidebar").assertCountEquals(0)
                        val open = capture("$window-assistant-over", s, d)
                        if (!d) light = open else assertTrue("light and dark must differ", !open.sameAs(light!!))
                        rule.onNodeWithContentDescription("Close").performTouchInput { click(centerRight) }
                        frames()
                        assertTrue(!sidebarOverShown.value)
                        rule.onNodeWithContentDescription("Sidebar").performClick()
                        frames()
                        rule.onNodeWithText("Weekend plans").performScrollTo().performClick()
                        frames()
                        assertTrue("a pick dismisses the sidebar", !sidebarOverShown.value)
                        capture("$window-assistant-picked", s, d)
                    } else if (window == "folded") {
                        rule.onAllNodesWithContentDescription("Hide sidebar").assertCountEquals(0)
                        rule.onNodeWithContentDescription("Sidebar").assertExists()
                        capture("$window-assistant-away", s, d)
                    } else {
                        rule.onAllNodesWithContentDescription("Sidebar").assertCountEquals(0)
                        rule.onNodeWithContentDescription("Hide sidebar").assertExists()
                        capture("$window-assistant-out", s, d)
                        val switcherOut = rule.onNodeWithTag("home-switcher").getBoundsInRoot()
                        val pageOut = rule.onNodeWithTag("assistant-page").getBoundsInRoot()
                        rule.onNodeWithContentDescription("Hide sidebar").performClick()
                        frames()
                        assertTrue(!columnShown.value)
                        rule.onAllNodesWithContentDescription("Hide sidebar").assertCountEquals(0)
                        rule.onNodeWithContentDescription("Sidebar").assertExists()
                        val switcherFolded = rule.onNodeWithTag("home-switcher").getBoundsInRoot()
                        val pageFolded = rule.onNodeWithTag("assistant-page").getBoundsInRoot()
                        if (s == AppStyle.Glass) {
                            val pageCenter = (pageFolded.left.value + pageFolded.right.value) / 2f
                            val switcherCenter = (switcherFolded.left.value + switcherFolded.right.value) / 2f
                            assertEquals("Top-bar switcher stayed centered in $window $s $d", pageCenter, switcherCenter, 0.5f)
                        } else {
                            assertEquals("Top-bar switcher shifted in $window $s $d",
                                switcherOut.left.value - pageOut.left.value,
                                switcherFolded.left.value - pageFolded.left.value, 0.01f)
                        }
                        capture("$window-assistant-folded", s, d)
                        rule.onNodeWithContentDescription("Sidebar").performClick()
                        frames()
                        assertTrue(columnShown.value)
                        rule.onAllNodesWithContentDescription("Sidebar").assertCountEquals(0)
                        rule.onNodeWithContentDescription("Hide sidebar").assertExists()
                    }
                    rule.runOnIdle { navigator.selectTab(AppTab.Agents) }
                    if (window.startsWith("upright")) {
                        frames()
                        capture("$window-agents-empty-away", s, d)
                        rule.onNodeWithContentDescription("Show list").performClick()
                        awaitText(agent.displayTitle)
                        capture("$window-agents-empty-over", s, d)
                    }
                    awaitText(agent.displayTitle)
                    rule.onNodeWithText(agent.displayTitle, useUnmergedTree = true).performClick()
                    awaitText("The trip outline is ready.")
                    if (window.startsWith("upright")) {
                        val full = rule.onNodeWithTag("agent-page").getBoundsInRoot()
                        assertEquals(0f, full.left.value, 0.01f)
                        assertEquals(673f, (full.right - full.left).value, 0.01f)
                        capture("$window-agents-away", s, d)
                        rule.onNodeWithContentDescription("Sidebar").performClick()
                        frames()
                        assertTrue(agentsOverShown.value)
                        capture("$window-agents-over", s, d)
                        rule.onNodeWithContentDescription("Close").performTouchInput { click(centerRight) }
                        frames()
                        assertTrue(!agentsOverShown.value)
                        rule.onNodeWithContentDescription("Sidebar").performClick()
                        frames()
                        rule.onNodeWithText(agent.displayTitle).performClick()
                        frames()
                        assertTrue("reselecting the current row dismisses the list", !agentsOverShown.value)
                        capture("$window-agents-picked", s, d)
                        rule.onNodeWithContentDescription("Sidebar").performClick()
                        frames()
                        rule.onNodeWithText(otherAgent.displayTitle, useUnmergedTree = true).performClick()
                        awaitText("The packing list is ready.")
                        assertTrue(!agentsOverShown.value)
                        assertEquals("a list pick replaces the root detail", 2, stacks.getValue(AppTab.Agents).size)
                        rule.onNodeWithContentDescription("Sidebar").performClick()
                        frames()
                        assertTrue("the second picked detail can reopen its list", agentsOverShown.value)
                        rule.onNodeWithContentDescription("Close").performTouchInput { click(centerRight) }
                        frames()
                    } else {
                        if (dragMax) {
                            val resize = app.getString(persona.core.designsystem.R.string.designsystem_pane_resize)
                            repeat(3) {
                                rule.onNodeWithContentDescription(resize).performTouchInput {
                                    swipe(center, center + androidx.compose.ui.geometry.Offset(300f, 0f), 400)
                                }
                                repeat(100) { rule.mainClock.advanceTimeByFrame() }
                            }
                            frames()
                            // The 400dp placement token must not shorten the old 1000 - 320dp drag limit.
                            assertEquals(680f, dividerStore!!.share(AppTab.Agents)!! * 1000f, 0.01f)
                        }
                        capture("$window-agents-detail", s, d)
                    }
                }
            }
        }
    }
    private fun frames() {
        Snapshot.sendApplyNotifications()
        repeat(16) { rule.mainClock.advanceTimeByFrame() }
        rule.waitForIdle()
    }
    private fun awaitText(text: String) {
        val deadline = System.nanoTime() + 30_000_000_000L
        do {
            frames()
            if (rule.onAllNodesWithText(text, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()) return
            Thread.sleep(20)
        } while (System.nanoTime() < deadline)
        error("Missing $text: ${rule.onRoot().printToString()}")
    }
    private fun capture(stage: String, style: AppStyle, dark: Boolean, settle: Boolean = true): Bitmap {
        if (settle) frames()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val out = File("build/foldable-window-shots").also { it.mkdirs() }
        File(out, "$stage-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream()
            .use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        return bitmap
    }
}

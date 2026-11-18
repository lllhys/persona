@file:OptIn(androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi::class)

package persona.android.navigation

import android.app.Application
import android.content.ComponentName
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.material3.adaptive.layout.PaneExpansionState
import androidx.compose.material3.adaptive.layout.rememberPaneExpansionState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.navigation3.runtime.NavEntry
import androidx.navigation3.runtime.NavKey
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows
import org.robolectric.annotation.Config
import persona.android.ui.PaneSplitter
import persona.core.designsystem.DetailLeading
import persona.core.designsystem.SplitDetailContext
import persona.core.designsystem.resolveDetailLeading
import persona.core.navigation.AgentsRoute
import persona.core.navigation.AppTab
import persona.core.navigation.AssistantRoute
import persona.core.navigation.ChatRoute
import persona.core.navigation.ConversationKind
import persona.core.navigation.MachineSpacesRoute
import persona.core.navigation.SettingsPage
import persona.core.navigation.SettingsPageRoute
import persona.core.navigation.SettingsRoute
import persona.core.navigation.SpaceRoute
import persona.core.navigation.TerminalRoute

@RunWith(AndroidJUnit4::class)
@Config(application = Application::class, sdk = [35])
class SplitDetailDecisionTest {
    @get:Rule val rule = createEmptyComposeRule()

    private val chat = ChatRoute("mac", "s1", ConversationKind.Agent)
    private val secondChat = ChatRoute("mac", "s2", ConversationKind.Agent)
    private val space = SpaceRoute("space-1", "Workspace")
    private val terminal = TerminalRoute("mac", "s1")
    private val onBack: () -> Unit = {}
    private val onShowList: () -> Unit = {}

    @Test fun listPageItselfGetsNoSplitDetailContext() {
        for (listHidden in listOf(false, true)) {
            assertNull(agentsSplitDetailForEntry(AppTab.Agents, listOf(AgentsRoute), entryIndex = 0, twoPanes = true, listHidden = listHidden, onShowList = onShowList))
            assertNull(agentsSplitDetailForEntry(AppTab.Agents, listOf(AgentsRoute, chat), entryIndex = 0, twoPanes = true, listHidden = listHidden, onShowList = onShowList))
        }
    }

    @Test fun firstPageWithListBesideHidesBack() {
        for (stack in listOf(listOf(AgentsRoute, chat), listOf(AgentsRoute, space), listOf(AgentsRoute, MachineSpacesRoute("mac")))) {
            val split = agentsSplitDetailForEntry(AppTab.Agents, stack, entryIndex = 1, twoPanes = true, listHidden = false, onShowList = onShowList)
            assertNotNull(stack.toString(), split)
            assertEquals(DetailLeading.Hidden, resolveDetailLeading(split, onBack))
        }
    }

    @Test fun perEntryDecisionKeepsFirstPageWithoutBackWhileSecondPageIsOnTop() {
        val stack = listOf(AgentsRoute, chat, secondChat)
        val firstSplit = agentsSplitDetailForEntry(AppTab.Agents, stack, entryIndex = 1, twoPanes = true, listHidden = false, onShowList = onShowList)
        val secondSplit = agentsSplitDetailForEntry(AppTab.Agents, stack, entryIndex = 2, twoPanes = true, listHidden = false, onShowList = onShowList)

        assertNull(agentsSplitDetailForEntry(AppTab.Agents, stack, entryIndex = 0, twoPanes = true, listHidden = false, onShowList = onShowList))
        assertEquals(DetailLeading.Hidden, resolveDetailLeading(firstSplit, onBack))
        assertEquals(DetailLeading.Back(onBack), resolveDetailLeading(secondSplit, onBack))
    }

    @Test fun transitionRetainsPerEntryLeadingAcrossPushAndPop() {
        val app = ApplicationProvider.getApplicationContext<Application>()
        Shadows.shadowOf(app.packageManager).addActivityIfNotPresent(ComponentName(app, ComponentActivity::class.java))
        val stack = mutableStateListOf<NavKey>(AgentsRoute, chat)
        var listHidden by mutableStateOf(false)
        val listKey = NavEntry<NavKey>(AgentsRoute) {}.contentKey
        val d1Key = NavEntry<NavKey>(chat) {}.contentKey
        val d2Key = NavEntry<NavKey>(secondChat) {}.contentKey
        var listSplit: SplitDetailContext? = SplitDetailContext(isFirstPage = true)
        var d1Split: SplitDetailContext? = null
        var d2Split: SplitDetailContext? = null
        var keepD2Alive by mutableStateOf(false)
        var keepD1Alive by mutableStateOf(true)

        ActivityScenario.launch(ComponentActivity::class.java).use { scenario ->
            scenario.onActivity { activity ->
                activity.setContent {
                    val tabSplit = rememberAgentsTabSplit(AppTab.Agents, stack, twoPanes = true, listHidden = listHidden, onShowList = onShowList)
                    listSplit = rememberEntrySplitDetail(tabSplit, listKey)
                    if (keepD1Alive) d1Split = rememberEntrySplitDetail(tabSplit, d1Key)
                    if (keepD2Alive) d2Split = rememberEntrySplitDetail(tabSplit, d2Key)
                }
            }
            rule.runOnIdle {
                assertNull(listSplit)
                assertEquals(DetailLeading.Hidden, resolveDetailLeading(d1Split, onBack))

                // Push d2 onto [Agents, d1]: d1 stays without Back while d2 is on top; d2 keeps Back.
                keepD2Alive = true
                stack.add(secondChat)
            }
            rule.runOnIdle {
                assertNull(listSplit)
                assertEquals(DetailLeading.Hidden, resolveDetailLeading(d1Split, onBack))
                assertEquals(DetailLeading.Back(onBack), resolveDetailLeading(d2Split, onBack))

                // Pop d2 while its exit transition still composes it: d2 keeps Back and d1 stays without Back.
                stack.removeAt(stack.lastIndex)
            }
            rule.runOnIdle {
                assertEquals(DetailLeading.Hidden, resolveDetailLeading(d1Split, onBack))
                assertEquals(DetailLeading.Back(onBack), resolveDetailLeading(d2Split, onBack))

                // Pop d1 while its exit transition still composes it: d1 stays without Back instead of flashing Back.
                keepD2Alive = false
                stack.removeAt(stack.lastIndex)
            }
            rule.runOnIdle {
                assertNull(listSplit)
                assertEquals(DetailLeading.Hidden, resolveDetailLeading(d1Split, onBack))
            }
        }
    }

    @Test fun firstPageWithCollapsedListShowsSidebarActionAndExpandingUpdatesKeptChoice() {
        val app = ApplicationProvider.getApplicationContext<Application>()
        Shadows.shadowOf(app.packageManager).addActivityIfNotPresent(ComponentName(app, ComponentActivity::class.java))
        lateinit var state: PaneExpansionState
        val memory = ColumnMemory(initialSidebarShown = true, initialAgentsShown = true, persist = true)
        lateinit var splitter: PaneSplitter
        splitter = PaneSplitter(
            tab = AppTab.Agents,
            store = null,
            collapsible = true,
            initialCollapsed = false,
            onCollapsedChange = { collapsed ->
                memory.toggleAgentsBeside(shown = !collapsed, twoPanes = true, columnsOver = false, tabShown = true, foreground = true)
            },
        ).apply { width = 1000f; detailOpen = true }

        ActivityScenario.launch(ComponentActivity::class.java).use { scenario ->
            scenario.onActivity { activity -> activity.setContent { state = rememberPaneExpansionState() } }
            rule.runOnIdle {
                splitter.collapse(state)
                assertTrue(splitter.listHidden)
                assertFalse(memory.agentsShown)

                val split = agentsSplitDetailForEntry(
                    tab = AppTab.Agents,
                    stack = listOf(AgentsRoute, chat),
                    entryIndex = 1,
                    twoPanes = true,
                    listHidden = splitter.listHidden,
                    onShowList = { splitter.expand(state) },
                )
                val leading = resolveDetailLeading(split, onBack)
                assertTrue(leading is DetailLeading.ShowList)

                (leading as DetailLeading.ShowList).onClick()
                assertFalse(splitter.listHidden)
                assertFalse(splitter.collapsed)
                assertTrue(memory.agentsShown)
            }
        }
    }

    @Test fun pushedPagesAndCompactAndNonAgentsStacksKeepBack() {
        for (stack in listOf(
            listOf(AgentsRoute, space, chat),
            listOf(AgentsRoute, chat, secondChat),
            listOf(AgentsRoute, chat, terminal),
        )) {
            val split = agentsSplitDetailForEntry(AppTab.Agents, stack, entryIndex = stack.lastIndex, twoPanes = true, listHidden = true, onShowList = onShowList)
            assertEquals(DetailLeading.Back(onBack), resolveDetailLeading(split, onBack))
        }

        assertNull(agentsSplitDetailForEntry(AppTab.Agents, listOf(AgentsRoute, chat), entryIndex = 1, twoPanes = false, listHidden = true, onShowList = onShowList))
        assertNull(agentsSplitDetailForEntry(AppTab.Agents, listOf(AgentsRoute, SettingsRoute, SettingsPageRoute(SettingsPage.About)), entryIndex = 2, twoPanes = true, listHidden = false, onShowList = onShowList))
        assertNull(agentsSplitDetailForEntry(AppTab.Persona, listOf(AssistantRoute, chat), entryIndex = 1, twoPanes = true, listHidden = false, onShowList = onShowList))
        assertNull(agentsSplitDetailForEntry(AppTab.Agents, listOf(AgentsRoute), entryIndex = 1, twoPanes = true, listHidden = false, onShowList = onShowList))
    }

    @Test fun separatingFoldKeepsListVisibleEvenIfCollapsedStateWasSaved() {
        val splitter = PaneSplitter(
            tab = AppTab.Agents,
            store = null,
            collapsible = true,
            initialCollapsed = true,
        ).apply {
            width = 1000f
            detailOpen = true
            hinges = listOf(PaneHinge(left = 499f, right = 501f, separating = true))
        }
        assertTrue(splitter.split.fixed)
        assertFalse(splitter.listHidden)
        val split = agentsSplitDetailForEntry(AppTab.Agents, listOf(AgentsRoute, chat), entryIndex = 1, twoPanes = true, listHidden = splitter.listHidden, onShowList = onShowList)
        assertEquals(DetailLeading.Hidden, resolveDetailLeading(split, onBack))
    }
}

package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.navigation3.runtime.NavKey
import androidx.navigation3.runtime.entryProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.AppTab
import persona.core.navigation.ChatRoute
import persona.core.navigation.MachineDetailRoute
import persona.core.navigation.MachineSpacesRoute
import persona.core.navigation.Navigator
import persona.core.navigation.SpaceRoute

/**
 * A computer's projects as a destination (#4338 A11): it builds the Agents list's model itself, lists the computer's own
 * projects with the pins and mutes of the pairing, and opens what a tap names: a space's page, an agent outside any space,
 * the computer's details.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h1000dp-xhdpi")
class MachineSpacesEntriesTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val sessions = listOf(
        Session("mac", "a", title = "Fix the login bug", cwd = "/code/app", state = "working", spaceKind = "herdr", spaceId = "w1", spaceName = "app",
            slotId = "t1", slotIndex = 1, lastText = "Updating the retry logic", updatedAt = "2026-01-02T11:00:00Z"),
        Session("mac", "b", title = "Draft the release notes", cwd = "/code/loose", state = "working", lastText = "Writing the summary",
            updatedAt = "2026-01-02T11:30:00Z"),
        Session("mac", "c", title = "Sort the receipts", cwd = "/code/scratch", state = "idle", spaceKind = "herdr", spaceId = "w2", spaceName = "scratch",
            updatedAt = "2025-12-30T11:00:00Z"),
        Session("box", "d", title = "Roll out the fix", cwd = "/srv/api", state = "working", spaceKind = "herdr", spaceId = "w3", spaceName = "api",
            updatedAt = "2026-01-02T11:00:00Z"),
    )
    private val machines = listOf(Machine("mac", "MacBook Pro", online = true, icon = "laptop"), Machine("box", "Workstation", online = true, icon = "server"))
    private val navigated = mutableListOf<NavKey>()
    private var backs = 0

    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) { navigated += key }
        override fun back() { backs++ }
        override fun selectTab(tab: AppTab) = Unit
    }

    private fun open(pinnedSpaces: List<String> = emptyList(), mutedSpaces: List<String> = emptyList()) {
        val dependencies = agentsDependencies(sessions, machines, pinnedSpaces = pinnedSpaces, mutedSpaces = mutedSpaces)
        val provider = entryProvider<NavKey> { machineSpacesEntries(navigator, dependencies) }
        rule.setContent { PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = true) { provider(MachineSpacesRoute("mac")).Content() } }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("Working").fetchSemanticsNodes().isNotEmpty() }
    }

    @Test fun listsTheComputersOwnProjectsAndOpensWhatATapNames() {
        open()
        rule.onNodeWithText("MacBook Pro").assertExists()
        rule.onNodeWithText("3 projects", substring = true).assertExists()
        // The other computer's project is not listed; this one's busy and idle projects are.
        assertEquals(0, rule.onAllNodesWithText("api").fetchSemanticsNodes().size)
        rule.onNodeWithText("scratch").assertExists()
        rule.onNodeWithText("app").performClick()
        rule.onNodeWithText("scratch").performClick()
        rule.onNodeWithText("Draft the release notes").performClick()
        rule.onNodeWithContentDescription("Machine details").performClick()
        rule.onNodeWithContentDescription("Back").performClick()
        assertEquals(listOf<NavKey>(SpaceRoute("mac/w1", "app"), SpaceRoute("mac/w2", "scratch"), ChatRoute("mac", "b"), MachineDetailRoute("mac")), navigated)
        assertEquals(1, backs)
    }

    /** The space page's computer, at the top of its cards, opens the computer's projects (it opened the details before). */
    @Test fun theSpacePagesComputerOpensItsProjects() {
        val dependencies = agentsDependencies(sessions, machines)
        val provider = entryProvider<NavKey> { spaceEntries(navigator, dependencies) }
        rule.setContent { PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = true) { provider(SpaceRoute("mac/w1", "app")).Content() } }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("MacBook Pro").fetchSemanticsNodes().isNotEmpty() }
        // TalkBack says what a tap does as iOS labels it: the projects of that computer.
        assertEquals("All projects on MacBook Pro", rule.onNodeWithText("MacBook Pro").fetchSemanticsNode().config[SemanticsActions.OnClick].label)
        rule.onNodeWithText("MacBook Pro").performClick()
        assertEquals(listOf<NavKey>(MachineSpacesRoute("mac")), navigated)
    }

    @Test fun aPinnedProjectStaysInWorkingAndAMutedOneIsMarkedWithThePairingsPreferences() {
        open(pinnedSpaces = listOf("mac/scratch"), mutedSpaces = listOf("mac/app"))
        // Pinned, the quiet project is among the working ones: "Idle" has no section left.
        assertEquals(0, rule.onAllNodesWithText("Idle").fetchSemanticsNodes().size)
        rule.onNodeWithContentDescription("Pinned").assertExists()
        rule.onNodeWithContentDescription("Muted").assertExists()
    }
}

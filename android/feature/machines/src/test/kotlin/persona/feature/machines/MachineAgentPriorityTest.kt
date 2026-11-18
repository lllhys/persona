package persona.feature.machines

import androidx.activity.ComponentActivity
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.hasScrollToNodeAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.time.Instant
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.MachineMetrics
import persona.core.contracts.MetricRange
import persona.core.contracts.MetricsDashboard
import persona.core.contracts.MetricsHistory
import persona.core.contracts.Session
import persona.core.contracts.api.MachineMetricsApi
import persona.core.contracts.api.SessionsRepository
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.AppTab
import persona.core.navigation.MachineDetailRoute
import persona.core.navigation.Navigator

class MachineAgentPriorityTest {
    @Test
    fun summaryNeedsYouIsTheSumOfTheVisibleCardTallies() {
        val machines = listOf(Machine("one", online = true), Machine("two", online = true))
        val sessions = listOf(
            Session("one", "waiting-one", state = "needs_you", status = "blocked"),
            Session("one", "waiting-two", state = "needs_you", status = "blocked"),
            Session("one", "working", state = "working", status = "working"),
            Session("one", "unread", state = "unread", status = "done"),
            Session("two", "waiting-three", state = "needs_you", status = "blocked"),
        )
        val metrics = machines.associate { it.id to MachineMetrics(it.id, online = true) }
        val entries = machineListEntries(machineListIds(machines, metrics), machines, metrics, sessions, now = 1_000L)

        val summary = machineSummary(entries)

        assertEquals(3, summary.needsYou)
        assertEquals(entries.sumOf { it.tally.needsYou }, summary.needsYou)
        assertEquals(4, entries.single { it.id == "one" }.tally.total)
        assertEquals(1, entries.single { it.id == "one" }.tally.idle)
        assertEquals(1, entries.single { it.id == "two" }.tally.needsYou)
    }

    @Test
    fun detailPreviewUsesTheExistingSessionPriorityAndItsFiveSixBoundary() {
        val states = listOf("idle", "working", "needs_you", "unread", "working", "needs_you")
        val sessions = states.mapIndexed { offset, state ->
            val index = offset + 1
            val status = when (state) {
                "needs_you" -> "blocked"
                "unread" -> "done"
                else -> state
            }
            Session("machine", "session-$index", title = "Agent $index", state = state, status = status,
                updatedAt = Instant.ofEpochSecond(1_793_600_000L - index).toString())
        }
        val prioritized = machineSessions(sessions.reversed(), "machine")

        assertEquals(listOf("session-3", "session-6", "session-4", "session-2", "session-5", "session-1"), prioritized.map { it.sessionId })
        assertEquals(prioritized.take(5), visibleMachineAgents(prioritized, showAll = false))
        assertEquals(5, visibleMachineAgents(prioritized, showAll = false).size)
        assertEquals(prioritized, visibleMachineAgents(prioritized, showAll = true))
        assertEquals(6, visibleMachineAgents(prioritized, showAll = true).size)
        assertEquals(emptyList<Session>(), visibleMachineAgents(emptyList(), showAll = false))
    }
}

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class MachineAgentPriorityUiTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val now = 1_793_600_000L

    private fun sessions(machine: String = "machine", count: Int = 6): List<Session> = (1..count).map { index ->
        Session(machine, "session-$index", title = "Agent $index", cwd = "/work/example", state = "working", status = "working",
            updatedAt = Instant.ofEpochSecond(now - index).toString())
    }

    private fun model(machineId: String?, machines: List<Machine>, sessions: List<Session>): MachinesViewModel {
        val metrics = machines.map { MachineMetrics(it.id, online = it.online, system = it.system) }
        val api = object : MachineMetricsApi {
            override suspend fun dashboard() = MetricsDashboard(metrics, step = 10)
            override suspend fun history(machineId: String, range: MetricRange) =
                MetricsHistory(metrics.first { it.machineId == machineId }, range.wire, range.step)
        }
        val repository = object : SessionsRepository {
            override val machines = MutableStateFlow(machines)
            override val sessions = MutableStateFlow(sessions)
            override suspend fun refresh() = Unit
        }
        return MachinesViewModel(api, repository, machineId) { now }.also { model ->
            runBlocking { model.load(MetricRange.Hour) }
        }
    }

    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }

    @Config(sdk = [35], qualifiers = "en-w500dp-h3000dp-xhdpi")
    @Test
    fun agentsComeBeforeMonitoringOnceAndShowAllSurvivesSaveableRestoration() {
        val machine = Machine("machine", online = true)
        val model = model("machine", listOf(machine), sessions())
        val restoration = StateRestorationTester(rule)
        restoration.setContent {
            PersonaTheme(reduceMotion = true) {
                MachinesScreen("machine", model, navigator)
            }
        }

        rule.onAllNodesWithText("Running sessions").assertCountEquals(1)
        rule.onNodeWithTag("machine-now").assertIsDisplayed()
        val agentsTop = rule.onNodeWithText("Running sessions").getUnclippedBoundsInRoot().top
        val monitoringTop = rule.onNodeWithTag("machine-now").getUnclippedBoundsInRoot().top
        assertTrue(agentsTop < monitoringTop)
        rule.onNodeWithText("Agent 6").assertDoesNotExist()
        rule.onNodeWithText("Show all 6").performClick()
        rule.onNodeWithText("Agent 6").assertIsDisplayed()
        rule.onNodeWithText("Show less").assertIsDisplayed()

        restoration.emulateSavedInstanceStateRestore()

        rule.onNodeWithText("Show less").assertIsDisplayed()
        rule.onNodeWithText("Agent 6").assertIsDisplayed()
        rule.onAllNodesWithText("Running sessions").assertCountEquals(1)
        rule.onNodeWithText("Show less").performClick()
        rule.onNodeWithText("Agent 6").assertDoesNotExist()
    }

    @Config(sdk = [35], qualifiers = "en-w500dp-h3000dp-xhdpi")
    @Test
    fun changingMachinesDoesNotShareTheExpandedState() {
        val machines = listOf(Machine("one", online = true), Machine("two", online = true))
        val models = machines.associate { machine ->
            machine.id to model(machine.id, machines, sessions(machine.id))
        }
        var selected by mutableStateOf("one")
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                MachinesScreen(selected, models.getValue(selected), navigator)
            }
        }
        rule.onNodeWithText("Show all 6").performClick()
        rule.onNodeWithText("Show less").assertIsDisplayed()

        rule.runOnIdle { selected = "two" }

        rule.onNodeWithText("Show all 6").assertIsDisplayed()
        rule.onNodeWithText("Agent 6").assertDoesNotExist()
    }

    @Config(sdk = [35], qualifiers = "en-w500dp-h1200dp-xhdpi")
    @Test
    fun emptyAgentsKeepTheirNotice() {
        val machine = Machine("machine", online = true)
        val empty = model("machine", listOf(machine), emptyList())
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                MachinesScreen("machine", empty, navigator)
            }
        }
        rule.onNodeWithText("No sessions running on this machine").assertIsDisplayed()
        rule.onNodeWithText("Show all 0").assertDoesNotExist()
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
    @Test
    fun backFromADetailKeepsTheMachineListReadingPosition() {
        val machines = (1..12).map { Machine("machine-$it", name = "Machine $it", online = true) }
        val list = model(null, machines, emptyList())
        val details = machines.associate { it.id to model(it.id, machines, emptyList()) }
        var selected by mutableStateOf<String?>(null)
        val navigating = object : Navigator {
            override fun navigate(key: NavKey) {
                if (key is MachineDetailRoute) selected = key.machineId
            }
            override fun back() { selected = null }
            override fun selectTab(tab: AppTab) = Unit
        }
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                MachinesScreen(selected, selected?.let { details.getValue(it) } ?: list, navigating)
            }
        }

        rule.onNode(hasScrollToNodeAction()).performScrollToNode(hasText("Machine 12"))
        rule.onNodeWithText("Machine 12").performClick()
        rule.onNodeWithContentDescription("Back").assertIsDisplayed().performClick()

        rule.onNodeWithText("Machine 12").assertIsDisplayed()
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
    @Test
    fun summaryUsesSingularAndPluralAgentsCorrectly() {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Text(machineSummaryText(MachineSummary(2, 3, 3, 1)))
                Text(machineSummaryText(MachineSummary(2, 3, 3, 2)))
            }
        }
        rule.onNodeWithText("2 of 3 online · 3 needing attention · 1 agent needs you").assertExists()
        rule.onNodeWithText("2 of 3 online · 3 needing attention · 2 agents need you").assertExists()
    }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
    @Test
    fun summaryUsesChineseResourcesForOneAgent() {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Text(machineSummaryText(MachineSummary(2, 3, 3, 1)))
            }
        }
        rule.onNodeWithText("2/3 台在线，3 台需要注意，1 个 Agent 需要你").assertExists()
    }

    @Config(sdk = [35], qualifiers = "ja-w400dp-h900dp-xhdpi")
    @Test
    fun summaryUsesJapaneseResourcesForOneAgent() {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Text(machineSummaryText(MachineSummary(2, 3, 3, 1)))
            }
        }
        rule.onNodeWithText("2/3台がオンライン、3台が要注意、1件のエージェントが要対応").assertExists()
    }
}

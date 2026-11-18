package persona.feature.machines

import androidx.activity.ComponentActivity
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.util.Locale

/** A computer that just dropped off reads as offline at once, not Online with its last numbers until the next poll (#3730). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h2000dp-xhdpi")
class MachinesLiveOnlineTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val now = 1_793_600_000L
    private val sample = MetricSample(now - 5, cpu = 93.5, memUsed = 54L shl 30, memTotal = 64L shl 30)
    // The last poll still had it online, with a fresh sample; the live list (WebSocket) has since seen it go.
    private val polled = MachineMetrics("lab", true, latest = sample)
    private val api = object : MachineMetricsApi {
        override suspend fun dashboard() = MetricsDashboard(listOf(polled), step = 10)
        override suspend fun history(machineId: String, range: MetricRange) = MetricsHistory(polled, range.wire, range.step)
    }
    private val repository = object : SessionsRepository {
        override val machines = MutableStateFlow(listOf(Machine("lab", name = "Lab", online = false)))
        override val sessions = MutableStateFlow(emptyList<Session>())
        override suspend fun refresh() = Unit
    }
    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }

    @Test fun theLiveListsOfflineWinsOverTheLastPoll() {
        val list = MachinesViewModel(api, repository, null) { now }
        val page = MachinesViewModel(api, repository, "lab") { now }
        runBlocking { list.load(MetricRange.Hour); page.load(MetricRange.Hour) }
        var detail by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(reduceMotion = true) { if (detail) MachinesScreen("lab", page, navigator) else MachinesScreen(null, list, navigator) }
        }
        val cpu = MetricFormat.percent(93.5, Locale.ENGLISH)
        rule.onNodeWithText("0 of 1 online · 1 needing attention · 0 agents need you").assertExists()
        rule.onNodeWithText("Offline").assertExists()
        rule.onNodeWithText("Online").assertDoesNotExist()
        detail = true
        rule.waitForIdle()
        rule.onNodeWithText("Offline").assertExists()
        assertEquals(0, rule.onAllNodesWithText(cpu).fetchSemanticsNodes().size)
        // Back online in the live list: the page trusts the poll's sample again.
        repository.machines.value = listOf(Machine("lab", name = "Lab", online = true))
        rule.waitForIdle()
        assertEquals(true, rule.onAllNodesWithText(cpu).fetchSemanticsNodes().isNotEmpty())
    }
}

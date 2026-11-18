package persona.android.ui

import androidx.compose.runtime.mutableStateOf
import androidx.navigation3.runtime.NavKey
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Test
import persona.android.navigation.AppNavigator
import persona.core.navigation.*

class AgentsOverlayTest {
    private val chat = ChatRoute("computer", "original", ConversationKind.Agent)

    @Test fun nonDetailListActionsPushWithoutDiscardingTheCurrentChat() {
        for (target in listOf<NavKey>(NewAgentRoute(), TodosRoute, TodoDetailRoute("trip"), ConnectionSettingsRoute)) {
            val fixture = Fixture()
            fixture.pick(target)
            assertFalse(fixture.shown.value)
            val pages = if (target == ConnectionSettingsRoute) listOf(SettingsRoute, SettingsPageRoute(SettingsPage.Diagnostics))
                else listOf(target)
            assertEquals(listOf(AgentsRoute, chat) + pages, fixture.agents)
            repeat(pages.size) { fixture.navigator.back() }
            assertEquals(listOf(AgentsRoute, chat), fixture.agents)
        }
    }

    @Test fun anAgentsDetailPickReplacesOnlyTheAgentsDetail() {
        for (target in listOf<NavKey>(ChatRoute("computer", "next", ConversationKind.Agent),
            ChatRoute("computer", "next"), TerminalRoute("computer", "next"))) {
            val fixture = Fixture()
            fixture.pick(target)
            assertFalse(fixture.shown.value)
            assertEquals(AppTab.Agents, fixture.selected)
            assertEquals(listOf(AgentsRoute, target), fixture.agents)
            fixture.navigator.back()
            assertEquals(listOf<NavKey>(AgentsRoute), fixture.agents)
        }
    }

    @Test fun assistantAndKindlessPersonaPicksKeepThePreviousAgent() {
        for (kind in listOf(ConversationKind.Assistant, null)) {
            val fixture = Fixture()
            val target = ChatRoute("computer", "assistant", kind)
            fixture.pick(target)
            assertFalse(fixture.shown.value)
            assertEquals(AppTab.Persona, fixture.selected)
            assertEquals(listOf(AgentsRoute, chat), fixture.agents)
            assertEquals(listOf(AssistantRoute, target), fixture.persona)
            fixture.navigator.back()
            assertEquals(AppTab.Agents, fixture.selected)
            assertEquals(listOf(AgentsRoute, chat), fixture.agents)
        }
    }

    @Test fun aKindlessCurrentRowPickRetainsTheExactChatKey() {
        val fixture = Fixture()
        fixture.pick(chat.copy(kind = null))
        assertFalse(fixture.shown.value)
        assertEquals(listOf(AgentsRoute, chat), fixture.agents)
        assertSame(chat, fixture.agents.last())
    }

    private inner class Fixture {
        val agents = mutableListOf<NavKey>(AgentsRoute, chat)
        val persona = mutableListOf<NavKey>(AssistantRoute)
        var selected = AppTab.Agents
        val shown = mutableStateOf(true)
        val navigator = AppNavigator(mapOf(AppTab.Agents to agents, AppTab.Persona to persona), { selected }, { selected = it },
            chatTab = { if (it.sessionId == "assistant") AppTab.Persona else AppTab.Agents })
        fun pick(target: NavKey) = pickAgentsList(navigator, agents, shown, target)
    }
}

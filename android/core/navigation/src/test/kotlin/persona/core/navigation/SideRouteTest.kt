package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import org.junit.Assert.assertEquals
import org.junit.Test

/** A link to a side session (#7944) opens its parent's page, which opens the side's own chat over itself. */
class SideRouteTest {
    @Test fun aSidesOwnChatGoesOverThePageTheLinkOpenedAndBackComesToIt() {
        val stacks = mapOf<AppTab, MutableList<NavKey>>(AppTab.Persona to mutableListOf(AssistantRoute), AppTab.Agents to mutableListOf(AgentsRoute))
        val landed = InteractionRoute(ChatRoute("m", "side", ConversationKind.Agent), "q")
        ResolvedRoute(AppTab.Agents, landed).restore(stacks)
        val side = InteractionRoute(ChatRoute("m", "side", ConversationKind.Agent, side = true), "q")
        ResolvedRoute(AppTab.Agents, side).restore(stacks)
        assertEquals(listOf(AgentsRoute, landed, side), stacks.getValue(AppTab.Agents))
        // Opened again, the side's chat is the same page.
        ResolvedRoute(AppTab.Agents, ChatRoute("m", "side", ConversationKind.Agent, side = true)).restore(stacks)
        assertEquals(3, stacks.getValue(AppTab.Agents).size)
    }
}

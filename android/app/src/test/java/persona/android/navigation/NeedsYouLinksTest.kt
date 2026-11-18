package persona.android.navigation

import androidx.navigation3.runtime.NavKey
import persona.core.contracts.SessionKey
import persona.core.navigation.*
import persona.feature.assistant.NeedsYouLine
import org.junit.Assert.assertEquals
import org.junit.Test

/** The Persona home's "Needs you now" opens a request in its conversation and an inbox item in the inbox. */
class NeedsYouLinksTest {
    private val stacks = mapOf<AppTab, MutableList<NavKey>>(
        AppTab.Persona to mutableListOf(AssistantRoute), AppTab.Agents to mutableListOf(AgentsRoute),
    )

    @Test fun aRequestOpensItsConversationOnAgents() {
        val line = NeedsYouLine.Agent(SessionKey("m", "s"), "r1", "plan", null, "迁移", "计划等你批准")
        assertEquals(AppTab.Agents, needsYouRoute(line).open(stacks))
        assertEquals(ChatRoute("m", "s", ConversationKind.Agent), stacks.getValue(AppTab.Agents).last())
        assertEquals(listOf<NavKey>(AssistantRoute), stacks.getValue(AppTab.Persona))
    }

    @Test fun aSideSessionsRequestNamesTheSideSoItsParentOpensWithItShown() {
        val line = NeedsYouLine.Agent(SessionKey("m", "parent"), "r2", "question", null, "Plan", "Which format?",
            side = SessionKey("m", "side"))
        assertEquals(AppTab.Agents, needsYouRoute(line).open(stacks))
        assertEquals(ChatRoute("m", "side", ConversationKind.Agent), stacks.getValue(AppTab.Agents).last())
    }

    @Test fun anInboxItemOpensUnderPersonaWithTheInboxParent() {
        val line = NeedsYouLine.Inbox("item-1", lark = true, itemKind = "dm", who = "发布群", what = "要你确认")
        assertEquals(AppTab.Persona, needsYouRoute(line).open(stacks))
        assertEquals(listOf(AssistantRoute, InboxRoute, InboxItemRoute("item-1")), stacks.getValue(AppTab.Persona))
    }
}

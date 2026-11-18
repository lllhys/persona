package persona.android.navigation

import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.navigation.AppTab
import persona.core.navigation.ChatRoute
import persona.core.navigation.ExtensionUiRoute
import persona.core.navigation.InboxRoute
import persona.core.navigation.MachineDetailRoute
import persona.core.navigation.TodoDetailRoute
import persona.feature.inbox.InboxEventRoute
import persona.feature.inbox.LarkRoute
import persona.feature.inbox.MailRoute

class AssistantStackEvidenceTest {
    @Test
    fun inboxDetailsPublishTheirRealRouteAndStableItemKey() {
        assertEquals(AssistantStackEvidence("inbox", null), assistantStackEvidence(InboxRoute))
        assertEquals(AssistantStackEvidence("inbox/lark", "lark:f1:100"), assistantStackEvidence(LarkRoute("f1", "lark:f1:100")))
        assertEquals(AssistantStackEvidence("inbox/mail", "mail:acc1:t3:503"), assistantStackEvidence(MailRoute("mail:acc1:t3:503")))
        assertEquals(AssistantStackEvidence("inbox/event", "event"), assistantStackEvidence(InboxEventRoute("event")))
    }

    @Test
    fun sidebarDetailsPublishTheirOwnKeys() {
        assertEquals(AssistantStackEvidence("machines/detail", "machine"), assistantStackEvidence(MachineDetailRoute("machine")))
        assertEquals(AssistantStackEvidence("todos/detail", "todo"), assistantStackEvidence(TodoDetailRoute("todo")))
        assertEquals(
            AssistantStackEvidence("extension/extension/page", "extension/page"),
            assistantStackEvidence(ExtensionUiRoute("extension", "page", "Page")),
        )
    }

    @Test
    fun chatRoutesKeepTheOriginalTabContract() {
        assertEquals(
            AssistantStackEvidence("Agents/chat", "machine/session"),
            assistantStackEvidence(ChatRoute("machine", "session"), AppTab.Agents),
        )
        assertEquals(
            AssistantStackEvidence("Persona/chat", "machine/session"),
            assistantStackEvidence(ChatRoute("machine", "session"), AppTab.Persona),
        )
    }
}

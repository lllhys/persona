package persona.feature.inbox

import persona.core.navigation.InboxRoute
import org.junit.Assert.*
import org.junit.Test

class InboxSelectionTest {
    @Test fun larkSelectionFollowsFeedAcrossItemBoundariesAndPositions() {
        listOf(LarkRoute("feed"), LarkRoute("feed", "another-item", 42L)).forEach {
            assertTrue(isInboxItemSelected(it, "item", "feed"))
            assertFalse(isInboxItemSelected(it, "item", "other-feed"))
            assertFalse(isInboxItemSelected(it, "item", null))
        }
    }

    @Test fun mailSelectionUsesItemIdentityWithoutSelectingLarkRows() {
        assertTrue(isInboxItemSelected(MailRoute("item"), "item", null))
        assertTrue(isInboxItemSelected(MailRoute("item", 3), "item", null))
        assertFalse(isInboxItemSelected(MailRoute("other-item"), "item", null))
        assertFalse(isInboxItemSelected(MailRoute("item"), "item", "feed"))
        assertFalse(isInboxItemSelected(null, "item", null))
        assertFalse(isInboxItemSelected(InboxRoute, "item", null))
    }

    @Test fun momentSelectionUsesItemIdentity() {
        assertTrue(isInboxItemSelected(LarkMomentRoute("post-1", "item"), "item", null))
        assertFalse(isInboxItemSelected(LarkMomentRoute("post-1", "other-item"), "item", null))
        assertFalse(isInboxItemSelected(LarkMomentRoute("post-1", null), "item", null))
    }
}

package persona.core.domain.inbox

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Where a landing leaves the bar (#9053, docs/assistant/inbox.md "Section bar"): a list that already shows the item,
 * on All or on the item's own choice, does not move; any other choice moves to the item's, keeping what a search set aside.
 */
class InboxBarLandedTest {
    private val done = InboxBar.Landing(InboxBar.DONE, false)
    private val collapsed = InboxBar.Landing(InboxBar.ALL, true)

    @Test fun aListShowingTheItemStays() {
        assertEquals(InboxBar.State(InboxBar.ALL), InboxBar.landed(InboxBar.State(InboxBar.ALL), done))
        assertEquals(InboxBar.State(InboxBar.DONE), InboxBar.landed(InboxBar.State(InboxBar.DONE), done))
        assertEquals(InboxBar.State(InboxBar.ALL, InboxBar.FYI), InboxBar.landed(InboxBar.State(InboxBar.ALL, InboxBar.FYI), collapsed))
    }

    @Test fun anotherChoiceMovesToTheItems() {
        assertEquals(InboxBar.State(InboxBar.DONE), InboxBar.landed(InboxBar.State(InboxBar.FYI), done))
        assertEquals(InboxBar.State(InboxBar.ALL), InboxBar.landed(InboxBar.State(InboxBar.ACTION), collapsed))
        assertEquals(InboxBar.State(InboxBar.DONE, InboxBar.ACTION), InboxBar.landed(InboxBar.State(InboxBar.FYI, InboxBar.ACTION), done))
    }
}

package persona.android.capsule

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.feature.agents.AgentsPreferencesSnapshot

class ChildSessionsTest {
    @Test fun unreadCountsOnlyTheParentWhileAChildWithoutItsParentStaysReachable() {
        val parent = Session("m", "p", state = "unread", children = SessionChildren(1))
        val child = Session("m", "c", state = "unread", foldedUnder = "p")
        val preferences = AgentsPreferencesSnapshot()
        assertEquals(listOf("p"), capsuleUnreadSessions(listOf(parent, child), preferences, true).map { it.sessionId })
        assertEquals(listOf("c"), capsuleUnreadSessions(listOf(child), preferences, true).map { it.sessionId })
    }

    @Test fun anUnreadSideSessionIsItsParentsUnreadAndNoEntryOfItsOwn() {
        val parent = Session("m", "p", state = "idle")
        val side = Session("m", "s", state = "unread", foldedUnder = "p", foldedAs = FoldedAs.SIDE)
        val preferences = AgentsPreferencesSnapshot()
        assertEquals(listOf("p"), capsuleUnreadSessions(listOf(parent, side), preferences, true).map { it.sessionId })
        assertTrue(capsuleUnreadSessions(listOf(parent, side.copy(state = "idle")), preferences, true).isEmpty())
    }
}

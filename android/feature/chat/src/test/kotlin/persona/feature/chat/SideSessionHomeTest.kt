package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.FoldedAs
import persona.core.contracts.Interaction
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.domain.sort.SideFold

/** Where side sessions (#7944) land and which chat shows their cards. */
class SideSessionHomeTest {
    private val parent = Session("m", "p", title = "Plan the release")
    private val side = Session("m", "s", title = "Check the screenshots", foldedUnder = "p", foldedAs = FoldedAs.SIDE)
    private val subtask = Session("m", "c", foldedUnder = "p", foldedSide = "s")
    private val rows = listOf(parent, side, subtask)
    private val features = listOf("sessions_hierarchy", SideFold.FEATURE)

    @Test fun aLinkToASideOpensItsParentWithTheSideShownAndTheSideRouteOpensTheSide() {
        val landed = childSessionHome(side.key, side, parent)
        assertEquals(ChildSessionHome(parent.key, "s"), landed)
        assertEquals(side.key, landed.shownSide(rows, features))
        assertEquals(ChildSessionHome(side.key), childSessionHome(side.key, side, parent, side = true))
        // A side's own subtask lands in that side's chat.
        assertEquals(side.key, childSessionHome(subtask.key, subtask, parent).shownSide(rows, features))
        // Without the hub's side folding, a link keeps today's landing: the parent's card.
        assertNull(landed.shownSide(rows, listOf("sessions_hierarchy")))
        assertNull(childSessionHome(parent.key, parent).shownSide(rows, features))
        // The side route only means itself for a side: a plain child still opens its parent.
        val child = Session("m", "k", foldedUnder = "p")
        assertEquals(ChildSessionHome(parent.key, "k"), childSessionHome(child.key, child, parent, side = true))
        assertNull(childSessionHome(child.key, child, parent).shownSide(rows + child, features))
    }

    @Test fun aSidesCardShowsInItsParentAndInItsOwnChatAndASubtasksCardOnlyInItsSides() {
        val own = Interaction("own", machineId = "m", sessionId = "p", kind = "question")
        val asks = Interaction("side", machineId = "m", sessionId = "s", kind = "question", foldedUnder = "p", foldedAs = FoldedAs.SIDE)
        val deep = Interaction("deep", machineId = "m", sessionId = "c", kind = "permission", foldedUnder = "p", foldedSide = "s")
        val child = Interaction("child", machineId = "m", sessionId = "k", kind = "plan", foldedUnder = "p")
        val cards = listOf(own, asks, deep, child)
        assertEquals(listOf("own", "side", "child"), chatPageCards(cards, parent.key, emptySet(), sideFold = true, sidePage = false).map { it.id })
        val inSide = chatPageCards(cards, side.key, emptySet(), sideFold = true, sidePage = true)
        assertEquals(listOf("side", "deep"), inSide.map { it.id })
        // In its own chat the side's card is its own, not "From side session"; the subtask's keeps its source.
        assertNull(inSide.first().foldedUnder)
        assertEquals("p", inSide.last().foldedUnder)
        assertEquals(SessionKey("m", "s"), inSide.first().sessionKey)
        // Without the feature: every card on its home, as before.
        assertEquals(cards.map { it.id }, chatPageCards(cards, parent.key, emptySet(), sideFold = false, sidePage = false).map { it.id })
        assertTrue(chatPageCards(cards, side.key, emptySet(), sideFold = false, sidePage = false).isEmpty())
    }
}

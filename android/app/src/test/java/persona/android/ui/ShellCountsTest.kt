package persona.android.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Interaction
import persona.core.contracts.Session
import persona.core.contracts.Todo
import persona.core.contracts.api.AssistantManagerChoice
import persona.core.contracts.api.CurrentAssistant
import persona.core.contracts.api.HubManager
import persona.core.contracts.api.ManagerRoster
import persona.core.domain.inbox.InboxInteraction
import persona.core.domain.inbox.InboxItem
import persona.core.domain.sort.AgentPreferences
import persona.feature.inbox.InboxRecord

/**
 * The shell's counts (Root.kt through [ShellCounts], docs/assistant/managers.md "Clients"): beside the pages only the
 * current assistant's; the switcher's badges each assistant's agents once plus its inbox; the dot on the sidebar's
 * button when another assistant than the current one has something waiting.
 */
class ShellCountsTest {
    private val roster = ManagerRoster(listOf(
        HubManager("assistant", "助手", online = true, isDefault = true, builtin = true),
        HubManager("m-2", "工作", online = true, inboxNeedsYou = 2),
        HubManager("m-3", "家里", online = false),
    ), "assistant")

    private fun on(id: String?) = CurrentAssistant(roster, AssistantManagerChoice.Choice("hub", id, "assistant"))

    private val sessions = listOf(
        Session(machineId = "box", sessionId = "s1", state = "working"), // no stamp: the default's
        Session(machineId = "mac", sessionId = "s2", manager = "m-2", state = "working"),
        Session(machineId = "mac", sessionId = "s3", manager = "m-2", state = "needs_you"),
        Session(machineId = "box", sessionId = "s4", state = "working", quiet = true),
        Session(machineId = "mac", sessionId = "s5", manager = "m-2", state = "needs_you"),
    )
    private val cards = listOf(
        Interaction("c1", "box", "s1"),
        Interaction("c2", "mac", "s2", manager = "m-2"),
        Interaction("c3", "mac", "s2", manager = "m-2"), // the same agent's second card
        Interaction("c4", "box", "s4"), // a quiet task's
        Interaction("c5", "mac", "s5", manager = "m-2"), // a muted session's
    )
    private val mutes = AgentPreferences(sessions = listOf("mac/s5"))

    @Test fun besideThePagesOnlyTheCurrentAssistantsCount() {
        val default = ShellCounts(on(null), sessions, mutes)
        // Its cards: c1 and the quiet c4, of which the tab badge counts c1 alone.
        assertEquals(listOf("c1", "c4"), cards.filter(default::owned).map { it.id })
        assertEquals(listOf("c1"), cards.filter(default::owned).filter(default::countsCard).map { it.id })
        val work = ShellCounts(on("m-2"), sessions, mutes)
        assertEquals(listOf("c2", "c3", "c5"), cards.filter(work::owned).map { it.id })
        assertEquals(listOf("c2", "c3"), cards.filter(work::owned).filter(work::countsCard).map { it.id })
        // To-dos: its own open ones, quiet ones left out.
        val todos = listOf(Todo("t1"), Todo("t2", manager = "m-2"), Todo("t3", manager = "m-2"), Todo("t4", manager = "m-2", quiet = true),
            Todo("t5", manager = "m-2", doneAt = "2026-11-10T08:00:00Z"), Todo("t6", manager = "gone")) // a deleted manager's: the default's
        assertEquals(2, default.todos(todos))
        assertEquals(2, work.todos(todos))
        // The inbox: its items and its agents' cards.
        fun record(id: String, manager: String?) = InboxRecord(InboxItem(id, title = id), manager = manager)
        val records = listOf(record("i1", null), record("i2", "m-2"), record("i3", "assistant"), record("i4", "gone"))
        assertEquals(listOf("i1", "i3", "i4"), ShellCounts.records(on(null), records).map { it.item.id })
        assertEquals(listOf("i2"), ShellCounts.records(on("m-2"), records).map { it.item.id })
        val inboxCards = listOf(InboxInteraction("k1", "box", "s1"), InboxInteraction("k2", "mac", "s2", manager = "m-2"))
        assertEquals(listOf("k2"), ShellCounts.interactions(on("m-2"), inboxCards).map { it.id })
    }

    /** The Agents tab's badge counts sessions, not cards (#3580): the same number iOS and the web show. */
    @Test fun theAgentsBadgeCountsEachWaitingAgentOnce() {
        val work = ShellCounts(on("m-2"), sessions, mutes)
        val mine = cards.filter(work::owned)
        // 工作: s2 once for its two cards, s3 in Needs you with no card; the muted s5 not.
        assertEquals(2, work.agentsWaiting(mine))
        // 助手: s1's card; its quiet s4 not.
        val default = ShellCounts(on(null), sessions, mutes)
        assertEquals(1, default.agentsWaiting(cards.filter(default::owned)))
        // A card on no session counts by itself.
        assertEquals(3, work.agentsWaiting(mine + Interaction("c6", "mac", "")))
    }

    @Test fun theBadgesCountEachAgentOnceAndTheDotIsAnotherAssistantsBadge() {
        val default = ShellCounts(on(null), sessions, mutes)
        // 助手: s1 (its quiet s4 not); 工作: s2 once for two cards, s3 in Needs you, its inbox's 2 (the muted s5 not).
        assertEquals(mapOf("assistant" to 1, "m-2" to 1 + 1 + 2), default.waiting(roster, cards))
        assertTrue(default.othersWaiting(roster, cards))
        // On 工作 the dot is about 助手's agent; once that is answered nothing waits elsewhere, and its own badge is no dot.
        val work = ShellCounts(on("m-2"), sessions, mutes)
        assertTrue(work.othersWaiting(roster, cards))
        assertFalse(work.othersWaiting(roster, cards.filterNot { it.id == "c1" }))
        // Without a roster (an older hub) nothing waits anywhere.
        assertEquals(emptyMap<String, Int>(), default.waiting(null, cards))
        assertFalse(default.othersWaiting(null, cards))
    }
}

package persona.core.contracts

import persona.core.contracts.api.AssistantManagerChoice
import persona.core.contracts.api.CurrentAssistant
import persona.core.contracts.api.waiting
import persona.core.contracts.api.HubManager
import persona.core.contracts.api.ManagerRoster
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ManagerRosterTest {
    @After fun reset() = AssistantManagerChoice.reset()

    private val roster = ManagerRoster(listOf(
        HubManager("assistant", "助手", online = true, builtin = true),
        HubManager("m-2", "工作", online = true, isDefault = true),
        HubManager("m-3", "家里", online = false, lastSeen = "2026-11-05T08:00:00Z"),
        HubManager("m-4", "新的", installing = "2026-11-05T09:10:00Z"),
    ), defaultId = "m-2")

    @Test fun installedIsTheDefaultFirstThenTheRosterAndMultiCountsThem() {
        assertEquals(listOf("m-2", "assistant", "m-3"), roster.installed.map { it.id })
        assertTrue(roster.isMulti)
        // An installing manager is not one yet: one installed is not multi.
        val one = ManagerRoster(listOf(HubManager("assistant", online = true, isDefault = true), HubManager("m-4", installing = "x")), "assistant")
        assertFalse(one.isMulti)
        assertNull(one.groups(listOf("a")) { null })
    }

    @Test fun groupsFollowTheRosterLeaveEmptyOnesOutAndUnstampedAreTheDefaults() {
        val items = listOf("a" to "assistant", "b" to null, "c" to "m-3", "d" to "gone", "e" to "m-2")
        val groups = requireNotNull(roster.groups(items) { it.second })
        assertEquals(listOf("m-2", "assistant", "m-3", "gone"), groups.map { it.id })
        assertEquals(listOf("b", "e"), groups.first().items.map { it.first })
        assertNull(groups.last().manager)
        assertEquals(listOf("m-2", "m-3"), requireNotNull(roster.groups(listOf("x" to "m-3", "y" to null)) { it.second }).map { it.id })
    }

    @Test fun theChoiceNamesItsManagerAndOwnsItsItems() {
        AssistantManagerChoice.choose("hub", null, "assistant")
        assertTrue(AssistantManagerChoice.current.value.owns(null))
        assertFalse(AssistantManagerChoice.current.value.owns("m-2"))
        assertNull(AssistantManagerChoice.header("hub"))
        AssistantManagerChoice.choose("hub", "m-2", "assistant")
        assertTrue(AssistantManagerChoice.current.value.owns("m-2"))
        assertFalse(AssistantManagerChoice.current.value.owns(null))
        assertEquals("m-2", AssistantManagerChoice.header("hub"))
        assertNull(AssistantManagerChoice.header("another"))
        // A default that is not the hub's own assistant: picking it sends no header, and unstamped items are its.
        AssistantManagerChoice.choose("hub", "m-2", "m-2")
        assertNull(AssistantManagerChoice.header("hub"))
        assertTrue(AssistantManagerChoice.current.value.owns(null))
        assertTrue(AssistantManagerChoice.current.value.owns("m-2"))
    }

    @Test fun stampsAreRead() {
        val event = JsonCodec.decodeFromString(ServerEvent.serializer(),
            """{"type":"assistant_feed","conversation":"c1","manager":"m-2","item":{"id":"a1","conversation":"c1","at":"2026-11-05T08:00:00Z","kind":"plan","ref":"x"}}""")
        // another manager's live update (ws.broadcast stamps it): read as ever, the stamp left aside
        assertEquals("a1", (event as AssistantFeedEvent).item.id)
        assertEquals("m-2", JsonCodec.decodeFromString(Machine.serializer(), """{"id":"lab","manager":"m-2"}""").manager)
        assertNull(JsonCodec.decodeFromString(Machine.serializer(), """{"id":"lab"}""").manager)
        assertEquals("m-3", JsonCodec.decodeFromString(Session.serializer(), """{"session_id":"s","manager":"m-3"}""").manager)
    }

    /** A thing is shown under its own installed manager; no stamp, or one the roster lost, under the default. */
    @Test fun everythingIsShownUnderOneInstalledManager() {
        assertEquals("m-2", roster.shownUnder(null))
        assertEquals("m-2", roster.shownUnder(""))
        assertEquals("assistant", roster.shownUnder("assistant"))
        assertEquals("m-3", roster.shownUnder("m-3"))
        assertEquals("m-2", roster.shownUnder("gone"))
        // one still installing has nothing of its own yet
        assertEquals("m-2", roster.shownUnder("m-4"))
    }

    @Test fun theCurrentAssistantOwnsWhatIsStampedWithIt() {
        val default = CurrentAssistant(roster, AssistantManagerChoice.Choice("hub", null, "m-2"))
        assertEquals("m-2", default.id)
        assertTrue(default.owns(null) && default.owns("m-2") && default.owns("gone"))
        assertFalse(default.owns("assistant"))
        val other = CurrentAssistant(roster, AssistantManagerChoice.Choice("hub", "m-3", "m-2"))
        assertEquals("家里", other.manager?.name)
        assertTrue(other.owns("m-3"))
        assertFalse(other.owns(null) || other.owns("gone"))
        // another group's computers are no manager's of this hub: on every assistant's pages
        assertTrue(other.ownsOn("mac~g0123abcd", "assistant"))
        assertFalse(other.ownsOn("mac", "assistant"))
        assertTrue(CurrentAssistant(roster, AssistantManagerChoice.Choice("hub", "m-3", "m-2"), group = "g0123abcd").ownsOn("mac~g0123abcd", "m-3"))
        // before the roster is read (or on a hub without one) the choice's own default decides
        val unread = CurrentAssistant(null, AssistantManagerChoice.Choice("hub", null, "assistant"))
        assertTrue(unread.owns(null) && unread.owns("assistant"))
        assertFalse(unread.owns("m-2"))
    }

    /** The switcher's badge: the cards of a manager's agents, its sessions in Needs you without one, plus its inbox's "Needs you". */
    @Test fun waitingCountsCardsNeedsYouSessionsAndTheInbox() {
        val counted = roster.copy(managers = roster.managers.map {
            when (it.id) { "assistant" -> it.copy(inboxNeedsYou = 12); "m-3" -> it.copy(inboxNeedsYou = 0); else -> it }
        })
        val sessions = listOf(
            Session(machineId = "mac", sessionId = "s1", manager = "assistant", state = "working"),
            Session(machineId = "mac", sessionId = "s2", manager = "assistant", state = "needs_you"),
            Session(machineId = "nas", sessionId = "s3", manager = "m-3", state = "needs_you"),
            Session(machineId = "box", sessionId = "s4", state = "needs_you"),
            Session(machineId = "far~g0123abcd", sessionId = "s5", manager = "assistant", state = "needs_you"),
            // awaiting a reply is not Needs you, nor is unread
            Session(machineId = "box", sessionId = "s6", state = "idle", awaitsReply = true),
            Session(machineId = "box", sessionId = "s7", state = "unread"),
        )
        val cards = listOf(
            Interaction("c1", "mac", "s1"), // its session's manager
            Interaction("c2", "mac", "s2"), // a card on a session in Needs you: once
            Interaction("c3", "nas", "s3", manager = "m-3"),
            Interaction("c3", "nas", "s3", manager = "m-3"), // the same card twice
            Interaction("c4", "box", "gone"), // no stamp anywhere: the default's
            Interaction("c5", "far~g0123abcd", "s5", manager = "assistant"), // another group's
            Interaction("c6", "mac", "s1", manager = "m-3"), // its own stamp wins over its session's
        )
        val waiting = counted.waiting(cards, sessions)
        // assistant: c1, c2 and 12 in its inbox; m-3: c3, c6; the default m-2: c4 and s4 (stuck without a card)
        assertEquals(mapOf("assistant" to 2 + 12, "m-3" to 2, "m-2" to 2), waiting)
        // what the tabs leave out (muted, quiet) is left out here, and a muted card's session is not counted instead
        assertEquals(mapOf("assistant" to 12), counted.waiting(cards, sessions, counts = { false }, sessionCounts = { false }))
        assertEquals(mapOf("assistant" to 12, "m-2" to 1), counted.waiting(cards, sessions, counts = { false }))
        // an older hub (no inbox count) and no cards: nothing waits
        assertTrue(roster.waiting(emptyList(), emptyList()).isEmpty())
        val current = CurrentAssistant(counted, AssistantManagerChoice.Choice("hub", null, "m-2"))
        assertTrue(current.othersWaiting(waiting))
        assertFalse(current.othersWaiting(mapOf("m-2" to 3, "assistant" to 0)))
    }

    /** Each agent once (#3374's rule, the same on every client): two cards are one agent; quiet and muted ones none. */
    @Test fun waitingCountsEachAgentOnce() {
        val sessions = listOf(
            Session(machineId = "mac", sessionId = "two", manager = "m-3", state = "working"),
            Session(machineId = "mac", sessionId = "quiet", manager = "m-3", state = "needs_you", quiet = true),
            Session(machineId = "mac", sessionId = "muted", manager = "m-3", state = "needs_you"),
            Session(machineId = "mac", sessionId = "card-and-needs-you", manager = "m-3", state = "needs_you"),
        )
        val cards = listOf(
            Interaction("c1", "mac", "two", manager = "m-3"),
            Interaction("c2", "mac", "two", manager = "m-3"), // a second card of the same agent
            Interaction("c3", "mac", "quiet"), // a quiet task's card
            Interaction("c4", "mac", "muted"), // a muted session's card
            Interaction("c5", "mac", "card-and-needs-you"), // a card and Needs you: still one agent
            Interaction("c6", "mac", "no-such-session", manager = "m-3"), // on no listed session: once by itself
            Interaction("c7", "mac", "no-such-session-either", manager = "m-3"),
        )
        val muted = setOf(SessionKey("mac", "muted"))
        val waiting = roster.waiting(cards, sessions, sessionCounts = { !it.quiet && it.key !in muted })
        assertEquals(mapOf("m-3" to 1 + 1 + 2), waiting)
        // A quiet session's cards and a muted one's are never counted, whichever filter says so.
        assertEquals(mapOf("m-3" to 2), roster.waiting(cards.filter { it.sessionId in setOf("quiet", "muted", "no-such-session", "no-such-session-either") },
            sessions.filter { it.sessionId in setOf("quiet", "muted") }, sessionCounts = { !it.quiet && it.key !in muted }))
    }

    @Test fun theRosterReadsTheInboxCount() {
        val read = JsonCodec.decodeFromString(ManagerRoster.serializer(),
            """{"default":"assistant","managers":[{"id":"assistant","online":true,"inbox_needs_you":3},{"id":"m-2","online":false}]}""")
        assertEquals(listOf(3, null), read.managers.map { it.inboxNeedsYou })
    }
}

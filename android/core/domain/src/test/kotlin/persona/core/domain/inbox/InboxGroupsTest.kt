package persona.core.domain.inbox

import java.time.Instant
import java.time.LocalDateTime
import java.time.ZoneId
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The inbox's groups (#2059, #2576), as web lib/inbox-groups.test.ts: the time boundaries (calendar days; just now only
 * in Done; the last week by day, then 7+ days and 1+ month), the source order, one group means none, the folds and ticks.
 */
class InboxGroupsTest {
    private val zone = ZoneId.of("Asia/Shanghai")
    private fun local(month: Int, day: Int, hour: Int, minute: Int = 0) = LocalDateTime.of(2026, month, day, hour, minute).atZone(zone).toInstant()
    private val now = local(11, 9, 15) // Mon 9 Nov 2026, 15:00
    private fun ago(days: Long, hours: Long = 0, minutes: Long = 0): Instant =
        now.atZone(zone).minusDays(days).minusHours(hours).minusMinutes(minutes).toInstant()
    private fun key(at: Instant?, recent: Boolean = false) = InboxGroups.timeKey(at, now, zone, recent)

    @Test fun calendarDaysNot24Hours() {
        assertEquals("today", key(local(11, 9, 0, 5)))
        assertEquals("yesterday", key(local(11, 8, 23, 55)))
        assertEquals("d:2026-11-07", key(local(11, 7, 9)))
        assertEquals("d:2026-11-03", key(local(11, 3, 9))) // 6 days: still its own day
        assertEquals("week", key(local(11, 2, 9)))          // 7 days
        assertEquals("week", key(local(10, 11, 9)))         // 29 days
        assertEquals("month", key(local(10, 10, 9)))        // 30 days
    }

    @Test fun justNowOnlyInDoneAndWithinTheHour() {
        assertEquals("today", key(ago(0, 0, 30)))
        assertEquals("just", key(ago(0, 0, 30), recent = true))
        assertEquals("today", key(ago(0, 1, 5), recent = true))
    }

    @Test fun theFutureIsTodayAndNoTimeGoesLast() {
        assertEquals("today", key(now.plusSeconds(3600)))
        assertEquals("month", key(null))
    }

    @Test fun doneNewestFirst() {
        val done = listOf("m" to ago(40), "d5" to ago(5), "j" to ago(0, 0, 10), "w" to ago(9), "y" to ago(1), "t" to ago(0, 3), "d2" to ago(2))
        val groups = InboxGroups.done(done, now, zone) { it.second }
        assertEquals(listOf("just", "today", "yesterday", "d:2026-11-07", "d:2026-11-04", "week", "month"), groups.map { it.key })
        assertEquals(listOf(listOf("j"), listOf("t"), listOf("y"), listOf("d2"), listOf("d5"), listOf("w"), listOf("m")),
            groups.map { g -> g.items.map { it.first } })
    }

    @Test fun bySourceInTheFiltersOrderItemsInTheirOrder() {
        val list = listOf("g1" to InboxGroups.sourceKey("github", "gh", connector = true), "m1" to InboxGroups.sourceKey("mail", "acc1", false),
            "l1" to "lark", "x1" to InboxGroups.sourceKey("moments", "x", false), "a1" to InboxGroups.AGENTS, "l2" to "lark",
            "m2" to InboxGroups.sourceKey("mail", "acc2", false))
        val groups = InboxGroups.bySource(list) { it.second }
        assertEquals(listOf("agents", "lark", "mail:acc1", "mail:acc2", "c:gh", "moments"), groups.map { it.key })
        assertEquals(listOf("l1", "l2"), groups[1].items.map { it.first })
        assertEquals("other", InboxGroups.sourceKey("", "", false))
    }

    @Test fun oneGroupIsNoGroup() {
        assertEquals(emptyList<InboxGroups.Group<String>>(), InboxGroups.shown(InboxGroups.bySource(listOf("a", "b")) { "lark" }))
        assertEquals(2, InboxGroups.shown(InboxGroups.byTime(listOf(ago(0), ago(1)), now, zone) { it }).size)
    }

    @Test fun openAtFirstAndTheFoldsKept() {
        assertEquals(true, InboxGroups.openAtFirst("action", "lark"))
        assertEquals(listOf(true, true, false, false), listOf("just", "today", "yesterday", "week").map { InboxGroups.openAtFirst("done", it) })
        val folded = InboxGroups.toggle("action", "lark", emptyList())
        assertEquals(false, InboxGroups.isOpen("action", "lark", folded))
        assertEquals(true, InboxGroups.isOpen("action", "lark", InboxGroups.toggle("action", "lark", folded)))
        val opened = InboxGroups.toggle("done", "week", emptyList())
        assertEquals(true, InboxGroups.isOpen("done", "week", opened))
        // Expand all, then collapse all: Done's flips are only what differs from how they start.
        val keys = listOf("just", "today", "week")
        val all = InboxGroups.setAll("done", keys, open = true, flipped = listOf("action/lark"))
        assertEquals(setOf("action/lark", "done/week"), all.toSet())
        assertEquals(setOf("action/lark", "done/just", "done/today"), InboxGroups.setAll("done", keys, open = false, flipped = all).toSet())
    }

    @Test fun aGroupsTick() {
        val group = listOf("a", "b", "c")
        assertEquals(InboxGroups.Tick.None, InboxGroups.tick(group, setOf("x")))
        assertEquals(InboxGroups.Tick.Some, InboxGroups.tick(group, setOf("a")))
        assertEquals(InboxGroups.Tick.All, InboxGroups.tick(group, setOf("a", "b", "c", "x")))
        assertEquals(setOf("a", "b", "c", "x"), InboxGroups.pickGroup(group, setOf("a", "x")))
        assertEquals(setOf("x"), InboxGroups.pickGroup(group, setOf("a", "b", "c", "x")))
        assertEquals(InboxGroups.Tick.None, InboxGroups.tick(emptyList<String>(), emptySet()))
    }
}

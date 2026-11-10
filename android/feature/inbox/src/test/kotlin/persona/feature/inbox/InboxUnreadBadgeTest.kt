package persona.feature.inbox

import org.junit.Assert.*
import org.junit.Test
import persona.core.domain.inbox.InboxItem
import persona.core.domain.inbox.buildInboxList

class InboxUnreadBadgeTest {
    private fun record(id: String, read: Boolean = false, done: Boolean = false, tier: String = "action", status: String? = null) =
        InboxRecord(InboxItem(id, kind = "dm", tier = tier, status = status,
            doneAt = if (done) "2026-11-10T00:00:00Z" else null), read = read)

    @Test fun unreadAndPendingAreIndependentAcrossSections() {
        val unread = record("unread")
        val pending = record("pending", read = true)
        val done = record("done", read = true, done = true)
        assertEquals(1, inboxUnreadCount(listOf(unread)))
        assertEquals(0, inboxUnreadCount(listOf(pending)))
        assertEquals(0, inboxUnreadCount(listOf(done)))
        assertEquals(0, inboxUnreadCount(listOf(record("unread-done", done = true))))
        val records = listOf(unread, pending, done, record("fyi", tier = "fyi"), record("noise", tier = "noise"))
        assertEquals(1, inboxUnreadCount(records))
        val list = buildInboxList(records.map { it.item })
        assertEquals(setOf("unread", "pending"), list.groups.first { it.id == "action" }.rows.map { it.id }.toSet())
        assertFalse(pending.item.isDone)
    }

    @Test fun standaloneEntriesUseTheCurrentActionSection() {
        val excluded = listOf(record("fyi", tier = "fyi"), record("noise", tier = "noise"),
            record("working", status = "working"), record("reclassified-fyi", status = "fyi"))
        assertEquals(0, inboxUnreadCount(excluded))
        assertEquals(1, inboxUnreadCount(excluded + record("reclassified-action", tier = "fyi", status = "action")))
    }

    @Test fun fyiConversationsDoNotCountAndReconciledSectionsWin() {
        val records = listOf(record("fyi", tier = "fyi").copy(feedId = "room"),
            record("noise", tier = "noise").copy(feedId = "room"))
        val grouped = buildInboxConversations("paired", InboxSnapshot(records = records))
        assertEquals(InboxConversationSection.Fyi, grouped.rows.single().section)
        assertEquals(0, inboxUnreadCount(records, grouped))
        val actionable = grouped.copy(rows = grouped.rows.map { it.copy(section = InboxConversationSection.Action) })
        assertEquals(1, inboxUnreadCount(records, actionable))
    }

    @Test fun conversationsCountOnceAndReadPendingNeverCounts() {
        val records = listOf(record("a").copy(feedId = "room"), record("b").copy(feedId = "room"), record("document"))
        val grouped = buildInboxConversations("paired", InboxSnapshot(records = records))
        assertEquals(2, inboxUnreadCount(records, grouped))
        assertEquals(1, inboxUnreadCount(records.filter { it.item.id == "document" }, grouped))
        assertEquals(0, inboxUnreadCount(emptyList(), grouped))
        val read = buildInboxConversations("paired", InboxSnapshot(records = records),
            setOf(InboxReadVersion("a", null), InboxReadVersion("b", null)))
        assertEquals(1, inboxUnreadCount(records, read))
        assertEquals(InboxConversationSection.ReadPending, read.rows.single().section)
        assertTrue(read.rows.single().records.none { it.item.isDone })
    }
}

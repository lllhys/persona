package persona.core.data

import androidx.room.Room
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import persona.core.contracts.HubScope
import persona.core.contracts.ImagePayload
import persona.core.contracts.Item
import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class SessionItemsCacheTest {
    private val scope = HubScope("hub", "items-test")
    private lateinit var database: ItemsDatabase

    @Before fun open() {
        database = Room.inMemoryDatabaseBuilder(RuntimeEnvironment.getApplication(), ItemsDatabase::class.java).build()
    }
    @After fun close() = database.close()

    @Test fun crossTurnAgentReportsKeepTheirLaunchAndEveryReportAcrossRestoration() = runBlocking {
        val cache = SessionItemsCache(database, scope)
        val session = SessionKey("computer", "parent")
        val use = Item("launch", 2, role = "assistant", kind = "tool_use", toolUseId = "toolu_launch", toolName = "Agent")
        fun report(seq: Int, status: String) = Item("report$seq", seq, role = "user", kind = "tool_result",
            toolUseId = "toolu_launch", text = "Report $seq", fullSize = 8192,
            task = persona.core.contracts.TaskNote(kind = "agent", status = status, toolUseId = "toolu_launch", taskId = "agent-a"))
        val items = listOf(Item("u1", 1, role = "user", kind = "text", text = "Check tests"), use, report(3, "running"),
            Item("u2", 4, role = "user", kind = "text", text = "Check styles"),
            Item("u3", 5, role = "user", kind = "text", text = "Continue"), report(6, "running"), report(7, "completed"))
        cache.save(session, items)
        val restored = SessionItemsCache(database, scope).load(session)
        assertEquals(items, restored.items)
        val lines = persona.core.domain.lines.chatLines(restored.items.map { it.lineItem() })
        val call = lines.lines.filterIsInstance<persona.core.domain.lines.ChatLine.Work>().flatMap { it.steps }
            .filterIsInstance<persona.core.domain.lines.WorkItem.Tool>().single().call
        assertEquals(listOf("report3", "report6", "report7"), call.results.map { it.id })
        assertEquals(false, call.running)
        assertEquals(8192, call.results.first().item.fullSize)
    }

    @Test fun newlyRetainedReportsKeepTheirEarlierLaunchesRecursively() = runBlocking {
        val cache = SessionItemsCache(database, scope)
        val session = SessionKey("computer", "parent")
        fun user(seq: Int) = Item("u$seq", seq, role = "user", kind = "text", text = "Continue")
        fun use(seq: Int, call: String) = Item("use-$call", seq, kind = "tool_use", toolUseId = call, toolName = "Agent")
        fun report(seq: Int, call: String) = Item("report-$call", seq, kind = "tool_result", toolUseId = call, text = "Report",
            task = persona.core.contracts.TaskNote(kind = "agent", status = "running", toolUseId = call))
        val items = listOf(user(1), use(2, "earlier"), user(3), use(4, "later"), report(5, "earlier"),
            user(6), user(7), report(8, "later"))
        cache.save(session, items)
        assertEquals(items, SessionItemsCache(database, scope).load(session).items)
    }

    @Test fun ledgerAsksForTheFullScanOnlyWhenUnknownOrOverBudget() {
        val a = SessionKey("m", "a"); val b = SessionKey("m", "b"); val c = SessionKey("m", "c")
        // (session, bytes after its save) -> (full scan, total); budget 30, seeded with a = 10, b = 10.
        val steps = listOf(
            Triple(a, 15L, false to 25L),
            Triple(c, 5L, false to 30L), // at the budget: the scan would evict nothing
            Triple(c, 6L, true to 31L),
            Triple(b, 0L, false to 21L), // emptied: its share goes
            Triple(b, 0L, false to 21L),
        )
        val ledger = ItemsLedger()
        assertEquals(true, ledger.needsScan(a, 1, Long.MAX_VALUE))
        ledger.seed(listOf(BudgetEntry(a, 10, 1), BudgetEntry(b, 10, 2)))
        steps.forEachIndexed { i, (session, bytes, expected) ->
            assertEquals("step $i", expected, ledger.needsScan(session, bytes, 30) to ledger.total)
        }
        ledger.clear()
        assertEquals(true to null, ledger.needsScan(a, 1, Long.MAX_VALUE) to ledger.total)
    }

    @Test fun overBudgetSavesEvictTheOldestUnpinnedSessionsAsTheFullScanDid() = runBlocking {
        val chat = listOf(Item("1#0", 1, role = "user", kind = "text", text = "q".repeat(400)),
            Item("2#0", 2, role = "assistant", kind = "text", text = "a".repeat(400)))
        val bytes = chat.sumOf { JsonCodec.encodeToString(it).toByteArray().size }.toLong()
        val cache = SessionItemsCache(database, scope, budget = bytes * 5 / 2)
        val (a, b, c, d) = listOf("a", "b", "c", "d").map { SessionKey("m", it) }
        suspend fun cached() = database.items().sizes(scope.path).map { it.session to it.bytes }

        cache.retain(a)
        listOf(a, b).forEach { cache.save(it, chat) }
        assertEquals(listOf("a" to bytes, "b" to bytes), cached())
        // Over budget: a is the oldest but on screen, so b goes, with its cursor.
        cache.save(c, chat)
        assertEquals(listOf("a" to bytes, "c" to bytes), cached())
        assertEquals(null, database.items().cursor(scope.path, "m", "b"))
        cache.release(a)
        cache.save(d, chat)
        assertEquals(listOf("c" to bytes, "d" to bytes), cached())
        // A session shrinking under budget evicts nothing.
        cache.save(c, chat.take(1))
        assertEquals(listOf("c", "d"), cached().map { it.first }.sorted())
    }

    /**
     * A save writes only what changed and leaves what deleting the session and inserting the window left: the same rows,
     * cursor and sizes. The reference clears the session before each save, so every row is inserted as before.
     */
    @Test fun savesWriteOnlyChangedRowsAndLeaveWhatTheFullRewriteLeft() = runBlocking {
        val reference = Room.inMemoryDatabaseBuilder(RuntimeEnvironment.getApplication(), ItemsDatabase::class.java).build()
        try {
            val diff = SessionItemsCache(database, scope)
            val full = SessionItemsCache(reference, scope)
            val session = SessionKey("m", "a")
            database.openHelper.writableDatabase.apply {
                execSQL("CREATE TEMP TABLE written(id TEXT)")
                execSQL("CREATE TEMP TRIGGER count_written AFTER INSERT ON items BEGIN INSERT INTO written VALUES (new.id); END")
            }
            fun turn(n: Int, answer: String = "answer $n") = listOf(Item("u$n", n * 10, role = "user", kind = "text", text = "q$n"),
                Item("a$n", n * 10 + 1, role = "assistant", kind = "text", text = answer))
            fun image(data: String, seq: Int = 42) = Item("img", seq, role = "assistant", kind = "image",
                image = ImagePayload(data = data, width = 4, height = 4))
            val more = Item("n3", 32, role = "assistant", kind = "text", text = "more")
            val edited = turn(3, "answer 3, edited") + more
            // More than one delete's worth of ids leave together in "empty".
            val steps = (1..1000).map { Item("t$it", 100 + it, role = "assistant", kind = "thinking", text = "step $it") }
            // (step, items, rows the diff writes); the window is the last two turns.
            val sequence = listOf(
                Triple("first", turn(1) + turn(2) + turn(3), 4),
                Triple("append", turn(1) + turn(2) + turn(3) + more, 1),
                Triple("edit", turn(1) + turn(2) + edited, 1),
                Triple("same", turn(1) + turn(2) + edited, 0),
                Triple("slide", turn(2) + edited + turn(4), 2),
                Triple("image", edited + turn(4) + image("AAAA"), 1),
                // The original bytes are not cached: other bytes leave the row as it was.
                Triple("image bytes", edited + turn(4) + image("BBBB"), 0),
                Triple("moved", edited + turn(4) + image("BBBB", seq = 45), 1),
                Triple("long turn", turn(4) + turn(5) + steps, 1002),
                Triple("empty", emptyList(), 0),
                Triple("again", turn(6), 2),
            )
            for ((step, items, writes) in sequence) {
                val snapshot = CachedChat(items, "h1", items.maxOfOrNull { it.seq } ?: 0, step == "slide")
                database.openHelper.writableDatabase.execSQL("DELETE FROM written")
                diff.save(session, snapshot)
                reference.items().clear(scope.path, session.machineId, session.sessionId)
                full.save(session, snapshot)
                suspend fun ItemsDatabase.state() = Triple(
                    items().observe(scope.path, session.machineId, session.sessionId).first(),
                    items().cursor(scope.path, session.machineId, session.sessionId)?.copy(accessed_at = 0),
                    items().sizes(scope.path).map { it.session to it.bytes })
                assertEquals(step, reference.state(), database.state())
                assertEquals(step, full.ledgerTotal, diff.ledgerTotal)
                val written = database.openHelper.writableDatabase.query("SELECT count(*) FROM written")
                    .use { it.moveToFirst(); it.getInt(0) }
                assertEquals("$step rows written", writes, written)
            }
        } finally { reference.close() }
    }
}

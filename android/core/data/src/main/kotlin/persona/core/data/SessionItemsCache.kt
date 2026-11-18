package persona.core.data

import androidx.room.withTransaction
import persona.core.contracts.HubScope
import persona.core.contracts.Item
import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.domain.ContinuityReducer
import persona.core.domain.lines.chatTurns
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.encodeToString

class SessionItemsCache(
    private val database: ItemsDatabase,
    private val scope: HubScope,
    private val budget: Long = CacheBudgets().items,
) : ChatItemsCache {
    init { require(budget >= 0) }
    private val active = mutableMapOf<SessionKey, Int>()
    private val ledger = ItemsLedger()
    /** Held around every transaction that reads or changes [ledger], so a failed one drops it before the next uses it. */
    private val ledgerLock = Mutex()
    internal var onRelease: () -> Unit = {}
    internal fun activeSessions(): Set<SessionKey> = synchronized(active) { active.keys.toSet() }
    internal val ledgerTotal: Long? get() = ledger.total
    /** Full scans run so far (tests). */
    internal var scans = 0
        private set

    override suspend fun retain(session: SessionKey) = database.withTransaction {
        synchronized(active) { active[session] = (active[session] ?: 0) + 1 }
        database.items().touch(scope.path, session.machineId, session.sessionId, System.currentTimeMillis())
    }

    override suspend fun release(session: SessionKey) {
        // A group's data can close while a page still watches one of its chats (the group merged into another or was
        // disconnected, docs/android/android.md "Multiple hubs"): the cache is gone with it, there is nothing left to release.
        if (!database.isOpen) return
        try {
            ledgerTransaction {
                synchronized(active) {
                    val count = (active[session] ?: 1) - 1
                    if (count == 0) active.remove(session) else active[session] = count
                }
                trimIfNeeded()
            }
        } catch (closed: IllegalStateException) {
            if (database.isOpen) throw closed
            return
        }
        onRelease()
    }

    /** The full scan: every cached session's bytes, evicting over budget; leaves [ledger] holding what stays. */
    private suspend fun trim() {
        scans++
        val dao = database.items()
        val entries = dao.sizes(scope.path).map { BudgetEntry(SessionKey(it.machine, it.session), it.bytes, it.accessed) }
        val pinned = activeSessions()
        val evicted = evictionCandidates(entries, budget) { it in pinned }.toSet()
        evicted.forEach {
            dao.clear(scope.path, it.machineId, it.sessionId)
            dao.clearCursor(scope.path, it.machineId, it.sessionId)
        }
        ledger.seed(entries.filter { it.key !in evicted })
    }

    /**
     * The full scan only when it can evict something: the total is not known yet, or it is over the budget. Opening and
     * closing a chat ran it every time, summing every stored row inside a write transaction (#4045); at or under the
     * budget it evicts nothing, whatever is pinned or touched, and [ledger] already knows the total.
     */
    private suspend fun trimIfNeeded() {
        if (ledger.total.let { it == null || it > budget }) trim()
    }

    /** A transaction that uses [ledger]; when it fails, nothing it recorded is trusted and the next save scans again. */
    private suspend fun <T> ledgerTransaction(block: suspend () -> T): T = ledgerLock.withLock {
        try { database.withTransaction { block() } }
        catch (failure: Throwable) { ledger.clear(); throw failure }
    }

    fun observe(session: SessionKey) = database.items().observe(scope.path, session.machineId, session.sessionId).map { rows ->
        rows.map { JsonCodec.decodeFromString<Item>(it.json) }
    }

    override suspend fun load(session: SessionKey): CachedChat = ledgerTransaction {
        database.items().touch(scope.path, session.machineId, session.sessionId, System.currentTimeMillis())
        trimIfNeeded()
        val cursor = database.items().cursor(scope.path, session.machineId, session.sessionId)
        CachedChat(observe(session).first(), cursor?.history, cursor?.covered ?: 0, cursor?.has_more)
    }

    /** Pass the reducer's authoritative merged view, not an isolated incremental page. */
    suspend fun save(session: SessionKey, reducer: ContinuityReducer) =
        save(session, CachedChat(reducer.items, reducer.history, reducer.covered, reducer.hasMore))

    suspend fun save(session: SessionKey, items: List<Item>) = save(session, CachedChat(items))

    override suspend fun save(session: SessionKey, snapshot: CachedChat) {
        val ordered = snapshot.items.distinctBy { it.id }.sortedWith(compareBy<Item> { it.seq }.thenBy { part(it) })
        val turns = chatTurns(ordered.map { it.lineItem() })
        // The last two turns the user started: a turn a background task's wake opened has no message to start from,
        // and goes with the turn before it (iOS `ChatCache.tail` counts the user's questions the same way).
        val first = turns.filter { it.users.isNotEmpty() }.takeLast(2).firstOrNull()?.users?.firstOrNull()?.item
        var start = first?.let { item -> ordered.indexOfFirst { it.id == item.id }.coerceAtLeast(0) } ?: 0
        // A subagent can hand back after several user turns. Its reports cannot survive restart without the
        // launching call: keep that exchange too, including earlier reports, rather than orphaning the result.
        val taskCalls = persona.core.domain.lines.TaskIndex.build(ordered.map { it.lineItem() }).calls
        val launches = ordered.withIndex().filter { it.value.kind == "tool_use" }
            .mapNotNull { (index, item) -> item.toolUseId?.let { it to index } }.toMap()
        var exchange = 0
        val exchangeStarts = ordered.mapIndexed { index, item ->
            if (item.kind == "text" && item.role == "user" && item.queued != true) exchange = index
            exchange
        }
        // Walk backwards through the retained range. Lowering start also visits the newly kept reports, whose
        // launching exchanges may precede this one: the reference closure is preserved in a single pass.
        var inspect = ordered.lastIndex
        while (inspect >= start) {
            val item = ordered[inspect--]
            if (item.task?.kind != "agent") continue
            val call = item.toolUseId ?: item.task?.toolUseId ?: taskCalls[item.id] ?: continue
            val launch = launches[call] ?: continue
            if (launch < start) start = exchangeStarts[launch]
        }
        val rows = ordered.drop(start).map { item ->
            // Inline originals belong to the display-sized image cache, not the items database.
            val cached = publicRefusalItem(item.image?.let { item.copy(image = it.copy(data = "", lazy = true)) } ?: item)
            ItemRow(scope.path, session.machineId, session.sessionId, item.seq, part(item), item.id, JsonCodec.encodeToString(cached))
        }
        ledgerTransaction {
            val dao = database.items()
            // The same rows as deleting the session and inserting the window, written as a difference: a streaming
            // update changes an item or two of a window of hundreds, and rewriting every row went to the WAL each time.
            val stored = dao.stored(scope.path, session.machineId, session.sessionId).associateBy { it.id }
            val kept = rows.mapTo(HashSet()) { it.id }
            stored.keys.filter { it !in kept }.chunked(DELETE_CHUNK).forEach {
                dao.delete(scope.path, session.machineId, session.sessionId, it)
            }
            val changed = rows.filter { row ->
                val old = stored[row.id]
                old == null || old.seq != row.seq || old.part != row.part || old.json != row.json
            }
            if (changed.isNotEmpty()) dao.insert(changed)
            if (rows.isEmpty()) dao.clearCursor(scope.path, session.machineId, session.sessionId)
            else dao.putCursor(SessionCursorRow(scope.path, session.machineId, session.sessionId,
                snapshot.history, snapshot.covered, snapshot.hasMore == true || start > 0,
                System.currentTimeMillis()))
            // Only this session's rows are read; the whole cache only when the total is over budget (where eviction
            // starts) or not known yet. Under budget the full scan evicted nothing either.
            if (ledger.needsScan(session, dao.size(scope.path, session.machineId, session.sessionId), budget)) trim()
        }
    }

    private fun part(item: Item): Int = item.id.substringAfterLast('#').toIntOrNull() ?: 0

    private companion object {
        /** Ids per [ItemsDao.delete]: with its three key variables, under SQLite's 999 before 3.32 (API 30). */
        const val DELETE_CHUNK = 900
    }
}

/**
 * The JSON bytes each cached session of one scope holds (what [ItemsDao.sizes] sums), kept in step with every write of
 * its [SessionItemsCache]: the only writer of that scope's items database. Unknown until a full scan [seed]s it and
 * again after [clear]. Access times are not kept: eviction always reads them from the database.
 */
internal class ItemsLedger {
    private var sessions: MutableMap<SessionKey, Long>? = null

    /** Every session's bytes together; null while unknown. */
    var total: Long? = null
        private set

    fun seed(entries: List<BudgetEntry<SessionKey>>) {
        sessions = entries.filter { it.bytes > 0 }.associateTo(HashMap()) { it.key to it.bytes }
        total = entries.sumOf { it.bytes }
    }

    /**
     * [session] was just written and now holds [bytes]. Whether the full scan has to run: the total is unknown, or
     * it is over [budget], where eviction starts. At or under it the scan would evict nothing.
     */
    fun needsScan(session: SessionKey, bytes: Long, budget: Long): Boolean {
        val known = sessions ?: return true
        val before = if (bytes > 0) known.put(session, bytes) else known.remove(session)
        val now = total!! + bytes - (before ?: 0)
        total = now
        return now > budget
    }

    fun clear() { sessions = null; total = null }
}

/**
 * A refusal notice as it may be kept (#2528, web `publicRefusalItem`): its text is the refusal's words (else its
 * explanation, else its subtype), never the record's whole line, which an older hub sent as the notice's text.
 */
internal fun publicRefusalItem(item: Item): Item {
    val r = item.modelRefusal ?: return item
    return item.copy(text = r.text?.takeIf(String::isNotEmpty) ?: r.explanation?.takeIf(String::isNotEmpty) ?: r.subtype)
}

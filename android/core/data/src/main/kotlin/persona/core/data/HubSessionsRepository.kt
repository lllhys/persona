package persona.core.data

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

internal data class VerifiedSessionLists(
    val machines: List<Machine>? = null,
    val sessions: Map<String, List<Session>> = emptyMap(),
)

/** List events refresh the selected HTTP view; listing never subscribes to chat item streams. */
class HubSessionsRepository(
    private val transport: ChatTransport,
    private val connection: HubConnection,
    scope: CoroutineScope,
    private val snapshots: ListSnapshotStore? = null,
) : SessionsRepository, VerifiedSessionsRepository {
    private val mutableMachines = MutableStateFlow<List<Machine>>(emptyList())
    override val machines = mutableMachines.asStateFlow()
    private val mutableSessions = MutableStateFlow<List<Session>>(emptyList())
    override val sessions = mutableSessions.asStateFlow()
    private val mutableListStatus = MutableStateFlow(SessionListStatus())
    override val listStatus = mutableListStatus.asStateFlow()
    internal val verifiedLists = MutableStateFlow(VerifiedSessionLists())
    override fun verifiedSessions(): Map<String, List<Session>> = verifiedLists.value.sessions
    private val lock = Mutex()
    private val refreshing = Mutex()
    private var revision = 0L
    private var machinesChanged = 0L
    private val sessionChanges = mutableMapOf<String, Long>()
    private val revisionChanges = mutableMapOf<SessionKey, Long>()
    private enum class Mode { Unknown, Paged, Legacy }
    private var mode = Mode.Unknown
    private var listCapability: Boolean? = null
    private var generation = 0L
    private var activeRequest: Job? = null
    // The cursor belongs to the frozen baseline, NEVER to a delta's newer revision.
    private var baselineRevision: String? = null
    private var checkpoint: String? = null
    // Only opaque cursor history grows while traversing the snapshot, never a global row/change cache.
    private val pageStarts = mutableListOf<String?>(null)
    private data class LoadedPage(val index: Int, val keys: Set<SessionKey>)
    private val loadedPages = ArrayDeque<LoadedPage>()
    private val invalidations = Channel<Unit>(Channel.CONFLATED)

    private val restored = scope.async {
        val machines = snapshots.restore<List<Machine>>("machines")
        var sessions: List<Session>? = null
        quietly { sessions = snapshots?.sessionWindow(WINDOW_SIZE) }
        lock.withLock {
            machines?.let { mutableMachines.value = it }
            sessions?.let { mutableSessions.value = it }
        }
    }

    private suspend fun persist() {
        snapshots.save("machines", mutableMachines.value)
        // A cache is a first-paint hint, not a row checkpoint or a complete verified baseline.
        snapshots.save("sessions", if (mode == Mode.Legacy) mutableSessions.value else mutableSessions.value.take(WINDOW_SIZE))
    }

    /**
     * Pushed events write the first-paint snapshot at once and then at most every [PERSIST_MILLIS], outside the lock:
     * serializing every listed session (some titles megabytes long) on each event kept the phone busy while agents ran,
     * once the capsule kept the process alive. Refreshes still commit before they return.
     */
    private val persistRequests = kotlinx.coroutines.channels.Channel<Unit>(kotlinx.coroutines.channels.Channel.CONFLATED)
    private fun persistLater() { persistRequests.trySend(Unit) }

    init {
        scope.launch {
            for (request in persistRequests) {
                runCatching { lock.withLock { persist() } }
                delay(PERSIST_MILLIS)
            }
        }
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            connection.events.collect { event ->
                if (event is SessionsListEvent) {
                    invalidations.trySend(Unit)
                    return@collect
                }
                restored.await()
                lock.withLock {
                    when (event) {
                        is Hello -> {
                            val enabled = "sessions_list" in event.features
                            val changed = (listCapability != null && listCapability != enabled) ||
                                (mode == Mode.Legacy && enabled) || (mode == Mode.Paged && !enabled)
                            listCapability = enabled
                            if (changed) {
                                generation++
                                activeRequest?.cancel()
                                mode = Mode.Unknown
                                resetPages()
                                mutableSessions.value = mutableSessions.value.take(WINDOW_SIZE)
                                verifiedLists.value = VerifiedSessionLists()
                                mutableListStatus.update { SessionListStatus(filter = it.filter) }
                                invalidations.trySend(Unit)
                            }
                            return@withLock
                        }
                        is MachinesEvent -> {
                            machinesChanged = ++revision
                            mutableMachines.value = event.machines
                            if (connection.state.value == ConnectionState.Online) verifiedLists.update { prior ->
                                prior.copy(machines = event.machines, sessions = prior.sessions.filterKeys { machine ->
                                    mode == Mode.Legacy && event.machines.firstOrNull { it.id == machine }?.online == true &&
                                        prior.machines?.firstOrNull { it.id == machine }?.online != false
                                })
                            }
                        }
                        is SessionsEvent -> {
                            // A negotiated paged client must not adopt an accidental full legacy push.
                            if (mode == Mode.Paged) return@withLock
                            sessionChanges[event.machineId] = ++revision
                            revisionChanges.keys.removeAll { it.machineId == event.machineId }
                            val remaining = mutableSessions.value.filterNot { it.machineId == event.machineId }
                            val incoming = if (mode == Mode.Legacy) event.sessions else event.sessions.take(WINDOW_SIZE)
                            mutableSessions.value = (remaining + incoming.map { it.copy(machineId = event.machineId) })
                                .let { if (mode == Mode.Legacy) it else it.take(WINDOW_SIZE) }
                            if (mode == Mode.Legacy && connection.state.value == ConnectionState.Online) verifiedLists.update {
                                it.copy(sessions = if (it.machines?.firstOrNull { machine -> machine.id == event.machineId }?.online == false)
                                    it.sessions - event.machineId
                                else it.sessions + (event.machineId to incoming.map { session -> session.copy(machineId = event.machineId) }))
                            }
                            if (mode == Mode.Legacy) mutableListStatus.update { it.copy(total = mutableSessions.value.size) }
                        }
                        is RevsEvent -> {
                            revision++
                            if (mode != Mode.Paged) event.revs.forEach { (sessionId, rev) ->
                                revisionChanges[SessionKey(event.machineId, sessionId)] = rev
                            }
                            mutableSessions.update { list -> list.map { session ->
                                event.revs[session.sessionId]?.takeIf { session.machineId == event.machineId }
                                    ?.let { session.copy(rev = it) } ?: session
                            } }
                        }
                        else -> return@withLock
                    }
                    persistLater()
                }
            }
        }
        scope.launch {
            for (ignored in invalidations) {
                delay(INVALIDATION_DELAY_MS)
                while (invalidations.tryReceive().isSuccess) { /* Coalesce a burst, retaining hints during HTTP. */ }
                try { quietly { recoverDelta() } }
                catch (cancelled: CancellationException) { currentCoroutineContext().ensureActive() }
            }
        }
        scope.launch {
            connection.state.collectLatest {
                if (it == ConnectionState.Online) {
                    val heldMode = lock.withLock { mode }
                    if (heldMode == Mode.Paged) {
                        if (connection.resumed) lock.withLock { verifyHeld() }
                        quietly { recoverDelta() }
                    } else if (connection.resumed && heldMode == Mode.Legacy) lock.withLock { verifyHeld() }
                    else quietly { refresh() }
                } else lock.withLock { verifiedLists.value = VerifiedSessionLists() }
            }
        }
    }

    /** Serialize HTTP operations, but let a changed filter cancel the operation and reject late results. */
    private suspend fun request(block: suspend (Long, SessionListFilter) -> Unit) = coroutineScope {
        refreshing.withLock {
            restored.await()
            val job = currentCoroutineContext().job
            val (epoch, filter) = lock.withLock {
                activeRequest = job
                mutableListStatus.update { it.copy(loading = true, failed = false) }
                generation to mutableListStatus.value.filter
            }
            var failed = false
            try { block(epoch, filter) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { failed = true; throw error }
            finally { withContext(NonCancellable) { lock.withLock {
                if (generation == epoch) mutableListStatus.update { it.copy(loading = false, failed = failed) }
                if (activeRequest === job) activeRequest = null
            } } }
        }
    }

    override suspend fun listCounts(filter: SessionListFilter): SessionListCounts {
        if (lock.withLock { mode != Mode.Paged }) throw SessionListsUnsupported()
        return transport.sessionCounts(filter)
    }

    override suspend fun refresh() = request { epoch, filter -> firstPage(epoch, filter) }

    private suspend fun firstPage(epoch: Long, filter: SessionListFilter) {
        if (filter.requiresCompleteMembership) {
            fallbackLegacy(epoch)
            return
        }
        if (lock.withLock { mode == Mode.Legacy }) {
            legacyRefresh(epoch)
            return
        }
        val page = try { transport.sessionPage(filter) }
        catch (_: SessionListsUnsupported) {
            fallbackLegacy(epoch)
            return
        }
        currentCoroutineContext().ensureActive()
        lock.withLock {
            if (epoch != generation) return
            mode = Mode.Paged
            baselineRevision = page.revision
            pageStarts.clear()
            pageStarts.add(null)
            page.nextCursor?.let { pageStarts.add(it) }
            loadedPages.clear()
            loadedPages.add(LoadedPage(0, page.sessions.take(WINDOW_SIZE).mapTo(mutableSetOf()) { it.key }))
            checkpoint = page.revision
            mutableSessions.value = page.sessions.take(WINDOW_SIZE)
            verifiedLists.value = VerifiedSessionLists(machines = verifiedLists.value.machines)
            mutableListStatus.update { it.copy(paged = true, loaded = true, hasMore = page.hasMore, hasPrevious = false, total = page.total) }
            persist()
        }
        try { refreshMetadata(epoch, filter, includeMachines = true) }
        catch (_: SessionListsUnsupported) { fallbackLegacy(epoch) }
    }

    private suspend fun refreshMetadata(epoch: Long, filter: SessionListFilter, includeMachines: Boolean = false) {
        val before = lock.withLock { revision }
        coroutineScope {
            val machines = if (includeMachines) async { transport.machines() } else null
            val counts = async { transport.sessionCounts(filter.copy(state = null)) }
            val machineCounts = if (filter.machineId != null) async {
                transport.sessionCounts(filter.copy(state = null, machineId = null))
            } else counts
            val stateCounts = counts.await()
            val crossMachineCounts = machineCounts.await()
            val machineList = machines?.await()
            lock.withLock {
                if (epoch != generation) return@withLock
                if (machineList != null && machinesChanged <= before) {
                    mutableMachines.value = machineList
                    if (connection.state.value == ConnectionState.Online) verifiedLists.update { it.copy(machines = machineList) }
                }
                mutableListStatus.update { it.copy(counts = stateCounts, machineCounts = crossMachineCounts) }
                persist()
            }
        }
    }

    override suspend fun setListFilter(filter: SessionListFilter) {
        val changed = lock.withLock {
            if (mutableListStatus.value.filter == filter) return
            // A view policy is not a server capability failure. Reprobe pagination when leaving that view.
            if (mutableListStatus.value.filter.requiresCompleteMembership != filter.requiresCompleteMembership) mode = Mode.Unknown
            generation++
            activeRequest?.cancel()
            resetPages()
            if (mode != Mode.Legacy) mutableSessions.value = emptyList()
            verifiedLists.value = VerifiedSessionLists()
            mutableListStatus.value = SessionListStatus(paged = mode == Mode.Paged, filter = filter)
            true
        }
        if (changed) refresh()
    }

    override suspend fun loadMore() = loadPage(previous = false)
    override suspend fun loadPrevious() = loadPage(previous = true)

    private suspend fun loadPage(previous: Boolean) = request { epoch, filter ->
        val target = lock.withLock {
            if (mode != Mode.Paged || loadedPages.isEmpty()) return@request
            val index = if (previous) loadedPages.first().index - 1 else loadedPages.last().index + 1
            if (index !in pageStarts.indices) return@request
            index to pageStarts[index]
        }
        // The protocol supplies no cursor for page zero. Going back to it starts a fresh frozen view.
        if (previous && target.first == 0) {
            firstPage(epoch, filter)
            return@request
        }
        val baseline = lock.withLock { baselineRevision } ?: return@request
        try {
            val page = transport.sessionPage(filter, cursor = target.second)
            currentCoroutineContext().ensureActive()
            // Keep <= one page of candidates private while replaying changes from the OLD baseline.
            // No global change map: unloaded identities are ignored on each delta page.
            val candidates = page.sessions.take(WINDOW_SIZE).associateByTo(linkedMapOf()) { it.key }
            val reconciled = streamDelta(epoch, filter, baseline, candidates)
            lock.withLock {
                if (epoch != generation) return@withLock
                val loadedPage = LoadedPage(target.first, candidates.keys.toSet())
                if (previous) loadedPages.addFirst(loadedPage) else loadedPages.addLast(loadedPage)
                if (loadedPages.size > MAX_LOADED_PAGES) {
                    if (previous) loadedPages.removeLast() else loadedPages.removeFirst()
                }
                val allowed = loadedPages.flatMapTo(mutableSetOf()) { it.keys }
                val retained = mutableSessions.value.filter { it.key in allowed }
                val existing = retained.mapTo(mutableSetOf()) { it.key }
                val added = candidates.values.filterNot { it.key in existing }
                mutableSessions.value = if (previous) added + retained else retained + added
                if (page.nextCursor != null && target.first + 1 == pageStarts.size) pageStarts.add(page.nextCursor)
                checkpoint = reconciled.revision
                mutableListStatus.update { it.copy(
                    hasMore = loadedPages.last().index + 1 in pageStarts.indices,
                    hasPrevious = loadedPages.first().index > 0,
                    total = reconciled.total,
                ) }
                persist()
            }
            refreshMetadata(epoch, filter)
        } catch (_: SessionListExpired) { firstPage(epoch, filter) }
        catch (_: SessionListsUnsupported) { fallbackLegacy(epoch) }
    }

    private suspend fun recoverDelta() = request { epoch, filter ->
        val since = lock.withLock { if (mode == Mode.Paged) checkpoint else null }
        if (since == null) {
            // A paged WebSocket emits invalidations even when this view uses the complete HTTP fallback.
            if (filter.requiresCompleteMembership || lock.withLock { mode != Mode.Legacy }) firstPage(epoch, filter)
            return@request
        }
        try {
            val last = streamDelta(epoch, filter, since)
            lock.withLock {
                if (epoch != generation) return@withLock
                checkpoint = last.revision
                mutableListStatus.update { it.copy(total = last.total) }
                persist()
            }
            refreshMetadata(epoch, filter)
        } catch (_: SessionListExpired) { firstPage(epoch, filter) }
        catch (_: SessionListsUnsupported) { fallbackLegacy(epoch) }
    }

    /** Apply each page directly to loaded rows/candidates; only return the final bounded page. */
    private suspend fun streamDelta(
        epoch: Long,
        filter: SessionListFilter,
        since: String,
        candidates: MutableMap<SessionKey, Session>? = null,
    ): SessionListPage {
        var cursor: String? = null
        while (true) {
            val page = transport.sessionPage(filter, cursor = cursor, since = since)
            currentCoroutineContext().ensureActive()
            lock.withLock {
                check(epoch == generation) { "Session filter changed" }
                val changes = page.sessions.associateBy { it.key }
                val removals = page.remove.mapNotNull { encoded ->
                    runCatching { JsonCodec.decodeFromString<List<String>>(encoded) }.getOrNull()
                        ?.takeIf { it.size == 2 }?.let { SessionKey(it[0], it[1]) }
                }.toSet()
                val loaded = mutableSessions.value.mapTo(mutableSetOf()) { it.key }
                val kept = mutableSessions.value.mapNotNull { row ->
                    if (row.key in removals) null else changes[row.key] ?: row
                }
                mutableSessions.value = arrivals(changes.values.filter { it.key !in loaded }, kept) + kept
                candidates?.let { window ->
                    removals.forEach { window.remove(it) }
                    changes.forEach { (key, row) -> if (window.containsKey(key)) window[key] = row }
                }
            }
            if (!page.hasMore) return page
            val next = requireNotNull(page.nextCursor) { "Missing session delta cursor" }
            require(next != cursor) { "Repeated session delta cursor" }
            cursor = next
        }
    }

    /**
     * Called under [lock]. An unloaded delta row joins only while page zero is loaded and it sorts above the window's
     * oldest row (or the whole list is loaded); rows that belong to later frozen pages wait for those pages.
     */
    private fun arrivals(rows: List<Session>, kept: List<Session>): List<Session> {
        val top = loadedPages.firstOrNull()?.takeIf { it.index == 0 } ?: return emptyList()
        val complete = loadedPages.last().index + 1 !in pageStarts.indices
        val oldest = kept.mapNotNull { it.updated }.minOrNull()
        val joined = rows.filter { row -> complete || (oldest != null && row.updated?.let { it > oldest } == true) }
        if (joined.isEmpty()) return joined
        // A burst past one extra page rebaselines page one rather than growing the window without bound.
        if (top.keys.size + joined.size > 2 * WINDOW_SIZE) throw SessionListExpired()
        loadedPages[0] = top.copy(keys = top.keys + joined.map { it.key })
        // Each time parsed once (`updated` parses its text on every read).
        return joined.map { it to it.updated }.sortedWith(compareByDescending<Pair<Session, java.time.Instant?>> { it.second }
            .thenBy { it.first.machineId }.thenBy { it.first.sessionId }).map { it.first }
    }

    /** Called under [lock]; never combines page tokens/checkpoints from different hub epochs. */
    private fun resetPages() {
        pageStarts.clear()
        pageStarts.add(null)
        loadedPages.clear()
        baselineRevision = null
        checkpoint = null
    }

    private suspend fun fallbackLegacy(epoch: Long) {
        lock.withLock {
            if (epoch != generation) return
            mode = Mode.Legacy
            resetPages()
        }
        legacyRefresh(epoch)
    }

    /** Compatibility path: retain legacy refresh/WS merge semantics, only after explicit Unsupported. */
    private suspend fun legacyRefresh(epoch: Long) {
        val before = lock.withLock { revision }
        val (machines, sessions) = coroutineScope {
            val machines = async { transport.machines() }
            val sessions = async { transport.sessions() }
            machines.await() to sessions.await()
        }
        lock.withLock {
            if (epoch != generation) return
            if (machinesChanged <= before) mutableMachines.value = machines
            val changed = sessionChanges.filterValues { it > before }.keys
            mutableSessions.value = (sessions.filterNot { it.machineId in changed } +
                mutableSessions.value.filter { it.machineId in changed }).map { session ->
                revisionChanges[session.key]?.takeIf { it > (session.rev ?: Long.MIN_VALUE) }?.let { session.copy(rev = it) } ?: session
            }
            revisionChanges.clear()
            mutableListStatus.update { it.copy(paged = false, loaded = true, hasMore = false, hasPrevious = false, total = mutableSessions.value.size) }
            verifyHeld()
            persist()
        }
    }

    /** A partial filtered window is NEVER evidence that a machine's other sessions were removed. */
    private fun verifyHeld() {
        if (connection.state.value != ConnectionState.Online) return
        if (mode == Mode.Paged) {
            verifiedLists.value = VerifiedSessionLists(machines = mutableMachines.value)
            return
        }
        if (mode == Mode.Legacy) verifiedLists.value = VerifiedSessionLists(
            mutableMachines.value,
            (mutableMachines.value.map { it.id } + mutableSessions.value.map { it.machineId })
                .distinct().associateWith { machine -> mutableSessions.value.filter { it.machineId == machine } },
        )
    }

    override suspend fun adopt(session: Session) {
        if (session.machineId.isBlank() || session.sessionId.isBlank()) return
        restored.await()
        lock.withLock {
            if (mutableSessions.value.any { it.key == session.key }) return@withLock
            // A single start response isn't authoritative filtered membership. Reconcile the bounded list instead.
            if (mode == Mode.Paged) { invalidations.trySend(Unit); return@withLock }
            sessionChanges[session.machineId] = ++revision
            mutableSessions.value = (mutableSessions.value + session).let { if (mode == Mode.Legacy) it else it.take(WINDOW_SIZE) }
            persist()
        }
    }

    override suspend fun applyMachine(machine: Machine) {
        if (machine.id.isBlank()) return
        restored.await()
        lock.withLock {
            machinesChanged = ++revision
            val list = mutableMachines.value
            mutableMachines.value = if (list.any { it.id == machine.id }) list.map { if (it.id == machine.id) machine else it } else list + machine
            persist()
        }
    }

    private companion object {
        const val WINDOW_SIZE = 50
        const val MAX_LOADED_PAGES = 10
        const val INVALIDATION_DELAY_MS = 40L
    }
}

internal suspend inline fun quietly(block: () -> Unit) {
    try { block() }
    catch (cancelled: CancellationException) { throw cancelled }
    catch (_: Exception) { /* A later refresh or reconnect reconciles state. */ }
}

/** How often pushed session events write the first-paint snapshot at most. */
private const val PERSIST_MILLIS = 10_000L

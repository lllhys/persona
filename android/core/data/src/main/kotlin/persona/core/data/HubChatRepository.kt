package persona.core.data

import persona.core.contracts.CoreText.Key
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.domain.ContinuityReducer
import persona.core.domain.ContinuitySync
import persona.core.domain.ContinuitySync.Action
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.sync.withPermit
import kotlinx.serialization.json.*
import java.time.Instant
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Owns continuity for one pairing. Retains at most 64 session entries in access-order LRU;
 * observed entries are protected, idle entries lose their jobs on eviction. A 65th simultaneous
 * observer is rejected. These are unmeasured policy limits, not performance measurements.
 */
class HubChatRepository(
    private val transport: ChatTransport,
    private val connection: HubConnection,
    private val cache: ChatItemsCache,
    private val sessions: SessionsRepository,
    private val scope: CoroutineScope,
    private val foreground: StateFlow<Boolean> = MutableStateFlow(true),
    private val now: () -> Instant = Instant::now,
    private val seen: (suspend (SessionKey) -> Unit)? = null,
    /** Told the session's transcript whenever it gains items (the outbox finds the messages sent from here in it). */
    private val transcript: (suspend (SessionKey, List<Item>) -> Unit)? = null,
) : ChatRepository {
    private class Entry(val key: SessionKey, val scope: CoroutineScope) {
        val reducer = ContinuityReducer()
        val realtime = ChatRealtimeState(key)
        val sync = ContinuitySync(reducer)
        val lock = Mutex()
        val requests = Mutex()
        val saving = Mutex()
        /** An items event's save is waiting for [saving]; later events ride on it (it reads the state once it starts). */
        val persistPending = AtomicBoolean(false)
        val ready = CompletableDeferred<Unit>()
        val state = MutableStateFlow(ChatSnapshot(verified = false))
        var subscribed = false
        var seenHistory: String? = null
        var references = 0
        var opening: Job? = null
        var timer: Job? = null
        var lastSeen: Item? = null
        var previousSession: Session? = null
        var currentSessions: List<Session>? = null
        var machineOnline: Boolean? = null
        var closeConfirmed = false
        var listedGone = false
        var stopRequested = false
        var turnDurationSeconds: Long? = null
        var durationAfterSeq: Int = 0
        var lastUserId: String? = null
        var eventError: String? = null
        var observedTranscript: List<Item>? = null
        /** Back online or reopened and no live delta since: the empty delta that ends a turn may have been missed. */
        var reconnected = false
    }

    private val entries = LinkedHashMap<SessionKey, Entry>(64, 0.75f, true)
    private val images = ImageBytesCache()
    private val backgroundSlots = Semaphore(3)

    init {
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            connection.events.collect { event ->
                when (event) {
                    is ItemsEvent -> existing(SessionKey(event.machineId, event.sessionId))?.let { entry ->
                        entry.ready.await()
                        val actions = entry.lock.withLock {
                            val gap = entry.reducer.apply(event.items, event.history, event.fromSeq, event.toSeq,
                                event.backlog, event.hasMore, event.moreAfter)
                            entry.eventError = null
                            publish(entry, verified = gap == ContinuityReducer.Gap.None &&
                                (event.backlog || entry.state.value.verified))
                            entry.sync.settle(gap)
                        }
                        dispatch(entry, actions)
                        if (entry.persistPending.compareAndSet(false, true)) entry.scope.launch { persist(entry) }
                    }
                    is DeltaEvent -> update(SessionKey(event.machineId, event.sessionId)) { entry ->
                        entry.realtime.delta(event, entry.reducer.items)
                        entry.reconnected = false
                        if (!event.turnFinished) {
                            entry.turnDurationSeconds = null
                            entry.durationAfterSeq = entry.reducer.items.lastOrNull()?.seq ?: 0
                        }
                        else {
                            entry.stopRequested = false
                            requestSync(entry.key)
                        }
                    }
                    is ScreenEvent -> update(SessionKey(event.machineId, event.sessionId)) { entry ->
                        entry.realtime.screen(event, entry.reducer.items)
                        if (event.status in listOf("idle", "done", "offline", "blocked")) entry.stopRequested = false
                    }
                    // Every open session records the call, but only the one showing its row is rebuilt (#5341).
                    is ToolCallEvent -> retainedEntries().forEach { entry -> entry.lock.withLock {
                        if (entry.realtime.tool(event, entry.reducer.items)) publish(entry)
                    } }
                    is HookEvent -> update(SessionKey(event.machineId, event.sessionId)) { entry ->
                        if (event.event == "Stop") {
                            entry.stopRequested = false
                            entry.realtime.finish()
                        }
                        requestSync(entry.key)
                    }
                    is ErrorEvent -> event.sessionKey?.let { key -> update(key) { it.eventError = event.error.ifBlank { DataText.of(Key.UnknownError) } } }
                    is SessionsEvent -> retainedEntries().filter { it.key.machineId == event.machineId }.forEach { entry ->
                        entry.lock.withLock {
                            if (sessions !is HubSessionsRepository && connection.state.value == ConnectionState.Online)
                                entry.currentSessions = event.sessions.map { it.copy(machineId = event.machineId) }
                            publish(entry)
                        }
                    }
                    is MachinesEvent -> retainedEntries().forEach { entry -> entry.lock.withLock {
                        if (sessions !is HubSessionsRepository && connection.state.value == ConnectionState.Online)
                            entry.machineOnline = event.machines.firstOrNull { it.id == entry.key.machineId }?.online
                        publish(entry)
                    } }
                    is RevsEvent -> event.revs.keys.forEach { sid -> requestSync(SessionKey(event.machineId, sid)) }
                    else -> Unit
                }
            }
        }
        scope.launch {
            connection.state.collect { state ->
                val active = synchronized(entries) { entries.values.filter { it.references > 0 } }
                if (state == ConnectionState.Online) {
                    active.forEach { entry -> entry.scope.launch {
                        entry.ready.await()
                        dispatch(entry, entry.lock.withLock { entry.reconnected = true; entry.sync.reconnect() })
                        catchUp(entry)
                    } }
                } else retainedEntries().forEach { entry ->
                    synchronized(entries) { entry.subscribed = false }
                    entry.lock.withLock {
                        entry.currentSessions = null
                        entry.machineOnline = null
                        publish(entry, verified = false)
                    }
                }
            }
        }
        scope.launch {
            var previous = emptyMap<SessionKey, Long?>()
            val verified = (sessions as? HubSessionsRepository)?.verifiedLists ?: MutableStateFlow(VerifiedSessionLists())
            combine(sessions.sessions, verified) { list, _ -> list }.collect { list ->
                list.forEach { session ->
                    if (session.rev != previous[session.key]) requestSync(session.key)
                }
                retainedEntries().forEach { entry -> entry.lock.withLock {
                    publish(entry)
                } }
                previous = list.associate { it.key to it.rev }
            }
        }
        scope.launch {
            foreground.collect { if (it && connection.state.value == ConnectionState.Online) {
                backgroundCandidates().forEach(::requestSync)
            } }
        }
    }

    /** Reconcile only observed sessions; HTTP catch-up drains every forward page. */
    suspend fun reconcileOpened() = coroutineScope {
        val active = synchronized(entries) { entries.values.filter { it.references > 0 } }
        active.map { entry -> async {
            entry.ready.await()
            entry.opening?.join()
            entry.timer?.cancelAndJoin()
            synchronized(entries) { entry.subscribed = false }
            entry.lock.withLock {
                entry.sync.finishFill()
                publish(entry, verified = false)
                dispatch(entry, entry.sync.reconnect())
            }
            catchUp(entry)
            entry.state.first { it.verified }
        } }.awaitAll()
        Unit
    }

    /** Wrap the paired hub's action service so confirmed operations update the same snapshot. */
    fun trackActions(delegate: ChatActions): ChatActions = object : ChatActions {
        override suspend fun isMuted(session: Session) = delegate.isMuted(session)
        override suspend fun execute(session: Session, action: ChatAction): ChatActionResult {
            val result = delegate.execute(session, action)
            recordAction(session, action, result)
            return result
        }
        override suspend fun verify(session: Session, action: ChatAction): ChatActionResult {
            val result = delegate.verify(session, action)
            recordAction(session, action, result, verified = true)
            return result
        }
        override suspend fun editQueue(session: Session, keep: List<Int>) = delegate.editQueue(session, keep)
        override suspend fun info(session: Session) = delegate.info(session)
        override suspend fun fork(session: Session, atItemId: String): String {
            // A finished streamed reply keeps showing its draft id; the node only knows the transcript one (#5305).
            val entry = existing(session.key)
            return delegate.fork(session, entry?.lock?.withLock { entry.realtime.canonicalId(atItemId) } ?: atItemId)
        }
        override suspend fun rewind(session: Session, target: RewindTarget, restore: RewindRestore) = delegate.rewind(session, target, restore)
    }

    /** The composer shares interruption state with menu actions. Failed requests leave it unchanged. */
    fun trackComposer(delegate: ChatComposerService): ChatComposerService = object : ChatComposerService by delegate {
        override suspend fun interrupt(session: SessionKey) {
            delegate.interrupt(session)
            update(session) { it.stopRequested = true }
        }
    }

    /** Only recorded durations may be supplied; never derive them from message timestamps. */
    suspend fun recordTurnDuration(session: SessionKey, seconds: Long?) {
        require(seconds == null || seconds >= 0)
        update(session) { it.turnDurationSeconds = seconds }
    }

    private suspend fun recordAction(session: Session, action: ChatAction, result: ChatActionResult, verified: Boolean = false) {
        update(session.key) { entry ->
            if (entry.previousSession == null) entry.previousSession = session
            if (result == ChatActionResult.Done) when (action) {
                ChatAction.Interrupt -> entry.stopRequested = !verified
                ChatAction.Close -> entry.closeConfirmed = true
                else -> Unit
            }
        }
    }

    override fun observe(session: SessionKey): Flow<ChatSnapshot> = flow {
        cache.retain(session)
        try {
            val entry = synchronized(entries) {
                val entry = entry(session)
                entry.references++
                if (entry.references == 1) {
                    entry.opening = entry.scope.launch {
                        entry.ready.await()
                        // Cold opens always verify against a folded latest page, including when cache exists.
                        entry.lock.withLock { entry.reconnected = true }
                        latest(entry, initial = true)
                        dispatch(entry, entry.lock.withLock { entry.sync.reconnect() })
                    }
                }
                entry
            }
            try { emitAll(entry.state) }
            finally {
                synchronized(entries) {
                    entry.references--
                    if (entry.references == 0) {
                        entry.opening?.cancel()
                        entry.timer?.cancel()
                        entry.subscribed = false
                        connection.send(envelope("unsubscribe", session))
                    }
                }
            }
        } finally { withContext(NonCancellable) { cache.release(session) } }
    }

    override suspend fun loadOlder(session: SessionKey) {
        val entry = existing(session) ?: return
        entry.ready.await()
        entry.requests.withLock {
            val action = entry.lock.withLock {
                entry.sync.loadOlder().firstOrNull()?.also {
                    entry.state.update { it.copy(loadingOlder = true, error = null) }
                }
            } as? Action.FetchOlder ?: return
            try {
                val history = entry.lock.withLock { entry.reducer.history }
                val page = transport.items(session, ItemsQuery(beforeSeq = action.beforeSeq))
                entry.lock.withLock {
                    if (history != entry.reducer.history || page.history != null && history != null && page.history != history) {
                        entry.sync.finishOlder()
                    } else entry.sync.older(page.items, page.hasMore)
                    publish(entry)
                }
                persist(entry)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { entry.lock.withLock { entry.state.update { it.copy(error = DataText.of(Key.ChatHistoryFailed)) } } }
            finally { withContext(NonCancellable) { entry.lock.withLock {
                entry.sync.finishOlder()
                entry.state.update { it.copy(loadingOlder = false) }
            } } }
        }
    }

    override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int): List<Item> {
        require(fromSeq >= 0 && toSeq >= fromSeq)
        val entry = existing(session) ?: return emptyList()
        entry.ready.await()
        return entry.requests.withLock {
            val fold = entry.lock.withLock { entry.reducer.items.firstOrNull {
                it.kind == "fold" && it.seq == fromSeq && it.fold?.throughSeq == toSeq
            } } ?: return@withLock emptyList()
            val history = entry.lock.withLock { entry.reducer.history }
            var actions = entry.lock.withLock { entry.sync.openFold(fold.id) }
            val work = mutableListOf<Item>()
            try {
                while (actions.isNotEmpty()) {
                    val action = actions.single() as Action.FetchRange
                    val page = transport.items(session, ItemsQuery(fromSeq = action.fromSeq, throughSeq = action.throughSeq))
                    check(page.items.none { it.kind == "fold" } && (!page.more ||
                        page.items.lastOrNull()?.seq?.let { it >= action.fromSeq && it < action.throughSeq } == true))
                    actions = entry.lock.withLock {
                        check(history == entry.reducer.history)
                        check(page.history == null || history == null || page.history == history)
                        work += page.items
                        entry.sync.range(page.items, page.more)
                    }
                }
                entry.lock.withLock { publish(entry) }
                persist(entry)
                work.distinctBy { it.id }
            } finally { withContext(NonCancellable) { entry.lock.withLock { entry.sync.cancelFold() } } }
        }
    }

    override suspend fun loadImage(session: SessionKey, itemId: String) =
        images.get("${session.path}:$itemId") { transport.image(session, itemId) }
    override suspend fun fullText(session: SessionKey, itemId: String): String {
        val entry = existing(session)
        val canonical = entry?.lock?.withLock { entry.realtime.canonicalId(itemId) } ?: itemId
        return transport.fullItem(session, canonical).text.orEmpty()
    }

    override suspend fun markSeen(session: SessionKey, itemId: String) {
        val entry = existing(session) ?: return
        entry.lock.withLock {
            if (!entry.state.value.verified || !foreground.value || connection.state.value != ConnectionState.Online) return
            val canonical = entry.realtime.canonicalId(itemId)
            val item = entry.reducer.items.firstOrNull { it.id == canonical } ?: return
            val last = entry.lastSeen
            if (last != null && compareValuesBy(item, last, { it.seq }, { part(it) }) <= 0) return
            val prior = entry.state.value
            entry.lastSeen = item
            entry.state.update { it.copy(lastSeenMessageId = entry.realtime.displayId(item.id)) }
            try {
                if (seen != null) seen.invoke(session)
                else check(connection.send(envelope("seen", session))) { "Read receipt was not sent" }
            } catch (failure: Exception) {
                entry.lastSeen = last
                entry.state.value = prior
                throw failure
            }
        }
    }

    private fun entry(key: SessionKey): Entry = synchronized(entries) {
        entries.getOrPut(key) {
            if (entries.size >= 64) {
                val victim = entries.values.firstOrNull { it.references == 0 }
                    ?: error("Too many active chat sessions")
                entries.remove(victim.key)
                victim.ready.complete(Unit)
                victim.scope.cancel()
            }
            Entry(key, CoroutineScope(scope.coroutineContext + SupervisorJob(scope.coroutineContext[Job]))).also { entry -> entry.scope.launch {
                try {
                    val cached = cache.load(key)
                    entry.lock.withLock {
                        entry.reducer.restore(cached.items, cached.history, cached.covered, cached.hasMore)
                        publish(entry, verified = false)
                    }
                } catch (cancelled: CancellationException) { throw cancelled }
                catch (_: Exception) { /* The latest page can recover an unavailable disk cache. */ }
                finally { entry.ready.complete(Unit) }
            } }
        }
    }

    private fun existing(key: SessionKey): Entry? = synchronized(entries) { entries[key] }

    private fun retainedEntries(): List<Entry> = synchronized(entries) { entries.values.toList() }

    private suspend fun update(key: SessionKey, change: (Entry) -> Unit) {
        val entry = existing(key) ?: return
        entry.ready.await()
        entry.lock.withLock { change(entry); publish(entry) }
    }

    private fun backgroundCandidates(): List<SessionKey> = sessions.sessions.value
        .filter { it.isLive || it.updated?.isAfter(now().minusSeconds(24 * 60 * 60)) == true }
        .map { it to it.updated } // each time parsed once, not on every comparison
        .sortedWith(compareByDescending<Pair<Session, java.time.Instant?>> { it.first.isLive }.thenByDescending { it.second }.thenBy { it.first.key.path })
        .take(40).map { it.first.key }

    private fun requestSync(key: SessionKey) {
        val active = existing(key)?.let { synchronized(entries) { it.references > 0 } } == true
        if (!active && (!foreground.value || key !in backgroundCandidates())) return
        if (connection.state.value != ConnectionState.Online) return
        val entry = synchronized(entries) {
            if (key !in entries && entries.size >= 64 && entries.values.none { it.references == 0 }) return
            entry(key)
        }
        entry.scope.launch {
            entry.ready.await()
            val actions = entry.lock.withLock { entry.sync.settle(ContinuityReducer.Gap.Fill) }
            dispatch(entry, actions)
        }
    }

    private fun dispatch(entry: Entry, actions: List<Action>) {
        actions.forEach { action -> when (action) {
            is Action.Subscribe -> synchronized(entries) {
                if (entry.references > 0 && !entry.subscribed) entry.subscribed = connection.send(buildJsonObject {
                    envelope("subscribe", entry.key).forEach { (key, value) -> put(key, value) }
                    put("lazy_images", true); put("fold_work", action.foldWork)
                    action.sinceSeq?.let { put("since_seq", it) }
                })
            }
            is Action.ScheduleFill -> synchronized(entries) {
                if (entry.timer?.isActive != true) entry.timer = entry.scope.launch {
                    delay(action.delayMillis)
                    backgroundSlots.withPermit { catchUp(entry) }
                }
            }
            is Action.FetchLatest -> entry.scope.launch { latest(entry) }
            else -> error("Unexpected standalone continuity action: $action")
        } }
    }

    private suspend fun latest(entry: Entry, initial: Boolean = false) = entry.requests.withLock {
        try {
            val history = entry.lock.withLock { entry.reducer.history }
            val page = transport.items(entry.key, ItemsQuery())
            entry.lock.withLock {
                if (history != entry.reducer.history && page.history != entry.reducer.history) {
                    dispatch(entry, listOf(Action.FetchLatest()))
                    return@withLock
                }
                if (initial) entry.reducer.latest(page.items, page.history, page.toSeq, page.hasMore)
                else entry.sync.reload(page.items, page.history, page.toSeq, page.hasMore)
                val overtaken = entry.reducer.items.any { it.seq > entry.reducer.covered }
                entry.eventError = null
                publish(entry, verified = !overtaken)
                if (overtaken) dispatch(entry, entry.sync.settle(ContinuityReducer.Gap.Fill))
            }
            persist(entry)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { entry.lock.withLock {
            entry.state.update { it.copy(loading = false, error = DataText.of(Key.ChatSyncFailed), verified = false) }
        } }
        finally { withContext(NonCancellable) { entry.lock.withLock { entry.sync.finishFill() } } }
    }

    private suspend fun catchUp(entry: Entry) = entry.requests.withLock {
        var actions = entry.lock.withLock { entry.sync.fillTimer() }
        try {
            while (actions.isNotEmpty()) {
                actions = when (val action = actions.single()) {
                    is Action.FetchAfter -> {
                        val history = entry.lock.withLock { entry.reducer.history }
                        val page = transport.items(entry.key, ItemsQuery(afterSeq = action.afterSeq))
                        entry.lock.withLock {
                            if (history != entry.reducer.history && page.history != entry.reducer.history) {
                                return@withLock listOf(Action.FetchLatest())
                            }
                            val result = entry.sync.after(page.items, page.history, page.fromSeq, page.toSeq, page.moreAfter)
                            val overtaken = entry.reducer.items.any { it.seq > entry.reducer.covered }
                            entry.eventError = null
                            publish(entry, verified = result.gap == ContinuityReducer.Gap.None && !overtaken)
                            if (result.actions.isEmpty() && overtaken) entry.sync.fillTimer() else result.actions
                        }
                    }
                    is Action.FetchLatest -> {
                        val history = entry.lock.withLock { entry.reducer.history }
                        val page = transport.items(entry.key, ItemsQuery())
                        entry.lock.withLock {
                            if (history != entry.reducer.history && page.history != entry.reducer.history) {
                                return@withLock listOf(Action.FetchLatest())
                            }
                            entry.sync.reload(page.items, page.history, page.toSeq, page.hasMore)
                            val overtaken = entry.reducer.items.any { it.seq > entry.reducer.covered }
                            entry.eventError = null
                            publish(entry, verified = !overtaken)
                            if (overtaken) entry.sync.fillTimer() else emptyList()
                        }
                    }
                    else -> error("Unexpected catch-up action: $action")
                }
            }
            persist(entry)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { entry.lock.withLock {
            entry.state.update { it.copy(error = DataText.of(Key.ChatSyncFailed), verified = false) }
        } }
        finally { withContext(NonCancellable) { entry.lock.withLock { entry.sync.finishFill() } } }
    }

    private suspend fun persist(entry: Entry) = entry.saving.withLock {
        // From here an event's change may be missing from the snapshot below, so the next event starts another save.
        entry.persistPending.set(false)
        quietly {
            val snapshot = entry.lock.withLock { CachedChat(entry.reducer.items, entry.reducer.history,
                entry.reducer.covered, entry.reducer.hasMore) }
            cache.save(entry.key, snapshot)
        }
    }

    private fun publish(entry: Entry, verified: Boolean = entry.state.value.verified) {
        (sessions as? HubSessionsRepository)?.verifiedLists?.value?.let { authority ->
            entry.currentSessions = authority.sessions[entry.key.machineId]
            entry.machineOnline = authority.machines?.firstOrNull { it.id == entry.key.machineId }?.online
        }
        if (entry.seenHistory != entry.reducer.history) {
            if (entry.seenHistory != null) {
                entry.realtime.reset()
                entry.turnDurationSeconds = null
                entry.durationAfterSeq = 0
            }
            entry.lastSeen = null
            entry.seenHistory = entry.reducer.history
        }
        val metadata = entry.currentSessions?.firstOrNull { it.key == entry.key }
            ?: sessions.sessions.value.firstOrNull { it.key == entry.key }
        val previous = metadata ?: entry.previousSession
        val userId = entry.reducer.items.lastOrNull { it.startsRecordedTurn }?.id
        if (entry.lastUserId != userId) {
            if (entry.lastUserId != null) entry.turnDurationSeconds = null
            entry.lastUserId = userId
        }
        val durations = recordedTurnDurations(entry.reducer.items)
        durations[userId]?.takeIf { it.seq > entry.durationAfterSeq }?.let { entry.turnDurationSeconds = it.seconds }
        if (metadata != null) entry.previousSession = metadata
        val machineOnline = entry.machineOnline.takeIf { connection.state.value == ConnectionState.Online }
        // Ended and replaced stay latched while lists are unknown; a verified list that drops the session and later
        // lists it again (resume reuses the id, #4986) undoes them, a confirmed close included.
        var relisted = false
        entry.currentSessions?.takeIf { machineOnline == true }?.let { current ->
            val listed = current.firstOrNull { it.key == entry.key }
            if (listed == null || listed.status == "offline") entry.listedGone = true
            else if (entry.listedGone) {
                entry.listedGone = false
                entry.closeConfirmed = false
                relisted = true
            }
        }
        val runtime = previous?.let {
            SessionRuntimeState.map(it, entry.currentSessions, machineOnline,
                closeConfirmed = entry.closeConfirmed, stopRequested = entry.stopRequested,
                recordedTurnDurationSeconds = entry.turnDurationSeconds)
        } ?: SessionRuntimeState(turnDurationSeconds = entry.turnDurationSeconds)
        if (runtime.ended || metadata?.status in listOf("idle", "done", "offline", "blocked")) entry.stopRequested = false
        val seenAt = parseTime(metadata?.seenAt)
        val lastSeen = entry.lastSeen?.id ?: entry.reducer.items.lastOrNull {
            seenAt != null && parseTime(it.ts)?.let { time -> time <= seenAt } == true
        }?.id
        // Only the hub's fresh list counts (cleared while offline); a session missing from it is left alone. While it
        // still works the hub resends its live text, so the draft stays and is overwritten in place.
        if (entry.reconnected && entry.currentSessions?.firstOrNull { it.key == entry.key }?.status
                ?.let { it != "working" && it != "blocked" } == true) {
            entry.realtime.finish()
            entry.reconnected = false
        }
        val items = entry.realtime.items(entry.reducer.items)
        entry.state.update { it.copy(items = items, loading = false,
            history = entry.reducer.history,
            streamingItemIds = entry.realtime.streamingItemIds(),
            hasOlder = entry.reducer.hasMore == true, error = entry.eventError, verified = verified,
            lastSeenMessageId = lastSeen?.let(entry.realtime::displayId),
            ended = runtime.ended || (it.ended && !relisted), replaced = runtime.replaced || (it.replaced && !relisted),
            stopping = runtime.stopping, turnDurationSeconds = runtime.turnDurationSeconds,
            recordedTurnDurations = durations.mapValues { it.value.seconds }) }
        // However the items came (an event, a page, a catch-up), a message sent from here that is in them is shown.
        val observed = entry.reducer.items
        if (transcript != null && observed.isNotEmpty() && observed != entry.observedTranscript) {
            entry.observedTranscript = observed
            entry.scope.launch {
                try { transcript.invoke(entry.key, observed) }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (_: Exception) { entry.observedTranscript = null }
            }
        }
    }

    private fun envelope(type: String, session: SessionKey) = buildJsonObject {
        put("type", type); put("machine_id", session.machineId); put("session_id", session.sessionId)
    }
    private fun part(item: Item) = item.id.substringAfterLast('#').toIntOrNull() ?: 0
}

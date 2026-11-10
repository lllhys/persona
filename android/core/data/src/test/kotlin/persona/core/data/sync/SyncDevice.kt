package persona.core.data.sync

import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.serialization.json.*
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.data.*
import persona.core.domain.CardAnswer
import persona.core.domain.ChatTaps
import persona.core.domain.chatBubbles
import persona.core.domain.lines.LineItem
import persona.core.domain.lines.LineOptions
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.chatLines
import persona.core.domain.lines.isLiveStatus
import java.time.Instant
import persona.core.domain.lines.Fold as LineFold
import persona.core.domain.lines.TaskNote as LineTask

// One device of a chat sync scenario: the app's real chat state (HubChatRepository, HubInteractionCenter,
// OutboxMessageSender, HubSessionsRepository) wired as ChatDataRepositories and GroupRuntime wire it, over a socket
// and HTTP the case drives, plus the chat page's own rules (ChatTaps, chatBubbles) where ChatScreen applies them.

/** What the device drew, in the README's terms ("Views"). */
internal data class SyncView(
    val messages: List<Message>, val cards: Map<String, String>, val status: String?, val live: String?,
) {
    data class Message(val from: String, val text: String, val state: String?) {
        override fun toString() = "$from[${state ?: "-"}] $text"
    }
}

/** The device's disk: what a relaunched app starts from. */
internal class SyncDisk {
    val outbox = MemoryOutboxStore()
    var items = CachedChat()
}

internal class SyncDevice(val name: String, private val run: SyncRun, private val disk: SyncDisk) {
    val key = SessionKey(run.hub.machine, run.hub.session)
    private val job = SupervisorJob(run.background.coroutineContext[Job])
    /** The app's lifetime; a coroutine of it that fails is the case's failure, not the test run's. */
    val scope = CoroutineScope(run.background.coroutineContext + job +
        CoroutineExceptionHandler { _, error -> run.fail("$name: the app failed: $error") })
    private val socket = FakeConnection().apply { state.value = ConnectionState.Connecting; open = false }
    /** The socket as the app's repositories see it: what it sends lands in the device's requests. */
    private val connection = object : HubConnection by socket {
        override fun send(message: JsonObject): Boolean = socket.send(message).also { sent ->
            if (sent && message.str("type") == "subscribe" && message.str("session_id") == key.sessionId)
                run.note(buildJsonObject { put("ws", "subscribe"); message["since_seq"]?.let { put("since_seq", it) } })
        }
    }
    private val http = FakeChatTransport().apply {
        machineList = listOf(Machine(run.hub.machine, name = "Laptop", online = true))
        fetch = { query -> run.read("/items", itemsRequest(query)) { itemsPage(query) }.wire<ItemsResponse>() }
        pendingFetch = { run.read("/interactions", get("/interactions")) { JsonArray(run.hub.pending.map(run::tie)) }.wire<List<Interaction>>() }
        listFetch = { run.read("/sessions", get("/sessions")) { JsonArray(listOf(run.hub.row())) }.wire<List<Session>>() }
    }
    /** The hub's HTTP API: reads from [http]; a card's answer waits for the case's `reply` ([resolveCard]). */
    private val transport = object : ChatTransport by http {
        override suspend fun sessionPage(filter: SessionListFilter, cursor: String?, since: String?) =
            run.read("/sessions", get("/sessions")) {
                buildJsonObject {
                    put("sessions", JsonArray(listOf(run.hub.row()))); put("remove", JsonArray(emptyList()))
                    put("has_more", false); put("revision", run.hub.listRevision()); put("total", 1)
                }
            }.wire<SessionListPage>()
        override suspend fun sessionCounts(filter: SessionListFilter) = run.read("/sessions", get("/sessions")) {
            buildJsonObject { put("total", 1); put("revision", run.hub.listRevision()) }
        }.wire<SessionListCounts>()
        override suspend fun resolve(id: String, resolution: Resolution) { resolveOutcome(id, resolution) }
        override suspend fun resolveOutcome(id: String, resolution: Resolution) = resolveCard(id)
    }
    val messages = Messages()
    private val cache = FakeItemsCache(disk.items)
    private val clock = { Instant.ofEpochMilli(run.nowMillis()) }
    val sessions = HubSessionsRepository(transport, connection, scope)
    val interactions = HubInteractionCenter(transport, connection, scope, sessions = sessions.sessions, clock = clock)
    val sender = OutboxMessageSender(HubScope("sync-hub", "generation-1"), disk.outbox, messages, scope) { run.nowMillis() }
    val chats = HubChatRepository(transport, connection, cache, sessions, scope,
        transcript = sender::observeTranscript)
    /** What the chat page holds: the repository's snapshot, and the taps on its cards (ChatScreen's state). */
    var snapshot = ChatSnapshot()
        private set
    var taps = ChatTaps()
        private set

    init {
        sender.connectActivity { key -> sessions.sessions.value.firstOrNull { it.key == key }?.status in setOf("working", "blocked") }
        // GroupRuntime: every items push goes to the outbox too.
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            socket.events.collect { if (it is ItemsEvent) sender.observeTranscript(SessionKey(it.machineId, it.sessionId), it.items) }
        }
        // ChatScreen: marks for cards the hub no longer lists are dropped (LaunchedEffect(allRequests)).
        scope.launch { interactions.pending.collect { list -> taps = taps.pending(list.map { it.id }.toSet()) } }
    }

    /** The chat page opens (it shows the chat until the case ends or the app is killed). */
    fun open() { scope.launch { chats.observe(key).collect { snapshot = it } } }

    val online get() = socket.open

    /**
     * A new socket: its hello, the hub's connection snapshot (machines, the session list; to a device that pages its
     * list, as this app does, the list's hint), then [frames] (replayed broadcasts, the pending cards). The app then
     * catches up (GroupRuntime's onReady: the outbox looks its messages up) before it sends again; a socket that did not
     * resume shows Online only once that is done.
     */
    suspend fun connect(resumed: Boolean, frames: List<JsonObject>) {
        socket.open = true
        socket.resumed = resumed
        socket.state.value = ConnectionState.Updating
        emit(buildJsonObject {
            put("type", "hello"); put("server_version", "test"); put("client_id", "client-$name"); put("hub_id", "hub1")
            put("hub_name", "Hub"); put("event_stream", run.hub.stream); put("event_seq", run.hub.eventSeq); put("resumed", resumed)
            put("features", JsonArray(FEATURES.map(::JsonPrimitive)))
        })
        emit(buildJsonObject {
            put("type", "machines")
            put("machines", buildJsonArray { add(buildJsonObject { put("id", run.hub.machine); put("name", "Laptop"); put("online", true) }) })
        })
        emit(buildJsonObject { put("type", "sessions_list"); put("revision", run.hub.listRevision()) })
        for (frame in frames) if (!deliver(frame)) run.fail("$name: a frame this app does not read: $frame")
        if (resumed) socket.state.value = ConnectionState.Online
        scope.launch {
            try { sender.onReady() }
            finally {
                if (socket.open) {
                    messages.readyToSend.value = true
                    socket.state.value = ConnectionState.Online
                }
            }
        }
        run.settle()
    }

    fun drop() {
        socket.open = false
        messages.readyToSend.value = false
        socket.state.value = ConnectionState.Connecting
    }

    /** A frame of the case's, as this app's socket hands it on (OkHttpHubConnection); false when it is not one. */
    suspend fun deliver(frame: JsonObject): Boolean = when (frame.str("type")) {
        // The connection keeps it to itself (it reconnects sooner).
        "hub_restarting" -> true
        // This app pages its session list (`features=sessions_list`): the hub sends it the list's hint, not the rows.
        "sessions_delta" -> { emit(buildJsonObject { put("type", "sessions_list"); put("revision", run.hub.listRevision()) }); true }
        else -> emit(run.tie(frame))
    }

    private suspend fun emit(frame: JsonObject): Boolean {
        val event = ServerEvent.decode(frame) ?: return false
        socket.events.emit(event)
        run.settle()
        return true
    }

    /** The app is killed: everything it ran stops, its requests are never answered. */
    fun kill() {
        disk.items = cache.saved
        job.cancel()
    }

    // ── The user ──

    /** Typed and sent (ChatScreen.send): the page picks the message's id. */
    fun send(text: String): String = java.util.UUID.randomUUID().toString().also { id ->
        scope.launch { sender.send(key, text, id) }
    }

    /** A failed bubble's Retry (ChatDeliveryActions). */
    fun retry(id: String): Boolean {
        val message = sender.outbox.value.firstOrNull { it.clientMessageId == id } ?: return false
        if (message.state !in setOf(DeliveryState.Refused, DeliveryState.Rejected, DeliveryState.NeedsConfirmation)) return false
        scope.launch { sender.retry(id) }
        return true
    }

    /** A failed bubble's Delete, or Discard for one whose delivery is unknown (ChatDeliveryActions). */
    fun discard(id: String): Boolean {
        val message = sender.outbox.value.firstOrNull { it.clientMessageId == id } ?: return false
        scope.launch { if (message.state == DeliveryState.Refused) sender.cancel(id) else sender.discard(id) }
        return true
    }

    /**
     * A tap on a card's answer (ChatScreen's InteractionPanel callback): it goes if the page's taps allow it, the card
     * stays locked while the hub answers, and what the answer did comes from the reply the same way
     * (InteractionSubmission). False when the card is not on screen.
     */
    fun answer(id: String, resolution: Resolution): Boolean {
        val all = interactions.pending.value
        val card = taps.shown(all).firstOrNull { it.id == id && it.sessionKey == key } ?: return false
        if (!taps.canTap(card, all, clock())) return true
        taps = taps.tapped(id)
        scope.launch {
            try {
                var changed = false
                val answer = try {
                    interactions.resolveOutcome(id, resolution)
                    CardAnswer.Withdrawn
                } catch (cancelled: CancellationException) { throw cancelled }
                catch (_: InteractionGoneException) { CardAnswer.Withdrawn }
                catch (_: InteractionPromptChangedException) { changed = true; CardAnswer.Withdrawn }
                catch (error: Exception) {
                    val failure = hubCodedFailure(error)
                    val status = failure?.status
                    if (status != null && (status in 400..499 || status == 503 && failure?.code == "assistant_offline")) CardAnswer.Refused
                    else CardAnswer.Unconfirmed
                }
                taps = taps.answered(id, answer)
                if (changed || answer != CardAnswer.Withdrawn) {
                    try { interactions.refresh() } catch (e: CancellationException) { throw e } catch (_: Exception) { }
                }
            } finally { taps = taps.idle() }
        }
        return true
    }

    // ── What it drew ──

    /** The chat page as ChatScreen draws it: the transcript's lines, then the outbox's bubbles; the cards; the state. */
    fun view(): SyncView {
        val items = snapshot.items
        val streaming = snapshot.streamingItemIds
        val row = sessions.sessions.value.firstOrNull { it.key == key }
        val out = mutableListOf<SyncView.Message>()
        // ChatTimeline.lines: no turn_duration lines; the page follows the latest (no frozen window, no open work).
        val lines = chatLines(items.filter { it.kind != "turn_duration" }.map(::lineItem),
            LineOptions(live = isLiveStatus(row?.status))).lines
        for (line in lines) {
            val item = (line as? ChatLine.Row)?.row?.takeIf { it.type == "user" || it.type == "reply" }?.item?.item ?: continue
            if (item.id in streaming) continue
            out += if (item.role == "user") SyncView.Message("user", item.text.orEmpty(), "shown")
                else SyncView.Message("agent", item.text.orEmpty(), null)
        }
        // Below every line (ChatScreen's list starts with them, from the bottom).
        for (bubble in chatBubbles(sender.outbox.value, key, items))
            out += SyncView.Message("user", bubble.text, if (bubble.state in FAILED) "failed" else "pending")
        val cards = taps.shown(interactions.pending.value).filter { it.sessionKey == key }
            .associate { it.id to if (taps.answerable(it, clock())) "open" else "answering" }
        val live = items.firstOrNull { it.id in streaming }?.text
        return SyncView(out, cards, row?.agentState?.wire, live)
    }

    // ── Requests ──

    private fun get(path: String) = buildJsonObject { put("get", path) }

    private fun itemsRequest(query: ItemsQuery) = buildJsonObject {
        put("get", "/items")
        query.afterSeq?.let { put("after_seq", it) }
    }

    private fun itemsPage(query: ItemsQuery): JsonObject = when {
        query.afterSeq != null -> run.hub.after(query.afterSeq!!)
        query.fromSeq != null -> run.hub.range(query.fromSeq!!, query.throughSeq!!)
        query.beforeSeq != null -> run.hub.before(query.beforeSeq!!)
        else -> run.hub.latest()
    }.let(run::tie)

    /**
     * An answer to a card, as HubChatTransport.resolveOutcome sends it: one op id for the answer; cut off (no reply, a
     * 502, 503 or 504) it goes again with that id once the socket is back, for up to a minute; 410 is
     * [InteractionGoneException], `prompt_changed` [InteractionPromptChangedException], other errors the hub's.
     */
    private suspend fun resolveCard(id: String): ResolveOutcome {
        val op = java.util.UUID.randomUUID().toString()
        val deadline = run.nowMillis() + 60_000
        while (true) {
            run.note(buildJsonObject { put("post", "resolve"); put("id", id) })
            val reply = run.write(id, op)
            try { return reply.resolveOutcome() }
            catch (error: HubReply) {
                if (error.status == 410) throw InteractionGoneException(error.body.str("status"))
                if (error.code == "prompt_changed") throw InteractionPromptChangedException()
                val remaining = deadline - run.nowMillis()
                if (!error.cutOff || remaining <= 0) throw error
                delay(minOf(500L, remaining))
                val back = withTimeoutOrNull((deadline - run.nowMillis()).coerceAtLeast(0)) {
                    socket.state.first { it == ConnectionState.Online }
                } != null
                if (!back) throw error
            }
        }
    }

    /**
     * The message transport (HttpMessageTransport's contract): sends only once caught up; each POST waits for the
     * case's reply, a retry with an op id the hub carried out gets its receipt at once; a lookup reads the op's
     * receipt (`/ops/{id}`), then the transcript.
     */
    inner class Messages : MessageTransport {
        override val readyToSend = MutableStateFlow(false)
        override val supportsClientOps = true

        override suspend fun send(request: MessageRequest): MessageTransportResult {
            if (!readyToSend.value) return MessageTransportResult.NotSent
            val message = run.posted(request.clientMessageId, request.opId)
            return run.write(message, request.opId).sendResult()
        }

        override suspend fun reconcile(request: MessageRequest, createdAt: Long, allowTextMatch: Boolean): MessageEvidence? {
            val receipt = run.read("/ops", buildJsonObject { put("get", "/ops"); put("id", run.keyOfOp(request.opId)) }) {
                run.receipt(request.opId) ?: JsonNull
            }
            if (receipt is JsonObject && receipt.str("state") in setOf("done", "failed", "unknown")) {
                val result = SyncReply(receipt.obj("result")!!).sendResult()
                if (result != MessageTransportResult.Accepted) return MessageEvidence.Receipt(result)
                return transcriptEvidence(request, createdAt, allowTextMatch) ?: MessageEvidence.Receipt(result)
            }
            return transcriptEvidence(request, createdAt, allowTextMatch && !supportsClientOps)
        }

        private suspend fun transcriptEvidence(request: MessageRequest, createdAt: Long, allowTextMatch: Boolean): MessageEvidence? {
            val items = http.items(key, ItemsQuery()).items
            val shown = items.any { item ->
                MessageEcho.carries(item, request.clientMessageId) || MessageEcho.carries(item, request.opId) ||
                    allowTextMatch && MessageEcho.arrived(request.text, createdAt, item)
            }
            return MessageEvidence.Shown.takeIf { shown }
        }
    }

    private companion object {
        /** What the hello offers that these cases touch (server/internal/proto Features). */
        val FEATURES = listOf("error_codes", "client_hello", "app_ping", "client_msg_id", "client_ops", "send_refusals",
            "screen_dialog", "resumable_interactions", "sessions_list", "event_stream")
        /** A bubble with the words, the reason and a way to send them again (OutboxStatus). */
        val FAILED = setOf(DeliveryState.Refused, DeliveryState.Rejected, DeliveryState.NeedsConfirmation)
    }
}

/** ChatTimeline's Item.asLineItem (feature/chat ChatState.kt). */
private fun lineItem(item: Item) = LineItem(item,
    item.fold?.let { LineFold(it.items, it.tools, it.thoughts, it.throughSeq, it.throughId, parseTime(it.until)) },
    item.task?.let { LineTask(it.kind, it.title, it.status, it.toolUseId, it.taskId, it.detail, it.exitCode, it.outputFile) })

/** A `reply` step's answer to one request: its status and body, or the connection cut. */
internal class SyncReply(val step: JsonObject) {
    val cut get() = step.bool("cut") == true
    val status get() = step.int("status") ?: 200
    val body get() = step.obj("body") ?: JsonObject(emptyMap())

    /** As HttpMessageTransport reads a reply: a cut is no receipt, an error the hub's fields. */
    fun sendResult(): MessageTransportResult {
        if (cut) return MessageTransportResult.NoReceipt
        if (body.str("state") in setOf("pending", "unknown")) return MessageTransportResult.NoReceipt
        if (status in 200..299) return MessageTransportResult.Accepted
        return MessageTransportResult.HubError(body.str("code"), body.str("delivered"), body.str("error"), body.bool("retryable") ?: false)
    }

    /** As HubChatTransport reads a reply to an answer: the outcome, or [HubReply]. */
    fun resolveOutcome(): ResolveOutcome {
        if (cut || status !in 200..299) throw HubReply(this)
        return ResolveOutcome(body.bool("already") == true, body.str("status"), body.obj("resolution")?.wire<Resolution>())
    }
}

/** A request the hub answered with an error, or that got no answer ([cutOff]: the transport tries it again). */
internal class HubReply(reply: SyncReply) : java.io.IOException(
    if (reply.cut) "connection reset" else reply.body.str("error") ?: "HTTP ${reply.status}"), HubCodedFailure {
    override val status: Int? = reply.status.takeUnless { reply.cut }
    override val code: String? = reply.body.str("code").takeUnless { reply.cut }
    val body = reply.body
    val cutOff = reply.cut || reply.status in setOf(502, 503, 504)
}

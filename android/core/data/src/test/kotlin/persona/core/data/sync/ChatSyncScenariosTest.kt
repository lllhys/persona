package persona.core.data.sync

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.*
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.Parameterized
import persona.core.contracts.Resolution
import persona.core.domain.FixtureReader
import java.time.Instant
import kotlin.time.Duration.Companion.seconds

/**
 * The shared chat sync scenarios (spec/fixtures/sync, README there) against the app's own chat state code (#1111): the
 * real repositories and outbox (SyncDevice), the chat page's taps and bubbles (persona.core.domain.ChatTaps), and a
 * hub that answers reads from the case and holds writes until it replies (SyncHub, SyncRun). Each case runs once per
 * device, as the web's replay does. After every step the page is projected onto the README's view and checked against
 * the three rules and the step's `expect`; at the end, once settled, against `end`.
 */
@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(Parameterized::class)
class ChatSyncScenariosTest(private val name: String, private val case: JsonObject, private val device: String) {
    @Test fun replay() = runTest(timeout = 60.seconds) {
        val failures = SyncRun(this, case, device).play()
        val known = KNOWN[name]
        if (known == null) {
            assertEquals("$name:\n" + failures.joinToString("\n"), emptyList<String>(), failures)
            return@runTest
        }
        assertTrue("$name passes now: take it off KNOWN (${known.why})", failures.isNotEmpty())
        val other = failures.filterNot { known.because.containsMatchIn(it) }
        assertEquals("$name fails for another reason than ${known.why}:\n" + failures.joinToString("\n"), emptyList<String>(), other)
    }

    /** A case this app does not pass yet: why, and what each of its failures must say. */
    private class Known(val why: String, val because: Regex)

    companion object {
        /**
         * Cases Android does not pass yet. Each must still fail, and only for its stated reason: a fixed one comes off
         * the list, and a new failure is not hidden behind an old entry.
         */
        private val KNOWN: Map<String, Known> = mapOf(
            "card_answered_then_resumed_snapshot" to Known(
                "#2767: the pending-card snapshot after a resumed hello brings back a card this device answered " +
                    "(HubInteractionCenter un-ends a card on any `interaction` frame, snapshot or replay)",
                Regex("""card ix1 is answerable again|open cards \[ix1], want \[]""")),
            "card_answer_fails_card_back" to Known(
                "#2768: an answer refused with 503 `assistant_offline` is retried as a cut-off for up to a minute " +
                    "(HubChatTransport.cutOff), so the card stays locked instead of coming back at once",
                Regex("""open cards \[], want \[ix1]""")),
            "send_failed_stays_above_later" to Known(
                "#2769: ChatScreen draws the outbox's bubbles below every transcript line, so a failed message drops " +
                    "below the ones sent after it",
                Regex("""got {2}\[.*If it rains: the aquarium\., user\[failed] Find a dinner place\.]""")),
            "start_answer_cut_then_receipt" to Known(
                "can't be played yet: a new chat starts from the new-conversation page, not this harness's chat",
                Regex("can't be played yet: a new chat")),
            "start_refused_then_resent_new_op" to Known(
                "can't be played yet: a new chat starts from the new-conversation page, not this harness's chat",
                Regex("can't be played yet: a new chat")),
        )

        @JvmStatic @Parameterized.Parameters(name = "{0}")
        fun cases(): List<Array<Any>> = FixtureReader.read("sync").flatMap { fixture ->
            val case = fixture.value
            val name = case.str("name")!!
            val devices = (case["devices"] as JsonArray).map { it.jsonPrimitive.content }
            devices.map { device -> arrayOf(if (devices.size > 1) "$name as $device" else name, case, device) }
        }
    }
}

/** A case can not be played here (yet): said as a failure, so the known list names it. */
internal class Unplayable(message: String) : Exception(message)

/** One case played as one device. */
@OptIn(ExperimentalCoroutinesApi::class)
internal class SyncRun(private val test: TestScope, private val case: JsonObject, private val me: String) {
    val hub = SyncHub(case.obj("chat")!!, case.obj("start")!!.obj("hub")!!)
    val background get() = test.backgroundScope
    private val failures = mutableListOf<String>()
    private val disk = SyncDisk()
    private lateinit var device: SyncDevice
    /** Each step's view in the test's output (SYNC_TRACE=1). */
    private val trace = System.getenv("SYNC_TRACE") == "1"

    /** The case's clock: its lines were written at the start; virtual time runs from there. */
    fun nowMillis() = START + test.testScheduler.currentTime

    fun fail(failure: String) { failures += failure }

    // ── What the device sent, in the README's terms ("requests") ──

    private val requests = mutableListOf<JsonObject>()
    private var checked = 0

    fun note(request: JsonObject) { requests += request }

    private class Sent(val id: String, val text: String, val clientId: String) {
        /** The operation ids its POSTs went under, the first one the message's own. */
        val ops = mutableListOf(clientId)
        var discarded = false
    }
    private val sent = mutableListOf<Sent>()

    /** The case's id for the message whose POSTs went under [op]; [op] itself for one this page never sent. */
    fun keyOfOp(op: String): String = sent.firstOrNull { op in it.ops }?.id ?: op

    /** A POST for the message the page sent as [clientMessageId] under [op]: the case's id for it. */
    fun posted(clientMessageId: String, op: String): String {
        val message = sent.firstOrNull { it.clientId == clientMessageId }
        val key = message?.id ?: clientMessageId
        val new = message != null && op !in message.ops
        if (new) message!!.ops += op
        note(buildJsonObject { put("post", "messages"); put("id", key); put("new_op", new) })
        return key
    }

    /**
     * The case ties a line to its own message id (`m1`); the page sent that message under ids of its own. The node tags
     * the line with the op id the message last went with, so the hub here does too.
     */
    fun tie(value: JsonObject): JsonObject = tieElement(value) as JsonObject

    private fun tieElement(value: JsonElement): JsonElement = when (value) {
        is JsonObject -> JsonObject(value.mapValues { (k, v) ->
            if (k == "client_msg_id" && v is JsonPrimitive && v.isString)
                sent.lastOrNull { it.id == v.content }?.ops?.last()?.let(::JsonPrimitive) ?: v
            else tieElement(v)
        })
        is JsonArray -> JsonArray(value.map(::tieElement))
        else -> value
    }

    // ── Reads: answered at once from the hub's state, or held until `release` ──

    private val armed = mutableSetOf<String>()
    private class HeldRead(val path: String, val stale: JsonElement, val fresh: () -> JsonElement) {
        val gate = CompletableDeferred<Boolean>()
    }
    private val heldReads = mutableListOf<HeldRead>()

    suspend fun read(path: String, request: JsonObject, answer: () -> JsonElement): JsonElement {
        note(request)
        if (!armed.remove(path)) return answer()
        // What the hub held when the read came, delivered late (fresh only when the case settles).
        val held = HeldRead(path, answer(), answer)
        heldReads += held
        return if (held.gate.await()) held.fresh() else held.stale
    }

    // ── Writes: each request waits for the case's `reply` ──

    private class HeldWrite(val key: String, val op: String) {
        val reply = CompletableDeferred<SyncReply>()
    }
    private val writes = mutableListOf<HeldWrite>()
    /** What a request under an op id the hub carried out gets at once: op id → `{status, body}`. */
    private val receipts = mutableMapOf<String, JsonObject>()

    suspend fun write(key: String, op: String): SyncReply {
        receipts[op]?.let { return SyncReply(it) }
        val held = HeldWrite(key, op)
        writes += held
        try { return held.reply.await() } finally { writes -= held }
    }

    /** `GET /api/v1/ops/{op}`: the receipt, `pending` while the hub works on it, nothing for an op it never saw. */
    fun receipt(op: String): JsonObject? = receipts[op]?.let { result ->
        buildJsonObject { put("state", if ((result.int("status") ?: 200) in 200..299) "done" else "failed"); put("result", result) }
    } ?: if (writes.any { it.op == op }) buildJsonObject { put("state", "pending") } else null

    // ── The rules' bookkeeping (README "What every consumer checks") ──

    /** Cards this device must never show open again: its answer took, or the hub said they ended. */
    private val ended = mutableSetOf<String>()
    /** Cards whose answer from here is in flight. */
    private val answering = mutableSetOf<String>()

    private fun saw(frame: JsonObject) {
        when (frame.str("type")) {
            "interaction_resolved" -> frame.str("id")?.let(ended::add)
            "interaction" -> frame.obj("interaction")?.str("id")?.takeIf { it in hub.raised }?.let(ended::remove)
        }
    }

    // ── Playing it ──

    suspend fun play(): List<String> {
        if (case.obj("chat")?.bool("new") == true)
            return listOf("can't be played yet: a new chat starts from the new-conversation page, which this harness does not drive")
        // The device already shows the start: connected (the hub's pending cards with its first hello), the chat open
        // and loaded, its first catch-up done.
        device = SyncDevice(me, this, disk)
        device.connect(resumed = false, frames = hub.pending.map(::cardFrame))
        device.open()
        advance(2_000)
        requests.clear()
        for ((i, step) in case.objects("steps").withIndex()) {
            val where = "step ${i + 1} (${step.keys.filter { it != "expect" }.joinToString("+")})"
            try { step(step, where) }
            catch (unplayable: Unplayable) { return failures + "$where: can't be played yet: ${unplayable.message}" }
            advance(FRAME_MILLIS)
            check(where, step.obj("expect")?.obj(me), whole = false)
        }
        finish()
        check("end", case.obj("end")!!.obj(me), whole = true)
        return failures
    }

    private suspend fun step(step: JsonObject, where: String) {
        step.obj("hub")?.let(hub::apply)
        val frames = step.obj("frames")?.objects(me).orEmpty()
        var framesSent = false
        step.obj("socket")?.takeIf { it.str("device") == me }?.let { socket ->
            if (socket.bool("drop") == true) device.drop()
            socket.obj("open")?.let { open ->
                val resumed = open.bool("resumed") == true
                frames.forEach(::saw)
                device.connect(resumed, frames)
                framesSent = true
                // A full refresh reads the hub's cards: an answer in flight for one it no longer has is over.
                if (!resumed) answering.filter { id -> hub.pending.none { it.str("id") == id } }.forEach(ended::add)
            }
        }
        if (!framesSent) for (frame in frames) {
            saw(frame)
            if (!device.deliver(frame)) fail("$where: a frame this app does not read: $frame")
        }
        step.obj("user")?.takeIf { it.str("device") == me }?.let { user(it, where) }
        step.obj("reply")?.takeIf { it.str("device") == me }?.let { reply(it, where) }
        step.long("wait")?.let(::advance)
        step.obj("hold")?.takeIf { it.str("device") == me }?.let { armed += it.str("get")!! }
        step.obj("release")?.takeIf { it.str("device") == me }?.let { release ->
            val held = heldReads.firstOrNull { it.path == release.str("get") }
            if (held == null) fail("$where: release of ${release.str("get")}: the device never read it")
            else { heldReads -= held; held.gate.complete(false) }
        }
    }

    private suspend fun user(user: JsonObject, where: String) {
        user.obj("send")?.let { send ->
            val text = send.str("text")!!
            sent += Sent(send.str("id")!!, text, device.send(text))
        }
        user.obj("answer")?.let { answer ->
            val id = answer.str("id")!!
            val before = requests.size
            if (!device.answer(id, answer.obj("resolution")!!.wire<Resolution>())) fail("$where: no card $id on screen")
            settle()
            if (requests.drop(before).any { it.str("post") == "resolve" && it.str("id") == id }) answering += id
        }
        user.str("retry")?.let { id ->
            val message = sent.firstOrNull { it.id == id } ?: throw Unplayable("retry of $id, never sent here")
            if (!device.retry(message.clientId)) fail("$where: no failed bubble for $id")
        }
        user.str("discard")?.let { id ->
            val message = sent.firstOrNull { it.id == id } ?: throw Unplayable("discard of $id, never sent here")
            message.discarded = true
            if (!device.discard(message.clientId)) fail("$where: no bubble to discard for $id")
        }
        if (user.bool("relaunch") == true) relaunch()
        user.obj("start")?.let { throw Unplayable("a new chat's start") }
        for (what in listOf("open", "close", "background", "foreground")) if (what in user) throw Unplayable("user $what")
        settle()
    }

    /** The hub answers every request in flight for the write. */
    private fun reply(reply: JsonObject, where: String) {
        val write = reply.str("write")!!
        val mine = writes.filter { it.key == write }
        if (mine.isEmpty()) {
            fail("$where: reply to $write: the device has no request for it in flight")
            return
        }
        val answer = SyncReply(reply)
        val done = reply.bool("done") ?: (!answer.cut && answer.status in 200..299)
        for (held in mine) {
            if (done) (reply.obj("receipt") ?: if (!answer.cut && "status" in reply) buildJsonObject {
                put("status", answer.status); put("body", answer.body)
            } else null)?.let { receipts[held.op] = it }
            held.reply.complete(answer)
        }
        writes -= mine.toSet()
        if (write in answering && !answer.cut) {
            answering -= write
            if (answer.status in 200..299 || answer.status == 410) ended += write
        }
        settle()
    }

    /**
     * The app is killed and starts again from what it stored (the outbox, the chat's cache), back on the chat it
     * showed. What it had in flight dies with it: nobody hears those answers. The case's next `socket.open` greets it.
     */
    private fun relaunch() {
        settle()
        device.kill()
        writes.clear()
        heldReads.clear()
        settle()
        device = SyncDevice(me, this, disk)
        device.open()
        settle()
    }

    /** Everything settles: the socket open, held reads answered from the hub's final state, timers run out. */
    private suspend fun finish() {
        if (!device.online) {
            answering.filter { id -> hub.pending.none { it.str("id") == id } }.forEach(ended::add)
            device.connect(resumed = false, frames = hub.pending.map(::cardFrame))
        }
        armed.clear()
        heldReads.toList().forEach { it.gate.complete(true) }
        heldReads.clear()
        advance(SETTLE_MILLIS)
    }

    private fun cardFrame(card: JsonObject) = buildJsonObject { put("type", "interaction"); put("interaction", card) }

    // ── Checks ──

    private fun check(where: String, want: JsonObject?, whole: Boolean) {
        val view = device.view()
        if (trace) println("SYNC ${case.str("name")} $me $where: $view")
        // never missing, never twice
        for (message in sent) {
            if (message.discarded) continue
            val shown = view.messages.count { it.from == "user" && it.text == message.text }
            if (shown != 1) fail("$where: sent message ${message.id} (\"${message.text}\") shows $shown times")
        }
        // no revived cards
        for ((id, state) in view.cards) {
            if (state == "open" && (id in ended || id in answering)) fail("$where: card $id is answerable again")
        }
        want ?: return
        (want["messages"] as? JsonArray)?.let { list ->
            val expected = list.map { it.jsonObject }.map { SyncView.Message(it.str("from")!!, it.str("text")!!, it.str("state")) }
            if (expected != view.messages) fail("$where: messages\n  want ${expected}\n  got  ${view.messages}")
        }
        (want["cards"] as? JsonObject)?.let { cards ->
            val open = cards.filterValues { it.jsonPrimitive.content == "open" }.keys.sorted()
            val got = view.cards.filterValues { it == "open" }.keys.sorted()
            if (open != got) fail("$where: open cards $got, want $open (shown: ${view.cards})")
        }
        want.str("status")?.let { if (it != view.status) fail("$where: status ${view.status}, want $it") }
        if ("live" in want) {
            val live = (want["live"] as? JsonPrimitive)?.takeIf { it.isString }?.content
            if (live != view.live) fail("$where: live ${view.live}, want $live")
        }
        (want["requests"] as? JsonArray)?.let { wanted ->
            var at = checked
            for (request in wanted.map { it.jsonObject }) {
                val found = (at until requests.size).firstOrNull { matches(requests[it], request) }
                if (found == null) {
                    fail("$where: requests: no $request in ${requests.drop(at)}")
                    break
                }
                at = found + 1
            }
            checked = requests.size
        }
        if (whole && want["cards"] == null) fail("$where: the case's end lists no cards")
    }

    private fun matches(seen: JsonObject, want: JsonObject): Boolean = when {
        // any one of these, where the protocol allows several ways (#2770)
        want["one_of"] is JsonArray -> (want["one_of"] as JsonArray).any { matches(seen, it.jsonObject) }
        want.str("get") == "/ops" -> seen.str("get") == "/ops" && seen.str("id") == want.str("id")
        want.str("get") != null -> seen.str("get") == want.str("get") &&
            (want.int("after_seq") == null || seen.int("after_seq") == want.int("after_seq"))
        want.str("ws") != null -> seen.str("ws") == want.str("ws") &&
            (want.int("since_seq") == null || seen.int("since_seq") == want.int("since_seq"))
        else -> seen.str("post") == want.str("post") &&
            if (want.bool("new_op") == true) seen.bool("new_op") == true else seen.str("id") == want.str("id")
    }

    // ── The clock ──

    /** What is due now (zero-delay work, a frame's redraw). */
    fun settle() = test.testScheduler.runCurrent()

    private fun advance(millis: Long) {
        test.testScheduler.advanceTimeBy(millis)
        test.testScheduler.runCurrent()
    }

    private companion object {
        val START = Instant.parse("2026-01-02T03:04:05Z").toEpochMilli()
        /** After each step, as the web's replay lets a few frames go by: a list's hint is read after 40 ms. */
        const val FRAME_MILLIS = 100L
        /** Long enough for every timer the chat runs (the outbox's longest wait is 60 s plus 15 minutes). */
        const val SETTLE_MILLIS = 30 * 60_000L
    }
}

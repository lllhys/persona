package persona.core.data

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.Parameterized
import persona.core.contracts.Item
import persona.core.contracts.JsonCodec
import persona.core.domain.ContinuityReducer.Gap
import persona.core.domain.ContinuitySync.Action
import persona.core.domain.FixtureReader

/** Both adapters replay every shared continuity case through their production accept boundary. */
@RunWith(Parameterized::class)
class ContinuityAdapterFixtureTest(
    private val name: String,
    private val fixture: FixtureReader.Case,
    private val selection: String,
) {
    @Test fun replay() = runBlocking {
        val driver = if (selection == "rust") RustContinuityDriver(FlushContinuityFrameScheduler()) else null
        val mismatches = java.util.concurrent.ConcurrentLinkedQueue<ContinuityMismatch>()
        val adapter: ContinuityAdapter = when (selection) {
            "rust" -> RustContinuityAdapter(CHAT, requireNotNull(driver))
            "shadow" -> ShadowContinuityAdapter(CHAT, sink = { mismatches.add(it) }, afterFirstFrame = { it() })
            else -> KotlinContinuityAdapter(CHAT)
        }
        try {
            val input = fixture.value.getValue("input").jsonObject
            val initial = input.getValue("initial").jsonObject
            val restored = requireNotNull(adapter.accept(ContinuityAdapter.Input.Restore(initial.state())))
            assertEquals("$name restore chat", CHAT, restored.chat)
            // The native fixture does not read cached items before the first input merges retained evidence.
            assertEquals("$name restore requests", emptyList<Action>(), restored.requests)
            val events = input.getValue("events").jsonArray
            val expected = fixture.value.getValue("expected").jsonArray
            assertEquals("$name step count", events.size, expected.size)
            events.forEachIndexed { index, raw ->
                val event = raw.jsonObject
                val label = "${fixture.file}/$name step $index"
                val action = when (val kind = event.text("kind")) {
                    "http_latest" -> ContinuityAdapter.Input.Latest(event.batch())
                    "ws_items" -> ContinuityAdapter.Input.Push(event.batch())
                    "http_after" -> ContinuityAdapter.Input.After(event.batch())
                    "http_reload" -> ContinuityAdapter.Input.Reload(event.items(), event.text("history"), event.number("to_seq"),
                        requireNotNull(event.boolean("has_more")))
                    "http_older" -> ContinuityAdapter.Input.Older(event.items(), requireNotNull(event.boolean("has_more")))
                    "http_range" -> ContinuityAdapter.Input.Range(event.items(), event.flag("more"))
                    "continue_fill" -> ContinuityAdapter.Input.ContinueFill
                    "reconnect" -> ContinuityAdapter.Input.Reconnect
                    "load_older" -> ContinuityAdapter.Input.LoadOlder
                    "open_fold" -> ContinuityAdapter.Input.OpenFold(requireNotNull(event.text("fold_id")))
                    else -> error("$label unsupported continuity event $kind")
                }
                val result = requireNotNull(adapter.accept(action)) { "$label test scheduler did not apply the input" }
                val want = expected[index].jsonObject
                assertEquals("$label chat", CHAT, result.chat)
                assertEquals("$label state", want.state(), result.state)
                assertEquals("$label published state", result.state, adapter.state)
                assertEquals("$label item_ids", want.getValue("item_ids").jsonArray.map { it.jsonPrimitive.content },
                    result.state.items.map { it.id })
                // Gap is the Kotlin reducer's step result, not a Rust boundary field. Rust verification is
                // tested through ChatPatch.verified; do not rebuild a second continuity state machine here.
                if (selection != "rust") assertEquals("$label gap", want.text("gap"), when (result.gap) {
                    Gap.None -> "none"
                    Gap.Fill -> "fill"
                    Gap.Reload -> "reload"
                })
                assertEquals("$label actions", want.getValue("actions").jsonArray.toList(), result.requests.map { it.json() })
            }
        } finally {
            adapter.close()
            ShadowContinuityDiagnostics.awaitIdle()
            assertEquals("$name shadow mismatches", emptyList<ContinuityMismatch>(), mismatches.toList())
            driver?.close()
        }
    }

    companion object {
        private const val CHAT = "fixture/adapter"

        @JvmStatic @Parameterized.Parameters(name = "{0}")
        fun cases(): List<Array<Any>> = FixtureReader.read("continuity").flatMap { fixture ->
            listOf("kotlin", "rust", "shadow").map { selection ->
                arrayOf("$selection/${fixture.value.getValue("name").jsonPrimitive.content}", fixture, selection)
            }
        }
    }
}

private fun JsonObject.state() = ContinuityAdapter.State(items(), text("history"), requireNotNull(number("covered")),
    boolean("has_more"), flag("loaded_once"))
private fun JsonObject.batch() = ContinuityAdapter.Batch(items(), text("history"), number("from_seq"), number("to_seq"),
    flag("backlog"), boolean("has_more"), flag("more_after"))
private fun JsonObject.text(key: String): String? = get(key)?.jsonPrimitive?.contentOrNull
private fun JsonObject.number(key: String): Int? = get(key)?.jsonPrimitive?.intOrNull
private fun JsonObject.boolean(key: String): Boolean? = get(key)?.jsonPrimitive?.booleanOrNull
private fun JsonObject.flag(key: String): Boolean = boolean(key) == true
private fun JsonObject.items(): List<Item> = get("items")?.let { JsonCodec.decodeFromJsonElement<List<Item>>(it) } ?: emptyList()

private fun Action.json(): JsonObject = buildJsonObject {
    when (val action = this@json) {
        is Action.FetchAfter -> { put("kind", "fetch_after"); put("after_seq", action.afterSeq) }
        is Action.FetchLatest -> { put("kind", "fetch_latest"); put("fold_work", action.foldWork) }
        is Action.Subscribe -> {
            put("kind", "subscribe"); put("fold_work", action.foldWork)
            action.sinceSeq?.let { put("since_seq", it) }
        }
        is Action.FetchOlder -> {
            put("kind", "fetch_older"); put("before_seq", action.beforeSeq); put("fold_work", action.foldWork)
        }
        is Action.FetchRange -> {
            put("kind", "fetch_range"); put("from_seq", action.fromSeq); put("through_seq", action.throughSeq)
        }
    }
}

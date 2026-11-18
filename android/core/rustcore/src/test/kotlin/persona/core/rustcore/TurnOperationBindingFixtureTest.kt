package persona.core.rustcore

import java.io.File
import java.util.concurrent.CopyOnWriteArrayList
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.rustcore.wire.Action
import persona.core.rustcore.wire.ChatEvent
import persona.core.rustcore.wire.ChatPatch
import persona.core.rustcore.wire.ChatSnapshot
import persona.core.rustcore.wire.Config
import persona.core.rustcore.wire.Effect
import persona.core.rustcore.wire.ListOp
import persona.core.rustcore.wire.Request
import persona.core.rustcore.wire.WireBatch

/**
 * Replays existing operation checkpoints through real Kotlin/JNI dispatch/drain and turn projection. Raw item JSON is
 * retained across the runtime boundary; expected turns come only from the corpus and its checked-in iOS overlay.
 */
class TurnOperationBindingFixtureTest {
    private val root: File = generateSequence(File(".").canonicalFile) { it.parentFile }
        .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory }
        ?: error("Cannot locate the repository")
    private val fixtures = root.resolve("spec/fixtures/lines")
    private val overlay = Json.parseToJsonElement(fixtures.resolve("ios/differences.json").readText()).jsonObject
    private val wake = overlay.getValue("turn_wake_state").jsonObject
    private val patchCases = overlay.getValue("cases").jsonArray.map { it.jsonObject }
    private val observedOperations = mutableSetOf<String>()

    private class Recorder : CoreHost {
        val effects = CopyOnWriteArrayList<Effect>()
        override fun changed(version: Long) = Unit
        override fun run(effect: Effect) { effects.add(effect) }
    }

    private fun replayOperation(core: RustCore, host: Recorder, items: MutableList<JsonElement>, op: JsonObject) {
        val name = op.getValue("op").jsonPrimitive.content
        observedOperations.add(name)
        val page = op.items()
        when (name) {
            "merge" -> core.dispatchChat(ChatEvent.Pushed(WireBatch(page.toString(), null, null, null, false, null, false)))
            "replace" -> core.dispatchChat(ChatEvent.Reloaded(page.toString(), "replacement", lastSeq(page), false))
            "unfold" -> {
                val foldId = op.getValue("fold_id").jsonPrimitive.content
                val fold = items.map { it.jsonObject }.single { it.getValue("id").jsonPrimitive.content == foldId }
                val range = fold.getValue("fold").jsonObject
                core.dispatchChat(ChatEvent.OpenFold(foldId))
                core.settle(items)
                val effect = host.effects.single()
                assertTrue("OpenFold requests the original fold", effect is Effect.Fetch)
                val request = (effect as Effect.Fetch).request
                assertEquals(Request.FetchRange(foldId, fold.getValue("seq").jsonPrimitive.long,
                    range.getValue("through_seq").jsonPrimitive.long), request)
                host.effects.clear()
            }
            "burst_replace" -> {
                for (batch in op.getValue("batches").jsonArray) {
                    core.dispatchChat(ChatEvent.Pushed(WireBatch(batch.toString(), null, null, null, false, null, false)))
                }
                core.settle(items) // Accumulate both background batches before the replacement drain.
                host.effects.clear()
                core.dispatchChat(ChatEvent.Reloaded(page.toString(), "replacement", lastSeq(page), false))
            }
            else -> error("Unknown operation: $name")
        }
        core.settle(items)
        if (name == "unfold") {
            core.dispatchChat(ChatEvent.Range(null, page.toString(), false))
            core.settle(items)
        }
        assertEquals("$name: no fetch is needed after supplying its operation's items", emptyList<Effect>(), host.effects)
        host.effects.clear()
    }

    private fun RustCore.dispatchChat(event: ChatEvent) {
        assertTrue(dispatch(Action.Chat(CHAT, event)))
    }

    private fun RustCore.settle(items: MutableList<JsonElement>): ChatPatch {
        assertTrue("the runtime did not settle", flush(5_000))
        val patch = drain(CHAT)
        for (op in patch.ops) {
            when (op) {
                is ListOp.Remove -> items.removeAt(op.index.toInt())
                is ListOp.Insert -> items.add(op.index.toInt(), Json.parseToJsonElement(op.json))
                is ListOp.Update -> items[op.index.toInt()] = Json.parseToJsonElement(op.json)
            }
        }
        return patch
    }

    private fun expectedTurns(name: String, state: JsonObject, applyPatch: Boolean): JsonArray {
        val expected = state.toMutableMap()
        val wakeDrop = wake.getValue("drop").jsonPrimitive.content
        val wakeSet = wake.getValue("set").jsonPrimitive.content
        expected["turns"] = JsonArray(state.getValue("turns").jsonArray.map { element ->
            val turn = element.jsonObject.toMutableMap()
            assertTrue("$name: stale wake drop", turn.remove(wakeDrop) != null)
            assertTrue("$name: stale wake set", !turn.containsKey(wakeSet))
            val pending = wake.getValue("pending").jsonArray.any { reference ->
                reference.jsonObject.getValue("case").jsonPrimitive.content == name &&
                    reference.jsonObject.getValue("turn").jsonPrimitive.content == turn.getValue("id").jsonPrimitive.content
            }
            turn[wakeSet] = JsonPrimitive(pending)
            JsonObject(turn)
        })
        if (applyPatch) {
            for (entry in patchCases.filter { it.getValue("name").jsonPrimitive.content == name }) {
                assertTrue(entry.keys.all { it in setOf("name", "why", "source", "replace", "turn_fields") })
                assertTrue(entry.getValue("why").jsonPrimitive.content.isNotEmpty())
                assertTrue(entry.getValue("source").jsonArray.isNotEmpty())
                for ((key, replacement) in entry["replace"]?.jsonObject.orEmpty()) {
                    assertNotEquals("$name: stale replace $key", expected[key], replacement)
                    expected[key] = replacement
                }
                for ((id, fields) in entry["turn_fields"]?.jsonObject.orEmpty()) {
                    val turns = expected.getValue("turns").jsonArray
                    assertEquals("$name/$id: exactly one turn", 1, turns.count {
                        it.jsonObject.getValue("id").jsonPrimitive.content == id
                    })
                    expected["turns"] = JsonArray(turns.map { element ->
                        val turn = element.jsonObject.toMutableMap()
                        if (turn.getValue("id").jsonPrimitive.content == id) {
                            for ((key, replacement) in fields.jsonObject) {
                                assertNotEquals("$name/$id: stale field $key", turn[key], replacement)
                                turn[key] = replacement
                            }
                        }
                        JsonObject(turn)
                    })
                }
            }
        }
        return expected.getValue("turns").jsonArray
    }

    @Test fun replayEveryOperationCheckpointThroughRealJni() {
        val cases = fixtureCases()
        assertEquals("the exact existing operation manifest", expectedCases, cases.map {
            it.getValue("name").jsonPrimitive.content
        }.toSet())
        var checkpoints = 0
        var turnObjects = 0
        for (case in cases) {
            val name = case.getValue("name").jsonPrimitive.content
            val input = case.getValue("input").jsonObject
            val operations = input.getValue("operations").jsonArray
            val expected = case.getValue("expected").jsonObject.getValue("checkpoints").jsonArray
            assertEquals("$name: one expected checkpoint per operation", operations.size, expected.size)
            val host = Recorder()
            RustCore(Config(), host).use { core ->
                assertTrue(core.dispatch(Action.OpenChat(CHAT, ChatSnapshot(
                    input.getValue("items").jsonArray.toString(), null, 0, null, false,
                ))))
                val items = mutableListOf<JsonElement>()
                core.settle(items)
                host.effects.clear()
                operations.forEachIndexed { index, element ->
                    val op = element.jsonObject
                    replayOperation(core, host, items, op)
                    val state = expected[index].jsonObject
                    val label = "$name checkpoint $index"
                    assertEquals("$label: complete item IDs", state.getValue("item_ids").jsonArray,
                        JsonArray(items.map { it.jsonObject.getValue("id") }))
                    val projectionInput = buildJsonObject {
                        for ((key, value) in input) {
                            if (key != "operations") put(key, value)
                        }
                        for (key in setOf("open_work", "live_turn_id")) {
                            op[key]?.let { put(key, it) }
                        }
                        put("items", JsonArray(items))
                    }
                    val actual = Json.parseToJsonElement(NativeBridge.projectTurns(projectionInput.toString()))
                    assertTurnsEqual(label, expectedTurns(name, state, applyPatch = true), actual)
                    if (patchCases.any { it.getValue("name").jsonPrimitive.content == name }) {
                        assertNotEquals("$label: overlay is necessary", expectedTurns(name, state, applyPatch = false), actual)
                    }
                    checkpoints++
                    turnObjects += actual.jsonArray.size
                }
            }
        }
        assertEquals(setOf("merge", "unfold", "burst_replace"), observedOperations)
        assertEquals("five fold operations, three incremental merges and two one-step cases", 10, checkpoints)
        assertEquals("one turn per checkpoint except incremental's two and three", 13, turnObjects)
        println("JNI operation turn replay: 4 cases; $checkpoints checkpoints; $turnObjects complete turn objects")
    }

    @Test fun replayExistingBurstAsAStandaloneReplacement() {
        val case = fixtureCases().first { it.getValue("name").jsonPrimitive.content == "background_bursts_and_replacement" }
        val name = case.getValue("name").jsonPrimitive.content
        val input = case.getValue("input").jsonObject
        val burst = input.getValue("operations").jsonArray.single().jsonObject
        val replacement = buildJsonObject {
            put("op", "replace")
            put("items", burst.getValue("items").jsonArray)
        }
        val host = Recorder()
        RustCore(Config(), host).use { core ->
            assertTrue(core.dispatch(Action.OpenChat(CHAT, ChatSnapshot("[]", null, 0, null, false))))
            val items = mutableListOf<JsonElement>()
            core.settle(items)
            host.effects.clear()
            replayOperation(core, host, items, replacement)
            val state = case.getValue("expected").jsonObject.getValue("checkpoints").jsonArray.single().jsonObject
            assertEquals("replacement item IDs", state.getValue("item_ids").jsonArray,
                JsonArray(items.map { it.jsonObject.getValue("id") }))
            val actual = Json.parseToJsonElement(NativeBridge.projectTurns(buildJsonObject {
                put("items", JsonArray(items))
            }.toString()))
            assertTurnsEqual("$name standalone replace", expectedTurns(name, state, applyPatch = true), actual)
            assertEquals(setOf("replace"), observedOperations)
        }
    }

    @Test fun rejectsMalformedProjectionAndAWrongCheckpointExpectation() {
        assertEquals("", NativeBridge.projectTurns("{\"invalid\":true}"))
        assertEquals("", NativeBridge.projectTurns("not JSON"))
        val case = requireNotNull(fixtures.listFiles { file -> file.isFile && file.extension == "json" })
            .flatMap { Json.parseToJsonElement(it.readText()).jsonObject.getValue("cases").jsonArray }
            .map { it.jsonObject }
            .first { it.getValue("name").jsonPrimitive.content == "incremental_with_late_result_and_older_page" }
        val input = case.getValue("input").jsonObject
        val firstOperation = input.getValue("operations").jsonArray.first().jsonObject
        val actualInput = buildJsonObject {
            put("items", firstOperation.getValue("items").jsonArray)
        }
        val actual = Json.parseToJsonElement(NativeBridge.projectTurns(actualInput.toString()))
        val expected = case.getValue("expected").jsonObject.getValue("checkpoints").jsonArray.first().jsonObject
        val wrong = JsonArray(listOf(JsonObject(emptyMap())))
        assertNotEquals("the negative checkpoint must differ", wrong, actual)
        val failure = assertThrows("the checkpoint comparison accepted wrong turns", AssertionError::class.java) {
            assertTurnsEqual("incremental_with_late_result_and_older_page checkpoint 0", wrong, actual)
        }
        assertTrue(failure.message.orEmpty().contains("checkpoint 0"))
        assertEquals(expectedTurns(
            "incremental_with_late_result_and_older_page", expected, applyPatch = true,
        ), actual)
    }

    private fun fixtureCases(): List<JsonObject> = requireNotNull(
        fixtures.listFiles { file -> file.isFile && file.extension == "json" },
    ).sortedBy { it.name }.flatMap { file ->
        Json.parseToJsonElement(file.readText()).jsonObject.getValue("cases").jsonArray
    }.map { it.jsonObject }.filter { it.getValue("input").jsonObject.containsKey("operations") }

    private fun assertTurnsEqual(label: String, expected: JsonElement, actual: JsonElement) {
        assertEquals(label, expected, actual)
    }

    private companion object {
        const val CHAT = "fixture/turn-operations"
        val expectedCases = setOf(
            "background_bursts_and_replacement", "fold_replaces_extends_and_unfolds",
            "incremental_with_late_result_and_older_page", "outside_fold_tool_keeps_late_result",
        )
    }
}

private fun JsonObject.items(): JsonArray = (get("items") ?: JsonArray(emptyList())).jsonArray
private fun lastSeq(items: JsonArray): Long = items.last().jsonObject.getValue("seq").jsonPrimitive.content.toLong()

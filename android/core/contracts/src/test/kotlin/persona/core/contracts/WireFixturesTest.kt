package persona.core.contracts

import java.io.File
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.Parameterized

@RunWith(Parameterized::class)
class WireFixturesTest(private val name: String, private val fixture: JsonObject) {
    companion object {
        @JvmStatic @Parameterized.Parameters(name = "{0}")
        fun cases(): List<Array<Any>> {
            val dir = File(System.getProperty("persona.repo.root"), "spec/fixtures/wire")
            val files = requireNotNull(dir.listFiles { f -> f.extension == "json" })
            require(files.isNotEmpty()) { "Missing wire fixtures" }
            return files.sortedBy { it.name }.flatMap { file ->
                JsonCodec.parseToJsonElement(file.readText()).jsonArray.map {
                    arrayOf(it.jsonObject.getValue("name").jsonPrimitive.content, it.jsonObject)
                }
            }
        }

        private val projection = Json(JsonCodec) { encodeDefaults = true; explicitNulls = true }

        private inline fun <reified T> model(input: JsonElement): JsonElement =
            projection.encodeToJsonElement(JsonCodec.decodeFromJsonElement<T>(input))
    }

    @Test fun decodesSharedSampleAndChecksEveryDocumentedField() {
        val input = fixture.getValue("input")
        val decoded = when (val decoder = fixture.getValue("decoder").jsonPrimitive.content) {
            "event", "compat_event" -> ServerEvent.decode(input.toString()).also {
                // The socket decodes the frame it already parsed; both ways must read the same event.
                assertEquals(it, ServerEvent.decode(input))
            }?.let { projection.encodeToJsonElement(it) }
            "sessions" -> model<SessionsResponse>(input)
            "machines" -> model<MachinesResponse>(input)
            "interactions" -> model<InteractionsResponse>(input)
            "items", "items_after", "items_range" -> model<ItemsResponse>(input)
            "fleet" -> model<FleetResponse>(input)
            "inbox" -> model<InboxResponse>(input)
            "feed" -> model<FeedResponse>(input)
            "queue_edit" -> model<QueueEdit>(input)
            "lark" -> model<LarkMessages>(input)
            "health" -> model<Health>(input)
            "error" -> model<ErrorBody>(input)
            "compat_session" -> {
                val session = JsonCodec.decodeFromJsonElement<Session>(input)
                JsonObject(projection.encodeToJsonElement(session).jsonObject + mapOf(
                    "updated_at_meaningful" to JsonPrimitive(parseTime(session.updatedAt) != null),
                    "seen_at_meaningful" to JsonPrimitive(parseTime(session.seenAt) != null),
                ))
            }
            else -> error("Unregistered fixture decoder: $decoder")
        }
        val expected = fixture.getValue("expected").jsonObject
        val fields = expected.getValue("fields").jsonObject.toMutableMap()
        var accepted = expected.getValue("accepted").jsonPrimitive.boolean
        // The shared golden records current iOS behavior. These explicit differences implement D5.
        when (name) {
            "items_folded_tasks" -> {
                fields["/items/1/fold/tasks/0/replace"] = JsonPrimitive("todo")
                fields["/items/1/fold/tasks/0/entries/0/status"] = JsonPrimitive("in_progress")
                fields["/items/1/fold/tasks/0/source_item_id"] = JsonPrimitive("entry_tool#0")
                fields["/items/1/fold/tasks/0/source_seq"] = JsonPrimitive(3)
            }
            "hello_wrong_known_type" -> {
                accepted = true
                fields["/features"] = JsonArray(emptyList())
                fields["/type"] = JsonPrimitive("hello")
            }
            "session_bad_optional_field" -> {
                accepted = true
                fields["/sessions/0/session_id"] = JsonPrimitive("s1")
                fields["/sessions/0/background"] = JsonNull
            }
            "interaction_unknown_kind" -> {
                accepted = true
                fields["/interaction/kind"] = JsonPrimitive("unknown")
            }
            "fleet_unknown_values" -> {
                fields["/fleet/phase"] = JsonPrimitive("unknown")
                fields["/fleet/items/0/kind"] = JsonPrimitive("unknown")
            }
            "date_at_cutoff" -> fields["/updated_at_meaningful"] = JsonPrimitive(true)
        }
        assertEquals("$name accepted", accepted, decoded != null)
        for ((pointer, value) in fields) {
            assertJson("$name $pointer", value, at(decoded, pointer))
        }
    }

    private fun at(root: JsonElement?, pointer: String): JsonElement {
        var current = root
        for (part in pointer.removePrefix("/").split('/')) {
            val key = part.replace("~1", "/").replace("~0", "~")
            current = when (val node = current) {
                is JsonObject -> node[key]
                is JsonArray -> key.toIntOrNull()?.let { node.getOrNull(it) }
                else -> null
            }
        }
        return current ?: JsonNull
    }

    private fun assertJson(label: String, expected: JsonElement, actual: JsonElement) {
        if (expected is JsonPrimitive && actual is JsonPrimitive && !expected.isString && !actual.isString) {
            val a = expected.content.toBigDecimalOrNull()
            val b = actual.content.toBigDecimalOrNull()
            if (a != null && b != null) { assertEquals(label, 0, a.compareTo(b)); return }
        }
        assertEquals(label, expected, actual)
    }
}

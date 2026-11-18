package persona.core.domain.sort

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.Parameterized
import persona.core.contracts.Interaction
import persona.core.contracts.JsonCodec
import persona.core.contracts.Session
import persona.core.contracts.SessionChildren
import persona.core.domain.FixtureReader

/**
 * Side sessions (#7944) against spec/fixtures/side-fold, which the hub's manager and every client read: the rows the
 * hub sends (the input with `expected.fold` applied) list as `expected.rows` in their folded states, the parent counts
 * the sides it holds as the hub does, and each card has its home and its chat.
 */
@RunWith(Parameterized::class)
class SideFoldFixturesTest(private val name: String, private val fixture: FixtureReader.Case) {
    @Test fun rowsStatesAndCards() {
        val input = fixture.value.getValue("input").jsonObject
        val expected = fixture.value.getValue("expected").jsonObject
        val fold = expected.getValue("fold").jsonObject
        val paged = input["paged"]?.jsonPrimitive?.booleanOrNull == true
        val raw = input.getValue("sessions").jsonArray.map { it.jsonObject }
        fun id(row: JsonObject) = "${row.text("machine_id")}/${row.text("session_id")}"
        // What the hub sends: each row with what the manager added; a paged list's rows come folded already.
        val sessions = raw.map { row ->
            JsonCodec.decodeFromJsonElement<Session>(if (paged) row else JsonObject(row + fold[id(row)]?.jsonObject.orEmpty()))
        }
        val listed = foldRows(sessions.filterNot { it.archived }.map(::AgentSession), hierarchy = true)
        val want = expected.getValue("rows").jsonArray.map { it.jsonObject.text("id") to it.jsonObject.text("state") }
        assertEquals(name, want, listed.map { it.session.key.path to it.state.wire })
        // Counted from the sides this list holds, the parent's rollup is the hub's.
        if (!paged) listed.forEach { row ->
            val hub = fold[row.session.key.path]?.jsonObject?.get("children")?.let { JsonCodec.decodeFromJsonElement<SessionChildren>(it) }
            assertEquals("$name: ${row.session.key.path} children", hub, row.session.children)
        }
        // The hub stamps a card with its session's fold.
        val cards = input.getValue("interactions").jsonArray.map { it.jsonObject }.map { card ->
            val own = fold["${card.text("machine_id")}/${card.text("session_id")}"]?.jsonObject.orEmpty()
                .filterKeys { it in setOf("folded_under", "folded_as", "folded_side") }
            JsonCodec.decodeFromJsonElement<Interaction>(JsonObject(card + own))
        }
        val wantCards = expected.getValue("cards").jsonArray.map {
            Triple(it.jsonObject.text("id"), it.jsonObject.text("home"), it.jsonObject.text("chat"))
        }
        assertEquals("$name: cards", wantCards, cards.map { Triple(it.id, it.homeKey.path, (it.sideHome ?: it.homeKey).path) })
    }

    private fun JsonObject.text(name: String): String = getValue(name).jsonPrimitive.content

    companion object {
        @JvmStatic @Parameterized.Parameters(name = "{0}")
        fun cases(): List<Array<Any>> = FixtureReader.read("side-fold").map {
            arrayOf(it.value.getValue("name").jsonPrimitive.content, it)
        }
    }
}

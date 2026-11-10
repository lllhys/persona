package persona.core.domain.sort

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.int
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
import persona.core.domain.FixtureReader

/** The Agents tab's Needs you, one row per waiting session, and its badge (#3580) against spec/fixtures/needs-you, which iOS reads too. */
@RunWith(Parameterized::class)
class AgentsNeedsYouFixtureTest(private val name: String, private val fixture: FixtureReader.Case) {
    @Test fun rowsAndBadge() {
        val input = fixture.value.getValue("input").jsonObject
        val expected = fixture.value.getValue("expected").jsonObject
        val quiet = input.strings("quiet").toSet()
        // The hub marks a quiet task's agents on the session; the fixture lists them apart.
        val sessions = input.list<Session>("sessions").map { if (it.key.path in quiet) it.copy(quiet = true) else it }
        val byKey = sessions.associateBy { it.key }
        val mutes = input.getValue("mutes").jsonObject.let {
            AgentPreferences(spaces = it.strings("spaces"), sessions = it.strings("sessions"), allowed = it.strings("allowed").toSet())
        }
        val rows = needsYouRows(needsYouCards(input.list<Interaction>("interactions"), byKey), needsYouWaiting(sessions))
        val want = expected.getValue("rows").jsonArray.map { row ->
            row.jsonObject.getValue("id").jsonPrimitive.content to row.jsonObject.strings("cards")
        }
        assertEquals(name, want, rows.map { it.id to it.cards.map(Interaction::id) })
        assertEquals("$name: badge", expected.getValue("badge").jsonPrimitive.int, needsYouBadge(rows, byKey, mutes))
    }

    private inline fun <reified T> JsonObject.list(name: String): List<T> =
        getValue(name).jsonArray.map { JsonCodec.decodeFromJsonElement<T>(it) }

    private fun JsonObject.strings(name: String): List<String> = getValue(name).jsonArray.map { it.jsonPrimitive.content }

    companion object {
        @JvmStatic @Parameterized.Parameters(name = "{0}")
        fun cases(): List<Array<Any>> = FixtureReader.read("needs-you").map {
            arrayOf(it.value.getValue("name").jsonPrimitive.content, it)
        }
    }
}

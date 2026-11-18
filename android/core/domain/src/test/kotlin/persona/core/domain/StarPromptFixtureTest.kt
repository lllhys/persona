package persona.core.domain

import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.Parameterized

/** When each GitHub star prompt shows (#5761) against spec/fixtures/star-prompt, which the web replays too (iOS: #5910). */
@RunWith(Parameterized::class)
class StarPromptFixtureTest(private val name: String, private val fixture: FixtureReader.Case) {
    @Test fun replays() {
        val case = fixture.value
        val now = case.getValue("now").jsonPrimitive.long
        val before = case.getValue("state").jsonObject.state()
        val event = case.getValue("event").takeUnless { it is JsonNull }?.jsonPrimitive?.content?.let { key ->
            StarPromptEvent.entries.firstOrNull { it.key == key } ?: error("$name: unknown event $key")
        }
        val step = event?.let { starPromptAfter(before, it, now) } ?: StarPromptStep(before)
        val expected = case.getValue("expected").jsonObject
        assertEquals(name, expected.getValue("state").jsonObject.state(), step.state)
        assertEquals("$name: pill", expected.getValue("pill").jsonPrimitive.boolean, showsStarPill(step.state))
        assertEquals("$name: card", expected.getValue("card").jsonPrimitive.boolean, showsStarCard(step.state, now))
        assertEquals("$name: toast", expected.getValue("toast").jsonPrimitive.boolean, step.toast)
    }

    private fun JsonObject.state() = StarPromptState(
        launches = getValue("launches").jsonPrimitive.int,
        threshold = getValue("threshold").jsonPrimitive.int,
        snoozedUntil = getValue("snoozedUntil").takeUnless { it is JsonNull }?.jsonPrimitive?.long,
        done = getValue("done").jsonPrimitive.boolean,
        pillHidden = getValue("pillHidden").jsonPrimitive.boolean,
        welcomed = getValue("welcomed").jsonPrimitive.boolean,
    )

    companion object {
        @JvmStatic @Parameterized.Parameters(name = "{0}")
        fun cases(): List<Array<Any>> = FixtureReader.read("star-prompt").map {
            arrayOf(it.value.getValue("name").jsonPrimitive.content, it)
        }
    }
}

package persona.core.domain.tasks

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.JsonCodec
import persona.core.contracts.Todo

class CheckTest {
    private fun todo(json: String) = JsonCodec.decodeFromString(Todo.serializer(), json)
    private fun checked(verdict: String, extra: String = "", reason: String? = " Waiting for CI ") =
        todo("""{"id":"t"$extra,"check":{"verdict":"$verdict",${reason?.let { "\"reason\":\"$it\"," } ?: ""}"at":"2026-11-10T08:00:00Z"}}""")

    @Test
    fun aCheckThatFoundItNotDoneSaysWhy() {
        for (verdict in listOf("not_done", "waiting", "needs_user", "undelivered", "a_later_verdict")) {
            assertEquals(verdict, "Waiting for CI", checked(verdict).notDoneReason)
        }
        assertEquals("", checked("waiting", reason = null).notDoneReason)
        assertEquals("Waiting for CI", checked("waiting", ""","reopened_at":"2026-11-10T07:00:00Z"""").notDoneReason)
    }

    @Test
    fun doneUnsureClosedStaleOrAbsentSayNothing() {
        assertNull(todo("""{"id":"t"}""").notDoneReason)
        assertNull(checked("done").notDoneReason)
        assertNull(checked("unsure").notDoneReason)
        assertNull(checked("waiting", ""","done_at":"2026-11-10T09:00:00Z"""").notDoneReason)
        assertNull(checked("waiting", ""","reopened_at":"2026-11-10T09:00:00Z"""").notDoneReason)
    }
}

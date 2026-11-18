package persona.core.rustcore

import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Drives the actual Rust JNI implementation through Kotlin, not merely its `external fun` declaration.
 * The complete expected projection is compared for every agents_list fixture. No Android device is involved.
 */
class SortBindingFixtureTest {
    private val fixtures: File = generateSequence(File(".").canonicalFile) { it.parentFile }
        .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory }
        ?.resolve("spec/fixtures/sort") ?: error("Cannot locate spec/fixtures/sort")

    @Test fun replayEveryProjectionThroughJni() {
        val files = requireNotNull(fixtures.listFiles { file -> file.isFile && file.extension == "json" })
            .sortedBy { it.name }
        assertEquals(
            "sort fixture file set",
            listOf("filters.json", "folding.json", "grouping.json", "held.json", "hub_state.json", "ordering.json", "session_state.json"),
            files.map { it.name },
        )
        val replayed = mutableSetOf<String>()
        for (file in files) {
            val document = Json.parseToJsonElement(file.readText()).jsonObject
            if (document.getValue("operation").jsonPrimitive.content != "agents_list") continue
            for (case in document.getValue("cases").jsonArray) {
                val value = case.jsonObject
                val name = value.getValue("name").jsonPrimitive.content
                val actual = NativeBridge.projectSort(value.getValue("input").jsonObject.toString())
                assertTrue("$file/$name: JNI returned no projection", actual.isNotEmpty())
                assertEquals("$file/$name", value.getValue("expected"), Json.parseToJsonElement(actual))
                assertTrue("duplicate projection case $name", replayed.add(name))
            }
        }
        assertEquals(
            "JNI must pin every agents_list case by name",
            setOf(
                "active_space_uses_rank_not_slots", "compact_expands_idle_without_reordering",
                "cross_machine_same_session_id", "cross_machine_same_session_id_reversed",
                "empty_list", "filter_all", "filter_needsyou", "filter_unread", "filter_working",
                "folded_machine_hides_rows", "folded_space_keeps_counts", "folded_state_and_expanded_idle",
                "kept_cannot_escape_base", "held_quiet_row_keeps_unread_state_section", "held_quiet_space_stays_active",
                "held_row_stays_out_of_quiet_idle_fold", "machine_search_before_counts", "machine_uses_registry_order",
                "mute_allow_and_explicit_override", "pinned_offline_is_visible", "pinned_sessions_use_pin_order_once",
                "quick_filter_ignores_rank", "quick_spaces_use_updated_time", "rank_all_states_and_mutes",
                "search_expands_idle", "space_header_counts", "space_time_tie", "space_time_tie_reversed",
                "spaces_pin_busy_muted_quiet", "unread_keeps_seen_rows", "unread_reselected_drops_seen_rows",
                "working_since_then_id", "working_since_then_id_reversed",
            ),
            replayed,
        )
        println("JNI sort host replay: ${replayed.size} cases, full expected equality")
    }

    @Test fun malformedInputIsRejected() {
        assertEquals("", NativeBridge.projectSort("{\"invalid\":true}"))
        assertEquals("", NativeBridge.projectSort("not JSON"))
    }
}

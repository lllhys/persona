package persona.core.data

import java.io.File
import java.time.Instant
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Machine
import persona.core.domain.sort.AgentGrouping
import persona.core.domain.sort.AgentListSettings
import persona.core.domain.sort.AgentPreferences
import persona.core.domain.sort.AgentQuickFilter
import persona.core.domain.sort.AgentSession
import persona.core.contracts.Session
import persona.core.domain.sort.AgentSort
import persona.core.rustcore.RustCoreSort

/** Runs production Kotlin and the real host JNI projection on the same converted fixture input. */
class AgentListAdapterFixtureTest {
    private val fixtures = generateSequence(File(".").canonicalFile) { it.parentFile }
        .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory }
        ?.resolve("spec/fixtures/sort") ?: error("Cannot locate spec/fixtures/sort")

    @Test fun kotlinAndRustReplayTheSameInputs() {
        val reports = mutableListOf<AgentListMismatch>()
        val shadow = ShadowAgentListAdapter(projectRust = RustCoreSort::project, sink = reports::add)
        val names = mutableSetOf<String>()
        for (file in requireNotNull(fixtures.listFiles()).sortedBy { it.name }) {
            if (file.extension != "json") continue
            val document = Json.parseToJsonElement(file.readText()).jsonObject
            if (document.getValue("operation").jsonPrimitive.content != "agents_list") continue
            for (case in document.getValue("cases").jsonArray) {
                val value = case.jsonObject
                val name = value.getValue("name").jsonPrimitive.content
                assertTrue("duplicate projection case $name", names.add(name))
                shadow.project(value.getValue("input").jsonObject.adapterInput())
                ShadowAgentListDiagnostics.awaitIdle()
            }
        }
        assertEquals(33, names.size)
        assertEquals(setOf(
            "active_space_uses_rank_not_slots", "compact_expands_idle_without_reordering", "cross_machine_same_session_id",
            "cross_machine_same_session_id_reversed", "empty_list", "filter_all", "filter_needsyou", "filter_unread", "filter_working",
            "folded_machine_hides_rows", "folded_space_keeps_counts", "folded_state_and_expanded_idle", "held_quiet_row_keeps_unread_state_section",
            "held_quiet_space_stays_active", "held_row_stays_out_of_quiet_idle_fold", "kept_cannot_escape_base",
            "machine_search_before_counts", "machine_uses_registry_order", "mute_allow_and_explicit_override", "pinned_offline_is_visible",
            "pinned_sessions_use_pin_order_once", "quick_filter_ignores_rank", "quick_spaces_use_updated_time", "rank_all_states_and_mutes",
            "search_expands_idle", "space_header_counts", "space_time_tie", "space_time_tie_reversed", "spaces_pin_busy_muted_quiet",
            "unread_keeps_seen_rows", "unread_reselected_drops_seen_rows", "working_since_then_id", "working_since_then_id_reversed",
        ), names)
        assertEquals("production adapter parity: $reports", 0, shadow.mismatchCount)
    }

    @Test fun heldMultiHubAndConditionalQueriesUseRealJni() {
        val reports = mutableListOf<AgentListMismatch>()
        val shadow = ShadowAgentListAdapter(projectRust = RustCoreSort::project, sink = reports::add)
        val rows = listOf(
            AgentSession(Session(machineId = "m~g1234abc", sessionId = "pinned", state = "idle", updatedAt = "2026-01-01T00:00:00Z")),
            AgentSession(Session(machineId = "m~g1234abc", sessionId = "held", state = "idle", updatedAt = "2026-01-02T00:00:00Z")),
        )
        for (query in listOf("is:pinned", "machine:m", "is:error", "since:7d")) {
            val reports = mutableListOf<AgentListMismatch>()
            val shadow = ShadowAgentListAdapter(projectRust = RustCoreSort::project, sink = reports::add)
            shadow.project(AgentListAdapter.Input(rows, listOf(Machine("m~g1234abc", "Example")),
                pins = AgentPreferences(sessions = listOf("m~g1234abc/pinned")),
                settings = AgentListSettings(grouping = AgentGrouping.State, query = query),
                held = "m~g1234abc/held", now = Instant.parse("2026-01-03T00:00:00Z")))
            ShadowAgentListDiagnostics.awaitIdle()
            // Multi-hub pin identity was fixed by #9027. The held row uses the same full hub identity.
            assertEquals("unexpected mismatch for $query: $reports", emptyList<AgentListMismatch>(), reports)
        }
    }
    @Test fun heldMatchesTheRealJniProjection() {
        val reports = mutableListOf<AgentListMismatch>()
        val shadow = ShadowAgentListAdapter(projectRust = RustCoreSort::project, sink = reports::add)
        val row = AgentSession(Session(machineId = "m", sessionId = "held", state = "idle", updatedAt = "2026-01-01T00:00:00Z"))
        shadow.project(AgentListAdapter.Input(listOf(row), listOf(Machine("m")),
            settings = AgentListSettings(grouping = AgentGrouping.State, folded = setOf("!state/idle")), held = "m/held"))
        ShadowAgentListDiagnostics.awaitIdle()
        assertEquals(emptyList<AgentListMismatch>(), reports)
    }
}

internal fun JsonObject.adapterInput(): AgentListAdapter.Input {
    fun strings(value: JsonObject, key: String) = value[key]?.jsonArray.orEmpty().map { it.jsonPrimitive.content }
    val preferences = getValue("pins").jsonObject.let { AgentPreferences(strings(it, "spaces"), strings(it, "sessions")) }
    val mutes = getValue("mutes").jsonObject.let {
        AgentPreferences(strings(it, "spaces"), strings(it, "sessions"), strings(it, "allowed").toSet())
    }
    val settings = getValue("settings").jsonObject
    fun strings(key: String) = settings[key]?.jsonArray.orEmpty().map { it.jsonPrimitive.content }.toSet()
    return AgentListAdapter.Input(
        sessions = getValue("sessions").jsonArray.map { AgentSession.fromJson(it.jsonObject) },
        machines = getValue("machines").jsonArray.map { Machine(it.jsonObject.getValue("id").jsonPrimitive.content, it.jsonObject.getValue("name").jsonPrimitive.content) },
        pins = preferences,
        mutes = mutes,
        settings = AgentListSettings(
            grouping = when (settings.getValue("grouping").jsonPrimitive.content) {
                "state" -> AgentGrouping.State
                "space" -> AgentGrouping.Space
                "machine" -> AgentGrouping.Machine
                else -> error("Unsupported fixture grouping")
            },
            quick = when (settings.getValue("quick").jsonPrimitive.content) {
                "all" -> AgentQuickFilter.All
                "needsYou" -> AgentQuickFilter.NeedsYou
                "unread" -> AgentQuickFilter.Unread
                "working" -> AgentQuickFilter.Working
                "backgroundWaiting" -> AgentQuickFilter.BackgroundWaiting
                else -> error("Unsupported fixture quick filter")
            },
            query = settings["query"]?.jsonPrimitive?.content.orEmpty(),
            machineFilter = settings["machine_filter"]?.jsonPrimitive?.content.orEmpty(),
            kept = strings("kept"),
            folded = strings("folded"),
            expandedIdle = strings("expanded_idle"),
            expandedQuiet = strings("expanded_quiet"),
            sort = if (settings["sort"]?.jsonPrimitive?.content == "updated") AgentSort.Updated else AgentSort.State,
        ),
        held = settings["held"]?.jsonPrimitive?.contentOrNull,
        now = settings["now"]?.jsonPrimitive?.contentOrNull?.let(Instant::parse) ?: Instant.EPOCH,
    )
}

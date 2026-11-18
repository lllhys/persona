package persona.core.rustcore

import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Replays the source-derived WebSocket envelope fixtures through the real host Kotlin/JNI boundary (#8955).
 * Expected objects come only from the unchanged fixture, never from the Rust projection. The raw input locator keeps
 * both members of duplicate keys; a normal JSON object would erase the boundary case before native code saw it.
 *
 * This is host-only diagnostic coverage: no Swift execution, event routing, production adapter or device claim.
 */
class WireEnvelopeBindingFixtureTest {
    private val root = generateSequence(File(".").canonicalFile) { it.parentFile }
        .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory }
        ?: error("Cannot locate the repository")
    private val file = root.resolve("spec/fixtures/wire-envelope/frames.json")
    private val document = Json.parseToJsonElement(file.readText()).jsonObject
    private val cases = document.getValue("cases").jsonArray.map { it.jsonObject }
    private val inputs = EnvelopeRawJson.inputs(file.readText())

    @Test fun everySourceDerivedCaseMatchesCompleteExpectedObjectThroughJni() {
        assertEquals(1, document.getValue("schema_version").jsonPrimitive.content.toInt())
        assertEquals("server_event_envelope", document.getValue("operation").jsonPrimitive.content)
        assertEquals("ios_current", document.getValue("profile").jsonPrimitive.content)
        assertEquals("exact ordered case manifest", CASES, cases.map {
            it.getValue("name").jsonPrimitive.content
        })
        assertEquals("a raw input per parsed case", cases.size, inputs.size)
        val replayed = mutableSetOf<String>()
        for ((case, input) in cases.zip(inputs)) {
            val name = case.getValue("name").jsonPrimitive.content
            assertTrue("$name: description", case.getValue("description").jsonPrimitive.content.isNotEmpty())
            assertTrue("$name: source", case.getValue("source").jsonArray.any {
                it.jsonPrimitive.content.contains(".swift")
            })
            assertTrue("$name: hand-derived", case.getValue("derived").jsonPrimitive.content.toBoolean())
            val actual = project(input)
            assertExpected(name, case.getValue("expected"), actual)
            assertTrue("$name: duplicate replay", replayed.add(name))
        }
        assertEquals(CASES.toSet(), replayed)
        println("JNI wire-envelope host replay: ${replayed.size} complete source-derived objects")
    }

    @Test fun rawLocatorPreservesDuplicateMembersForNativeReplay() {
        val duplicateFeatures = inputs[cases.indexOfFirst {
            it.getValue("name").jsonPrimitive.content == "hello_features_keep_first_duplicate"
        }]
        assertTrue(Regex("\"features\"").findAll(duplicateFeatures).count() == 2)
        assertEquals(buildJsonObject {
            put("outcome", "accepted")
            put("event", "hello")
            put("features", kotlinx.serialization.json.JsonArray(listOf(
                kotlinx.serialization.json.JsonPrimitive("first"),
            )))
        }, project(duplicateFeatures))
        val duplicateType = inputs[cases.indexOfFirst {
            it.getValue("name").jsonPrimitive.content == "items_duplicate_type_keeps_first"
        }]
        assertTrue(Regex("\"type\"").findAll(duplicateType).count() == 2)
        assertEquals("items", project(duplicateType).jsonObject.getValue("event").jsonPrimitive.content)
    }

    @Test fun malformedInputAndWrongExpectationsAreRejected() {
        assertEquals("", NativeBridge.projectWireEnvelope("not JSON"))
        assertEquals("", NativeBridge.projectWireEnvelope("{\"type\":\"inbox\""))
        val actual = project("{\"type\":\"inbox\"}")
        val wrong = buildJsonObject {
            put("outcome", "ignored")
            put("type", "inbox")
        }
        assertNotEquals(wrong, actual)
        var failed = false
        try {
            assertExpected("deliberately_wrong", wrong, actual)
        } catch (_: AssertionError) {
            failed = true
        }
        assertTrue("a wrong complete expectation must fail", failed)
    }

    private fun project(input: String): JsonElement {
        val output = NativeBridge.projectWireEnvelope(input)
        assertTrue("JNI returned no envelope projection", output.isNotEmpty())
        return Json.parseToJsonElement(output)
    }

    private fun assertExpected(name: String, expected: JsonElement, actual: JsonElement) {
        assertEquals(name, expected, actual)
    }

    private companion object {
        val CASES = listOf(
            "hello_defaults_to_empty_features",
            "hello_features_null_defaults_to_empty",
            "hello_wrong_features_type_drops_event",
            "hello_features_keep_first_duplicate",
            "hello_unknown_field_is_ignored",
            "inbox_shared_scalars_check_even_when_unused",
            "hello_wrong_from_seq_drops_event",
            "hello_rows_must_be_integer",
            "hello_wrong_build_type_drops_event",
            "hello_raw_manager_accepts_mixed_json",
            "hello_raw_account_boolean_is_not_string",
            "inbox_raw_json_fractional_number_is_accepted",
            "inbox_raw_json_finite_large_integer_is_accepted",
            "hello_null_optional_scalars_are_absent",
            "hello_public_url_is_not_rewritten",
            "hello_non_object_is_not_an_event",
            "inbox_requires_only_type",
            "inbox_null_known_fields_are_ignored",
            "inbox_wrong_items_type_drops_event",
            "unknown_type_is_ignored_after_valid_envelope",
            "unknown_type_wrong_backlog_drops_event",
            "unknown_type_still_checks_known_keys",
            "missing_type_is_dropped",
            "null_type_is_dropped",
            "numeric_type_is_dropped",
            "items_defaults_optional_fields",
            "items_null_array_and_flags_default",
            "items_missing_machine_id_drops_event",
            "items_missing_session_id_drops_event",
            "items_empty_session_ids_are_kept",
            "items_from_seq_must_be_integer",
            "items_backlog_must_be_boolean",
            "items_wrong_other_event_key_drops_event",
            "items_data_must_be_string_or_null",
            "items_duplicate_type_keeps_first",
            "items_raw_args_accepts_object",
            "items_invalid_item_scalar_drops_event",
            "items_unprojected_number_never_panics",
            "mesh_non_object_is_lenient",
            "mesh_bad_hubs_element_drops_only_hubs",
            "mesh_valid_hubs_keep_identity",
            "mesh_lenient_hub_field_keeps_identity",
            "mesh_lenient_scalar_fields_keep_identity",
            "mesh_wrong_outer_key_type_drops_event",
            "inbox_fleet_tolerates_bad_stats",
            "inbox_fleet_bad_required_scalar_drops_event",
            "inbox_fleet_missing_item_id_drops_event",
            "inbox_fleet_bad_look_drops_only_look",
            "inbox_fleet_bad_package_drops_whole_look",
            "inbox_fleet_phase_and_kind_are_lenient",
            "inbox_fleet_non_string_kind_large_integer_defaults_to_screen",
            "inbox_fleet_lenient_item_fields_keep_event",
            "inbox_fleet_lenient_slots_keep_event",
            "inbox_fleet_bad_loads_drops_only_loads",
            "inbox_fleet_fractional_since_is_a_double",
            "inbox_fleet_string_since_drops_event",
            "fleet_requires_fleet_payload",
            "fleet_valid_payload_projects_counts",
            "screen_missing_session_id_drops_event",
            "screen_unknown_status_falls_back_to_unknown",
            "screen_null_status_falls_back_to_unknown",
            "screen_numeric_status_drops_event",
            "sessions_list_requires_revision",
            "sessions_list_keeps_revision",
        )
    }
}

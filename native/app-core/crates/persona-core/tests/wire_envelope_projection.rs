//! Replays the complete source-derived WebSocket envelope expectations (#8955).
//!
//! Inputs are RawValue source text, not a map round-trip: the two duplicate-key cases must reach the decoder with
//! both members intact. Expected values were hand-derived from the cited Swift sources, never from this projection.
//! No Swift runtime, production adapter or device result is claimed.

use std::collections::BTreeSet;
use std::path::Path;

use persona_core::wire_envelope;
use serde::Deserialize;
use serde_json::Value;
use serde_json::value::RawValue;

#[derive(Deserialize)]
struct Document {
    schema_version: u32,
    operation: String,
    profile: String,
    cases: Vec<Case>,
}

#[derive(Deserialize)]
struct Case {
    name: String,
    description: String,
    source: Vec<String>,
    derived: bool,
    input: Box<RawValue>,
    expected: Value,
}

const CASES: [&str; 64] = [
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
];

#[test]
fn all_wire_envelope_cases_match_complete_expected_objects() {
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../spec/fixtures/wire-envelope");
    let mut files = std::fs::read_dir(root)
        .expect("wire-envelope fixture directory")
        .map(|entry| entry.expect("directory entry").path())
        .filter(|path| path.extension().is_some_and(|extension| extension == "json"))
        .collect::<Vec<_>>();
    files.sort();
    assert_eq!(files.len(), 1, "the fixture file manifest changed");
    assert_eq!(files[0].file_name().unwrap(), "frames.json");
    let document: Document = serde_json::from_slice(&std::fs::read(&files[0]).unwrap()).unwrap();
    assert_eq!(document.schema_version, 1);
    assert_eq!(document.operation, "server_event_envelope");
    assert_eq!(document.profile, "ios_current");
    let expected = CASES.into_iter().map(str::to_owned).collect::<Vec<_>>();
    let actual_names = document.cases.iter().map(|case| case.name.clone()).collect::<Vec<_>>();
    assert_eq!(actual_names, expected, "wire-envelope ordered case manifest changed");
    assert_eq!(
        CASES.into_iter().collect::<BTreeSet<_>>().len(),
        CASES.len(),
        "manifest duplicates"
    );
    let expected = CASES.into_iter().map(str::to_owned).collect::<BTreeSet<_>>();
    let mut replayed = BTreeSet::new();
    for case in document.cases {
        assert!(!case.description.is_empty(), "{}: description", case.name);
        assert!(!case.source.is_empty(), "{}: source", case.name);
        assert!(
            case.source.iter().any(|source| source.contains(".swift")),
            "{}: Swift source",
            case.name
        );
        assert!(case.derived, "{}: hand-derived marker", case.name);
        let actual = wire_envelope::project(case.input.get().as_bytes())
            .unwrap_or_else(|| panic!("{}: valid fixture input", case.name));
        assert_eq!(actual, case.expected, "{}: complete expected object", case.name);
        assert!(replayed.insert(case.name.clone()), "duplicate case: {}", case.name);
    }
    assert_eq!(replayed, expected, "wire-envelope fixture manifest changed");
    eprintln!(
        "wire-envelope projection: {} complete source-derived objects",
        replayed.len()
    );
}

#[test]
fn comparison_rejects_a_wrong_expected_object() {
    let actual = wire_envelope::project(br#"{"type":"inbox"}"#).unwrap();
    assert_ne!(actual, serde_json::json!({"outcome":"ignored","type":"inbox"}));
}

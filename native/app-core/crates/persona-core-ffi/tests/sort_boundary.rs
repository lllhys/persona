//! Replays the sort corpus through the exported C ABI, including ownership of its returned buffer.
//! This is a Rust caller of that ABI, not a separately compiled C client or a Swift test.

use std::collections::BTreeSet;
use std::fs;
use std::path::{Path, PathBuf};
use std::slice;

use persona_core::{persona_core_bytes_free, persona_core_project_sort};
use serde_json::Value;

fn fixture_dir() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../spec/fixtures/sort")
}

fn project(input: &[u8]) -> Option<Vec<u8>> {
    let bytes = unsafe { persona_core_project_sort(input.as_ptr(), input.len()) };
    if bytes.data.is_null() {
        assert_eq!((bytes.length, bytes.capacity), (0, 0));
        return None;
    }
    let output = unsafe { slice::from_raw_parts(bytes.data, bytes.length) }.to_vec();
    unsafe { persona_core_bytes_free(bytes) };
    Some(output)
}

#[test]
fn c_abi_replays_the_exact_projection_cases_and_rejects_bad_input() {
    let mut paths: Vec<PathBuf> = fs::read_dir(fixture_dir())
        .expect("sort fixtures")
        .map(|entry| entry.expect("directory entry").path())
        .filter(|path| path.extension().is_some_and(|extension| extension == "json"))
        .collect();
    paths.sort();
    let files: BTreeSet<String> = paths
        .iter()
        .map(|path| path.file_name().unwrap().to_string_lossy().into_owned())
        .collect();
    assert_eq!(
        files,
        BTreeSet::from([
            "filters.json".to_owned(),
            "folding.json".to_owned(),
            "grouping.json".to_owned(),
            "held.json".to_owned(),
            "hub_state.json".to_owned(),
            "ordering.json".to_owned(),
            "session_state.json".to_owned(),
        ])
    );
    let mut replayed = BTreeSet::new();
    for path in paths {
        let document: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
        if document["operation"] != "agents_list" {
            continue;
        }
        for case in document["cases"].as_array().unwrap() {
            let name = case["name"].as_str().unwrap().to_owned();
            let input = serde_json::to_vec(&case["input"]).unwrap();
            let actual: Value = serde_json::from_slice(&project(&input).expect(&name)).expect(&name);
            assert_eq!(actual, case["expected"], "C ABI: {name}");
            assert!(replayed.insert(name), "duplicate projection case");
        }
    }
    assert_eq!(
        replayed,
        BTreeSet::from([
            "active_space_uses_rank_not_slots".to_owned(),
            "compact_expands_idle_without_reordering".to_owned(),
            "cross_machine_same_session_id".to_owned(),
            "cross_machine_same_session_id_reversed".to_owned(),
            "empty_list".to_owned(),
            "filter_all".to_owned(),
            "filter_needsyou".to_owned(),
            "filter_unread".to_owned(),
            "filter_working".to_owned(),
            "folded_machine_hides_rows".to_owned(),
            "folded_space_keeps_counts".to_owned(),
            "folded_state_and_expanded_idle".to_owned(),
            "held_quiet_row_keeps_unread_state_section".to_owned(),
            "held_quiet_space_stays_active".to_owned(),
            "held_row_stays_out_of_quiet_idle_fold".to_owned(),
            "kept_cannot_escape_base".to_owned(),
            "machine_search_before_counts".to_owned(),
            "machine_uses_registry_order".to_owned(),
            "mute_allow_and_explicit_override".to_owned(),
            "pinned_offline_is_visible".to_owned(),
            "pinned_sessions_use_pin_order_once".to_owned(),
            "quick_filter_ignores_rank".to_owned(),
            "quick_spaces_use_updated_time".to_owned(),
            "rank_all_states_and_mutes".to_owned(),
            "search_expands_idle".to_owned(),
            "space_header_counts".to_owned(),
            "space_time_tie".to_owned(),
            "space_time_tie_reversed".to_owned(),
            "spaces_pin_busy_muted_quiet".to_owned(),
            "unread_keeps_seen_rows".to_owned(),
            "unread_reselected_drops_seen_rows".to_owned(),
            "working_since_then_id".to_owned(),
            "working_since_then_id_reversed".to_owned(),
        ]),
        "the C ABI boundary must pin the exact projection names"
    );
    assert_eq!(project(b"{\"invalid\":true}"), None);
    assert_eq!(project(&[0xff]), None);
    assert_eq!(project(b""), None);
    eprintln!("C ABI sort replay: {} cases, full expected equality", replayed.len());
}

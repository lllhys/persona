//! Replays every current-iOS StreamChunks split fixture with exact expected-object equality.
//! No expected value or prefix growth result is generated from the Rust projection.

use std::collections::BTreeSet;
use std::fs;
use std::path::{Path, PathBuf};

use persona_core::stream_chunks::Input;
use serde_json::Value;

fn fixture_dir() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../spec/fixtures/stream-chunks")
}

#[test]
fn all_stream_chunk_cases_match_complete_expected_objects() {
    let paths: Vec<PathBuf> = fs::read_dir(fixture_dir())
        .expect("stream-chunks fixture directory")
        .map(|entry| entry.expect("directory entry").path())
        .filter(|path| path.extension().is_some_and(|extension| extension == "json"))
        .collect();
    assert_eq!(
        paths
            .iter()
            .map(|path| path.file_name().unwrap().to_string_lossy())
            .collect::<Vec<_>>(),
        ["split.json"],
        "stream-chunks fixture file set changed"
    );
    let document: Value = serde_json::from_slice(&fs::read(&paths[0]).unwrap()).unwrap();
    assert_eq!(document["schema_version"], 1);
    assert_eq!(document["operation"], "stream_chunks_split");
    let cases = document["cases"].as_array().expect("cases");
    assert!(!cases.is_empty());

    let expected_names: BTreeSet<String> = [
        "blank_line_at_end_is_not_cut_yet",
        "carriage_return_is_not_a_line_separator",
        "consecutive_blank_lines_keep_their_newline",
        "crlf_carriage_return_is_not_a_blank_line",
        "crlf_fence_is_not_recognized",
        "cuts_after_blank_line_once_next_block_starts",
        "empty_input",
        "indented_fence_preserves_original_spaces",
        "inline_backticks_do_not_open_a_fence",
        "keeps_indented_continuation_with_block",
        "language_suffixed_line_does_not_close_fence",
        "leading_blank_line_can_finish_an_empty_block",
        "longer_fence_ignores_shorter_inner_closer",
        "never_cuts_inside_code_fence",
        "non_ascii_space_is_not_raw_indentation",
        "prewarm_uses_the_tail",
        "reassembles_full_text",
        "space_padded_separator_is_omitted",
        "tab_indented_continuation_stays_with_block",
        "tilde_fence_requires_tilde_closer",
        "unclosed_fence_keeps_remainder_in_tail",
    ]
    .into_iter()
    .map(str::to_owned)
    .collect();
    let mut replayed = BTreeSet::new();
    for case in cases {
        let name = case["name"].as_str().expect("case name");
        assert!(case["description"].is_string(), "{name}: description");
        assert!(!case["source"].as_array().expect("source").is_empty(), "{name}: source");
        assert_eq!(
            case.get("from_test").is_some(),
            case.get("derived").is_none(),
            "{name}: provenance"
        );
        if let Some(derived) = case.get("derived") {
            assert_eq!(derived, true, "{name}: derived marker");
        } else {
            assert!(case["from_test"].is_string(), "{name}: from_test");
        }
        let input: Input = serde_json::from_value(case["input"].clone()).expect(name);
        let chunks = persona_core::stream_chunks::split(&input);
        let actual = serde_json::to_value(&chunks).expect("chunks JSON");
        assert_eq!(&actual, &case["expected"], "{name}: complete expected object");
        if name != "space_padded_separator_is_omitted" {
            // Apple drops this whitespace-only separator rather than normalizing it to one LF.
            let rebuilt = chunks
                .done
                .into_iter()
                .chain([chunks.tail])
                .collect::<Vec<_>>()
                .join("\n\n");
            assert_eq!(rebuilt, input.text, "{name}: reconstruction invariant");
        }
        assert!(replayed.insert(name.to_owned()), "duplicate case: {name}");
    }
    assert_eq!(replayed, expected_names, "stream-chunks fixture manifest changed");
    eprintln!("stream-chunks projection: {} full expected objects", replayed.len());
}

//! Replays the answer, stream-chunk, stream-repair, turn, task-index, viewed-image and recents corpora through the
//! exported C ABI, including each returned buffer. The independently compiled C11 client (`tests/c_abi_smoke.c`)
//! exercises the
//! same boundary from outside Rust; these tests pin the contract for a Rust caller, not for Swift or a device.

use std::collections::BTreeSet;
use std::fs;
use std::path::{Path, PathBuf};
use std::slice;

use persona_core::{
    PersonaCoreBytes, persona_core_bytes_free, persona_core_display_answer, persona_core_project_answer,
    persona_core_project_plain_open_fence, persona_core_project_recents, persona_core_project_stream_chunks,
    persona_core_project_stream_repair, persona_core_project_task_index, persona_core_project_turns,
    persona_core_project_viewed_images,
};
use serde_json::Value;

fn fixtures() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../spec/fixtures")
}

fn call(projection: unsafe extern "C" fn(*const u8, usize) -> PersonaCoreBytes, input: &[u8]) -> Option<Vec<u8>> {
    let bytes = unsafe { projection(input.as_ptr(), input.len()) };
    if bytes.data.is_null() {
        assert_eq!((bytes.length, bytes.capacity), (0, 0));
        return None;
    }
    let output = unsafe { slice::from_raw_parts(bytes.data, bytes.length) }.to_vec();
    unsafe { persona_core_bytes_free(bytes) };
    Some(output)
}

fn project(projection: unsafe extern "C" fn(*const u8, usize) -> PersonaCoreBytes, input: &Value) -> Value {
    let input = serde_json::to_vec(input).unwrap();
    let output = call(projection, &input).expect("the C ABI returned no projection");
    serde_json::from_slice(&output).expect("the C ABI returned non-JSON bytes")
}

#[test]
fn c_abi_answer_replays_questions_and_recommendations_and_rejects_bad_input() {
    let mut replayed = BTreeSet::new();
    for (file, recommended, count) in [("questions.json", false, 15), ("recommended.json", true, 9)] {
        let document: Value = serde_json::from_slice(&fs::read(fixtures().join("answer").join(file)).unwrap()).unwrap();
        let cases = document["cases"].as_array().unwrap();
        assert_eq!(cases.len(), count, "{file}: exact case count");
        for case in cases {
            let name = case["name"].as_str().unwrap().to_owned();
            let input = &case["input"];
            let transition = project(persona_core_project_answer, input);
            let operation = &input["operation"];
            let mut actual = serde_json::json!({
                "allowed": transition["allowed"],
                "resolution": transition["resolution"],
            });
            if transition["page"] != operation["page"] {
                actual["next_page"] = transition["page"].clone();
            }
            let question = &input["interaction"]["questions"][operation["page"].as_u64().unwrap() as usize];
            if operation["action"] == "pick_option"
                && transition["allowed"] == Value::Bool(true)
                && question["multi_select"] == Value::Bool(true)
            {
                actual["picks"] = transition["picks"].clone();
            }
            if recommended {
                let label = question["options"][operation["option_index"].as_u64().unwrap() as usize]["label"]
                    .as_str()
                    .unwrap();
                let display = project(persona_core_display_answer, &Value::String(label.to_owned()));
                actual["display"] = display;
            }
            assert_eq!(actual, case["expected"], "C ABI answer: {name}");
            assert!(replayed.insert(name), "duplicate answer case");
        }
    }
    assert_eq!(replayed.len(), 24, "15 question + 9 recommendation cases");
    assert!(call(persona_core_project_answer, b"{\"invalid\":true}").is_none());
    assert!(call(persona_core_project_answer, b"not JSON").is_none());
    assert!(call(persona_core_project_answer, &[0xff]).is_none());
    assert!(call(persona_core_display_answer, b"{\"label\":1}").is_none());
    eprintln!("C ABI answer replay: {} complete expected objects", replayed.len());
}

#[test]
fn c_abi_stream_chunks_replays_all_cases_and_rejects_bad_input() {
    let document: Value =
        serde_json::from_slice(&fs::read(fixtures().join("stream-chunks/split.json")).unwrap()).unwrap();
    let cases = document["cases"].as_array().unwrap();
    assert_eq!(cases.len(), 21);
    let mut replayed = BTreeSet::new();
    for case in cases {
        let name = case["name"].as_str().unwrap().to_owned();
        let actual = project(persona_core_project_stream_chunks, &case["input"]);
        assert_eq!(actual, case["expected"], "C ABI stream_chunks: {name}");
        assert!(replayed.insert(name), "duplicate stream case");
    }
    assert_eq!(replayed.len(), 21);
    assert!(call(persona_core_project_stream_chunks, b"{\"invalid\":true}").is_none());
    assert!(call(persona_core_project_stream_chunks, b"not JSON").is_none());
    eprintln!(
        "C ABI stream_chunks replay: {} complete {{done, tail}} objects",
        replayed.len()
    );
}

#[test]
fn c_abi_stream_repair_calls_the_projection_and_rejects_bad_utf8_and_expectations() {
    // Independently transcribed from StreamRepairTests.closesAnOpenCodeFenceWithItsOwnMarker and
    // tellsWhereTheTextEnds; the second field is not derived from the projected output.
    let expected = serde_json::json!({
        "repaired": "text\n```ts\nconst a\n```",
        "end": "code",
    });
    let input = serde_json::json!({"text": "text\n```ts\nconst a"});
    let actual = project(persona_core_project_stream_repair, &input);
    assert_eq!(actual, expected);
    let mut wrong_end = expected.clone();
    wrong_end["end"] = serde_json::json!("text");
    assert_ne!(actual, wrong_end);
    let mut wrong_repaired = expected.clone();
    wrong_repaired["repaired"] = serde_json::json!("wrong");
    assert_ne!(actual, wrong_repaired);
    let mut extra = expected.clone();
    extra
        .as_object_mut()
        .unwrap()
        .insert("extra".to_owned(), Value::Bool(true));
    assert_ne!(actual, extra);
    assert!(call(persona_core_project_stream_repair, b"{\"invalid\":true}").is_none());
    assert!(call(persona_core_project_stream_repair, b"not JSON").is_none());
    assert!(call(persona_core_project_stream_repair, &[0xff]).is_none());
    eprintln!("C ABI stream_repair: Rust caller, complete object, invalid UTF-8 and negatives");
}

#[test]
fn c_abi_plain_open_fence_uses_the_independent_apple_expectation_and_rejects_bad_inputs() {
    let input = serde_json::json!({
        "text": "看图：\n```mermaid\ngraph TD",
        "drawn": ["mermaid", "math"],
    });
    let expected = serde_json::json!({"text": "看图：\n```\ngraph TD"});
    let actual = project(persona_core_project_plain_open_fence, &input);
    assert_eq!(actual, expected);
    let mut wrong = expected.clone();
    wrong["text"] = serde_json::json!("看图：\n```mermaid\ngraph TD");
    assert_ne!(actual, wrong);
    let mut extra = expected.clone();
    extra
        .as_object_mut()
        .unwrap()
        .insert("extra".to_owned(), Value::Bool(true));
    assert_ne!(actual, extra);
    for malformed in [
        &br#"{"text":"```mermaid"}"#[..],
        &br#"{"drawn":[]}"#[..],
        &br#"{"text":1,"drawn":[]}"#[..],
        b"not JSON",
        &[0xff],
    ] {
        assert!(call(persona_core_project_plain_open_fence, malformed).is_none());
    }
    eprintln!("C ABI plain_open_fence: independent Apple expectation, invalid UTF-8 and negatives");
}

fn lines_documents() -> Vec<Value> {
    let mut paths: Vec<PathBuf> = fs::read_dir(fixtures().join("lines"))
        .expect("lines fixtures")
        .map(|entry| entry.expect("directory entry").path())
        .filter(|path| path.extension().is_some_and(|extension| extension == "json"))
        .collect();
    paths.sort();
    assert_eq!(paths.len(), 9, "the lines file set is the manifest boundary");
    paths
        .iter()
        .map(|path| serde_json::from_slice(&fs::read(path).unwrap()).unwrap())
        .collect()
}

/// The current-iOS overlay, applied by the caller exactly as the Kotlin and C-client replays apply it.
fn expected_turns(case: &Value, overlay: &Value) -> Value {
    let name = case["name"].as_str().unwrap();
    let wake = &overlay["turn_wake_state"];
    let drop = wake["drop"].as_str().unwrap();
    let set = wake["set"].as_str().unwrap();
    let mut expected = case["expected"].clone();
    let turns = expected["turns"].as_array_mut().unwrap();
    for turn in turns.iter_mut() {
        let turn = turn.as_object_mut().unwrap();
        assert!(turn.remove(drop).is_some(), "{name}: stale wake drop");
        let id = turn["id"].as_str().unwrap();
        let pending = wake["pending"].as_array().unwrap().iter().any(|reference| {
            reference["case"] == Value::String(name.to_owned()) && reference["turn"] == Value::String(id.to_owned())
        });
        turn.insert(set.to_owned(), Value::Bool(pending));
    }
    for entry in overlay["cases"].as_array().unwrap() {
        if entry["name"] != Value::String(name.to_owned()) {
            continue;
        }
        if let Some(replace) = entry.get("replace") {
            for (key, value) in replace.as_object().unwrap() {
                expected[key] = value.clone();
            }
        }
        if let Some(fields) = entry.get("turn_fields") {
            for (id, patch) in fields.as_object().unwrap() {
                let mut matched = 0;
                for turn in expected["turns"].as_array_mut().unwrap() {
                    if turn["id"] == Value::String(id.clone()) {
                        matched += 1;
                        for (key, value) in patch.as_object().unwrap() {
                            turn[key] = value.clone();
                        }
                    }
                }
                assert_eq!(matched, 1, "{name}/{id}: the overlay must name exactly one turn");
            }
        }
    }
    expected["turns"].clone()
}

#[test]
fn c_abi_turns_replays_the_static_corpus_with_the_overlay() {
    let overlay: Value =
        serde_json::from_slice(&fs::read(fixtures().join("lines/ios/differences.json")).unwrap()).unwrap();
    let mut replayed = BTreeSet::new();
    let mut turn_objects = 0;
    for document in lines_documents() {
        for case in document["cases"].as_array().unwrap() {
            let name = case["name"].as_str().unwrap().to_owned();
            if case["expected"].get("checkpoints").is_some() || case["input"].get("operations").is_some() {
                continue;
            }
            let actual = project(persona_core_project_turns, &case["input"]);
            let expected = expected_turns(case, &overlay);
            assert_eq!(actual, expected, "C ABI turns: {name}");
            turn_objects += expected.as_array().unwrap().len();
            assert!(replayed.insert(name), "duplicate static turn case");
        }
    }
    assert_eq!(replayed.len(), 57, "the exact static turn manifest");
    assert_eq!(turn_objects, 73, "the 54 non-empty cases hold 73 complete turn objects");
    assert!(call(persona_core_project_turns, b"{\"invalid\":true}").is_none());
    assert!(call(persona_core_project_turns, b"not JSON").is_none());
    eprintln!(
        "C ABI turns replay: {} static cases, {turn_objects} turn objects",
        replayed.len()
    );
}

#[test]
fn c_abi_task_index_replays_all_cases_and_rejects_bad_input() {
    let mut replayed = BTreeSet::new();
    let mut receipt = false;
    for document in lines_documents() {
        for case in document["cases"].as_array().unwrap() {
            let name = case["name"].as_str().unwrap().to_owned();
            if case["input"].get("operations").is_some() {
                continue;
            }
            let expected = &case["expected"]["task_index"];
            let is_receipt = name == "background_running_badge";
            if expected.is_null() && !is_receipt {
                continue;
            }
            let actual = project(persona_core_project_task_index, &case["input"]);
            if is_receipt {
                assert_eq!(actual["started"], serde_json::json!(["call_a"]));
                assert_eq!(actual["calls"], serde_json::json!({}));
                assert_eq!(actual["statuses"], serde_json::json!({}));
                assert_eq!(actual["started_at"], serde_json::json!({}));
                receipt = true;
                continue;
            }
            assert_eq!(actual, *expected, "C ABI task_index: {name}");
            assert!(replayed.insert(name), "duplicate task-index case");
        }
    }
    assert_eq!(replayed.len(), 5, "the exact five-case task-index manifest");
    assert!(receipt, "the background receipt boundary must be replayed");
    assert!(call(persona_core_project_task_index, b"{\"invalid\":true}").is_none());
    assert!(call(persona_core_project_task_index, b"not JSON").is_none());
    eprintln!(
        "C ABI task_index replay: {} complete expected objects plus the receipt",
        replayed.len()
    );
}

#[test]
fn c_abi_recents_replays_all_cases_and_rejects_bad_inputs() {
    let names = [
        (
            "recent_conversations",
            vec![
                "open_into_empty",
                "open_goes_first",
                "open_again_moves_to_front",
                "open_the_first_again",
                "keeps_ten",
                "ignores_empty",
                "shown_skips_the_current",
                "shown_skips_the_missing",
                "shown_up_to_the_limit",
                "shown_without_a_current",
                "shown_nothing_else",
            ],
        ),
        (
            "recent_setups",
            vec![
                "record_into_empty",
                "record_moves_the_same_setup_first",
                "record_keeps_twenty",
                "record_differs_by_model",
                "cards_skip_unknown_machines",
                "cards_at_most_three",
                "cards_offline_shown",
                "model_of_the_latest_same_launcher",
                "model_other_launcher_is_default",
                "model_unknown_machine",
            ],
        ),
    ];
    let mut replayed = BTreeSet::new();
    for (domain, expected_names) in names {
        let document: Value =
            serde_json::from_slice(&fs::read(fixtures().join("recents").join(format!("{domain}.json"))).unwrap())
                .unwrap();
        assert_eq!(document["schema_version"], 1);
        let cases = document["cases"].as_array().unwrap();
        let actual_names: Vec<_> = cases.iter().map(|case| case["name"].as_str().unwrap()).collect();
        assert_eq!(actual_names, expected_names, "{domain}: exact case manifest");
        for case in cases {
            let name = case["name"].as_str().unwrap().to_owned();
            assert!(replayed.insert(name.clone()), "duplicate recents case: {name}");
            let input = serde_json::json!({
                "domain": domain,
                "operation": case["operation"],
                "input": case["input"],
            });
            let actual = project(persona_core_project_recents, &input);
            assert_eq!(actual, case["expected"], "C ABI recents: {domain}/{name}");
        }
    }
    assert_eq!(replayed.len(), 21, "11 conversation + 10 setup cases");
    let first = serde_json::json!({
        "domain": "recent_conversations", "operation": "open",
        "input": {"before": [], "opened": "s:m1/a"},
    });
    let actual = project(persona_core_project_recents, &first);
    assert_eq!(actual, serde_json::json!({"after": ["s:m1/a"]}));
    assert_ne!(actual, serde_json::json!({"after": ["s:m1/wrong"]}));
    for input in [
        serde_json::json!({}),
        serde_json::json!({"domain": "recent_conversations", "operation": "open"}),
        serde_json::json!({"domain": "recent_conversations", "operation": "unknown", "input": {}}),
        serde_json::json!({"domain": "unknown", "operation": "open", "input": {}}),
        serde_json::json!({"domain": 1, "operation": "open", "input": {}}),
        serde_json::json!({"domain": "recent_conversations", "operation": 1, "input": {}}),
        serde_json::json!({"domain": "recent_conversations", "operation": "open", "input": []}),
        serde_json::json!({"domain": "recent_conversations", "operation": "open", "input": {}}),
        serde_json::json!({
            "domain": "recent_conversations", "operation": "open", "input": {"before": 1, "opened": "s:m1/a"},
        }),
    ] {
        let input = serde_json::to_vec(&input).unwrap();
        assert!(
            call(persona_core_project_recents, &input).is_none(),
            "must reject: {input:?}"
        );
    }
    assert!(call(persona_core_project_recents, b"not JSON").is_none());
    assert!(call(persona_core_project_recents, &[0xff]).is_none());
    eprintln!("C ABI recents replay: 11 conversation + 10 setup complete objects; negatives rejected");
}

#[test]
fn c_abi_viewed_images_preserves_the_complete_overlay_line_and_rejects_bad_input() {
    let overlay: Value =
        serde_json::from_slice(&fs::read(fixtures().join("lines/ios/differences.json")).unwrap()).unwrap();
    let case = lines_documents()
        .into_iter()
        .flat_map(|document| document["cases"].as_array().unwrap().clone())
        .find(|case| case["name"] == "tool_images_belong_to_call")
        .expect("the ordinary viewed-image case must exist");
    let expected = overlay["cases"]
        .as_array()
        .unwrap()
        .iter()
        .find(|entry| entry["name"] == case["name"])
        .and_then(|entry| entry["replace"]["lines"].as_array())
        .unwrap()
        .iter()
        .find(|line| line["type"] == "viewed")
        .unwrap();
    let actual = project(persona_core_project_viewed_images, &case["input"]);
    assert_eq!(actual, *expected);
    assert_ne!(
        actual,
        serde_json::json!({
            "id": "vi-wrong",
            "image_ids": expected["image_ids"],
            "is_input": false,
            "type": "viewed",
        })
    );
    assert!(call(persona_core_project_viewed_images, b"{\"invalid\":true}").is_none());
    assert!(call(persona_core_project_viewed_images, b"not JSON").is_none());
    assert!(call(persona_core_project_viewed_images, b"{\"items\":[]}").is_none());
}

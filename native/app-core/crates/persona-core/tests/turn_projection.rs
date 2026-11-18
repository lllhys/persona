//! Replays the static `lines` corpus's complete turn objects through the pure row/turn projection.

use std::collections::BTreeSet;
use std::fs;
use std::path::{Path, PathBuf};

use persona_core::turns::Input;
use serde_json::Value;

fn current_apple_expected(case: &Value, overlay: &Value, apply_case_patch: bool) -> Value {
    let mut expected = case["expected"].clone();
    let wake = &overlay["turn_wake_state"];
    for turn in expected["turns"].as_array_mut().unwrap() {
        let pending = wake["pending"]
            .as_array()
            .unwrap()
            .iter()
            .any(|entry| entry["case"] == case["name"] && entry["turn"] == turn["id"]);
        let object = turn.as_object_mut().unwrap();
        assert!(object.contains_key(wake["drop"].as_str().unwrap()), "stale wake drop");
        assert!(!object.contains_key(wake["set"].as_str().unwrap()), "stale wake set");
        object.remove(wake["drop"].as_str().unwrap());
        object.insert(wake["set"].as_str().unwrap().to_owned(), Value::Bool(pending));
    }
    let name = case["name"].as_str().unwrap();
    if apply_case_patch
        && let Some(entry) = overlay["cases"]
            .as_array()
            .unwrap()
            .iter()
            .find(|entry| entry["name"] == name)
    {
        for key in entry.as_object().unwrap().keys() {
            assert!(
                ["name", "why", "source", "replace", "turn_fields"].contains(&key.as_str()),
                "unknown overlay entry key: {name}.{key}"
            );
        }
        assert!(entry["why"].is_string() && !entry["source"].as_array().unwrap().is_empty());
        for (key, value) in entry["replace"].as_object().into_iter().flatten() {
            assert!(
                expected.get(key).is_some_and(|old| old != value),
                "stale replace: {name}.{key}"
            );
            expected[key] = value.clone();
        }
        for (id, fields) in entry["turn_fields"].as_object().into_iter().flatten() {
            let indices: Vec<usize> = expected["turns"]
                .as_array()
                .unwrap()
                .iter()
                .enumerate()
                .filter_map(|(index, turn)| (turn["id"] == *id).then_some(index))
                .collect();
            assert_eq!(indices.len(), 1, "one {name}/{id} turn");
            let turn = &mut expected["turns"][indices[0]];
            for (key, value) in fields.as_object().unwrap() {
                assert!(
                    turn.get(key).is_some_and(|old| old != value),
                    "stale field: {name}/{id}.{key}"
                );
                turn[key] = value.clone();
            }
        }
    }
    expected["turns"].clone()
}

#[test]
fn exact_static_turn_manifest_and_full_expected_turns() {
    let mut paths: Vec<PathBuf> =
        fs::read_dir(Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../spec/fixtures/lines"))
            .unwrap()
            .map(|entry| entry.unwrap().path())
            .filter(|path| path.extension().is_some_and(|extension| extension == "json"))
            .collect();
    paths.sort();
    assert_eq!(paths.len(), 9, "the lines file set is the manifest boundary");
    let overlay_path =
        Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../spec/fixtures/lines/ios/differences.json");
    assert!(
        overlay_path.is_file(),
        "the repository's Apple replay overlay is required"
    );
    let overlay: Value = serde_json::from_slice(&fs::read(&overlay_path).unwrap_or_else(|error| {
        panic!(
            "current Apple differences from #8239 are required at {}: {error}",
            overlay_path.display()
        )
    }))
    .unwrap();
    let mut replayed = BTreeSet::new();
    let mut all_names = BTreeSet::new();
    let mut all_turns = BTreeSet::new();
    let mut patch_names = BTreeSet::new();
    for entry in overlay["cases"].as_array().unwrap() {
        assert!(
            patch_names.insert(entry["name"].as_str().unwrap().to_owned()),
            "duplicate overlay case"
        );
    }
    let mut total = 0;
    for path in paths {
        let document: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
        for case in document["cases"].as_array().unwrap() {
            total += 1;
            let name = case["name"].as_str().unwrap().to_owned();
            assert!(all_names.insert(name.clone()), "duplicate corpus case: {name}");
            for state in case["expected"]["checkpoints"]
                .as_array()
                .map_or_else(|| std::slice::from_ref(&case["expected"]), Vec::as_slice)
            {
                for turn in state["turns"].as_array().into_iter().flatten() {
                    all_turns.insert((name.clone(), turn["id"].as_str().unwrap().to_owned()));
                }
            }
            if case["expected"].get("checkpoints").is_some() || case["input"].get("operations").is_some() {
                continue;
            }
            let name = case["name"].as_str().unwrap().to_owned();
            let input: Input = serde_json::from_value(case["input"].clone()).expect(&name);
            let actual = persona_core::turns::project(&input);
            assert_eq!(
                actual,
                current_apple_expected(case, &overlay, true),
                "current Apple turn projection from #8239's replay: {}/{}",
                path.display(),
                name
            );
            if patch_names.contains(name.as_str()) {
                assert_ne!(
                    actual,
                    current_apple_expected(case, &overlay, false),
                    "unnecessary overlay case entry: {name}"
                );
            }
            assert!(replayed.insert(name.clone()), "duplicate case: {name}");
        }
    }
    assert!(patch_names.is_subset(&all_names), "overlay names a nonexistent case");
    let mut pending_names = BTreeSet::new();
    for pending in overlay["turn_wake_state"]["pending"].as_array().unwrap() {
        let key = (
            pending["case"].as_str().unwrap().to_owned(),
            pending["turn"].as_str().unwrap().to_owned(),
        );
        assert!(pending_names.insert(key.clone()), "duplicate pending overlay reference");
        assert!(
            all_turns.contains(&key),
            "overlay names a nonexistent pending turn: {key:?}"
        );
    }
    assert_eq!(total, 61, "actual lines corpus size");
    let expected: BTreeSet<String> = [
        "absorbed_user_images",
        "background_running_badge",
        "closed_work_identity",
        "compaction_stays_inside_work",
        "empty_session",
        "empty_thinking_between_tools_is_retained",
        "failed_tool_counts_known_failure",
        "fold_counts_tools_and_thoughts",
        "frozen_snapshot_excludes_newer_items",
        "frozen_uses_numeric_block_order",
        "interrupt_is_trailing_notice",
        "last_result_for_call_wins",
        "late_result_attaches_to_original_call",
        "live_answer_after_wake",
        "live_empty_work",
        "live_queued_message_keeps_position",
        "live_reply_stays_after_work",
        "live_tail_runs_behind_chip",
        "long_answer_after_wake",
        "menu_owner_per_message",
        "missing_start_time_hides_duration",
        "missing_start_time_uses_first_step",
        "missing_tool_result_is_running",
        "monitor_event_uses_first_detail_line",
        "multiple_interactions_in_footer",
        "multiple_wakes_pick_latest_answer",
        "notification_without_wake",
        "older_page_stays_visible_during_freeze",
        "only_last_reply_in_each_wake_is_candidate",
        "open_queued_messages_work",
        "open_work_identity",
        "orphan_assistant",
        "orphan_tool_result_is_not_a_row",
        "orphan_wake_opens_with_notice",
        "ordinary_user_starts_next_turn",
        "photos_after_wake_stay_with_message",
        "plain_exchange",
        "queued_without_existing_turn",
        "recap_separates_turns",
        "regular_user_images",
        "short_reply_after_wake_is_the_answer",
        "single_interaction_in_footer",
        "standalone_empty_thinking_hidden",
        "task_notices_find_calls",
        "task_status_outcomes",
        "task_title_containing_interrupt_is_not_interrupt",
        "task_without_loaded_call_uses_title",
        "thinking_before_between_after_tools",
        "thinking_narration_preserved",
        "three_users_apart_without_steps",
        "timed_work_ends_at_answer",
        "tool_images_belong_to_call",
        "trailing_tool_after_answer",
        "unknown_item_is_ignored",
        "wake_after_recap_reopens_turn",
        "wake_without_any_reply",
        "finished_queued_messages_apart",
    ]
    .into_iter()
    .map(str::to_owned)
    .collect();
    assert_eq!(
        replayed, expected,
        "exact static turn set with the current-Apple differences applied; only four operation cases are excluded"
    );
    assert_eq!(replayed.len(), 57);
    assert!(replayed.contains("wake_after_recap_reopens_turn"));
    assert!(replayed.contains("finished_queued_messages_apart"));
    eprintln!(
        "turn projection: {} exact static cases, full expected turns equality",
        replayed.len()
    );
}

#[test]
fn choosing_the_first_answer_fails_the_wake_corpus() {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../spec/fixtures/lines/wakes.json");
    let document: Value = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
    let mut detected = 0;
    for case in document["cases"].as_array().unwrap() {
        let input: Input = serde_json::from_value(case["input"].clone()).unwrap();
        let actual = persona_core::turns::answer_mutation(&input);
        let expected = case["expected"]["turns"][0]["answer_id"].clone();
        if actual.as_ref().map(|id| Value::String(id.clone())).as_ref() != Some(&expected) {
            detected += 1;
        }
    }
    assert!(
        detected > 0,
        "a first-answer mutation must not satisfy every wake fixture"
    );
}

/// Expectations below are written from current Apple source/tests, not from this Rust implementation's output.
/// They supplement the unchanged corpus and checked-in current-iOS overlay with source-derived edge boundaries.
/// These assertions are not an executed Swift replay; full corpus acceptance also relies on #8429's macOS run.
mod apple_source_regressions {
    use persona_core::turns::Input;
    use serde_json::{Value, json};

    fn project(items: Value) -> Value {
        assert!(items.is_array(), "test helper expects an item array");
        let input: Input = serde_json::from_value(json!({"items": items})).unwrap();
        persona_core::turns::project(&input)
    }

    #[test]
    fn synthetic_supervision_json_shape() {
        let text = "📋 督办：你派出去的任务有 2 个要你处理\n这是 Persona 的督办消息";
        for marked in [false, true] {
            let items = json!([
                {"id":"u1#0","seq":1,"role":"user","kind":"text","text":"First real request"},
                {"id":"u2#0","seq":2,"role":"user","kind":"text","text":text,"queued":true,
                 "injected":if marked {json!({"state":"supervise"})} else {Value::Null}},
                {"id":"u3#0","seq":3,"role":"user","kind":"text","text":"Next real request","queued":true},
            ]);
            assert!(items.is_array(), "items: {items}");
            let turns = project(items);
            assert_eq!(turns.as_array().unwrap().len(), 3);
        }
    }

    #[test]
    fn same_entry_text_image_text_stays_in_users_and_a_later_real_input_is_separate() {
        let turns = project(json!([
            {"id":"u1#0","seq":1,"role":"user","kind":"text","text":"First real request"},
            {"id":"t2#0","seq":2,"role":"assistant","kind":"tool_use","tool_use_id":"call2"},
            {"id":"u3#0","seq":3,"role":"user","kind":"image"},
            {"id":"u3#1","seq":3,"role":"user","kind":"text","queued":true,
             "injected":{"state":"supervise"}},
            {"id":"u3#2","seq":3,"role":"user","kind":"text","text":"Extra fictional detail"},
            {"id":"t4#0","seq":4,"role":"assistant","kind":"tool_use","tool_use_id":"call4"},
            {"id":"u5#0","seq":5,"role":"user","kind":"text","text":"Next real request","queued":true},
        ]));
        assert_eq!(turns.as_array().unwrap().len(), 3);
        assert_eq!(turns[0]["id"], "u1#0");
        assert_eq!(turns[1]["id"], "u3#1");
        assert_eq!(turns[1]["user_ids"], json!(["u3#1", "u3#0", "u3#2"]));
        assert_eq!(turns[1]["process_ids"], json!(["tools-t4#0"]));
        assert_eq!(turns[2]["user_ids"], json!(["u5#0"]));
        assert_eq!(turns[1]["has_pending_wake"], false);

        let image_only = project(json!([
            {"id":"u1#0","seq":1,"role":"user","kind":"text","injected":{"state":"supervise"}},
            {"id":"u4#0","seq":4,"role":"user","kind":"image"},
            {"id":"a5#0","seq":5,"role":"assistant","kind":"text"},
        ]));
        assert_eq!(image_only.as_array().unwrap().len(), 2);
        assert_eq!(image_only[1]["user_ids"], json!(["u4#0"]));
    }

    #[test]
    fn frozen_cut_precedes_tool_pairing_and_ordered_timestamps() {
        let mut input: Input = serde_json::from_value(json!({
            "frozen":{"seq":2,"part":0},
            "items":[
                {"id":"u1#0","seq":1,"role":"user","kind":"text","ts":"2026-01-01T00:00:10Z"},
                {"id":"t2#0","seq":2,"role":"assistant","kind":"tool_use","tool_use_id":"call2","ts":"2026-01-01T00:00:20Z"},
                {"id":"r3#0","seq":3,"role":"user","kind":"tool_result","tool_use_id":"call2","ts":"2026-01-01T00:00:05Z"},
                {"id":"t4#0","seq":4,"role":"assistant","kind":"tool_use","tool_use_id":"call4","ts":"2026-01-01T00:00:07Z"},
                {"id":"a5#0","seq":5,"role":"assistant","kind":"text","ts":"2026-01-01T00:00:30Z"},
            ]
        })).unwrap();
        let turns = persona_core::turns::project(&input);
        assert_eq!(turns[0]["ordered_ids"], json!(["u1#0", "tools-t2#0"]));
        assert_eq!(turns[0]["process_ids"], json!(["tools-t2#0"]));
        assert_eq!(turns[0]["end"], "2026-01-01T00:00:20Z");

        input.frozen = None;
        let turns = persona_core::turns::project(&input);
        assert_eq!(turns[0]["ordered_ids"], json!(["u1#0", "tools-t2#0", "a5#0"]));
        assert_eq!(turns[0]["process_ids"], json!(["tools-t2#0"]));
        assert_eq!(turns[0]["start"], "2026-01-01T00:00:10Z");
        assert_eq!(turns[0]["end"], "2026-01-01T00:00:30Z");
    }

    #[test]
    fn latest_untimestamped_result_falls_back_to_the_call_time() {
        let turns = project(json!([
            {"id":"u1#0","seq":1,"role":"user","kind":"text"},
            {"id":"t2#0","seq":2,"role":"assistant","kind":"tool_use","tool_use_id":"call2","ts":"2026-01-01T00:00:01Z"},
            {"id":"r3#0","seq":3,"role":"user","kind":"tool_result","tool_use_id":"call2","ts":"2026-01-01T00:00:05Z"},
            {"id":"r4#0","seq":4,"role":"user","kind":"tool_result","tool_use_id":"call2"},
        ]));
        assert_eq!(turns[0]["end"], "2026-01-01T00:00:01Z");
    }

    #[test]
    fn a_wake_opens_its_own_turn_and_the_turn_before_it_keeps_its_answer() {
        let turns = project(json!([
            {"id":"u1#0","seq":1,"role":"user","kind":"text"},
            {"id":"a2#0","seq":2,"role":"assistant","kind":"text"},
            {"id":"w3#0","seq":3,"role":"system","kind":"notice","wake":true},
            {"id":"a4#0","seq":4,"role":"assistant","kind":"text"},
        ]));
        assert_eq!(turns.as_array().unwrap().len(), 2, "{turns}");
        assert_eq!(turns[0]["ordered_ids"], json!(["u1#0", "a2#0"]));
        assert_eq!(turns[0]["answer_id"], "a2#0");
        assert_eq!(turns[0]["wake_ids"], json!([]));
        assert_eq!(turns[1]["user_ids"], json!([]));
        assert_eq!(turns[1]["ordered_ids"], json!(["w3#0", "a4#0"]));
        assert_eq!(turns[1]["wake_ids"], json!(["w3#0"]));
        assert_eq!(turns[1]["answer_id"], "a4#0");

        // Woken and at work with no words yet: the answer before the wake is untouched, the new work is the woken turn's.
        let turns = project(json!([
            {"id":"u1#0","seq":1,"role":"user","kind":"text"},
            {"id":"a2#0","seq":2,"role":"assistant","kind":"text","ts":"2026-01-01T00:00:02Z"},
            {"id":"w3#0","seq":3,"role":"system","kind":"notice","wake":true,"ts":"2026-01-01T00:00:03Z"},
            {"id":"t4#0","seq":4,"role":"assistant","kind":"tool_use","ts":"2026-01-01T00:00:04Z"},
        ]));
        assert_eq!(turns[0]["answer_id"], "a2#0");
        assert_eq!(turns[0]["end"], "2026-01-01T00:00:02Z");
        assert_eq!(turns[1]["process_ids"], json!(["w3#0", "tools-t4#0"]));
        assert_eq!(turns[1]["answer_id"], Value::Null);
        assert_eq!(turns[1]["start"], "2026-01-01T00:00:03Z");
        assert_eq!(turns[1]["end"], "2026-01-01T00:00:04Z");
    }

    /// #9140: a background check for one question reports back after a newer question was answered. Four final texts
    /// lie between the two messages; each is the answer of its own turn.
    #[test]
    fn the_answer_to_a_newer_question_survives_later_wakes() {
        let turns = project(json!([
            {"id":"u1#0","seq":1,"role":"user","kind":"text"},
            {"id":"t2#0","seq":2,"role":"assistant","kind":"tool_use","tool_use_id":"a"},
            {"id":"a3#0","seq":3,"role":"assistant","kind":"text"},
            {"id":"u4#0","seq":4,"role":"user","kind":"text"},
            {"id":"a5#0","seq":5,"role":"assistant","kind":"text"},
            {"id":"w6#0","seq":6,"role":"system","kind":"notice","wake":true},
            {"id":"t7#0","seq":7,"role":"assistant","kind":"tool_use","tool_use_id":"b"},
            {"id":"a8#0","seq":8,"role":"assistant","kind":"text"},
            {"id":"w9#0","seq":9,"role":"system","kind":"notice","wake":true},
            {"id":"a10#0","seq":10,"role":"assistant","kind":"text"},
            {"id":"w11#0","seq":11,"role":"system","kind":"notice","wake":true},
            {"id":"a12#0","seq":12,"role":"assistant","kind":"text"},
            {"id":"u13#0","seq":13,"role":"user","kind":"text"},
        ]));
        let field = |key: &str| {
            turns
                .as_array()
                .unwrap()
                .iter()
                .map(|turn| turn[key].clone())
                .collect::<Vec<_>>()
        };
        assert_eq!(
            field("id"),
            [
                json!("u1#0"),
                json!("u4#0"),
                json!("w6#0"),
                json!("w9#0"),
                json!("w11#0"),
                json!("u13#0")
            ]
        );
        assert_eq!(
            field("answer_id"),
            [
                json!("a3#0"),
                json!("a5#0"),
                json!("a8#0"),
                json!("a10#0"),
                json!("a12#0"),
                Value::Null
            ]
        );
        assert_eq!(
            turns[1]["process_ids"],
            json!([]),
            "the answer to the newer question is no step of any turn's work"
        );
        assert_eq!(turns[2]["process_ids"], json!(["w6#0", "tools-t7#0"]));
        assert_eq!(turns[2]["interim_count"], 0);
    }

    #[test]
    fn tool_result_wake_opens_a_turn_without_becoming_a_user_message() {
        let items = json!([
            {"id":"q1#0","seq":1,"role":"user","kind":"text"},
            {"id":"a2#0","seq":2,"role":"assistant","kind":"text"},
            {"id":"rc3#0","seq":3,"role":"system","kind":"recap"},
            {"id":"r4#0","seq":4,"role":"user","kind":"tool_result","tool_use_id":"agentcall",
             "task":{"kind":"agent"},"wake":true},
            {"id":"a5#0","seq":5,"role":"assistant","kind":"text"},
        ]);
        let turns = project(json!(items));
        assert_eq!(turns.as_array().unwrap().len(), 2, "wake: {turns}");
        assert_eq!(turns[0]["user_ids"], json!(["q1#0"]));
        assert_eq!(turns[0]["answer_id"], "a2#0", "wake: {turns}");
        assert_eq!(turns[1]["user_ids"], json!([]));
        assert_eq!(turns[1]["wake_ids"], json!(["r4#0"]), "{turns}");
        assert_eq!(turns[1]["process_ids"], json!(["r4#0"]));
        assert_eq!(turns[1]["answer_id"], "a5#0", "wake: {turns}");

        // Absorbed mid-turn (no wake): the result is its call's, and the words after the recap open a turn as before.
        let mut not_a_wake = items;
        not_a_wake[3]["wake"] = Value::Bool(false);
        let turns = project(not_a_wake);
        assert_eq!(turns.as_array().unwrap().len(), 2, "not-a-wake: {turns}");
        assert_eq!(turns[1]["wake_ids"], json!([]), "not-a-wake: {turns}");
        assert_eq!(turns[1]["process_ids"], json!([]), "not-a-wake: {turns}");
        assert_eq!(turns[0]["answer_id"], "a2#0", "not-a-wake: {turns}");
        assert_eq!(turns[1]["answer_id"], "a5#0", "not-a-wake: {turns}");
    }

    #[test]
    fn only_agent_tool_result_wakes_are_wakes_and_a_wake_follows_the_recaps_before_it() {
        for (task, wake, woken) in [
            (json!({"kind":"agent"}), true, true),
            (json!({"kind":"agent"}), false, false),
            (json!({"kind":"shell"}), true, false),
            (Value::Null, true, false),
        ] {
            let turns = project(json!([
                {"id":"q1#0","seq":1,"role":"user","kind":"text"},
                {"id":"a2#0","seq":2,"role":"assistant","kind":"text"},
                {"id":"rc3#0","seq":3,"role":"system","kind":"recap"},
                {"id":"r4#0","seq":4,"role":"user","kind":"tool_result","task":task,"wake":wake},
                {"id":"a5#0","seq":5,"role":"assistant","kind":"text"},
            ]));
            assert_eq!(turns.as_array().unwrap().len(), 2, "{task}/{wake}");
            assert_eq!(turns[0]["wake_ids"], json!([]), "{task}/{wake}");
            assert_eq!(
                turns[1]["wake_ids"],
                if woken { json!(["r4#0"]) } else { json!([]) },
                "{task}/{wake}"
            );
        }

        let turns = project(json!([
            {"id":"q1#0","seq":1,"role":"user","kind":"text"},
            {"id":"a2#0","seq":2,"role":"assistant","kind":"text"},
            {"id":"rc3#0","seq":3,"role":"system","kind":"recap"},
            {"id":"rc4#0","seq":4,"role":"system","kind":"recap"},
            {"id":"w5#0","seq":5,"role":"system","kind":"notice","wake":true},
            {"id":"t6#0","seq":6,"role":"assistant","kind":"tool_use"},
            {"id":"a7#0","seq":7,"role":"assistant","kind":"text"},
        ]));
        assert_eq!(turns.as_array().unwrap().len(), 2, "{turns}");
        assert_eq!(turns[0]["answer_id"], "a2#0");
        assert_eq!(turns[1]["wake_ids"], json!(["w5#0"]));
        assert_eq!(turns[1]["answer_id"], "a7#0");
    }

    #[test]
    fn compaction_notices_are_one_source_derived_step() {
        let input: Input = serde_json::from_value(json!({"items":[
            {"id":"u1#0","seq":1,"role":"user","kind":"text"},
            {"id":"a2#0","seq":2,"role":"assistant","kind":"text"},
            {"id":"n3#0","seq":3,"role":"system","kind":"notice","text":"上下文已压缩"}, // i18n-ignore
            {"id":"n4#0","seq":4,"role":"system","kind":"notice","text":"已从压缩的上下文继续"}, // i18n-ignore
            {"id":"n5#0","seq":5,"role":"system","kind":"notice","text":"Compacted while idle, fictional example"},
        ]}))
        .unwrap();
        let turns = persona_core::turns::project(&input);
        assert_eq!(turns[0]["ordered_ids"], json!(["u1#0", "a2#0", "n3#0"]));
        assert_eq!(turns[0]["process_ids"], json!(["n3#0"]));
        assert_eq!(turns[0]["answer_id"], "a2#0");

        let input: Input = serde_json::from_value(json!({"items":[
            {"id":"n1#0","seq":1,"role":"system","kind":"notice","text":"已从压缩的上下文继续"}, // i18n-ignore
            {"id":"u2#0","seq":2,"role":"user","kind":"text"},
        ]}))
        .unwrap();
        let turns = persona_core::turns::project(&input);
        assert_eq!(turns.as_array().unwrap().len(), 2);
        assert_eq!(turns[0]["ordered_ids"], json!(["n1#0"]));
        assert_eq!(turns[0]["process_ids"], json!(["n1#0"]));
        assert_eq!(turns[0]["answer_id"], Value::Null);
    }

    #[test]
    fn compaction_eligibility_and_interruption_preserve_other_notices() {
        for (suffix, task, wake) in [
            ("", Value::Null, false),
            ("", json!({"kind":"agent"}), false),
            ("", Value::Null, true),
            (" extra", Value::Null, false),
        ] {
            let turns = project(json!([
                {"id":"u1#0","seq":1,"role":"user","kind":"text"},
                {"id":"a2#0","seq":2,"role":"assistant","kind":"text"},
                {"id":"n3#0","seq":3,"role":"system","kind":"notice","text":format!("上下文已压缩{suffix}"), // i18n-ignore
                 "task":task,"wake":wake},
                {"id":"n4#0","seq":4,"role":"system","kind":"notice","text":"已从压缩的上下文继续"}, // i18n-ignore
                {"id":"n5#0","seq":5,"role":"system","kind":"notice","text":"Compacted while idle, fictional example"},
            ]));
            let expected = if suffix.is_empty() && task.is_null() && !wake {
                json!(["u1#0", "a2#0", "n3#0"])
            } else {
                // With no eligible open boundary, the continuation itself becomes one and absorbs the idle note.
                json!(["u1#0", "a2#0", "n3#0", "n4#0"])
            };
            // A notice that woke the agent is no compaction boundary: it opens a turn of its own like any wake.
            let all: Vec<Value> = turns
                .as_array()
                .unwrap()
                .iter()
                .flat_map(|turn| turn["ordered_ids"].as_array().unwrap().clone())
                .collect();
            assert_eq!(
                turns.as_array().unwrap().len(),
                if wake { 2 } else { 1 },
                "{task}/{wake}/{suffix}"
            );
            assert_eq!(Value::Array(all), expected, "{task}/{wake}/{suffix}");
        }

        let turns = project(json!([
            {"id":"u1#0","seq":1,"role":"user","kind":"text"},
            {"id":"a2#0","seq":2,"role":"assistant","kind":"text"},
            {"id":"n3#0","seq":3,"role":"system","kind":"notice","text":"上下文已压缩"}, // i18n-ignore
            {"id":"t4#0","seq":4,"role":"assistant","kind":"tool_use"},
            {"id":"n5#0","seq":5,"role":"system","kind":"notice","text":"Compacted while idle, fictional example"},
        ]));
        assert_eq!(
            turns[0]["ordered_ids"],
            json!(["u1#0", "a2#0", "n3#0", "tools-t4#0", "n5#0"])
        );

        let turns = project(json!([{
            "id":"c1#0","seq":1,"role":"system","kind":"custom",
            "compact":{"trigger":"manual","notes":[]}
        }]));
        assert_eq!(turns, json!([]));
    }

    #[test]
    fn supervision_legacy_recognition_preserves_real_user_provenance() {
        // i18n-ignore: canonical injected protocol text, not localized UI copy.
        let text = "📋 督办：你派出去的任务有 2 个要你处理\n这是 Persona 的督办消息";
        for marked in [false, true] {
            let turns = project(json!([
                {"id":"u1#0","seq":1,"role":"user","kind":"text","text":"First real request"},
                {"id":"u2#0","seq":2,"role":"user","kind":"text","text":text,"queued":true,
                 "injected":if marked {json!({"state":"supervise"})} else {Value::Null}},
                {"id":"u3#0","seq":3,"role":"user","kind":"text","text":"Next real request","queued":true},
            ]));
            assert_eq!(turns.as_array().unwrap().len(), 3);
            assert_eq!(turns[1]["user_ids"], json!(["u2#0"]));
            assert_eq!(turns[2]["user_ids"], json!(["u3#0"]));
        }

        for (provenance, marker) in [
            ("client_msg_id", json!("fictional-message")),
            ("from", json!("fictional-peer")),
        ] {
            let mut value = json!({"items":[
                {"id":"u1#0","seq":1,"role":"user","kind":"text","text":text},
                {"id":"u2#0","seq":2,"role":"user","kind":"text","text":"Next real request","queued":true},
            ]});
            value["items"][0]
                .as_object_mut()
                .unwrap()
                .insert(provenance.to_owned(), marker);
            let turns = project(value["items"].take());
            assert_eq!(turns[0]["user_ids"], json!(["u1#0", "u2#0"]));
        }

        let protected = project(json!([
            {"id":"u0#0","seq":0,"role":"user","kind":"text","text":"First real request"},
            {"id":"u1#0","seq":1,"role":"user","kind":"text",
            "text":format!("<persona-context kind=\"message-source\">{{\"author\":\"user\"}}</persona-context>\n{text}"),
            "queued":true,
            "injected":{"state":"supervise"}},
            {"id":"u2#0","seq":2,"role":"user","kind":"text","text":"Next real request","queued":true},
        ]));
        assert_eq!(protected[0]["user_ids"], json!(["u0#0", "u1#0", "u2#0"]));
    }

    #[test]
    fn malformed_source_and_previous_context_tags_protect_user_input() {
        // i18n-ignore: canonical injected protocol text, not localized UI copy.
        let text = "📋 督办：你派出去的任务有 2 个要你处理\n这是 Persona 的督办消息";
        for (tag, metadata) in [
            ("persona-context", "{\"kind\":42}"),
            ("persona-context", "not json"),
            ("persona-context", "{\"kind\":1e400}"),
            ("jarvis-context", "{\"author\":\"user\"}"),
        ] {
            let turns = project(json!([
                {"id":"u0#0","seq":0,"role":"user","kind":"text","text":"First real request"},
                {"id":"u1#0","seq":1,"role":"user","kind":"text","queued":true,
                 "text":format!("<{tag} kind=\"message-source\">{metadata}</{tag}>\n{text}"),
                 "injected":{"state":"supervise"}},
                {"id":"u2#0","seq":2,"role":"user","kind":"text","text":"Next real request","queued":true},
            ]));
            assert_eq!(turns.as_array().unwrap().len(), 1, "{tag}/{metadata}");
            assert_eq!(
                turns[0]["user_ids"],
                json!(["u0#0", "u1#0", "u2#0"]),
                "{tag}/{metadata}"
            );
        }

        let legacy_supervision = project(json!([
            {"id":"u0#0","seq":0,"role":"user","kind":"text","text":"First real request"},
            {"id":"u1#0","seq":1,"role":"user","kind":"text","queued":true,
             "text":format!("<jarvis-context kind=\"context\">Fictional context</jarvis-context>\n{text}")},
            {"id":"u2#0","seq":2,"role":"user","kind":"text","text":"Next real request","queued":true},
        ]));
        assert_eq!(legacy_supervision.as_array().unwrap().len(), 3);

        for (prefix, expected_turns) in [
            (
                "<persona-context-kind=\"context\">not a context block</persona-context>\n",
                1,
            ),
            (
                "<persona-context kind=\"message-source\"extra>{\"kind\":42}</persona-context>\n",
                1,
            ),
            (
                "<persona-context kind=\"context\">Fictional context</persona-context>\n",
                3,
            ),
            (
                "<persona-context kind=\"message-source\">{\"kind\":\"patrol\"}</persona-context>\n",
                3,
            ),
            (
                "<persona-context kind=\"message-source\">{\"author\":\"user\"}</persona-context>\n<persona-context kind=\"message-source\">{\"kind\":\"patrol\"}</persona-context>\n",
                1,
            ),
        ] {
            let turns = project(json!([
                {"id":"u0#0","seq":0,"role":"user","kind":"text","text":"First real request"},
                {"id":"u1#0","seq":1,"role":"user","kind":"text","queued":true,
                 "text":format!("{prefix}{text}")},
                {"id":"u2#0","seq":2,"role":"user","kind":"text","text":"Next real request","queued":true},
            ]));
            assert_eq!(turns.as_array().unwrap().len(), expected_turns, "{prefix}");
        }

        for text in [
            "📋 督办：你派出去的任务有 two 个要你处理\n这是 Persona 的督办消息",
            "📋 督办：你派出去的任务有 2 个要你处理",
        ] {
            let turns = project(json!([
                {"id":"u0#0","seq":0,"role":"user","kind":"text","text":"First real request"},
                {"id":"u1#0","seq":1,"role":"user","kind":"text","text":text,"queued":true},
                {"id":"u2#0","seq":2,"role":"user","kind":"text","text":"Next real request","queued":true},
            ]));
            assert_eq!(turns.as_array().unwrap().len(), 1, "{text}");
        }
    }
}

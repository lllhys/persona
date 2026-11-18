//! Replays the exact current-iOS question-answer and recommendation-display fixture contracts.

use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::{Path, PathBuf};

use persona_core::answer::{Action, Input, Transition};
use serde_json::{Map, Value, json};

fn fixture(file: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../../../spec/fixtures/answer")
        .join(file)
}

#[test]
fn question_and_recommended_cases_match_complete_expected_objects() {
    for (file, expected_names) in [
        (
            "questions.json",
            [
                "all_answered_can_submit_from_earlier_page",
                "blank_other_cannot_submit",
                "blank_other_uses_picks",
                "empty_answer_cannot_submit",
                "last_page_missing_answer_returns_to_gap",
                "multiple_other_overrides_picks",
                "multiple_pick_toggles_off",
                "multiple_pick_waits_for_submit",
                "multiple_picks_follow_option_order",
                "multiple_questions_send_original_keys",
                "next_page_does_not_send",
                "other_answer_trimmed",
                "sending_disables_option",
                "single_pick_clears_other",
                "single_tap_sends_original_label",
            ]
            .as_slice(),
        ),
        (
            "recommended.json",
            [
                "recommended_case_sensitive",
                "recommended_corefoundation_boundary",
                "recommended_chinese_ascii",
                "recommended_chinese_fullwidth",
                "recommended_suffix_only",
                "recommended_spaced",
                "recommended_trailing_space",
                "recommended_trims_prefix_space",
                "recommended_unspaced",
            ]
            .as_slice(),
        ),
    ] {
        let document: Value = serde_json::from_slice(&fs::read(fixture(file)).unwrap()).unwrap();
        assert_eq!(document["schema_version"], 1);
        let mut replayed = BTreeSet::new();
        for case in document["cases"].as_array().unwrap() {
            let name = case["name"].as_str().unwrap();
            assert!(case["derived"] == true && case.get("from_test").is_none());
            assert_eq!(case["input"]["profile"], "ios_current", "{file}/{name}");
            let input: Input = serde_json::from_value(case["input"].clone()).expect(name);
            let transition = persona_core::answer::project(&input).expect(name);
            let mut actual = Map::new();
            actual.insert("allowed".to_owned(), json!(transition.allowed));
            actual.insert(
                "resolution".to_owned(),
                transition
                    .resolution
                    .as_ref()
                    .map(|resolution| serde_json::to_value(resolution).unwrap())
                    .unwrap_or(Value::Null),
            );
            if transition.page != input.operation.page {
                actual.insert("next_page".to_owned(), json!(transition.page));
            }
            if input.operation.action == persona_core::answer::Action::PickOption
                && transition.allowed
                && input.interaction.questions[input.operation.page].multi_select
            {
                actual.insert("picks".to_owned(), serde_json::to_value(&transition.picks).unwrap());
            }
            if file == "recommended.json" {
                let label = &input.interaction.questions[input.operation.page].options
                    [input.operation.option_index.unwrap()]
                .label;
                actual.insert(
                    "display".to_owned(),
                    serde_json::to_value(persona_core::answer::display(label)).unwrap(),
                );
            }
            assert_eq!(Value::Object(actual), case["expected"], "{file}/{name}");
            assert!(replayed.insert(name.to_owned()), "duplicate case: {name}");
        }
        assert_eq!(
            replayed,
            expected_names.iter().map(|name| (*name).to_owned()).collect(),
            "exact manifest for {file}"
        );
        eprintln!("answer projection: {file}: {} full expected objects", replayed.len());
    }
}

#[test]
fn selected_labels_follow_option_order_not_pick_order_or_display_text() {
    let mut input: Input = serde_json::from_value(json!({
        "interaction":{"kind":"question","questions":[{
            "question":"Pick two","multi_select":true,
            "options":[{"label":"z (Recommended)"},{"label":"a"},{"label":"m"}],
        }]},
        "operation":{
            "action":"pick_option","page":0,"option_index":2,
            "picks":{"Pick two":["z (Recommended)"]},"other":{},"sending":false,
        }
    }))
    .unwrap();
    let picked = persona_core::answer::project(&input).unwrap();
    assert_eq!(picked.picks["Pick two"].len(), 2);
    input.operation.picks = picked.picks;
    input.operation.other = picked.other;
    input.operation.action = persona_core::answer::Action::Submit;
    let transition = persona_core::answer::project(&input).unwrap();
    assert_eq!(transition.resolution.unwrap().answers["Pick two"], "z (Recommended), m");
}

#[test]
fn disallowed_operations_leave_the_form_unchanged() {
    for action in ["advance", "submit"] {
        let input: Input = serde_json::from_value(json!({
            "interaction":{"kind":"question","questions":[{
                "question":"One","multi_select":false,"options":[{"label":"Yes"}],
            }]},
            "operation":{"action":action,"page":0,"picks":{"One":["Yes"]},"other":{},"sending":true},
        }))
        .unwrap();
        let transition = persona_core::answer::project(&input).unwrap();
        assert!(!transition.allowed);
        assert_eq!(transition.page, input.operation.page);
        assert_eq!(transition.picks, input.operation.picks);
        assert_eq!(transition.other, input.operation.other);
        assert_eq!(transition.resolution, None);
    }
}

#[test]
fn apple_whitespace_sets_include_zero_width_space_but_display_preserves_newlines() {
    // CoreFoundation's isWhitespace covers U+2000...U+200B; its newline set is separate.
    let input: Input = serde_json::from_value(json!({
        "interaction":{"kind":"question","questions":[{
            "question":"One","multi_select":false,"options":[{"label":"Yes"}],
        }]},
        "operation":{"action":"submit","page":0,"picks":{},"other":{"One":" \u{200b} "},"sending":false},
    }))
    .unwrap();
    let transition = persona_core::answer::project(&input).unwrap();
    assert!(!transition.allowed);
    assert_eq!(transition.resolution, None);

    let shown = persona_core::answer::display("\u{200b} A\n (Recommended)");
    assert_eq!(shown.text, "A\n");
    assert!(shown.recommended);
}

// The draft a click leaves for the next one (#9238).
//
// The rules are the current iOS `QuestionForm`'s (`pick`, `advance`, `submit` and `send` in
// apple/Persona/Views/Interactions/InteractionViews.swift). `TestResolutionFixturesIOS` (macOS, with
// PERSONA_VERIFY_IOS_FIXTURES=1) asserts them against that code itself, for every fixture case and for the two-question
// chain below, so these expectations are not read off this crate's output. Android's `AnswerBuilder` differs in one
// way, the key of an emptied Other entry (see `Transition`).

fn cases(file: &str) -> Vec<Value> {
    let document: Value = serde_json::from_slice(&fs::read(fixture(file)).unwrap()).unwrap();
    document["cases"].as_array().unwrap().clone()
}

/// A question-keyed map of picked labels.
fn picked(value: Value) -> BTreeMap<String, BTreeSet<String>> {
    serde_json::from_value(value).unwrap()
}

/// A question-keyed map of text: Other entries, or the answers a resolution carries.
fn texts(value: Value) -> BTreeMap<String, String> {
    serde_json::from_value(value).unwrap()
}

fn wire(action: Action) -> &'static str {
    match action {
        Action::PickOption => "pick_option",
        Action::Advance => "advance",
        Action::Submit => "submit",
    }
}

/// A client's form. Each click goes through `project`, and the transition's page, picks and Other entries are what
/// the next click starts from, as an adapter that keeps them would store them.
struct Form {
    questions: Value,
    page: usize,
    picks: BTreeMap<String, BTreeSet<String>>,
    other: BTreeMap<String, String>,
}

impl Form {
    fn new(questions: Value, other_entries: Value) -> Self {
        Self {
            questions,
            page: 0,
            picks: BTreeMap::new(),
            other: texts(other_entries),
        }
    }

    fn click(&mut self, action: Action, option_index: Option<usize>) -> Transition {
        let mut operation = json!({
            "action": wire(action), "page": self.page, "picks": self.picks, "other": self.other, "sending": false,
        });
        if let Some(index) = option_index {
            operation["option_index"] = json!(index);
        }
        let input: Input = serde_json::from_value(json!({
            "interaction": {"kind": "question", "questions": self.questions},
            "operation": operation,
        }))
        .unwrap();
        let transition = persona_core::answer::project(&input).unwrap();
        self.page = transition.page;
        self.picks = transition.picks.clone();
        self.other = transition.other.clone();
        transition
    }
}

fn single_choice(question: &str, labels: [&str; 2]) -> Value {
    json!({"question": question, "multi_select": false, "options": labels.map(|label| json!({"label": label}))})
}

fn two_single_choices() -> Value {
    json!([single_choice("One", ["A", "B"]), single_choice("Two", ["C", "D"])])
}

#[test]
fn every_fixture_case_leaves_the_draft_as_ios_does() {
    // An accepted single choice replaces its question's picks and empties its Other entry (the key stays); an accepted
    // multiple choice toggles its label and leaves Other alone; every other click, and any click made while an answer
    // is on its way, changes neither map. Other questions' entries are never touched.
    let mut checked = 0;
    for file in ["questions.json", "recommended.json"] {
        for case in cases(file) {
            let name = case["name"].as_str().unwrap();
            let input: Input = serde_json::from_value(case["input"].clone()).expect(name);
            let transition = persona_core::answer::project(&input).expect(name);
            let operation = &input.operation;
            let mut picks = operation.picks.clone();
            let mut other = operation.other.clone();
            if operation.action == Action::PickOption && transition.allowed {
                let question = &input.interaction.questions[operation.page];
                let label = question.options[operation.option_index.unwrap()].label.clone();
                if question.multi_select {
                    let selected = picks.entry(question.question.clone()).or_default();
                    if !selected.remove(&label) {
                        selected.insert(label);
                    }
                } else {
                    picks.insert(question.question.clone(), BTreeSet::from([label]));
                    other.insert(question.question.clone(), String::new());
                }
            }
            assert_eq!(transition.picks, picks, "{file}/{name}: picks");
            assert_eq!(transition.other, other, "{file}/{name}: other");
            checked += 1;
        }
    }
    assert_eq!(checked, 24, "15 question + 9 recommendation cases");
}

#[test]
fn a_pick_empties_its_other_entry_and_advance_and_submit_keep_what_they_are_given() {
    // The issue's case: two single choices; a custom answer exists for the first when its option is picked.
    let mut form = Form::new(two_single_choices(), json!({"One": "custom"}));

    let first = form.click(Action::PickOption, Some(1));
    assert!(first.allowed);
    assert_eq!(first.page, 1);
    assert_eq!(first.picks, picked(json!({"One": ["B"]})));
    assert_eq!(
        first.other,
        texts(json!({"One": ""})),
        "iOS keeps the emptied entry: the key stays"
    );
    assert_eq!(first.resolution, None);

    // The second question is still open, so neither click is allowed and both hand back the draft they were given.
    for action in [Action::Advance, Action::Submit] {
        let held = form.click(action, None);
        assert!(!held.allowed, "{action:?}");
        assert_eq!(held.page, 1, "{action:?}");
        assert_eq!(held.picks, picked(json!({"One": ["B"]})), "{action:?}");
        assert_eq!(held.other, texts(json!({"One": ""})), "{action:?}");
        assert_eq!(held.resolution, None, "{action:?}");
    }

    let second = form.click(Action::PickOption, Some(1));
    assert!(second.allowed);
    assert_eq!(second.page, 1, "the last page stays; a pick there does not send");
    assert_eq!(second.picks, picked(json!({"One": ["B"], "Two": ["D"]})));
    assert_eq!(second.other, texts(json!({"One": "", "Two": ""})));
    assert_eq!(second.resolution, None);

    // Submit sends both answers (iOS opens its review first, which the core does not model) and still holds the draft.
    let sent = form.click(Action::Submit, None);
    assert!(sent.allowed);
    assert_eq!(sent.picks, picked(json!({"One": ["B"], "Two": ["D"]})));
    assert_eq!(sent.other, texts(json!({"One": "", "Two": ""})));
    assert_eq!(sent.resolution.unwrap().answers, texts(json!({"One": "B", "Two": "D"})));
}

#[test]
fn a_typed_answer_does_not_survive_the_pick_into_the_draft_or_the_answers() {
    let mut form = Form::new(two_single_choices(), json!({"One": "custom"}));
    form.click(Action::PickOption, Some(1));
    form.click(Action::PickOption, Some(0));
    let sent = form.click(Action::Advance, None);
    assert_eq!(
        sent.resolution.as_ref().unwrap().answers,
        texts(json!({"One": "B", "Two": "C"}))
    );
    assert!(!format!("{sent:?}").contains("custom"), "{sent:?}");
}

#[test]
fn advance_and_submit_never_apply_the_single_choice_rule() {
    // A pick and a typed answer on one single choice together: neither form can make this state, but a draft restored
    // from elsewhere could hold it. iOS answers with the typed text and changes neither map (`answer`, `submit`,
    // `send`), so the core must not clear the entry or drop the pick on Submit or Advance either.
    let mut form = Form::new(json!([single_choice("One", ["A", "B"])]), json!({"One": "custom"}));
    form.picks = picked(json!({"One": ["A"]}));
    let sent = form.click(Action::Submit, None);
    assert!(sent.allowed);
    assert_eq!(sent.resolution.unwrap().answers, texts(json!({"One": "custom"})));
    assert_eq!(sent.picks, picked(json!({"One": ["A"]})));
    assert_eq!(sent.other, texts(json!({"One": "custom"})));

    let mut form = Form::new(two_single_choices(), json!({"One": "custom"}));
    form.picks = picked(json!({"One": ["A"]}));
    let moved = form.click(Action::Advance, None);
    assert!(moved.allowed);
    assert_eq!(moved.page, 1);
    assert_eq!(moved.resolution, None);
    assert_eq!(moved.picks, picked(json!({"One": ["A"]})));
    assert_eq!(moved.other, texts(json!({"One": "custom"})));
}

#[test]
fn a_multiple_choice_pick_toggles_its_label_and_leaves_the_typed_answer_for_submit() {
    let questions = json!([{
        "question": "One", "multi_select": true, "options": [{"label": "A"}, {"label": "B"}],
    }]);
    let mut form = Form::new(questions, json!({"One": "custom"}));
    for (index, expected) in [
        (0, json!({"One": ["A"]})),
        (1, json!({"One": ["A", "B"]})),
        (0, json!({"One": ["B"]})),
    ] {
        let toggled = form.click(Action::PickOption, Some(index));
        assert!(toggled.allowed);
        assert_eq!(toggled.resolution, None, "a multiple choice waits for Submit");
        assert_eq!(toggled.picks, picked(expected));
        assert_eq!(
            toggled.other,
            texts(json!({"One": "custom"})),
            "a multiple choice leaves Other alone"
        );
    }
    let sent = form.click(Action::Submit, None);
    assert_eq!(
        sent.resolution.unwrap().answers,
        texts(json!({"One": "custom"})),
        "typed text overrides the picks"
    );
    assert_eq!(sent.picks, picked(json!({"One": ["B"]})));
    assert_eq!(sent.other, texts(json!({"One": "custom"})));
}

#[test]
fn a_single_question_pick_sends_at_once_and_still_hands_back_the_emptied_entry() {
    let mut form = Form::new(json!([single_choice("One", ["A", "B"])]), json!({"One": "custom"}));
    let sent = form.click(Action::PickOption, Some(0));
    assert!(sent.allowed);
    assert_eq!(sent.picks, picked(json!({"One": ["A"]})));
    assert_eq!(sent.other, texts(json!({"One": ""})));
    assert_eq!(sent.resolution.unwrap().answers, texts(json!({"One": "A"})));
}

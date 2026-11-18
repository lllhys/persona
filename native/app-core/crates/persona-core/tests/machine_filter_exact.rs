use persona_core::sort::{Input, project};

#[test]
fn selected_machine_is_an_exact_path_not_a_substring() {
    let input: Input = serde_json::from_value(serde_json::json!({
        "sessions": [
            {"machine_id":"machine-a", "session_id":"selected", "state":"unread"},
            {"machine_id":"machine-a-old", "session_id":"other", "state":"working"}
        ],
        "machines": [
            {"id":"machine-a", "name":"Machine A"},
            {"id":"machine-a-old", "name":"Machine A Old"}
        ],
        "pins": {}, "mutes": {},
        "settings": {"grouping":"state", "quick":"all", "machine_filter":"machine-a"}
    }))
    .unwrap();
    let got = project(&input);
    assert_eq!(got.counts.unread, 1);
    assert_eq!(got.counts.working, 0, "the other machine must not affect scoped counts");
    let rows: Vec<&str> = got
        .groups
        .iter()
        .flat_map(|g| g.rows.iter().map(String::as_str))
        .collect();
    assert_eq!(
        rows,
        ["machine-a/selected"],
        "the selected computer's list must exclude machine-a-old"
    );
}

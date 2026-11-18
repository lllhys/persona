use persona_core::sort::Input;
use serde_json::json;

fn input(grouping: &str, filter: &str) -> serde_json::Value {
    json!({
        "sessions": [
            {"machine_id": "shared", "session_id": "first", "app_hub_group": "hub", "manager": "alpha",
             "title": "First", "cwd": "/home/user/project", "state": "working"},
            {"machine_id": "shared", "session_id": "second", "app_hub_group": "hub", "manager": "beta",
             "title": "Second", "cwd": "/home/user/project", "state": "working"}
        ],
        "machines": [
            {"id": "shared", "name": "Shared", "group": "hub", "manager": "alpha"},
            {"id": "shared", "name": "Shared", "group": "hub", "manager": "beta"}
        ],
        "pins": {"spaces": [], "sessions": []},
        "mutes": {"spaces": [], "sessions": [], "allowed": []},
        "settings": {"grouping": grouping, "quick": "all", "query": "", "machine_filter": filter,
            "kept": [], "folded": [], "expanded_idle": [], "expanded_quiet": [], "compact": false}
    })
}

#[test]
fn managers_on_one_hub_machine_keep_independent_groups_and_filters() {
    let projection = persona_core::sort::project(&serde_json::from_value(input("machine", "")).expect("input"));
    let groups = projection.groups;
    assert_eq!(groups.len(), 2);
    assert_eq!(groups[0].id, "machine/hub|alpha|shared");
    assert_eq!(groups[0].rows, ["hub|alpha|shared|first"]);
    assert_eq!(groups[1].id, "machine/hub|beta|shared");
    assert_eq!(groups[1].rows, ["hub|beta|shared|second"]);

    for (manager, session) in [("alpha", "first"), ("beta", "second")] {
        let filtered = persona_core::sort::project(
            &serde_json::from_value(input("machine", &format!("hub|{manager}|shared"))).expect("filter"),
        );
        assert_eq!(filtered.groups.len(), 1);
        assert_eq!(filtered.groups[0].rows, [format!("hub|{manager}|shared|{session}")]);
    }

    let legacy =
        persona_core::sort::project(&serde_json::from_value(input("machine", "hub|shared")).expect("legacy filter"));
    assert_eq!(
        legacy.groups.len(),
        2,
        "legacy group|machine includes both manager owners"
    );
}

#[test]
fn owner_qualified_space_group_keeps_legacy_preference_machine_key() {
    let mut value = input("space", "");
    value["pins"]["spaces"] = json!(["shared/project"]);
    value["sessions"][0]["space_name"] = json!("project");
    value["sessions"][1]["space_name"] = json!("project");
    let input: Input = serde_json::from_value(value).expect("space input");
    let projection = persona_core::sort::project(&input);
    assert_eq!(projection.groups.len(), 2);
    assert_eq!(projection.groups[0].id, "hub|alpha|shared/-");
    assert_eq!(projection.groups[1].id, "hub|beta|shared/-");
}

#[test]
fn manager_preferences_and_held_are_owner_qualified() {
    let mut value = input("state", "");
    value["sessions"][0]["state"] = json!("idle");
    value["sessions"][1]["state"] = json!("idle");
    value["manager_preferences"] = json!({
        "hub|alpha": {
            "pins": {"spaces": [], "sessions": ["shared/first"]},
            "mutes": {"spaces": [], "sessions": [], "allowed": []}
        },
        "hub|beta": {
            "pins": {"spaces": [], "sessions": []},
            "mutes": {"spaces": ["shared/project"], "sessions": [], "allowed": ["shared/second"]}
        }
    });
    value["hub_preferences"] = json!({
        "hub": {
            "pins": {"spaces": [], "sessions": ["shared/second"]},
            "mutes": {"spaces": [], "sessions": [], "allowed": []}
        }
    });
    value["settings"]["held"] = json!("hub|alpha|shared|first");
    value["settings"]["folded"] = json!(["!state/idle"]);
    value["settings"]
        .as_object_mut()
        .expect("settings")
        .remove("expanded_idle");
    let projection = persona_core::sort::project(&serde_json::from_value(value).expect("manager preferences"));
    assert_eq!(projection.groups[0].id, "pinned");
    assert_eq!(projection.groups[0].rows, ["hub|alpha|shared|first"]);
    assert!(!projection.groups[0].rows.contains(&"hub|beta|shared|second".to_owned()));
    assert_eq!(projection.groups[1].rows, ["hub|beta|shared|second"]);
    assert!(!projection.groups[1].rows.contains(&"hub|alpha|shared|first".to_owned()));
}

#[test]
fn archived_offline_rows_are_visible_only_in_explicit_history() {
    let mut value = input("machine", "");
    value["sessions"][0]["state"] = json!("offline");
    value["sessions"][0]["status"] = json!("offline");
    value["sessions"][0]["archived"] = json!(true);
    let without_history: Input = serde_json::from_value(value.clone()).expect("input");
    assert_eq!(persona_core::sort::project(&without_history).groups.len(), 1);
    value["settings"]["history"] = json!(true);
    let with_history: Input = serde_json::from_value(value).expect("history input");
    let groups = persona_core::sort::project(&with_history).groups;
    assert!(
        groups
            .iter()
            .any(|group| group.rows.contains(&"hub|alpha|shared|first".to_owned()))
    );
}

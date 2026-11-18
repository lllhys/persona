//! The fixture-facing part of Apple's `TaskIndex.build`: which calls went to the background, which notices belong to
//! them, and what the call descriptions/start times/status sequences are. It does not render task cards or run tasks.

use std::collections::{BTreeMap, BTreeSet};

use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::{state::utc_timestamp, turns::timestamp_text};

#[derive(Debug, Clone, Deserialize)]
pub struct Input {
    pub items: Vec<Item>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Item {
    pub id: String,
    pub ts: Option<String>,
    pub kind: String,
    pub text: Option<String>,
    pub tool_use_id: Option<String>,
    pub tool_name: Option<String>,
    pub tool_input: Option<Value>,
    pub task: Option<Task>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Task {
    pub kind: String,
    pub status: Option<String>,
    pub task_id: Option<String>,
    pub tool_use_id: Option<String>,
    pub detail: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize)]
pub struct Index {
    pub started: BTreeSet<String>,
    pub calls: BTreeMap<String, String>,
    pub descriptions: BTreeMap<String, String>,
    pub started_at: BTreeMap<String, String>,
    pub statuses: BTreeMap<String, Vec<Option<String>>>,
}

fn receipt(item: &Item) -> bool {
    if let Some(task) = &item.task
        && task.kind == "agent"
        && !matches!(task.status.as_deref(), Some("running" | "waiting") if task.detail.is_none())
    {
        return false;
    }
    let text = item.text.as_deref().unwrap_or_default();
    text.starts_with("Async agent launched successfully") || text.contains("moved to the background (ID:")
}

fn launched_id(text: &str) -> Option<&str> {
    for prefix in [
        "running in background with ID",
        "Monitor started (task",
        "moved to the background (ID)",
        "agentId:",
    ] {
        let mut rest = text.split(prefix);
        rest.next();
        for tail in rest {
            let tail = tail.trim_start_matches([' ', '\t', '\n', '\r', ':']);
            let id = tail
                .split(|character: char| !(character.is_ascii_alphanumeric() || matches!(character, '_' | '-')))
                .next()
                .unwrap_or_default();
            if !id.is_empty() {
                return Some(id);
            }
        }
    }
    None
}

fn started_at(item: &Item) -> Option<String> {
    let value = item.ts.as_deref()?;
    let (local, offset) = if value.ends_with('Z') {
        (value.to_owned(), 0_i128)
    } else if value.len() >= 25 && matches!(value.as_bytes().get(value.len() - 6), Some(b'+' | b'-')) {
        let split = value.len() - 6;
        let suffix = &value[split..];
        let sign = if suffix.as_bytes().first() == Some(&b'+') {
            1_i128
        } else {
            -1_i128
        };
        let hour = suffix.get(1..3)?.parse::<i128>().ok()?;
        let minute = suffix.get(4..6)?.parse::<i128>().ok()?;
        if suffix.as_bytes().get(3) != Some(&b':') || hour >= 24 || minute >= 60 {
            return None;
        }
        (format!("{}Z", &value[..split]), sign * (hour * 3_600 + minute * 60))
    } else {
        return None;
    };
    let nanos = utc_timestamp(&local)? - offset * 1_000_000_000;
    // The lines fixture uses ISO8601DateFormatter without fractional seconds, unlike turn timestamps.
    timestamp_text(nanos.div_euclid(1_000_000_000) * 1_000_000_000)
}

/// Build the fields the existing lines fixture compares, preserving input order for each call's notices.
pub fn project(input: &Input) -> Index {
    let mut index = Index::default();
    let mut by_task: BTreeMap<String, String> = BTreeMap::new();
    let mut uses: BTreeMap<String, &Item> = BTreeMap::new();
    for item in &input.items {
        match item.kind.as_str() {
            "tool_use" => {
                if let Some(id) = &item.tool_use_id {
                    uses.insert(id.clone(), item);
                }
            }
            "tool_result" => {
                if let Some(id) = &item.tool_use_id
                    && let Some(task) = &item.task
                    && task.kind == "agent"
                {
                    index.calls.insert(item.id.clone(), id.clone());
                    index.statuses.entry(id.clone()).or_default().push(task.status.clone());
                    if let Some(task_id) = &task.task_id {
                        by_task.insert(task_id.clone(), id.clone());
                    }
                }
                if item.task.as_ref().is_some_and(|task| task.kind == "agent") && !receipt(item) {
                    continue;
                }
                let Some(id) = &item.tool_use_id else { continue };
                let Some(text) = &item.text else { continue };
                if !(text.contains("background") || text.contains("Monitor started") || text.contains("agentId")) {
                    continue;
                }
                let Some(task_id) = launched_id(text) else { continue };
                if uses.get(id).is_some_and(|item| {
                    matches!(item.tool_name.as_deref(), Some("Agent" | "Task"))
                        && !text.contains("Async agent launched")
                        && !text.contains("moved to the background")
                        && item
                            .tool_input
                            .as_ref()
                            .and_then(|input| input.get("run_in_background"))
                            .and_then(Value::as_bool)
                            != Some(true)
                }) {
                    continue;
                }
                by_task.insert(task_id.to_owned(), id.clone());
                index.started.insert(id.clone());
                if let Some(item) = uses.get(id) {
                    if let Some(description) = item
                        .tool_input
                        .as_ref()
                        .and_then(|input| input.get("description"))
                        .and_then(Value::as_str)
                    {
                        index.descriptions.insert(id.clone(), description.to_owned());
                    }
                    if let Some(timestamp) = started_at(item) {
                        index.started_at.insert(id.clone(), timestamp);
                    }
                }
            }
            "notice" => {
                let Some(task) = &item.task else { continue };
                let Some(call) = task
                    .tool_use_id
                    .clone()
                    .or_else(|| task.task_id.as_ref().and_then(|id| by_task.get(id)).cloned())
                else {
                    continue;
                };
                index.calls.insert(item.id.clone(), call.clone());
                index
                    .statuses
                    .entry(call.clone())
                    .or_default()
                    .push(task.status.clone());
                if !index.descriptions.contains_key(&call)
                    && let Some(tool_use) = uses.get(&call)
                {
                    if let Some(description) = tool_use
                        .tool_input
                        .as_ref()
                        .and_then(|input| input.get("description"))
                        .and_then(Value::as_str)
                    {
                        index.descriptions.insert(call.clone(), description.to_owned());
                    }
                    if let Some(timestamp) = started_at(tool_use) {
                        index.started_at.insert(call.clone(), timestamp);
                    }
                }
            }
            _ => {}
        }
    }
    index
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn timestamp_offsets_and_whole_seconds() {
        let item = |ts: &str| Item {
            id: "tool#0".into(),
            ts: Some(ts.into()),
            kind: "tool_use".into(),
            text: None,
            tool_use_id: Some("call_a".into()),
            tool_name: Some("Bash".into()),
            tool_input: None,
            task: None,
        };
        for (input, expected) in [
            ("2026-01-01T08:01:00+08:00", "2026-01-01T00:01:00Z"),
            ("2026-01-01T00:01:00.500Z", "2026-01-01T00:01:00Z"),
            ("2025-12-31T19:01:00-05:00", "2026-01-01T00:01:00Z"),
        ] {
            assert_eq!(started_at(&item(input)).as_deref(), Some(expected));
        }
    }
}

//! The on-device recent-conversation and recent-setup lists (`spec/fixtures/recents`).
//! These functions only transform their inputs; storage and the current time belong to the clients.

use std::collections::{BTreeSet, HashMap};

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Fixture-facing operation input for host projections. `existing` supplies the shown operation's persistence
/// predicate explicitly, as a list rather than a closure. `domain` selects the existing fixture document and
/// `operation` one of its pure transformations; expected values stay outside the core.
#[derive(Debug, Deserialize)]
pub struct Input {
    pub domain: String,
    pub operation: String,
    pub input: Value,
}

/// The complete fixture-shaped result, keeping arrays, indexes and model strings exact.
#[derive(Debug, Serialize)]
#[serde(untagged)]
pub enum Projection {
    After { after: Vec<String> },
    Setups { after: Vec<RecentSetup> },
    Shown { shown: Vec<String> },
    Cards { cards: Vec<SetupCard> },
    Model { model: String },
}

/// Apply one of the existing pure operations without changing its behavior or introducing storage.
pub fn input_fixture(input: &Input) -> Result<Projection, String> {
    let object = input
        .input
        .as_object()
        .ok_or_else(|| "input must be an object".to_owned())?;
    let strings = |key: &str| -> Result<Vec<String>, String> {
        serde_json::from_value(object.get(key).cloned().ok_or_else(|| format!("missing {key}"))?)
            .map_err(|error| format!("{key}: {error}"))
    };
    let text = |key: &str| -> Result<&str, String> {
        object
            .get(key)
            .and_then(Value::as_str)
            .ok_or_else(|| format!("{key} must be a string"))
    };
    let setups = |key: &str| -> Result<Vec<RecentSetup>, String> {
        serde_json::from_value(object.get(key).cloned().ok_or_else(|| format!("missing {key}"))?)
            .map_err(|error| format!("{key}: {error}"))
    };
    match (input.domain.as_str(), input.operation.as_str()) {
        ("recent_conversations", "open") => Ok(Projection::After {
            after: open_conversation(&strings("before")?, text("opened")?),
        }),
        ("recent_conversations", "shown") => {
            let existing: BTreeSet<String> = strings("existing")?.into_iter().collect();
            let limit = object
                .get("limit")
                .and_then(Value::as_u64)
                .and_then(|value| usize::try_from(value).ok())
                .ok_or_else(|| "limit must be a nonnegative integer".to_owned())?;
            Ok(Projection::Shown {
                shown: shown_conversations(
                    &strings("entries")?,
                    text("current")?,
                    |key| existing.contains(key),
                    limit,
                ),
            })
        }
        ("recent_setups", "record") => Ok(Projection::Setups {
            after: record_setup(
                &setups("before")?,
                &serde_json::from_value(object.get("setup").cloned().ok_or_else(|| "missing setup".to_owned())?)
                    .map_err(|error| format!("setup: {error}"))?,
            ),
        }),
        ("recent_setups", "cards") => Ok(Projection::Cards {
            cards: setup_cards(
                &setups("entries")?,
                &serde_json::from_value::<Vec<Machine>>(
                    object
                        .get("machines")
                        .cloned()
                        .ok_or_else(|| "missing machines".to_owned())?,
                )
                .map_err(|error| format!("machines: {error}"))?,
            ),
        }),
        ("recent_setups", "model") => Ok(Projection::Model {
            model: setup_model(&setups("entries")?, text("machine")?, text("launcher")?).to_owned(),
        }),
        _ => Err("unknown recents operation".to_owned()),
    }
}

pub const MAX_RECENT_CONVERSATIONS: usize = 10;
pub const MAX_RECENT_SETUPS: usize = 20;
pub const MAX_RECENT_SETUP_CARDS: usize = 3;

/// The conversation just opened goes first, once. An empty key is ignored.
pub fn open_conversation(before: &[String], opened: &str) -> Vec<String> {
    if opened.is_empty() {
        return before.to_vec();
    }
    std::iter::once(opened.to_owned())
        .chain(before.iter().filter(|key| key.as_str() != opened).cloned())
        .take(MAX_RECENT_CONVERSATIONS)
        .collect()
}

/// Offer existing conversations, newest first, without the one already on the page.
pub fn shown_conversations<F>(entries: &[String], current: &str, existing: F, limit: usize) -> Vec<String>
where
    F: Fn(&str) -> bool,
{
    entries
        .iter()
        .filter(|key| key.as_str() != current && existing(key))
        .take(limit)
        .cloned()
        .collect()
}

/// One New Agent setup. `at` records a start but is not part of its identity.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct RecentSetup {
    pub machine: String,
    pub cwd: String,
    pub launcher: String,
    pub model: String,
    pub agent_type: String,
    pub mode: String,
    pub effort: String,
    pub at: String,
}

impl RecentSetup {
    pub fn same_setup(&self, other: &Self) -> bool {
        self.machine == other.machine
            && self.cwd == other.cwd
            && self.launcher == other.launcher
            && self.model == other.model
            && self.agent_type == other.agent_type
            && self.mode == other.mode
            && self.effort == other.effort
    }
}

/// Record a start: replace an identical setup's time and move it first, then retain the newest 20.
pub fn record_setup(before: &[RecentSetup], setup: &RecentSetup) -> Vec<RecentSetup> {
    std::iter::once(setup.clone())
        .chain(before.iter().filter(|entry| !entry.same_setup(setup)).cloned())
        .take(MAX_RECENT_SETUPS)
        .collect()
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
pub struct Machine {
    pub id: String,
    pub online: bool,
}

/// A card points into the entries array so no setup data needs to be duplicated.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
pub struct SetupCard {
    pub index: usize,
    pub online: bool,
}

/// Keep entries for machines the hub still lists. Offline machines remain visible but not pickable.
/// If a machine occurs twice in the registry, its first record wins, matching the reference clients.
pub fn setup_cards(entries: &[RecentSetup], machines: &[Machine]) -> Vec<SetupCard> {
    let mut online = HashMap::new();
    for machine in machines {
        online.entry(machine.id.as_str()).or_insert(machine.online);
    }
    entries
        .iter()
        .enumerate()
        .filter_map(|(index, entry)| {
            online.get(entry.machine.as_str()).map(|&is_online| SetupCard {
                index,
                online: is_online,
            })
        })
        .take(MAX_RECENT_SETUP_CARDS)
        .collect()
}

/// Restore the model of the machine's newest entry with the selected launcher, or the launcher default (`""`).
pub fn setup_model<'a>(entries: &'a [RecentSetup], machine: &str, launcher: &str) -> &'a str {
    entries
        .iter()
        .find(|entry| entry.machine == machine && entry.launcher == launcher)
        .map_or("", |entry| entry.model.as_str())
}

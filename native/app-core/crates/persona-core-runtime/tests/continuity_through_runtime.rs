//! Replays `spec/fixtures/continuity` the way a client sees the core: encoded actions in, effects through the host,
//! and the list rebuilt from drained patches. The clients' binding tests do the same through their own codecs.

use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use persona_core_runtime::wire::{
    self, Action, ChatEvent, ChatPatch, ChatSnapshot, Effect, ListOp, Request, WireBatch,
};
use persona_core_runtime::{Host, Runtime};
use serde_json::{Map, Value, json};

#[derive(Default)]
struct Recorder {
    effects: Mutex<Vec<Effect>>,
    changes: Mutex<Vec<(String, u64)>>,
}

impl Host for Recorder {
    fn changed_chat(&self, chat: &str, version: u64) {
        self.changes.lock().unwrap().push((chat.to_owned(), version));
    }
    fn run(&self, effect: &[u8]) {
        self.effects
            .lock()
            .unwrap()
            .push(wire::decode(effect).expect("effect decodes"));
    }
}

fn items_json(object: &Map<String, Value>) -> String {
    object.get("items").map_or_else(|| "[]".to_owned(), Value::to_string)
}
fn text(object: &Map<String, Value>, key: &str) -> Option<String> {
    object.get(key).and_then(Value::as_str).map(str::to_owned)
}
fn number(object: &Map<String, Value>, key: &str) -> Option<i64> {
    object.get(key).and_then(Value::as_i64)
}
fn boolean(object: &Map<String, Value>, key: &str) -> Option<bool> {
    object.get(key).and_then(Value::as_bool)
}
fn flag(object: &Map<String, Value>, key: &str) -> bool {
    boolean(object, key) == Some(true)
}
fn batch(event: &Map<String, Value>) -> WireBatch {
    WireBatch {
        items_json: items_json(event),
        history: text(event, "history"),
        from_seq: number(event, "from_seq"),
        to_seq: number(event, "to_seq"),
        backlog: flag(event, "backlog"),
        has_more: boolean(event, "has_more"),
        more_after: flag(event, "more_after"),
    }
}

fn request_json(request: &Request) -> Value {
    match request {
        Request::FetchAfter { after_seq } => json!({"kind": "fetch_after", "after_seq": after_seq}),
        Request::FetchLatest { fold_work } => json!({"kind": "fetch_latest", "fold_work": fold_work}),
        Request::Subscribe {
            since_seq: Some(since),
            fold_work,
        } => {
            json!({"kind": "subscribe", "fold_work": fold_work, "since_seq": since})
        }
        Request::Subscribe {
            since_seq: None,
            fold_work,
        } => json!({"kind": "subscribe", "fold_work": fold_work}),
        Request::FetchOlder { before_seq, fold_work } => {
            json!({"kind": "fetch_older", "before_seq": before_seq, "fold_work": fold_work})
        }
        Request::FetchRange {
            from_seq, through_seq, ..
        } => {
            json!({"kind": "fetch_range", "from_seq": from_seq, "through_seq": through_seq})
        }
    }
}

const CHAT: &str = "fixture/continuity";

#[test]
fn continuity_fixtures_through_runtime() {
    let dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../spec/fixtures/continuity");
    let mut files: Vec<PathBuf> = std::fs::read_dir(dir)
        .unwrap()
        .map(|e| e.unwrap().path())
        .filter(|p| p.extension().is_some_and(|e| e == "json"))
        .collect();
    files.sort();
    let mut steps = 0;
    let mut cases = 0;
    for file in files {
        let document: Value = serde_json::from_str(&std::fs::read_to_string(&file).unwrap()).unwrap();
        for case in document["cases"].as_array().unwrap() {
            cases += 1;
            let name = case["name"].as_str().unwrap();
            let host = Arc::new(Recorder::default());
            let runtime = Runtime::new(host.clone());
            let initial = case["input"]["initial"].as_object().unwrap();
            let snapshot = ChatSnapshot {
                items_json: items_json(initial),
                history: text(initial, "history"),
                covered: number(initial, "covered").unwrap(),
                has_more: boolean(initial, "has_more"),
                loaded_once: flag(initial, "loaded_once"),
            };
            assert!(runtime.dispatch(&wire::encode(&Action::OpenChat {
                chat: CHAT.into(),
                snapshot
            })));
            let mut list: Vec<String> = Vec::new();
            let drain = |runtime: &Runtime, list: &mut Vec<String>| -> ChatPatch {
                assert!(
                    runtime.flush(Duration::from_secs(5)),
                    "{name}: the worker did not finish"
                );
                let patch: ChatPatch = wire::decode(&runtime.drain(CHAT)).unwrap();
                for op in &patch.ops {
                    match op {
                        ListOp::Remove { index } => {
                            list.remove(*index as usize);
                        }
                        ListOp::Insert { index, json, .. } => list.insert(*index as usize, json.clone()),
                        ListOp::Update { index, json, .. } => list[*index as usize] = json.clone(),
                    }
                }
                patch
            };
            drain(&runtime, &mut list);
            host.effects.lock().unwrap().clear();
            let events = case["input"]["events"].as_array().unwrap();
            for (index, (event, want)) in events.iter().zip(case["expected"].as_array().unwrap()).enumerate() {
                let label = format!("{name} step {index}");
                let event = event.as_object().unwrap();
                let chat_event = match text(event, "kind").as_deref().unwrap() {
                    "http_latest" => Some(ChatEvent::Latest { batch: batch(event) }),
                    "ws_items" => Some(ChatEvent::Pushed { batch: batch(event) }),
                    "http_after" => Some(ChatEvent::After { batch: batch(event) }),
                    "http_reload" => Some(ChatEvent::Reloaded {
                        items_json: items_json(event),
                        history: text(event, "history"),
                        to_seq: number(event, "to_seq"),
                        has_more: boolean(event, "has_more").unwrap(),
                    }),
                    "http_older" => Some(ChatEvent::Older {
                        items_json: items_json(event),
                        has_more: boolean(event, "has_more").unwrap(),
                    }),
                    "http_range" => Some(ChatEvent::Range {
                        fold_id: None,
                        items_json: items_json(event),
                        more: flag(event, "more"),
                    }),
                    "reconnect" => Some(ChatEvent::Reconnected),
                    "load_older" => Some(ChatEvent::LoadOlder),
                    "open_fold" => Some(ChatEvent::OpenFold {
                        fold_id: text(event, "fold_id").unwrap(),
                    }),
                    "continue_fill" => None,
                    other => panic!("{label}: unknown event {other}"),
                };
                let action = match chat_event {
                    Some(event) => Action::Chat {
                        chat: CHAT.into(),
                        event,
                    },
                    None => Action::ContinueFill { chat: CHAT.into() },
                };
                assert!(runtime.dispatch(&wire::encode(&action)));
                let patch = drain(&runtime, &mut list);
                let effects = std::mem::take(&mut *host.effects.lock().unwrap());
                let mut requests = Vec::new();
                for effect in effects {
                    match effect {
                        Effect::Fetch { chat, request } => {
                            assert_eq!(chat, CHAT);
                            requests.push(request_json(&request));
                        }
                        Effect::Rejected { reason } => panic!("{label}: rejected: {reason}"),
                    }
                }
                let got: Vec<Value> = list.iter().map(|j| serde_json::from_str(j).unwrap()).collect();
                assert_eq!(Value::Array(got), want["items"], "{label} items");
                assert_eq!(json!(patch.covered), want["covered"], "{label} covered");
                assert_eq!(json!(patch.history), want["history"], "{label} history");
                assert_eq!(json!(patch.has_more), want["has_more"], "{label} has_more");
                assert_eq!(json!(patch.loaded_once), want["loaded_once"], "{label} loaded_once");
                assert_eq!(Value::Array(requests), want["actions"], "{label} actions");
                steps += 1;
            }
        }
    }
    assert_eq!((cases, steps), (39, 95), "every continuity case and step replays");
}

#[test]
fn one_chats_drain_does_not_consume_another_chats_wakeup() {
    let host = Arc::new(Recorder::default());
    let runtime = Runtime::new(host.clone());
    for chat in ["a", "b"] {
        assert!(runtime.dispatch(&wire::encode(&Action::OpenChat {
            chat: chat.into(),
            snapshot: ChatSnapshot::default(),
        })));
    }
    assert!(runtime.flush(Duration::from_secs(5)));
    assert_eq!(
        host.changes.lock().unwrap().as_slice(),
        [("a".to_owned(), 1), ("b".to_owned(), 2)]
    );
    let _ = runtime.drain("a");
    let batch = WireBatch {
        items_json: r#"[{"id":"1#0","seq":1,"role":"assistant","kind":"text","text":"new"}]"#.into(),
        history: Some("h".into()),
        from_seq: Some(0),
        to_seq: Some(1),
        ..WireBatch::default()
    };
    assert!(runtime.dispatch(&wire::encode(&Action::Chat {
        chat: "b".into(),
        event: ChatEvent::Pushed { batch },
    })));
    assert!(runtime.flush(Duration::from_secs(5)));
    assert_eq!(
        host.changes.lock().unwrap().last().map(|(chat, _)| chat.as_str()),
        Some("b"),
        "b must be woken again after a is drained"
    );
    let patch: ChatPatch = wire::decode(&runtime.drain("b")).unwrap();
    assert_eq!(patch.ops.len(), 1);
}

#[test]
fn drain_commits_one_complete_patch_instead_of_mixing_state_with_old_ops() {
    let host = Arc::new(Recorder::default());
    let runtime = Runtime::new(host);
    assert!(runtime.dispatch(&wire::encode(&Action::OpenChat {
        chat: "a".into(),
        snapshot: ChatSnapshot::default(),
    })));
    assert!(runtime.flush(Duration::from_secs(5)));
    let _ = runtime.drain("a");
    for seq in 1..=2 {
        let batch = WireBatch {
            items_json: format!(r#"[{{"id":"{seq}#0","seq":{seq},"role":"assistant","kind":"text","text":"new"}}]"#),
            history: Some("h".into()),
            from_seq: Some(seq - 1),
            to_seq: Some(seq),
            ..WireBatch::default()
        };
        assert!(runtime.dispatch(&wire::encode(&Action::Chat {
            chat: "a".into(),
            event: ChatEvent::Pushed { batch },
        })));
        assert!(runtime.flush(Duration::from_secs(5)));
        let patch: ChatPatch = wire::decode(&runtime.drain("a")).unwrap();
        assert_eq!(patch.ops.len(), 1);
        assert_eq!(patch.covered, seq);
        assert!(!patch.loaded_once);
        let idle: ChatPatch = wire::decode(&runtime.drain("a")).unwrap();
        assert!(idle.ops.is_empty());
        assert_eq!(
            idle.version, patch.version,
            "an unchanged drain keeps the committed version"
        );
        assert_eq!(idle.covered, patch.covered);
        assert_eq!(idle.history, patch.history);
        assert_eq!(idle.loaded_once, patch.loaded_once);
    }
}

#[test]
fn drained_state_suppresses_noop_publishes_and_wakes() {
    let host = Arc::new(Recorder::default());
    let runtime = Runtime::new(host.clone());
    assert!(runtime.dispatch(&wire::encode(&Action::OpenChat {
        chat: "a".into(),
        snapshot: ChatSnapshot {
            items_json: r#"[{"id":"1#0","seq":1,"role":"assistant","kind":"text","text":"cached"}]"#.into(),
            history: Some("h".into()),
            covered: 1,
            has_more: Some(false),
            loaded_once: true,
        },
    })));
    assert!(runtime.flush(Duration::from_secs(5)));
    let initial: ChatPatch = wire::decode(&runtime.drain("a")).unwrap();
    assert_eq!(initial.ops.len(), 1);
    host.changes.lock().unwrap().clear();
    let mut verified: Option<ChatPatch> = None;
    for _ in 0..2 {
        assert!(runtime.dispatch(&wire::encode(&Action::Chat {
            chat: "a".into(),
            event: ChatEvent::Latest {
                batch: WireBatch {
                    items_json: "[]".into(),
                    history: Some("h".into()),
                    to_seq: Some(1),
                    backlog: true,
                    has_more: Some(false),
                    ..WireBatch::default()
                },
            },
        })));
        assert!(runtime.flush(Duration::from_secs(5)));
        let patch: ChatPatch = wire::decode(&runtime.drain("a")).unwrap();
        if host.changes.lock().unwrap().is_empty() {
            verified = Some(patch);
            break;
        }
        // The first latest page verifies this restored chat. Its changed state must be drained before testing no-ops.
        assert!(patch.verified);
        assert!(patch.ops.is_empty());
        host.changes.lock().unwrap().clear();
    }
    let verified = verified.expect("the first changed latest is followed by a no-op");
    for _ in 0..3 {
        assert!(runtime.dispatch(&wire::encode(&Action::Chat {
            chat: "a".into(),
            event: ChatEvent::Latest {
                batch: WireBatch {
                    items_json: "[]".into(),
                    history: Some("h".into()),
                    to_seq: Some(1),
                    backlog: true,
                    has_more: Some(false),
                    ..WireBatch::default()
                },
            },
        })));
        assert!(runtime.flush(Duration::from_secs(5)));
        assert!(
            host.changes.lock().unwrap().is_empty(),
            "an unchanged drained chat must stay asleep"
        );
        let patch: ChatPatch = wire::decode(&runtime.drain("a")).unwrap();
        assert_eq!(patch.version, verified.version);
        assert!(patch.ops.is_empty());
        assert_eq!(patch.covered, 1);
        assert!(patch.loaded_once);
    }
}

#[test]
fn appending_to_one_cached_item_does_not_duplicate_it() {
    let host = Arc::new(Recorder::default());
    let runtime = Runtime::new(host);
    let initial = r#"[{"id":"1#0","seq":1,"role":"user","kind":"text","text":"synthetic"}]"#;
    assert!(runtime.dispatch(&wire::encode(&Action::OpenChat {
        chat: "a".into(),
        snapshot: ChatSnapshot {
            items_json: initial.into(),
            covered: 12,
            loaded_once: true,
            ..ChatSnapshot::default()
        },
    })));
    assert!(runtime.flush(Duration::from_secs(5)));
    let _ = runtime.drain("a");
    let batch = WireBatch {
        items_json: r#"[{"id":"13#0","seq":13,"role":"assistant","kind":"text","text":"new"}]"#.into(),
        history: Some("h".into()),
        from_seq: Some(12),
        to_seq: Some(15),
        ..WireBatch::default()
    };
    assert!(runtime.dispatch(&wire::encode(&Action::Chat {
        chat: "a".into(),
        event: ChatEvent::Pushed { batch }
    })));
    assert!(runtime.flush(Duration::from_secs(5)));
    let patch: ChatPatch = wire::decode(&runtime.drain("a")).unwrap();
    let kinds: Vec<&str> = patch
        .ops
        .iter()
        .map(|op| match op {
            ListOp::Insert { .. } => "insert",
            ListOp::Update { .. } => "update",
            ListOp::Remove { .. } => "remove",
        })
        .collect();
    assert_eq!(kinds, ["insert"], "one retained cached item plus one new item");
}

#[test]
fn rejects_garbage_and_unknown_chats() {
    let host = Arc::new(Recorder::default());
    let runtime = Runtime::new(host.clone());
    assert!(runtime.dispatch(&[0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f]));
    let event = Action::Chat {
        chat: "nowhere".into(),
        event: ChatEvent::Reconnected,
    };
    assert!(runtime.dispatch(&wire::encode(&event)));
    assert!(runtime.flush(Duration::from_secs(5)));
    let effects = host.effects.lock().unwrap();
    assert_eq!(effects.len(), 2);
    assert!(effects.iter().all(|e| matches!(e, Effect::Rejected { .. })));
}

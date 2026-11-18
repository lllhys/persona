//! Transcript rows grouped into exchanges, following iOS's `ChatStore.buildRows` and `ChatBlock.build`.
//!
//! This is the turn-projection slice of `lines`, not the rendered `ChatLine` projection. It owns tool/result pairing,
//! user-picture placement and the turn boundaries: ordinary and queued messages, and the wake of an idle agent, which
//! starts a turn of its own (#9140). Times are caller-supplied item timestamps; no clock or IO is read.

use serde::Deserialize;
use serde_json::Value;

use crate::{compat::CONTEXT_TAGS, state::utc_timestamp};

#[derive(Debug, Clone, Deserialize)]
pub struct Input {
    pub frozen: Option<Frozen>,
    /// Render options belong to the later ChatLine projection, not to turn grouping.
    #[serde(flatten)]
    pub options: serde_json::Map<String, Value>,
    pub items: Vec<Item>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Frozen {
    pub seq: i64,
    pub part: i64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Item {
    pub id: String,
    pub seq: i64,
    pub ts: Option<String>,
    pub role: String,
    pub kind: String,
    pub text: Option<String>,
    pub tool_use_id: Option<String>,
    #[serde(default)]
    pub queued: Option<bool>,
    #[serde(default)]
    pub wake: Option<bool>,
    pub task: Option<Task>,
    pub client_msg_id: Option<String>,
    pub from: Option<String>,
    pub injected: Option<Injected>,
    pub compact: Option<Compaction>,
    pub model_refusal: Option<Value>,
    pub fold: Option<Fold>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Compaction {
    pub trigger: Option<String>,
    pub notes: Option<Vec<String>>,
}

// i18n-ignore: exact hub notices used by ChatStore.compactions.
const COMPACTED_TEXT: &str = "上下文已压缩";
// i18n-ignore: exact hub notices used by ChatStore.compactions.
const CONTINUED_TEXT: &str = "已从压缩的上下文继续";

fn compaction_notice(item: &Item, text: &str) -> bool {
    item.kind == "notice" && item.task.is_none() && item.wake != Some(true) && item.text.as_deref() == Some(text)
}

/// Fold the boundary, its summary and any immediately following idle note before constructing rows. These are one
/// inspectable compaction step on Apple, not separate notices in the exchange's work.
fn compactions(items: &[Item]) -> Vec<Item> {
    let mut out: Vec<Item> = Vec::with_capacity(items.len());
    let mut open: Option<usize> = None;
    for item in items {
        if item.compact.is_some() || compaction_notice(item, COMPACTED_TEXT) {
            let mut boundary = item.clone();
            let compact = boundary.compact.get_or_insert(Compaction {
                trigger: None,
                notes: None,
            });
            compact
                .notes
                .get_or_insert_with(Vec::new)
                .push(item.text.clone().unwrap_or_else(|| COMPACTED_TEXT.to_owned()));
            out.push(boundary);
            open = Some(out.len() - 1);
            continue;
        }
        let continued = compaction_notice(item, CONTINUED_TEXT);
        let idle = item.kind == "notice"
            && item
                .text
                .as_deref()
                .is_some_and(|text| text.starts_with("Compacted while idle"));
        if continued || idle {
            if let Some(index) = open {
                let compact = out[index].compact.as_mut().expect("open compaction");
                compact
                    .notes
                    .get_or_insert_with(Vec::new)
                    .push(item.text.clone().unwrap_or_default());
                if idle {
                    compact.trigger = Some("idle".to_owned());
                }
                continue;
            }
            if continued {
                let mut boundary = item.clone();
                boundary.compact = Some(Compaction {
                    trigger: None,
                    notes: Some(vec![item.text.clone().unwrap_or_else(|| CONTINUED_TEXT.to_owned())]),
                });
                out.push(boundary);
                open = Some(out.len() - 1);
                continue;
            }
        }
        open = None;
        out.push(item.clone());
    }
    out
}

#[derive(Debug, Clone, Deserialize)]
pub struct Task {
    pub kind: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Injected {
    pub state: Option<String>,
}

/// `MessageSource.protectsUser`: the first leading message-source envelope is authoritative. An explicit user author,
/// malformed JSON or a non-string/missing kind vetoes legacy injection classification, even over explicit metadata.
fn context_prefix(text: &str) -> Option<(&'static str, &str)> {
    for tag in CONTEXT_TAGS {
        let Some(rest) = text.strip_prefix(&format!("<{tag}")) else {
            continue;
        };
        if rest
            .as_bytes()
            .first()
            .is_some_and(|byte| byte.is_ascii_whitespace() || matches!(byte, b'/' | b'>'))
        {
            return Some((tag, rest));
        }
    }
    None
}

fn protects_user(text: &str) -> bool {
    let mut rest = text.trim_start();
    while let Some((tag, _)) = context_prefix(rest) {
        let Some((attributes, after_attributes)) = rest.split_once('>') else {
            break;
        };
        let Some((body, after_body)) = after_attributes.split_once(&format!("</{tag}>")) else {
            break;
        };
        if attributes.contains("kind=\"message-source\"") {
            let Ok(metadata) = serde_json::from_str::<Value>(body) else {
                return true;
            };
            // Foundation's JSONDecoder rejects non-finite Doubles when serde_json accepts an overflowing number.
            fn has_non_finite_number(value: &Value) -> bool {
                match value {
                    Value::Number(number) => number.as_f64().is_none_or(|number| !number.is_finite()),
                    Value::Array(values) => values.iter().any(has_non_finite_number),
                    Value::Object(values) => values.values().any(has_non_finite_number),
                    _ => false,
                }
            }
            if has_non_finite_number(&metadata) {
                return true;
            }
            return metadata.get("author").and_then(Value::as_str) == Some("user")
                || !metadata
                    .get("kind")
                    .and_then(Value::as_str)
                    .is_some_and(|kind| !kind.trim().is_empty());
        }
        rest = after_body.trim_start();
    }
    false
}

fn leading_context_stripped(text: &str) -> &str {
    let mut rest = text.trim();
    while let Some((tag, after_open)) = context_prefix(rest) {
        let Some((_, after_attributes)) = after_open.split_once('>') else {
            break;
        };
        let Some((_, after_body)) = after_attributes.split_once(&format!("</{tag}>")) else {
            break;
        };
        rest = after_body.trim();
    }
    rest
}

/// The supervision subset of `Injected.of`: no generic substring matching and no inference from an app/peer input.
fn is_supervision(item: &Item) -> bool {
    if item.role != "user" || item.kind != "text" {
        return false;
    }
    let text = item.text.as_deref().unwrap_or_default();
    if protects_user(text) {
        return false;
    }
    if let Some(injected) = &item.injected {
        return injected.state.as_deref() == Some("supervise");
    }
    if item.client_msg_id.is_some() || item.from.is_some() {
        return false;
    }
    let text = leading_context_stripped(text);
    let Some((head, rest)) = text.split_once('\n') else {
        return false;
    };
    let Some(count) = head.strip_prefix("📋 督办：你派出去的任务有 ") else {
        return false;
    };
    let Some(count) = count.strip_suffix(" 个要你处理") else {
        return false;
    };
    !count.is_empty() && count.bytes().all(|byte| byte.is_ascii_digit()) && rest.contains("这是 Persona 的督办消息")
}

#[derive(Debug, Clone, Deserialize)]
pub struct Fold {
    /// These fields are present only for an actual fold item. Optional outer `fold` still deserializes an absent
    /// payload for every ordinary item; a real fold uses the existing zero-count compatibility behavior.
    #[serde(default)]
    pub tools: i64,
    #[serde(default)]
    pub thoughts: i64,
    #[serde(default)]
    pub through_seq: i64,
    #[serde(default)]
    pub through_id: String,
    #[serde(default)]
    pub until: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Kind {
    User,
    Assistant,
    Thinking,
    Tool,
    Notice,
    Image,
    Recap,
}

#[derive(Debug, Clone)]
struct Row {
    id: String,
    kind: Kind,
    entry: Option<String>,
    queued: bool,
    supervision: bool,
    wake: bool,
    refusal: bool,
    interrupt: bool,
    start: Option<i128>,
    end: Option<i128>,
    tools: i64,
    thoughts: i64,
}

impl Row {
    fn user_input(&self) -> bool {
        self.kind == Kind::User || (self.kind == Kind::Image && self.entry.is_some())
    }

    fn assistant(&self) -> bool {
        self.kind == Kind::Assistant
    }

    fn first_ts(&self) -> Option<i128> {
        self.start
    }

    fn last_ts(&self) -> Option<i128> {
        self.end
    }
}

fn row(item: &Item) -> Option<Row> {
    let timestamp = || item.ts.as_deref().and_then(utc_timestamp);
    let kind = match item.kind.as_str() {
        "text" if item.role == "user" => Kind::User,
        "text" => Kind::Assistant,
        "thinking" => Kind::Thinking,
        "notice" => Kind::Notice,
        "image" => Kind::Image,
        "recap" => Kind::Recap,
        _ => return None,
    };
    Some(Row {
        id: item.id.clone(),
        kind,
        entry: ((kind == Kind::User) || (kind == Kind::Image && item.role == "user" && item.tool_use_id.is_none()))
            .then(|| item.id.split('#').next().unwrap_or(&item.id).to_owned()),
        queued: item.queued == Some(true),
        supervision: is_supervision(item),
        wake: item.wake == Some(true),
        refusal: item.model_refusal.is_some(),
        interrupt: item.task.is_none() && item.text.as_deref().is_some_and(|text| text.contains("中断")), // i18n-ignore: hub notice
        start: timestamp(),
        end: timestamp(),
        tools: 0,
        thoughts: 0,
    })
}

fn rows(items: &[Item]) -> Vec<Row> {
    let mut ordered = Vec::with_capacity(items.len());
    let mut pictures = Vec::new();
    for item in items {
        let picture = item.kind == "image" && item.role == "user" && item.tool_use_id.is_none();
        if picture {
            if pictures.first().is_some_and(|first: &Item| first.seq != item.seq) {
                ordered.append(&mut pictures);
            }
            pictures.push(item.clone());
            continue;
        }
        if let Some(first) = pictures.first() {
            if item.kind == "text" && item.role == "user" && item.seq == first.seq {
                ordered.push(item.clone());
                ordered.append(&mut pictures);
            } else {
                ordered.append(&mut pictures);
                ordered.push(item.clone());
            }
        } else {
            ordered.push(item.clone());
        }
    }
    ordered.extend(pictures);
    let ordered = compactions(&ordered);
    let mut rows = Vec::new();
    let mut tools: Vec<Row> = Vec::new();
    let mut thinking: Vec<Row> = Vec::new();
    let flush_thoughts = |rows: &mut Vec<Row>, tools: &mut Vec<Row>, thinking: &mut Vec<Row>| {
        if tools.is_empty() {
            rows.append(thinking);
        } else {
            tools.append(thinking);
        }
    };
    let flush_run = |rows: &mut Vec<Row>, tools: &mut Vec<Row>| {
        if tools.is_empty() {
            return;
        }
        let id = format!("tools-{}", tools[0].id);
        rows.push(Row {
            id,
            kind: Kind::Tool,
            entry: None,
            queued: false,
            supervision: false,
            wake: false,
            refusal: false,
            interrupt: false,
            start: tools.iter().find_map(Row::first_ts),
            end: tools.iter().rev().find_map(Row::last_ts),
            tools: tools.iter().map(|row| row.tools).sum(),
            thoughts: tools.iter().map(|row| row.thoughts).sum::<i64>()
                + tools.iter().filter(|row| row.kind == Kind::Thinking).count() as i64,
        });
        tools.clear();
    };
    for item in &ordered {
        match item.kind.as_str() {
            "tool_use" => {
                flush_thoughts(&mut rows, &mut tools, &mut thinking);
                let timestamp = item.ts.as_deref().and_then(utc_timestamp);
                let result_end = ordered
                    .iter()
                    .rfind(|other| {
                        other.kind == "tool_result"
                            && other.tool_use_id.is_some()
                            && other.tool_use_id == item.tool_use_id
                    })
                    .and_then(|result| result.ts.as_deref().and_then(utc_timestamp));
                tools.push(Row {
                    id: item.id.clone(),
                    kind: Kind::Tool,
                    entry: None,
                    queued: false,
                    supervision: false,
                    wake: false,
                    refusal: false,
                    interrupt: false,
                    start: timestamp,
                    end: result_end.or(timestamp),
                    tools: 1,
                    thoughts: 0,
                });
            }
            "thinking" => {
                if let Some(row) = row(item) {
                    thinking.push(row);
                }
            }
            "fold" => {
                flush_thoughts(&mut rows, &mut tools, &mut thinking);
                let timestamp = item.ts.as_deref().and_then(utc_timestamp);
                tools.push(Row {
                    id: item.id.clone(),
                    kind: Kind::Tool,
                    entry: None,
                    queued: false,
                    supervision: false,
                    wake: false,
                    refusal: false,
                    interrupt: false,
                    start: timestamp,
                    end: item
                        .fold
                        .as_ref()
                        .and_then(|fold| fold.until.as_deref())
                        .and_then(utc_timestamp)
                        .or(timestamp),
                    tools: item.fold.as_ref().map_or(0, |fold| fold.tools),
                    thoughts: item.fold.as_ref().map_or(0, |fold| fold.thoughts),
                });
            }
            "tool_result" => {
                if item.task.as_ref().is_some_and(|task| task.kind == "agent") && item.wake == Some(true) {
                    flush_thoughts(&mut rows, &mut tools, &mut thinking);
                    flush_run(&mut rows, &mut tools);
                    let timestamp = || item.ts.as_deref().and_then(utc_timestamp);
                    rows.push(Row {
                        id: item.id.clone(),
                        kind: Kind::Notice,
                        entry: None,
                        queued: false,
                        supervision: false,
                        wake: true,
                        refusal: false,
                        interrupt: false,
                        start: timestamp(),
                        end: timestamp(),
                        tools: 0,
                        thoughts: 0,
                    });
                }
            }
            "image" if item.tool_use_id.is_some() => {}
            _ => {
                flush_thoughts(&mut rows, &mut tools, &mut thinking);
                flush_run(&mut rows, &mut tools);
                rows.extend(row(item));
            }
        }
    }
    flush_thoughts(&mut rows, &mut tools, &mut thinking);
    flush_run(&mut rows, &mut tools);
    rows
}

#[derive(Debug, Clone, Default)]
struct Turn {
    id: String,
    users: Vec<Row>,
    process: Vec<Row>,
    answer: Option<Row>,
    trailing: Vec<Row>,
    /// The wake that opened this turn (a background task's notice, or a subagent's report); empty when a message did.
    wakes: Vec<Row>,
    steps: Vec<Row>,
    ordered: Vec<Row>,
}

impl Turn {
    fn close(mut self) -> Self {
        self.steps = self.process.clone();
        if let Some(index) = self.process.iter().rposition(Row::assistant) {
            self.answer = Some(self.process.remove(index));
        }
        self
    }

    fn timestamp(&self, last: bool) -> Option<i128> {
        if last {
            self.steps.iter().chain(&self.trailing).rev().find_map(Row::last_ts)
        } else {
            self.users.iter().chain(&self.steps).find_map(Row::first_ts)
        }
    }

    fn json(&self) -> Value {
        let ids = |rows: &[Row]| rows.iter().map(|row| Value::String(row.id.clone())).collect::<Vec<_>>();
        serde_json::json!({
            "id": self.id,
            "user_ids": ids(&self.users),
            "process_ids": ids(&self.process),
            "answer_id": self.answer.as_ref().map(|row| &row.id),
            "trailing_ids": ids(&self.trailing),
            "wake_ids": ids(&self.wakes),
            // A turn has no pending wake and no tail since #9140 (a wake opens its own turn); the keys keep the
            // corpus's shape.
            "has_pending_wake": false,
            "tail_ids": [],
            "ordered_ids": ids(&self.ordered),
            "tool_count": self.process.iter().map(|row| row.tools).sum::<i64>(),
            "thought_count": self.process.iter().map(|row| row.thoughts).sum::<i64>()
                + self.process.iter().filter(|row| row.kind == Kind::Thinking).count() as i64,
            "interim_count": self.process.iter().filter(|row| row.assistant()).count(),
            "start": self.timestamp(false).and_then(timestamp_text),
            "end": self.timestamp(true).and_then(timestamp_text),
        })
    }
}

fn part(id: &str) -> i64 {
    id.rsplit_once('#').and_then(|(_, part)| part.parse().ok()).unwrap_or(0)
}

/// Replays one static `lines` input through the row and turn projections. Fixture operation sequences and the rendered
/// `ChatLine` / `TaskIndex` projections are deliberately outside this slice.
pub fn project(input: &Input) -> Value {
    let items = input.frozen.as_ref().map_or(input.items.as_slice(), |frozen| {
        let end = input
            .items
            .iter()
            .position(|item| (item.seq, part(&item.id)) > (frozen.seq, frozen.part))
            .unwrap_or(input.items.len());
        &input.items[..end]
    });
    let rows = rows(items);
    let mut current: Option<Turn> = None;
    let mut message_entry = None;
    let mut supervision_round = false;
    let mut blocks: Vec<Value> = Vec::new();
    let close = |current: &mut Option<Turn>, blocks: &mut Vec<Value>| {
        if let Some(turn) = current.take() {
            blocks.push(turn.close().json());
        }
    };
    for row in rows {
        if row.user_input() {
            let same = current
                .as_ref()
                .and_then(|turn| turn.ordered.last())
                .is_some_and(Row::user_input)
                && row.entry == message_entry;
            if let Some(turn) = current
                .as_mut()
                .filter(|_| same || (row.queued && !row.supervision && !supervision_round))
            {
                turn.users.push(row.clone());
                turn.ordered.push(row.clone());
            } else {
                close(&mut current, &mut blocks);
                current = Some(Turn {
                    id: row.id.clone(),
                    users: vec![row.clone()],
                    ordered: vec![row.clone()],
                    ..Turn::default()
                });
            }
            if !same {
                supervision_round = row.supervision;
            }
            message_entry = row.entry.clone();
            continue;
        }
        match row.kind {
            Kind::Recap => {
                close(&mut current, &mut blocks);
                blocks.push(serde_json::json!({"row_id": row.id}));
            }
            Kind::Notice => {
                if row.refusal {
                    let turn = current.get_or_insert_with(|| Turn {
                        id: row.id.clone(),
                        ..Turn::default()
                    });
                    turn.trailing.push(row);
                } else if row.wake {
                    // The turn before the wake was over and keeps its last words as its answer: the work the wake
                    // started is a turn of its own that opens with it, after a recap too (#9140).
                    close(&mut current, &mut blocks);
                    current = Some(Turn {
                        id: row.id.clone(),
                        process: vec![row.clone()],
                        wakes: vec![row.clone()],
                        ordered: vec![row],
                        ..Turn::default()
                    });
                } else if current.is_none() {
                    current = Some(Turn {
                        id: row.id.clone(),
                        process: vec![row.clone()],
                        ordered: vec![row],
                        ..Turn::default()
                    });
                } else if let Some(turn) = current.as_mut() {
                    if row.interrupt {
                        turn.trailing.push(row);
                    } else {
                        turn.process.push(row.clone());
                        turn.ordered.push(row);
                    }
                }
            }
            _ => {
                let turn = current.get_or_insert_with(|| Turn {
                    id: row.id.clone(),
                    ..Turn::default()
                });
                turn.process.push(row.clone());
                turn.ordered.push(row);
            }
        }
    }
    close(&mut current, &mut blocks);
    Value::Array(blocks.into_iter().filter(|block| block.get("id").is_some()).collect())
}

/// Answer selection mutation: choosing the first assistant row must disagree with the fixture corpus.
#[doc(hidden)]
pub fn answer_mutation(input: &Input) -> Option<String> {
    let turns = project(input);
    turns
        .as_array()?
        .first()?
        .as_object()?
        .get("ordered_ids")?
        .as_array()?
        .iter()
        .find_map(|id| {
            let id = id.as_str()?;
            input
                .items
                .iter()
                .find(|item| item.id == id && item.kind == "text" && item.role == "assistant")
                .map(|item| item.id.clone())
        })
}

pub(crate) fn timestamp_text(nanoseconds: i128) -> Option<String> {
    let seconds = nanoseconds.div_euclid(1_000_000_000);
    let days = seconds.div_euclid(86_400);
    let time = seconds.rem_euclid(86_400);
    let (year, month, day) = civil_from_days(days);
    let fraction = nanoseconds.rem_euclid(1_000_000_000);
    let mut result = format!(
        "{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}",
        time / 3600,
        time % 3600 / 60,
        time % 60
    );
    if fraction != 0 {
        result += &format!(".{fraction:09}");
        while result.ends_with('0') {
            result.pop();
        }
    }
    Some(result + "Z")
}

// Howard Hinnant's inverse civil calendar calculation, with 1970-01-01 as day zero.
fn civil_from_days(days: i128) -> (i128, i128, i128) {
    let days = days + 719_468;
    let era = days.div_euclid(146_097);
    let day_of_era = days - era * 146_097;
    let year_of_era = (day_of_era - day_of_era / 1460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let mut year = year_of_era + era * 400;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let month_prime = (5 * day_of_year + 2) / 153;
    let day = day_of_year - (153 * month_prime + 2) / 5 + 1;
    let month = month_prime + if month_prime < 10 { 3 } else { -9 };
    year += i128::from(month <= 2);
    (year, month, day)
}

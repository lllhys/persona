//! Source-derived WebSocket envelope projection (#8955).
//!
//! Pure diagnostic coverage, deliberately separate from `transport::decode_frame`: this never routes an event or
//! changes continuity behavior. The cases in `spec/fixtures/wire-envelope` are hand-derived from the cited Swift
//! sources and were not executed on iOS. Integer spellings and encodings retain transport's existing limitations.
//!
//! Acceptance is projected only for the fixture's event types. Unprojected nested models are shape-checked, never
//! claimed to be a complete Swift implementation. Items retain `Item::from_wire`'s documented limitations.

use std::borrow::Cow;

use serde::Deserialize;
use serde::de::{IgnoredAny, MapAccess, SeqAccess, Visitor};
use serde_json::value::RawValue;
use serde_json::{Map, Value, json};

use super::transport;
use super::transport::seen::{AnyStr, KeyFault, Seen, Shape, Slot, keyed, opens_object};
use crate::Item;

/// A JSONValue number, unlike an Envelope Int: fractional and exponent spellings are Doubles too.
#[derive(Debug)]
struct JsonNumberOrOther {
    valid: bool,
}
impl<'de> Deserialize<'de> for JsonNumberOrOther {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        deserializer
            .deserialize_any(JsonValueNumber)
            .map(|valid| Self { valid })
    }
}

#[derive(Debug)]
struct JsonValueNumber;
impl<'de> Visitor<'de> for JsonValueNumber {
    type Value = bool;

    fn expecting(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str("a JSONValue-compatible value")
    }

    fn visit_bool<E>(self, _: bool) -> Result<Self::Value, E> {
        Ok(true)
    }
    fn visit_i64<E>(self, _: i64) -> Result<Self::Value, E> {
        Ok(true)
    }
    fn visit_u64<E>(self, _: u64) -> Result<Self::Value, E> {
        Ok(true)
    }
    fn visit_f64<E>(self, _: f64) -> Result<Self::Value, E> {
        Ok(true)
    }
    fn visit_str<E>(self, _: &str) -> Result<Self::Value, E> {
        Ok(true)
    }
    fn visit_string<E>(self, _: String) -> Result<Self::Value, E> {
        Ok(true)
    }
    fn visit_none<E>(self) -> Result<Self::Value, E> {
        Ok(true)
    }
    fn visit_some<D: serde::Deserializer<'de>>(self, deserializer: D) -> Result<Self::Value, D::Error> {
        deserializer.deserialize_any(self)
    }
    fn visit_unit<E>(self) -> Result<Self::Value, E> {
        Ok(true)
    }
    fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Self::Value, A::Error> {
        let mut valid = true;
        while let Some(value) = seq.next_element::<JsonNumberOrOther>()? {
            valid &= value.valid;
        }
        Ok(valid)
    }
    fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Self::Value, A::Error> {
        let mut valid = true;
        while map.next_key::<serde::de::IgnoredAny>()?.is_some() {
            valid &= map.next_value::<JsonNumberOrOther>()?.valid;
        }
        Ok(valid)
    }
}

#[derive(Debug, Default)]
struct AnyValue {
    present: bool,
    valid: bool,
}
impl Slot for AnyValue {
    fn is_unset(&self) -> bool {
        !self.present
    }
}
impl<'de> Deserialize<'de> for AnyValue {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let valid = deserializer.deserialize_any(JsonValueNumber)?;
        Ok(Self { present: true, valid })
    }
}

#[derive(Debug)]
struct AnyArray;
impl<'de> Shape<'de> for AnyArray {
    fn seq<A: SeqAccess<'de>>(mut seq: A) -> Result<Option<Self>, A::Error> {
        while seq.next_element::<IgnoredAny>()?.is_some() {}
        Ok(Some(Self))
    }
}

#[derive(Debug)]
struct AnyObject;
impl<'de> Shape<'de> for AnyObject {
    fn map<A: MapAccess<'de>>(mut map: A) -> Result<Option<Self>, A::Error> {
        while map.next_entry::<IgnoredAny, IgnoredAny>()?.is_some() {}
        Ok(Some(Self))
    }
}

#[derive(Debug)]
struct Strings;
impl<'de> Shape<'de> for Strings {
    fn seq<A: SeqAccess<'de>>(mut seq: A) -> Result<Option<Self>, A::Error> {
        while let Some(value) = seq.next_element::<Seen<Cow<'de, str>>>()? {
            let Seen::Value(_) = value else {
                while seq.next_element::<IgnoredAny>()?.is_some() {}
                return Ok(None);
            };
        }
        Ok(Some(Self))
    }
}

#[derive(Debug)]
struct IntMap;
impl<'de> Shape<'de> for IntMap {
    fn map<A: MapAccess<'de>>(mut map: A) -> Result<Option<Self>, A::Error> {
        let mut valid = true;
        while map.next_key::<IgnoredAny>()?.is_some() {
            valid &= matches!(map.next_value::<Seen<i64>>()?, Seen::Value(_));
        }
        Ok(valid.then_some(Self))
    }
}

/// One raw JSON value at a known key. It may have any shape; its nested fields are checked by the owning projection.
#[derive(Debug, Default)]
struct Raw(Option<Box<RawValue>>);
impl Slot for Raw {
    fn is_unset(&self) -> bool {
        self.0.is_none()
    }
}
impl<'de> Deserialize<'de> for Raw {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        Box::<RawValue>::deserialize(deserializer).map(|value| Self(Some(value)))
    }
}

#[derive(Debug)]
struct Hub {
    hub_id: String,
    name: Option<String>,
    online: Option<bool>,
    is_self: Option<bool>,
}

keyed! {
    struct HubBody<'a> {
        hub_id = "hub_id": Seen<Cow<'a, str>>,
        name = "name": Raw,
        online = "online": Raw,
        is_self = "self": Raw,
        endpoints = "endpoints": Raw,
        tls_port = "tls_port": Raw,
        fp = "fp": Raw,
        center = "center": Raw,
    }
}

fn lenient_string(raw: Option<&RawValue>) -> Option<String> {
    let raw = raw?;
    if raw.get() == "null" {
        return None;
    }
    serde_json::from_str(raw.get()).ok()
}

fn lenient_bool(raw: Option<&RawValue>) -> Option<bool> {
    let raw = raw?;
    if raw.get() == "null" {
        return None;
    }
    serde_json::from_str(raw.get()).ok()
}

fn hub(raw: &RawValue) -> Option<Hub> {
    let body: HubBody<'_> = serde_json::from_str(raw.get()).ok()?;
    Some(Hub {
        hub_id: body.hub_id.required("hub_id").ok()?.into_owned(),
        // MeshHub's remaining properties use try?: an odd one drops only that property.
        name: lenient_string(body.name.0.as_deref()),
        online: lenient_bool(body.online.0.as_deref()),
        is_self: lenient_bool(body.is_self.0.as_deref()),
    })
}

/// MeshField always succeeds: a scalar, a bad `hubs` array or one bad MeshHub discards only hubs.
fn mesh(raw: Option<&RawValue>) -> Option<Vec<Hub>> {
    #[derive(Default)]
    struct Body(Raw);
    impl Slot for Body {
        fn is_unset(&self) -> bool {
            self.0.is_unset()
        }
    }
    impl<'de> Deserialize<'de> for Body {
        fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
            Raw::deserialize(deserializer).map(Self)
        }
    }
    keyed! {
        struct MeshBody<'a> {
            hubs = "hubs": Body,
        }
    }
    let body: MeshBody<'_> = serde_json::from_str(raw?.get()).ok()?;
    let raw = body.hubs.0.0?;
    if raw.get() == "null" {
        return None;
    }
    let elements: Vec<&RawValue> = serde_json::from_str(raw.get()).ok()?;
    elements.iter().map(|raw| hub(raw)).collect()
}

#[derive(Debug, Default)]
struct Stats {
    tpm: Option<i64>,
    tokens: Option<i64>,
    requests: Option<i64>,
}

keyed! {
    struct StatsBody<'a> {
        tpm = "tpm": Seen<i64>,
        tokens = "tokens": Seen<i64>,
        requests = "requests": Seen<i64>,
        loads = "loads": Raw,
    }
}

fn stats(raw: &RawValue) -> Option<Stats> {
    let body: StatsBody<'_> = serde_json::from_str(raw.get()).ok()?;
    Some(Stats {
        tpm: body.tpm.optional("tpm").ok()?,
        tokens: body.tokens.optional("tokens").ok()?,
        requests: body.requests.optional("requests").ok()?,
    })
}

#[derive(Debug)]
struct Look {
    design: String,
    light: i64,
    dark: i64,
}

keyed! {
    struct LookBody<'a> {
        design = "design": Seen<Cow<'a, str>>,
        light = "light": Seen<i64>,
        dark = "dark": Seen<i64>,
        package = "package": Raw,
    }
}

fn look(raw: &RawValue) -> Option<Look> {
    let body: LookBody<'_> = serde_json::from_str(raw.get()).ok()?;
    let unsigned = |value: Option<i64>| value.filter(|value| (0..=u32::MAX as i64).contains(value));
    let look = Look {
        design: body.design.required("design").ok()?.into_owned(),
        light: unsigned(body.light.required("light").ok())?,
        dark: unsigned(body.dark.required("dark").ok())?,
    };
    match body.package.0.as_deref() {
        Some(raw) if raw.get() != "null" => serde_json::from_str::<Option<String>>(raw.get()).ok()?,
        _ => None,
    };
    Some(look)
}

#[derive(Debug)]
struct FleetItem {
    id: String,
    kind: String,
}

keyed! {
    struct FleetItemBody<'a> {
        id = "id": Seen<Cow<'a, str>>,
        kind = "kind": RawEnum,
        title = "title": Seen<AnyStr>,
        place = "place": Seen<AnyStr>,
        detail = "detail": Seen<AnyStr>,
        interaction = "interaction": Seen<AnyStr>,
        since = "since": OptionalFleetDouble,
        background = "background": Raw,
        progress = "progress": Raw,
    }
}

#[derive(Debug, Default)]
struct RawEnum(Option<Box<RawValue>>);
impl Slot for RawEnum {
    fn is_unset(&self) -> bool {
        self.0.is_none()
    }
}
impl<'de> Deserialize<'de> for RawEnum {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let raw = Box::<RawValue>::deserialize(deserializer)?;
        Ok(Self(Some(raw)))
    }
}

#[derive(Debug)]
struct Number;
impl<'de> Shape<'de> for Number {
    fn int(_: i64) -> Option<Self> {
        Some(Self)
    }
    fn seq<A: SeqAccess<'de>>(mut seq: A) -> Result<Option<Self>, A::Error> {
        while seq.next_element::<IgnoredAny>()?.is_some() {}
        Ok(None)
    }
    fn map<A: MapAccess<'de>>(mut map: A) -> Result<Option<Self>, A::Error> {
        while map.next_entry::<IgnoredAny, IgnoredAny>()?.is_some() {}
        Ok(None)
    }
}

/// Fleet's Double fields differ from Envelope Int: a finite fraction or exponent is a valid JSON number.
#[derive(Debug)]
struct FleetDouble;
impl<'de> serde::de::Visitor<'de> for FleetDouble {
    type Value = Number;

    fn expecting(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str("a JSON number")
    }

    fn visit_i64<E>(self, _: i64) -> Result<Self::Value, E> {
        Ok(Number)
    }

    fn visit_u64<E>(self, _: u64) -> Result<Self::Value, E> {
        Ok(Number)
    }

    fn visit_f64<E>(self, _: f64) -> Result<Self::Value, E> {
        Ok(Number)
    }
}

#[derive(Debug, Default)]
struct OptionalFleetDouble {
    present: bool,
    valid: bool,
}
impl Slot for OptionalFleetDouble {
    fn is_unset(&self) -> bool {
        !self.present
    }
}
impl<'de> Deserialize<'de> for OptionalFleetDouble {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let valid = deserializer.deserialize_any(FleetDouble).is_ok();
        Ok(Self { present: true, valid })
    }
}

fn fleet_item(raw: &RawValue) -> Option<FleetItem> {
    let body: FleetItemBody<'_> = serde_json::from_str(raw.get()).ok()?;
    for result in [
        body.title.optional("title"),
        body.place.optional("place"),
        body.detail.optional("detail"),
        body.interaction.optional("interaction"),
    ] {
        result.ok()?;
    }
    // Fractional and exponent-form finite JSON numbers are a Double, not an integer, for Fleet.since.
    if body.since.present && !body.since.valid {
        return None;
    }
    // `kind` uses a tolerant String decode: every non-string JSON value, including a large integer, is `.screen`.

    Some(FleetItem {
        id: body.id.required("id").ok()?.into_owned(),
        kind: match body
            .kind
            .0
            .as_deref()
            .and_then(|raw| serde_json::from_str::<String>(raw.get()).ok())
            .as_deref()
        {
            Some(value @ ("permission" | "plan" | "question" | "screen" | "working" | "unread")) => value.to_owned(),
            _ => "screen".to_owned(),
        },
    })
}

#[derive(Debug)]
struct Fleet {
    phase: String,
    needs_you: i64,
    working: i64,
    unread: i64,
    items: Vec<FleetItem>,
    stats: Option<Stats>,
    look: Option<Look>,
}

keyed! {
    struct FleetBody<'a> {
        phase = "phase": RawEnum,
        needs_you = "needsYou": Seen<i64>,
        working = "working": Seen<i64>,
        unread = "unread": Seen<i64>,
        items = "items": RawFleetItems,
        working_since = "workingSince": OptionalFleetDouble,
        details = "details": Seen<bool>,
        assistant = "assistant": Seen<bool>,
        left = "left": Raw,
        right = "right": Raw,
        stats = "stats": Raw,
        look = "look": Raw,
    }
}

#[derive(Debug)]
struct RawFleetItems {
    present: bool,
    value: Option<Vec<FleetItem>>,
}
impl Default for RawFleetItems {
    fn default() -> Self {
        Self {
            present: false,
            value: Some(Vec::new()),
        }
    }
}
impl Slot for RawFleetItems {
    fn is_unset(&self) -> bool {
        !self.present
    }
}
impl<'de> Deserialize<'de> for RawFleetItems {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let raw = Box::<RawValue>::deserialize(deserializer)?;
        if raw.get() == "null" {
            return Ok(Self {
                present: true,
                value: Some(Vec::new()),
            });
        }
        let elements: Option<Vec<&RawValue>> = serde_json::from_str(raw.get()).ok();
        let value = elements.and_then(|elements| elements.iter().map(|raw| fleet_item(raw)).collect());
        Ok(Self { present: true, value })
    }
}

fn fleet(raw: &RawValue) -> Option<Fleet> {
    let body: FleetBody<'_> = serde_json::from_str(raw.get()).ok()?;
    let needs_you = body.needs_you.optional("needsYou").ok()?.unwrap_or(0);
    let working = body.working.optional("working").ok()?.unwrap_or(0);
    let unread = body.unread.optional("unread").ok()?.unwrap_or(0);
    body.details.optional("details").ok()?;
    body.assistant.optional("assistant").ok()?;
    if body.working_since.present && !body.working_since.valid {
        return None;
    }
    let items = match body.items {
        RawFleetItems { value: Some(items), .. } => items,
        RawFleetItems { value: None, .. } => return None,
    };
    let stats = match &body.stats.0 {
        Some(raw) if raw.get() != "null" => stats(raw),
        _ => None,
    };
    let look = match &body.look.0 {
        Some(raw) if raw.get() != "null" => look(raw),
        _ => None,
    };
    Some(Fleet {
        phase: match body
            .phase
            .0
            .as_deref()
            .and_then(|raw| serde_json::from_str::<String>(raw.get()).ok())
            .as_deref()
        {
            Some(value @ ("needsYou" | "working" | "done")) => value.to_owned(),
            _ => String::new(),
        },
        needs_you,
        working,
        unread,
        items,
        stats,
        look,
    })
}

keyed! {
    /// The full shared Envelope. Unprojected nested models are deliberately shape-only.
    struct EnvelopeBody<'a> {
        kind = "type": Seen<Cow<'a, str>>,
        machine_id = "machine_id": Seen<Cow<'a, str>>,
        session_id = "session_id": Seen<Cow<'a, str>>,
        machines = "machines": Seen<AnyArray>,
        sessions = "sessions": Seen<AnyArray>,
        upsert = "upsert": Seen<AnyArray>,
        remove = "remove": Seen<Strings>,
        revision = "revision": Seen<Cow<'a, str>>,
        items = "items": RawItems,
        backlog = "backlog": Seen<bool>,
        has_more = "has_more": Seen<bool>,
        more_after = "more_after": Seen<bool>,
        history = "history": Seen<Cow<'a, str>>,
        from_seq = "from_seq": Seen<i64>,
        to_seq = "to_seq": Seen<i64>,
        text = "text": Seen<Cow<'a, str>>,
        status = "status": Seen<Cow<'a, str>>,
        interaction = "interaction": Seen<AnyObject>,
        id = "id": Seen<Cow<'a, str>>,
        call_id = "call_id": Seen<Cow<'a, str>>,
        name = "name": Seen<Cow<'a, str>>,
        args = "args": AnyValue,
        event = "event": Seen<Cow<'a, str>>,
        message = "message": Seen<Cow<'a, str>>,
        last_assistant_message = "last_assistant_message": Seen<Cow<'a, str>>,
        error = "error": Seen<Cow<'a, str>>,
        revs = "revs": Seen<IntMap>,
        feed_ids = "feed_ids": Seen<Strings>,
        settings_changed = "settings_changed": Seen<bool>,
        ol_account = "ol_account": Seen<i64>,
        source = "source": Seen<Cow<'a, str>>,
        conversations = "conversations": Seen<Strings>,
        account = "account": AnyValue,
        session_ids = "session_ids": Seen<Strings>,
        fleet = "fleet": RawFleet,
        desktops = "desktops": Seen<AnyArray>,
        features = "features": RawStrings,
        channel = "channel": Seen<Cow<'a, str>>,
        build = "build": Seen<i64>,
        protocol = "protocol": Seen<i64>,
        min_app_build = "min_app_build": Seen<i64>,
        public_url = "public_url": Seen<Cow<'a, str>>,
        update = "update": Seen<AnyObject>,
        operation = "operation": Seen<AnyObject>,
        auth = "auth": Raw,
        epoch = "epoch": Seen<Cow<'a, str>>,
        seq = "seq": Seen<i64>,
        full = "full": Seen<bool>,
        cols = "cols": Seen<i64>,
        rows = "rows": Seen<i64>,
        data = "data": Seen<Cow<'a, str>>,
        unsupported = "unsupported": Seen<bool>,
        state = "state": Seen<Cow<'a, str>>,
        hub_id = "hub_id": Seen<Cow<'a, str>>,
        hub_name = "hub_name": Seen<Cow<'a, str>>,
        mesh = "mesh": Raw,
        item = "item": Seen<AnyObject>,
        view = "view": Seen<AnyObject>,
        action = "action": AnyValue,
        key = "key": Seen<Cow<'a, str>>,
        conversation = "conversation": Seen<Cow<'a, str>>,
        plan = "plan": Seen<AnyObject>,
        effective = "effective": Seen<Cow<'a, str>>,
        plugin = "plugin": Seen<Cow<'a, str>>,
        extension = "extension": Seen<Cow<'a, str>>,
        invalidate = "invalidate": Seen<bool>,
        badge = "badge": Seen<AnyObject>,
        cwd = "cwd": Seen<Cow<'a, str>>,
        dirs = "dirs": Seen<Strings>,
        git = "git": Seen<bool>,
        answered_by = "answered_by": AnyValue,
        reason = "reason": AnyValue,
        answers = "answers": AnyValue,
        resolution = "resolution": AnyValue,
        manager = "manager": AnyValue,
        recording_id = "recording_id": Seen<Cow<'a, str>>,
        changed = "changed": AnyValue,
    }
}

#[derive(Debug)]
struct RawFleet {
    present: bool,
    valid: bool,
    value: Option<Fleet>,
}
impl Default for RawFleet {
    fn default() -> Self {
        Self {
            present: false,
            valid: true,
            value: None,
        }
    }
}
impl Slot for RawFleet {
    fn is_unset(&self) -> bool {
        !self.present
    }
}
impl<'de> Deserialize<'de> for RawFleet {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let raw = Box::<RawValue>::deserialize(deserializer)?;
        if raw.get() == "null" {
            return Ok(Self {
                present: true,
                valid: true,
                value: None,
            });
        }
        let value = fleet(&raw);
        Ok(Self {
            present: true,
            valid: value.is_some(),
            value,
        })
    }
}

#[derive(Debug)]
struct RawStrings {
    present: bool,
    value: Option<Vec<String>>,
}
impl Default for RawStrings {
    fn default() -> Self {
        Self {
            present: false,
            value: Some(Vec::new()),
        }
    }
}
impl Slot for RawStrings {
    fn is_unset(&self) -> bool {
        !self.present
    }
}
impl<'de> Deserialize<'de> for RawStrings {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let raw = Box::<RawValue>::deserialize(deserializer)?;
        if raw.get() == "null" {
            return Ok(Self {
                present: true,
                value: Some(Vec::new()),
            });
        }
        Ok(Self {
            present: true,
            value: serde_json::from_str(raw.get()).ok(),
        })
    }
}

#[derive(Debug, Default)]
struct RawItems {
    present: bool,
    value: Option<Vec<Item>>,
}
impl Slot for RawItems {
    fn is_unset(&self) -> bool {
        !self.present
    }
}
impl<'de> Deserialize<'de> for RawItems {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let raw = Box::<RawValue>::deserialize(deserializer)?;
        if raw.get() == "null" {
            return Ok(Self {
                present: true,
                value: Some(Vec::new()),
            });
        }
        let elements: Option<Vec<&RawValue>> = serde_json::from_str(raw.get()).ok();
        let value = elements.and_then(|elements| {
            elements
                .into_iter()
                .map(|raw| Item::from_wire(raw).ok())
                .collect::<Option<Vec<_>>>()
        });
        Ok(Self { present: true, value })
    }
}

fn dropped(key: Option<&'static str>) -> Value {
    json!({ "outcome": "dropped", "key": key })
}

fn key_error(error: KeyFault) -> Value {
    dropped(Some(match error {
        KeyFault::Missing(key) | KeyFault::WrongType(key) => key,
    }))
}

fn accepted(event: &str) -> Map<String, Value> {
    Map::from_iter([("outcome".into(), json!("accepted")), ("event".into(), json!(event))])
}

fn session_key(body: &EnvelopeBody<'_>) -> Result<(String, String), Value> {
    let machine = body.machine_id.optional_ref("machine_id").map_err(key_error)?;
    let session = body.session_id.optional_ref("session_id").map_err(key_error)?;
    Ok((
        machine.ok_or_else(|| dropped(Some("machine_id")))?.to_string(),
        session.ok_or_else(|| dropped(Some("session_id")))?.to_string(),
    ))
}

/// Projects the source-derived envelope cases. `None` means the input is not one complete UTF-8 JSON document.
pub fn project(bytes: &[u8]) -> Option<Value> {
    let json = transport::utf8(bytes).ok()?;
    if !opens_object(json) {
        serde_json::from_str::<IgnoredAny>(json).ok()?;
        return Some(dropped(None));
    }
    let body: EnvelopeBody<'_> = transport::root(json).ok()?;
    // All these known keys are decoded before ServerEvent's switch, whether the event uses them or not.
    if body.items.present && body.items.value.is_none() {
        return Some(dropped(Some("items")));
    }
    if let Err(error) = check_envelope(&body) {
        return Some(error);
    }
    if body.features.value.is_none() {
        return Some(dropped(Some("features")));
    }
    if !body.fleet.valid {
        return Some(dropped(Some("fleet")));
    }
    let kind = match body.kind.optional_ref("type") {
        Ok(Some(kind)) => kind.to_string(),
        Ok(None) => return Some(dropped(Some("type"))),
        Err(error) => return Some(key_error(error)),
    };
    match kind.as_str() {
        "hello" => hello(body),
        "inbox" | "fleet" => match body.fleet.value {
            Some(fleet) => fleet_event(&kind, fleet),
            None if kind == "fleet" => Some(dropped(Some("fleet"))),
            None => Some(json!({ "outcome": "accepted", "event": "inbox" })),
        },
        "items" => items_event(body),
        "screen" => screen_event(body),
        "sessions_list" => match body.revision.optional_ref("revision") {
            Ok(Some(revision)) => Some(json!({
                "outcome": "accepted", "event": "sessions_list", "revision": revision,
            })),
            Ok(None) => Some(dropped(Some("revision"))),
            Err(error) => Some(key_error(error)),
        },
        // Other known event types need their complete associated values before they can be projected as accepted.
        "hub_update"
        | "extension_operation"
        | "plugin_operation"
        | "extension_auth"
        | "machines"
        | "sessions"
        | "sessions_delta"
        | "delta"
        | "interaction"
        | "interaction_resolved"
        | "interaction_reanswered"
        | "tool_call"
        | "hook_event"
        | "error"
        | "revs"
        | "lark"
        | "messaging"
        | "agents"
        | "agents.prefs"
        | "todos"
        | "personas"
        | "look"
        | "managers"
        | "directions"
        | "assistant_feed"
        | "assistant_view"
        | "assistant_view_block"
        | "assistant_view_action"
        | "task_plan"
        | "desktops"
        | "terminal"
        | "plugin_ui"
        | "extension_ui"
        | "fs_changed"
        | "terminal_control"
        | "meeting_live"
        | "subscription" => Some(json!({ "outcome": "out_of_scope", "type": kind })),
        _ => Some(json!({ "outcome": "ignored", "type": kind })),
    }
}

fn check_envelope(body: &EnvelopeBody<'_>) -> Result<(), Value> {
    macro_rules! check {
        ($($field:ident => $key:literal),+ $(,)?) => {
            $(body.$field.optional_ref($key).map_err(key_error)?;)+
        };
    }
    macro_rules! raw {
        ($($field:ident => $key:literal),+ $(,)?) => {
            $(
                if body.$field.present && !body.$field.valid {
                    return Err(dropped(Some($key)));
                }
            )+
        };
    }
    check! {
        machine_id => "machine_id", session_id => "session_id", revision => "revision",
        backlog => "backlog", has_more => "has_more", more_after => "more_after",
        history => "history", from_seq => "from_seq", to_seq => "to_seq", text => "text",
        status => "status", id => "id",
        machines => "machines", sessions => "sessions", upsert => "upsert", remove => "remove",
        interaction => "interaction", call_id => "call_id", name => "name",
        event => "event", message => "message", last_assistant_message => "last_assistant_message",
        error => "error", revs => "revs", feed_ids => "feed_ids", settings_changed => "settings_changed",
        ol_account => "ol_account", source => "source", conversations => "conversations",
        session_ids => "session_ids", desktops => "desktops", update => "update", operation => "operation",
        epoch => "epoch", seq => "seq", full => "full", cols => "cols", rows => "rows",
        data => "data", unsupported => "unsupported", state => "state", hub_id => "hub_id",
        hub_name => "hub_name", item => "item", view => "view", key => "key",
        conversation => "conversation", plan => "plan", effective => "effective", plugin => "plugin",
        extension => "extension", invalidate => "invalidate", badge => "badge", cwd => "cwd",
        dirs => "dirs", git => "git", recording_id => "recording_id",
    }
    raw! {
        args => "args", account => "account", action => "action", answered_by => "answered_by",
        reason => "reason", answers => "answers", resolution => "resolution", manager => "manager",
        changed => "changed",
    }
    Ok(())
}

fn hello(body: EnvelopeBody<'_>) -> Option<Value> {
    let mut result = accepted("hello");
    let features = match body.features {
        RawStrings {
            value: Some(features), ..
        } => features,
        RawStrings { value: None, .. } => return Some(dropped(Some("features"))),
    };
    result.insert("features".into(), json!(features));
    macro_rules! hello_text {
        ($field:ident, $key:literal) => {
            match body.$field {
                Seen::Missing => {}
                Seen::Null => {
                    result.insert($key.into(), Value::Null);
                }
                Seen::Value(value) => {
                    result.insert($key.into(), json!(value));
                }
                Seen::Wrong => return Some(dropped(Some($key))),
            }
        };
    }
    hello_text!(channel, "channel");
    hello_text!(public_url, "public_url");
    macro_rules! hello_int {
        ($field:ident, $key:literal) => {
            match body.$field {
                Seen::Missing => {}
                Seen::Null => {
                    result.insert($key.into(), Value::Null);
                }
                Seen::Value(value) => {
                    result.insert($key.into(), json!(value));
                }
                Seen::Wrong => return Some(dropped(Some($key))),
            }
        };
    }
    hello_int!(build, "build");
    hello_int!(protocol, "protocol_version");
    hello_int!(min_app_build, "min_app_build");
    if !matches!(body.mesh, Raw(None)) {
        result.insert(
            "mesh".into(),
            json!(mesh(body.mesh.0.as_deref()).map(|hubs| {
                hubs.into_iter()
                    .map(|hub| {
                        json!({
                            "hub_id": hub.hub_id, "name": hub.name, "online": hub.online, "self": hub.is_self,
                        })
                    })
                    .collect::<Vec<_>>()
            })),
        );
    }
    Some(Value::Object(result))
}

fn items_event(body: EnvelopeBody<'_>) -> Option<Value> {
    let (machine_id, session_id) = match session_key(&body) {
        Ok(key) => key,
        Err(error) => return Some(error),
    };
    let items = match (body.items.present, body.items.value) {
        (false, _) => Vec::new(),
        (true, Some(items)) => items,
        (true, None) => return Some(dropped(Some("items"))),
    };
    let mut result = accepted("items");
    let mut projected_items = Vec::with_capacity(items.len());
    for item in &items {
        let Ok(value) = serde_json::from_str(item.json()) else {
            // Item::from_wire intentionally does not inspect every nested field. A field the JSON projection
            // cannot represent is a dropped diagnostic result, never a panic or an iOS acceptance claim.
            return Some(dropped(Some("items")));
        };
        projected_items.push(value);
    }
    result.extend([
        ("machine_id".into(), json!(machine_id)),
        ("session_id".into(), json!(session_id)),
        ("items".into(), Value::Array(projected_items)),
    ]);
    for (seen, key) in [
        (body.backlog.optional_ref("backlog"), "backlog"),
        (body.has_more.optional_ref("has_more"), "has_more"),
        (body.more_after.optional_ref("more_after"), "more_after"),
    ] {
        match seen {
            Ok(value) => {
                result.insert(
                    key.into(),
                    json!(if key == "has_more" {
                        value.copied()
                    } else {
                        Some(value == Some(&true))
                    }),
                );
            }
            Err(error) => return Some(key_error(error)),
        }
    }
    for (seen, key) in [
        (
            body.history
                .optional_ref("history")
                .map(|value| value.map(|text| Value::from(text.to_string()))),
            "history",
        ),
        (
            body.from_seq
                .optional_ref("from_seq")
                .map(|value| value.copied().map(Value::from)),
            "from_seq",
        ),
        (
            body.to_seq
                .optional_ref("to_seq")
                .map(|value| value.copied().map(Value::from)),
            "to_seq",
        ),
    ] {
        match seen {
            Ok(value) => {
                result.insert(key.into(), json!(value));
            }
            Err(error) => return Some(key_error(error)),
        }
    }
    Some(Value::Object(result))
}

fn fleet_event(kind: &str, fleet: Fleet) -> Option<Value> {
    let mut result = accepted(kind);
    let mut projected = Map::from_iter([
        ("phase".into(), json!(fleet.phase)),
        ("needs_you".into(), json!(fleet.needs_you)),
        ("working".into(), json!(fleet.working)),
        ("unread".into(), json!(fleet.unread)),
        (
            "stats".into(),
            fleet.stats.map_or(Value::Null, |stats| {
                json!({
                    "tpm": stats.tpm.unwrap_or(0), "tokens": stats.tokens.unwrap_or(0),
                    "requests": stats.requests.unwrap_or(0),
                })
            }),
        ),
        (
            "look".into(),
            fleet.look.map_or(
                Value::Null,
                |look| json!({ "design": look.design, "light": look.light, "dark": look.dark }),
            ),
        ),
    ]);
    if !fleet.items.is_empty() {
        projected.insert(
            "items".into(),
            Value::Array(
                fleet
                    .items
                    .into_iter()
                    .map(|item| json!({ "id": item.id, "kind": item.kind }))
                    .collect(),
            ),
        );
    }
    result.insert("fleet".into(), Value::Object(projected));
    Some(Value::Object(result))
}

fn screen_event(body: EnvelopeBody<'_>) -> Option<Value> {
    let (machine_id, session_id) = match session_key(&body) {
        Ok(key) => key,
        Err(error) => return Some(error),
    };
    let text = match body.text.optional_ref("text") {
        Ok(text) => text.map(ToString::to_string).unwrap_or_default(),
        Err(error) => return Some(key_error(error)),
    };
    let status = match body.status.optional_ref("status") {
        Ok(Some(status)) => match &**status {
            "idle" | "working" | "blocked" | "done" | "unknown" | "offline" => status.to_string(),
            _ => "unknown".to_owned(),
        },
        Ok(None) => "unknown".to_owned(),
        Err(error) => return Some(key_error(error)),
    };
    Some(json!({
        "outcome": "accepted", "event": "screen", "machine_id": machine_id,
        "session_id": session_id, "text": text, "status": status,
    }))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn invalid_json_is_a_boundary_failure_not_a_projection() {
        assert_eq!(project(br#"{"type":"inbox""#), None);
        assert_eq!(project(b"not JSON"), None);
        assert_eq!(project(&[0xff]), None);
    }

    #[test]
    fn fleet_fractional_since_is_a_double_not_a_wrong_integer() {
        let frame = br#"{"type":"inbox","fleet":{"items":[{"id":"machine/session","since":1.5}]}}"#;
        assert_eq!(project(frame).expect("valid JSON")["outcome"], "accepted");
    }

    #[test]
    fn existing_item_fault_stays_at_the_envelope_items_key() {
        let frame = br#"{"type":"items","machine_id":"m","session_id":"s","items":[{"id":7}]}"#;
        let item: Result<Item, crate::item::ItemFault> =
            Item::from_wire(serde_json::from_str::<Box<RawValue>>(r#"{"id":7}"#).unwrap().as_ref());
        assert!(item.is_err());
        assert_eq!(project(frame), Some(dropped(Some("items"))));
    }
}

//! Shared WebSocket Envelope fields checked before `transport::decode_frame` routes an event (#8995).
//!
//! This is deliberately a shape gate, not a second full `ServerEvent`: it reads the scalar, list and raw-JSON fields
//! of the current iOS `Envelope`, while leaving Items, Fleet and the other nested models to their own decoders. It
//! therefore closes the wrong-known-key hole without claiming complete nested-model or Foundation compatibility.
//! `wire_envelope` owns its separate diagnostic projection and Mesh/Fleet fixture rules; production transport keeps
//! the existing `Item::from_wire` behavior and `Frame::Other` routing.

use std::borrow::Cow;

use serde::Deserialize;
use serde::de::{IgnoredAny, MapAccess, SeqAccess, Visitor};
use serde_json::value::RawValue;

use super::seen::{KeyFault, Seen, Shape, Slot, keyed};

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

#[derive(Debug)]
struct AnyValue {
    present: bool,
    valid: bool,
}
impl Slot for AnyValue {
    fn is_unset(&self) -> bool {
        !self.present
    }
}
impl Default for AnyValue {
    fn default() -> Self {
        Self {
            present: false,
            valid: true,
        }
    }
}
impl<'de> Deserialize<'de> for AnyValue {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let valid = deserializer.deserialize_any(JsonValueNumber)?;
        Ok(Self { present: true, valid })
    }
}

/// One JSONValue-compatible value, including fractional / exponent-form numbers and nested arrays / objects.
/// The existing transport limits on extreme floating-point values and encodings remain unchanged.
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
        while map.next_key::<IgnoredAny>()?.is_some() {
            valid &= map.next_value::<JsonNumberOrOther>()?.valid;
        }
        Ok(valid)
    }
}

/// Only an array is checked here. Each element remains the responsibility of the existing Items decoder, which
/// preserves the item JSON and reports its exact fault and index when the event is `items`.
#[derive(Debug, Default)]
struct ItemArray(Option<Box<RawValue>>);
impl Slot for ItemArray {
    fn is_unset(&self) -> bool {
        self.0.is_none()
    }
}
impl<'de> Deserialize<'de> for ItemArray {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        Box::<RawValue>::deserialize(deserializer).map(|value| Self(Some(value)))
    }
}

keyed! {
    /// All scalar / shape fields of the current iOS Envelope. Fleet and Mesh are deliberately not decoded here;
    /// their detailed fixture-backed diagnostic projection is not silently made a production model.
    pub(crate) struct EnvelopeFields<'a> {
        kind = "type": Seen<Cow<'a, str>>,
        machine_id = "machine_id": Seen<Cow<'a, str>>,
        session_id = "session_id": Seen<Cow<'a, str>>,
        machines = "machines": Seen<AnyArray>,
        sessions = "sessions": Seen<AnyArray>,
        upsert = "upsert": Seen<AnyArray>,
        remove = "remove": Seen<Strings>,
        revision = "revision": Seen<Cow<'a, str>>,
        items = "items": ItemArray,
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
        fleet = "fleet": Seen<AnyObject>,
        desktops = "desktops": Seen<AnyArray>,
        features = "features": Seen<Strings>,
        channel = "channel": Seen<Cow<'a, str>>,
        build = "build": Seen<i64>,
        protocol = "protocol": Seen<i64>,
        min_app_build = "min_app_build": Seen<i64>,
        public_url = "public_url": Seen<Cow<'a, str>>,
        update = "update": Seen<AnyObject>,
        operation = "operation": Seen<AnyObject>,
        auth = "auth": AnyValueObject,
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
        mesh = "mesh": AnyValueObject,
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

/// LenientAuth / MeshField accept any root shape and discard a payload they cannot use. Detailed contents are out of
/// this shape gate; they do not share the envelope's plain `JSONValue` raw-number behavior.
#[derive(Debug, Default)]
struct AnyValueObject {
    present: bool,
}
impl Slot for AnyValueObject {
    fn is_unset(&self) -> bool {
        !self.present
    }
}
impl<'de> Deserialize<'de> for AnyValueObject {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        deserializer
            .deserialize_ignored_any(IgnoredAny)
            .map(|_| Self { present: true })
    }
}

impl EnvelopeFields<'_> {
    pub(crate) fn validate(&self) -> Result<(), KeyFault> {
        macro_rules! check {
            ($($field:ident => $key:literal),+ $(,)?) => {
                $(self.$field.optional_ref($key)?;)+
            };
        }
        check! {
            kind => "type", machine_id => "machine_id", session_id => "session_id",
            machines => "machines", sessions => "sessions", upsert => "upsert", remove => "remove",
            revision => "revision", backlog => "backlog", has_more => "has_more",
            more_after => "more_after", history => "history", from_seq => "from_seq", to_seq => "to_seq",
            text => "text", status => "status", interaction => "interaction", id => "id",
            call_id => "call_id", name => "name", event => "event", message => "message",
            last_assistant_message => "last_assistant_message", error => "error", revs => "revs",
            feed_ids => "feed_ids", settings_changed => "settings_changed", ol_account => "ol_account",
            source => "source", conversations => "conversations", session_ids => "session_ids",
            fleet => "fleet", desktops => "desktops", features => "features", channel => "channel",
            build => "build", protocol => "protocol", min_app_build => "min_app_build",
            public_url => "public_url", update => "update", operation => "operation",
            epoch => "epoch", seq => "seq", full => "full", cols => "cols", rows => "rows",
            data => "data", unsupported => "unsupported", state => "state", hub_id => "hub_id",
            hub_name => "hub_name", item => "item", view => "view", key => "key",
            conversation => "conversation", plan => "plan", effective => "effective", plugin => "plugin",
            extension => "extension", invalidate => "invalidate", badge => "badge", cwd => "cwd",
            dirs => "dirs", git => "git", recording_id => "recording_id",
        }
        if let Some(items) = self.items.0.as_deref()
            && items.get() != "null"
            && serde_json::from_str::<Vec<&RawValue>>(items.get()).is_err()
        {
            return Err(KeyFault::WrongType("items"));
        }
        macro_rules! raw {
            ($($field:ident => $key:literal),+ $(,)?) => {
                $(
                    if self.$field.present && !self.$field.valid {
                        return Err(KeyFault::WrongType($key));
                    }
                )+
            };
        }
        raw! {
            args => "args", account => "account", action => "action", answered_by => "answered_by",
            reason => "reason", answers => "answers", resolution => "resolution", manager => "manager",
            changed => "changed",
        }
        Ok(())
    }
}

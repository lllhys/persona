//! Shared first-value-wins item readers for the wire and snapshot boundaries.
//!
//! `keyed!` is the only object scanner: it selects the first occurrence and skips later values without recursion.
//! The wire uses `Seen<T>` to name protocol-key faults; the cache uses `CacheSeen<T>` to preserve serde's original
//! field types, defaults, null handling and error positions. Fold normalization is shared as well.

use std::borrow::Cow;
use std::fmt;
use std::marker::PhantomData;

use serde::de::{Deserialize, Deserializer, Error, IgnoredAny, MapAccess, SeqAccess, Visitor};
use serde_json::value::RawValue;

use crate::transport::seen::{AnyStr, Key, Seen, Slot, keyed};

#[derive(Default)]
pub(crate) struct Raw<'a>(pub(crate) Option<&'a RawValue>);

impl<'de: 'a, 'a> Deserialize<'de> for Raw<'a> {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        <&'de RawValue>::deserialize(deserializer).map(|raw| Raw(Some(raw)))
    }
}

impl Slot for Raw<'_> {
    fn is_unset(&self) -> bool {
        self.0.is_none()
    }
}

keyed! {
    /// The keys iOS's `Item` declares with a plain type, read without failing on their type so a wire fault names
    /// its key. Cache uses the same `keyed!` scanner below with its legacy field types.
    pub_fields struct ItemHead<'a> {
        id = "id": Seen<Cow<'a, str>>,
        seq = "seq": Seen<i64>,
        role = "role": Seen<Cow<'a, str>>,
        kind = "kind": Seen<Cow<'a, str>>,
        tool_use_id = "tool_use_id": Seen<Cow<'a, str>>,
        text = "text": Seen<AnyStr>,
        tool_name = "tool_name": Seen<AnyStr>,
        from = "from": Seen<AnyStr>,
        client_msg_id = "client_msg_id": Seen<AnyStr>,
        is_error = "is_error": Seen<bool>,
        queued = "queued": Seen<bool>,
        wake = "wake": Seen<bool>,
        supervision_user = "supervision_user": Seen<bool>,
        full_size = "full_size": Seen<i64>,
        fold = "fold": Raw<'a>,
    }
}

keyed! {
    /// The keys of iOS's `Fold` with a plain type, shared by strict wire validation and tolerant cache defaults.
    pub_fields struct ItemFold<'a> {
        items = "items": Seen<i64>,
        tools = "tools": Seen<i64>,
        thoughts = "thoughts": Seen<i64>,
        through_seq = "through_seq": Seen<i64>,
        through_id = "through_id": Seen<Cow<'a, str>>,
        supervision_user = "supervision_user": Seen<bool>,
    }
}

/// A cache field read with its original serde type. Unlike `Seen<T>`, this does not turn a type error into a slot:
/// the error stays at the original line/column. Presence is separate from `T`, so `null` can be a valid first value
/// for `Option<String>` and a first `fold: null` still blocks later occurrences.
#[derive(Debug, Default)]
pub(crate) enum CacheSeen<T> {
    #[default]
    Missing,
    Value(T),
}

impl<T> Slot for CacheSeen<T> {
    fn is_unset(&self) -> bool {
        matches!(self, Self::Missing)
    }
}

impl<T> CacheSeen<T> {
    pub(crate) fn into_option(self) -> Option<T> {
        match self {
            Self::Missing => None,
            Self::Value(value) => Some(value),
        }
    }
}

impl<'de, T: Deserialize<'de>> Deserialize<'de> for CacheSeen<T> {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        T::deserialize(deserializer).map(Self::Value)
    }
}

keyed! {
    /// The legacy cache field types and declaration order. This is not another duplicate-key algorithm: `keyed!`
    /// performs exactly the same selection as `ItemHead`; only the boundary's type/default policy differs.
    pub_fields struct CacheHead<'a> {
        id = "id": CacheSeen<String>,
        seq = "seq": CacheSeen<i64>,
        kind = "kind": CacheSeen<Option<String>>,
        role = "role": CacheSeen<Option<String>>,
        tool_use_id = "tool_use_id": CacheSeen<Option<String>>,
        fold = "fold": CacheSeen<CacheFold>,
        supervision_user = "supervision_user": CacheSeen<Option<bool>>,
    }
}

/// A keyed or positional cached item. Serde's derived struct historically accepted the positional representation;
/// this adapter retains it and feeds the same `CacheHead`, including the same fold normalizer and defaults.
pub(crate) struct CacheItemHead<'a> {
    pub(crate) head: CacheHead<'a>,
}

impl<'de: 'a, 'a> Deserialize<'de> for CacheItemHead<'a> {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct Fields<'a>(PhantomData<&'a ()>);

        impl<'de: 'a, 'a> Visitor<'de> for Fields<'a> {
            type Value = CacheItemHead<'a>;

            fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                f.write_str("a cache item object or its legacy positional form")
            }

            fn visit_map<A: MapAccess<'de>>(self, map: A) -> Result<Self::Value, A::Error> {
                let head = CacheHead::deserialize(serde::de::value::MapAccessDeserializer::new(map))?;
                if matches!(head.id, CacheSeen::Missing) {
                    return Err(A::Error::missing_field("id"));
                }
                Ok(CacheItemHead { head })
            }

            fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Self::Value, A::Error> {
                let mut head = CacheHead {
                    id: CacheSeen::Value(seq.next_element()?.ok_or_else(|| A::Error::missing_field("id"))?),
                    ..CacheHead::default()
                };
                macro_rules! position {
                    ($field:ident, $type:ty) => {
                        head.$field = match seq.next_element::<$type>()? {
                            Some(value) => CacheSeen::Value(value),
                            None => CacheSeen::Missing,
                        };
                    };
                }
                position!(seq, i64);
                position!(kind, Option<String>);
                position!(role, Option<String>);
                position!(tool_use_id, Option<String>);
                position!(fold, CacheFold);
                position!(supervision_user, Option<bool>);
                Ok(CacheItemHead { head })
            }
        }

        deserializer.deserialize_any(Fields(PhantomData))
    }
}

/// The cache's historical optional fold. Value performs the initial parse, including serde's RawValue newtype and
/// legacy recursion/error positions. Only the outer object is then normalized to the first value of each key.
#[derive(Debug)]
pub(crate) struct CacheFold(pub(crate) Option<serde_json::Value>);

impl<'de> Deserialize<'de> for CacheFold {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct FoldVisitor;

        impl<'de> Visitor<'de> for FoldVisitor {
            type Value = CacheFold;

            fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                f.write_str("an optional cached fold")
            }

            fn visit_unit<E>(self) -> Result<Self::Value, E> {
                Ok(CacheFold(None))
            }

            fn visit_none<E>(self) -> Result<Self::Value, E> {
                Ok(CacheFold(None))
            }

            fn visit_newtype_struct<D: Deserializer<'de>>(self, deserializer: D) -> Result<Self::Value, D::Error> {
                deserializer.deserialize_any(self)
            }

            fn visit_some<D: Deserializer<'de>>(self, deserializer: D) -> Result<Self::Value, D::Error> {
                deserializer.deserialize_any(self)
            }

            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Self::Value, A::Error> {
                let mut object = serde_json::Map::new();
                while let Some(key) = map.next_key::<Key<'de>>()? {
                    let key = key.0.into_owned();
                    if object.contains_key(&key) {
                        map.next_value::<IgnoredAny>()?;
                    } else if key == "$serde_json::private::RawValue" && object.is_empty() {
                        let raw = map.next_value::<String>()?;
                        let value = serde_json::from_str::<Option<serde_json::Value>>(&raw).map_err(Error::custom)?;
                        return match map.next_key::<Key<'de>>()? {
                            Some(_) => Err(Error::custom("a raw cache fold has another key")),
                            None => Ok(CacheFold(value)),
                        };
                    } else {
                        object.insert(key, map.next_value()?);
                    }
                }
                Ok(CacheFold(Some(serde_json::Value::Object(object))))
            }

            fn visit_bool<E>(self, value: bool) -> Result<Self::Value, E> {
                Ok(CacheFold(Some(value.into())))
            }

            fn visit_i64<E>(self, value: i64) -> Result<Self::Value, E> {
                Ok(CacheFold(Some(value.into())))
            }

            fn visit_u64<E>(self, value: u64) -> Result<Self::Value, E> {
                Ok(CacheFold(Some(value.into())))
            }

            fn visit_f64<E: Error>(self, value: f64) -> Result<Self::Value, E> {
                serde_json::Number::from_f64(value)
                    .map(|number| CacheFold(Some(number.into())))
                    .ok_or_else(|| E::custom("a fold number is not finite"))
            }

            fn visit_str<E>(self, value: &str) -> Result<Self::Value, E> {
                Ok(CacheFold(Some(value.into())))
            }

            fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Self::Value, A::Error> {
                let mut values = Vec::new();
                while let Some(value) = seq.next_element::<FoldValue>()? {
                    match value {
                        FoldValue::Absent => values.push(serde_json::Value::Null),
                        FoldValue::Value(value) => values.push(value),
                    }
                }
                Ok(CacheFold(Some(values.into())))
            }
        }

        deserializer.deserialize_option(FoldVisitor)
    }
}

/// A fold's normalized comparison value. Null means absence; other JSON values retain the cache's tolerant shape.
/// Objects keep the first value of every key at the top level, matching the endpoints/supervision read by `ItemFold`.
/// Nested objects use `serde_json::Value`'s default last-value-wins for duplicate keys (not recursive first-value-wins).
/// serde_json still owns the bounded recursion and numeric/string validation of the selected tree.
#[derive(Debug)]
pub(crate) enum FoldValue {
    Absent,
    Value(serde_json::Value),
}

impl<'de> Deserialize<'de> for FoldValue {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct FoldVisitor;

        impl<'de> Visitor<'de> for FoldVisitor {
            type Value = FoldValue;

            fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                f.write_str("any cached fold value")
            }

            fn visit_unit<E>(self) -> Result<Self::Value, E> {
                Ok(FoldValue::Absent)
            }

            fn visit_none<E>(self) -> Result<Self::Value, E> {
                Ok(FoldValue::Absent)
            }

            fn visit_some<D: Deserializer<'de>>(self, deserializer: D) -> Result<Self::Value, D::Error> {
                Deserialize::deserialize(deserializer)
            }

            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Self::Value, A::Error> {
                let mut object = serde_json::Map::new();
                while let Some(key) = map.next_key::<Key<'de>>()? {
                    let key = key.0.into_owned();
                    if object.contains_key(&key) {
                        map.next_value::<IgnoredAny>()?;
                    } else {
                        object.insert(key, map.next_value()?);
                    }
                }
                Ok(FoldValue::Value(serde_json::Value::Object(object)))
            }

            fn visit_bool<E>(self, value: bool) -> Result<Self::Value, E> {
                Ok(FoldValue::Value(value.into()))
            }

            fn visit_i64<E>(self, value: i64) -> Result<Self::Value, E> {
                Ok(FoldValue::Value(value.into()))
            }

            fn visit_u64<E>(self, value: u64) -> Result<Self::Value, E> {
                Ok(FoldValue::Value(value.into()))
            }

            fn visit_f64<E: Error>(self, value: f64) -> Result<Self::Value, E> {
                serde_json::Number::from_f64(value).map_or_else(
                    || Err(E::custom("a fold number is not finite")),
                    |number| Ok(FoldValue::Value(number.into())),
                )
            }

            fn visit_str<E>(self, value: &str) -> Result<Self::Value, E> {
                Ok(FoldValue::Value(value.into()))
            }

            fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Self::Value, A::Error> {
                let mut values = Vec::new();
                while let Some(value) = seq.next_element::<FoldValue>()? {
                    match value {
                        FoldValue::Absent => values.push(serde_json::Value::Null),
                        FoldValue::Value(value) => values.push(value),
                    }
                }
                Ok(FoldValue::Value(values.into()))
            }
        }

        deserializer.deserialize_any(FoldVisitor)
    }
}

/// Normalizes the selected fold once for both its cache representation and the wire's repeated-fold comparison.
pub(crate) fn fold_value(json: &str) -> Result<Option<serde_json::Value>, serde_json::Error> {
    Ok(match serde_json::from_str(json)? {
        FoldValue::Absent => None,
        FoldValue::Value(value) => Some(value),
    })
}

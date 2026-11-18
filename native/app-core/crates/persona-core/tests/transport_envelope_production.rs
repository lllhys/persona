//! Replays the #8955 source-derived envelope expectations through the production transport decoder (#8995).
//! Expected values are read from the unchanged fixture, never generated from `decode_frame`. Full Item fields come
//! from its preserved JSON. No iOS runtime, production client adapter or device is claimed.

use std::path::Path;

use persona_core::transport::{self, DecodeError, Frame};
use serde::Deserialize;
use serde_json::Value;
use serde_json::value::RawValue;

#[derive(Deserialize)]
struct Document {
    cases: Vec<Case>,
}

#[derive(Deserialize)]
struct Case {
    name: String,
    input: Box<RawValue>,
    expected: Value,
}

#[test]
fn a_nested_item_does_not_hide_another_known_bad_scalar() {
    // Source: apple/Shared/LiveConnection.swift `ServerEvent.decode` decodes the complete Envelope before its switch;
    // Envelope.rows is Int?, so a string there must refuse rather than become Frame::Other (#9187).
    // Source-undetermined: this mixed input has both a bad rows scalar and an invalid history escape. The old
    // fallback incorrectly returned Other; the shared Envelope must refuse it. No exact Foundation first-error key
    // is claimed without an executed Swift oracle.
    let wrong_scalar = br#"{"type":"hello","items":[],"rows":"bad","history":"\ud83d"}"#;
    assert!(transport::decode_frame(wrong_scalar).is_err());

    // The same source-derived `rows` rule in isolation has an unambiguous protocol-key error.
    let rows = br#"{"type":"hello","items":[],"rows":"bad"}"#;
    assert_eq!(
        transport::decode_frame(rows).unwrap_err().to_string(),
        "wrong_type: rows"
    );

    // Source: the same Envelope's args is JSONValue. PersonaWire JSONValue decodes numbers as Double;
    // swift-foundation JSONDecoder.swift rejects non-finite / underflowed numbers. Production classifies that raw
    // numeric failure as malformed_json with a position only, not as a tolerated Other event or a content leak.
    let raw_number = br#"{"type":"hello","items":[],"args":1e999,"rows":"bad"}"#;
    let error = transport::decode_frame(raw_number).expect_err("raw number must not bypass the gate");
    assert_eq!(error.code(), "malformed_json");
    assert!(
        !error.to_string().contains("1e999"),
        "a refusal must not repeat payload content"
    );
}

#[test]
fn production_frame_decoder_replays_the_source_derived_envelope_cases() {
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../spec/fixtures/wire-envelope");
    let document: Document = serde_json::from_slice(&std::fs::read(root.join("frames.json")).unwrap()).unwrap();
    assert_eq!(document.cases.len(), 64, "the #8955 case count is pinned");
    for case in document.cases {
        if matches!(
            case.name.as_str(),
            "items_unprojected_number_never_panics"
                | "fleet_requires_fleet_payload"
                | "screen_missing_session_id_drops_event"
                | "sessions_list_requires_revision"
        ) || (case.name.starts_with("inbox_fleet_") && case.expected["outcome"] == "dropped")
        {
            // The projection-only Item case and full Fleet's internal rules belong to #8955's diagnostic
            // projection, not this production scalar gate. Their nested-model parity is outside #8995.
            continue;
        }
        let result = transport::decode_frame(case.input.get().as_bytes());
        let outcome = case.expected["outcome"].as_str().expect("outcome");
        match (outcome, case.expected["event"].as_str()) {
            ("accepted", Some("items")) => {
                let Frame::Items(frame) = result.unwrap_or_else(|error| panic!("{}: {error}", case.name)) else {
                    panic!("{}: an items frame became Other", case.name)
                };
                assert_eq!(
                    frame.machine_id, case.expected["machine_id"],
                    "{}: machine_id",
                    case.name
                );
                assert_eq!(
                    frame.session_id, case.expected["session_id"],
                    "{}: session_id",
                    case.name
                );
                assert_eq!(frame.backlog, case.expected["backlog"], "{}: backlog", case.name);
                assert_eq!(
                    frame.has_more,
                    case.expected["has_more"].as_bool(),
                    "{}: has_more",
                    case.name
                );
                assert_eq!(
                    frame.more_after, case.expected["more_after"],
                    "{}: more_after",
                    case.name
                );
                assert_eq!(
                    frame.history.as_deref(),
                    case.expected["history"].as_str(),
                    "{}: history",
                    case.name
                );
                assert_eq!(
                    frame.from_seq,
                    case.expected["from_seq"].as_i64(),
                    "{}: from_seq",
                    case.name
                );
                assert_eq!(frame.to_seq, case.expected["to_seq"].as_i64(), "{}: to_seq", case.name);
                let expected_items = case.expected["items"].as_array().expect("items");
                assert_eq!(frame.items.len(), expected_items.len(), "{}: item count", case.name);
                for (index, item) in frame.items.iter().enumerate() {
                    let actual: Value = serde_json::from_str(item.json()).unwrap();
                    assert_eq!(actual, expected_items[index], "{}: item {index} JSON", case.name);
                }
            }
            ("accepted", _) => assert!(
                matches!(result, Ok(Frame::Other)),
                "{}: a valid non-items frame remains Other",
                case.name
            ),
            ("ignored", _) => assert!(
                matches!(result, Ok(Frame::Other)),
                "{}: unknown event is Other",
                case.name
            ),
            ("dropped", _) => {
                let error = result.expect_err(&case.name);
                let key = case.expected["key"].as_str();
                match key {
                    None => assert!(matches!(error, DecodeError::NotObject), "{}: {error}", case.name),
                    Some(_) if case.name == "items_invalid_item_scalar_drops_event" => {
                        assert_eq!(error.to_string(), "bad_item: items[0] wrong_type id", "{}", case.name);
                    }
                    Some(_) if case.name == "items_unprojected_number_never_panics" => {
                        assert!(
                            matches!(error, DecodeError::Key(_) | DecodeError::Item { .. }),
                            "{}: no panic, only a refusal",
                            case.name
                        );
                    }
                    Some(key) => {
                        let session_key = case.name.starts_with("items_missing_");
                        let required_missing =
                            matches!(case.name.as_str(), "missing_type_is_dropped" | "null_type_is_dropped")
                                || session_key;
                        let expected_code = if required_missing { "missing_key" } else { "wrong_type" };
                        assert_eq!(error.to_string(), format!("{expected_code}: {key}"), "{}", case.name);
                        assert!(
                            matches!(error, DecodeError::Key(_)),
                            "{}: a known key error, not a fabricated route",
                            case.name
                        );
                    }
                }
            }
            _ => panic!("{}: unexpected outcome {outcome}", case.name),
        }
    }
}

//! Bounded property tests for the wire ↔ snapshot round trip and `with_supervision` (#9239).
//!
//! What this layer adds beyond the fixture replays:
//!
//! - The fixture replays in `transport_fixtures.rs` only exercise accepted payloads whose expectations were
//!   derived from the iOS sources. They never generate duplicate keys, escaped key names, RawValue-shaped folds,
//!   or very deep nesting — exactly the shapes that drove #8227 and #9224.
//! - This file generates those shapes from a small, seeded, bounded corpus and asserts three invariants:
//!   1. **Round trip.** Whatever `Item::from_wire` accepts must also be readable by `Item::from_raw`, with the
//!      same `id`, `seq`, `kind`, `role`, `tool_use_id`, `supervision_user` and the same fold range, and the
//!      kept JSON must be the original bytes.
//!   2. **Error positions.** When a decoder refuses an item, the error must name a position inside the original
//!      item, not a position inside a re-parsed sub-object (the #9224 fold-position regression).
//!   3. **`with_supervision` purity.** Promoting an item never mutates unrelated keys or string values,
//!      including keys that *look like* `supervision_user` after an escape (e.g. `"a\"supervision_user"`).
//!
//! Both formerly ignored #9224 regression properties run here, so a future regression fails the suite.
//!
//! Bounded by construction: every generator below produces a fixed, small set of cases (no random iteration, no
//! external fuzz service, no network). Total runtime is well under a second in both debug and release.

use persona_core::Item;
use serde_json::value::RawValue;

/// Builds a `Box<RawValue>` from a JSON literal. Panics if the literal is not valid JSON: that is a bug in the
/// generator, not in the code under test.
fn raw(json: &str) -> Box<RawValue> {
    RawValue::from_string(json.to_owned()).expect("generator must produce valid JSON")
}

/// Asserts the wire → snapshot round trip: the wire reader accepts, the snapshot reader accepts, the head fields
/// and fold range agree, and the snapshot reader preserves the raw bytes.
fn assert_round_trip(json: &str) {
    let wire = match Item::from_wire(raw(json).as_ref()) {
        Ok(item) => item,
        Err(fault) => panic!("from_wire refused a generator-produced item: {fault:?}\n  json: {json}"),
    };
    let snapshot = match Item::from_raw(raw(json)) {
        Ok(item) => item,
        Err(error) => panic!(
            "from_raw refused an item from_wire accepted: {error}\n  json: {json}\n  \
             this is the #8227 regression shape; either the wire reader is too lenient or the snapshot reader is \
             too strict"
        ),
    };
    assert_eq!(snapshot.id, wire.id, "id diverged: {json}");
    assert_eq!(snapshot.seq, wire.seq, "seq diverged: {json}");
    assert_eq!(snapshot.kind, wire.kind, "kind diverged: {json}");
    assert_eq!(snapshot.role, wire.role, "role diverged: {json}");
    assert_eq!(snapshot.tool_use_id, wire.tool_use_id, "tool_use_id diverged: {json}");
    assert_eq!(
        snapshot.supervision_user, wire.supervision_user,
        "supervision_user diverged: {json}"
    );
    assert_eq!(
        snapshot.has_supervision_evidence(),
        wire.has_supervision_evidence(),
        "supervision evidence diverged: {json}"
    );
    match (&wire.fold, &snapshot.fold) {
        (None, None) => {}
        (Some(w), Some(s)) => {
            assert_eq!(s.through_seq, w.through_seq, "fold.through_seq diverged: {json}");
            assert_eq!(s.through_id, w.through_id, "fold.through_id diverged: {json}");
        }
        _ => panic!(
            "fold presence diverged: wire={:?} snapshot={:?}\n  json: {json}",
            wire.fold.is_some(),
            snapshot.fold.is_some()
        ),
    }
    assert_eq!(snapshot.json(), json, "raw bytes not preserved: {json}");
}

// ---------------------------------------------------------------------------
// Round-trip: duplicate keys
// ---------------------------------------------------------------------------

/// The wire reader is documented to keep the first value of a duplicate key. The snapshot reader must do the
/// same — that was the regression #8922 fixed.
#[test]
fn duplicate_item_keys_round_trip() {
    let cases = [
        r#"{"id":"a#0","id":"b#0","seq":1,"kind":"text","role":"user"}"#,
        r#"{"id":"a#0","seq":1,"seq":2,"kind":"text","role":"user"}"#,
        r#"{"id":"a#0","seq":1,"kind":"text","kind":"tool_use","role":"user"}"#,
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","role":"assistant"}"#,
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","supervision_user":true,"supervision_user":false}"#,
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","tool_use_id":"t1","tool_use_id":"t2"}"#,
    ];
    for json in cases {
        assert_round_trip(json);
    }
}

/// Duplicate keys inside the fold follow the same first-value-wins rule.
#[test]
fn duplicate_fold_keys_round_trip() {
    let json = r#"{"id":"a#0","seq":1,"kind":"fold","role":"assistant","fold":{"items":1,"tools":0,"thoughts":0,"through_seq":5,"through_seq":9,"through_id":"b#0","through_id":"c#0","supervision_user":true}}"#;
    assert_round_trip(json);
}

// ---------------------------------------------------------------------------
// Round-trip: null / missing optional fields
// ---------------------------------------------------------------------------

#[test]
fn null_and_missing_fields_round_trip() {
    let cases = [
        // Optional keys present but null.
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","tool_use_id":null,"supervision_user":null}"#,
        // fold: null (vs absent).
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","fold":null}"#,
        // An empty kind / role is still a string.
        r#"{"id":"a#0","seq":0,"kind":"","role":""}"#,
        // The minimum shape the wire reader accepts.
        r#"{"id":"a","seq":0,"kind":"text","role":"user"}"#,
    ];
    for json in cases {
        assert_round_trip(json);
    }
}

// ---------------------------------------------------------------------------
// Round-trip: escaped quotes and unicode look-alikes in keys
// ---------------------------------------------------------------------------

#[test]
fn escaped_quotes_in_string_values_round_trip() {
    let cases = [
        r##"{"id":"a\"#0","seq":1,"kind":"text","role":"user"}"##,
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","text":"line1\nline2"}"#,
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","text":"tab\tand \\ backslash"}"#,
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","text":"unicode 😀 emoji"}"#,
    ];
    for json in cases {
        assert_round_trip(json);
    }
}

#[test]
fn unicode_lookalike_keys_do_not_confuse_the_reader() {
    // The first case writes `supervision_user` with a Cyrillic `а` in place of the first `a`. The second
    // writes it with a Greek `α` in the same position. Both are *different* keys and must not flip the
    // supervision bit. The third is an ASCII suffix variant — not a homoglyph, but still a distinct key.
    let cases = [
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","supervision_а":true}"#,
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","supervision_α":true}"#,
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","supervision_userr":true}"#,
    ];
    for json in cases {
        let item = Item::from_wire(raw(json).as_ref()).expect("unicode look-alike keys are accepted");
        assert!(
            !item.supervision_user,
            "a look-alike key must not set supervision_user: {json}"
        );
        assert_round_trip(json);
    }
}

// ---------------------------------------------------------------------------
// Round-trip: oversized numbers
// ---------------------------------------------------------------------------

#[test]
fn out_of_range_numbers_round_trip_when_accepted() {
    // Numbers the core reads must fit in i64. Numbers it does not read are kept verbatim.
    let cases = [
        // A huge integer inside an unrelated key is fine.
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","unrelated_big":170141183460469231731687303715884105727}"#,
        // A float that fits inside an unrelated key is fine.
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","unrelated_float":1.5}"#,
        // i64::MAX is accepted for seq.
        r#"{"id":"a#0","seq":9223372036854775807,"kind":"text","role":"user"}"#,
        // i64::MIN is accepted for seq.
        r#"{"id":"a#0","seq":-9223372036854775808,"kind":"text","role":"user"}"#,
    ];
    for json in cases {
        assert_round_trip(json);
    }
}

// ---------------------------------------------------------------------------
// Round-trip: deeply nested unrelated data
// ---------------------------------------------------------------------------

#[test]
fn deep_nesting_outside_the_fold_is_preserved() {
    // 64 nested arrays under a key the core does not read. Below serde_json's default 128-level recursion limit,
    // so both readers must accept this and preserve every byte.
    let inner = "[".repeat(64) + &"]".repeat(64);
    let json = format!(r#"{{"id":"a#0","seq":1,"kind":"text","role":"user","deep":{inner}}}"#);
    assert_round_trip(&json);
}

// ---------------------------------------------------------------------------
// Round-trip: long strings
// ---------------------------------------------------------------------------

#[test]
fn long_strings_round_trip() {
    // 32 KiB of text. The wire and snapshot readers must both accept this, and the bytes must round-trip
    // unchanged. This is a smoke test against accidental truncation, not a performance benchmark.
    let text = "x".repeat(32 * 1024);
    let json = format!(r#"{{"id":"a#0","seq":1,"kind":"text","role":"user","text":"{text}"}}"#);
    assert_round_trip(&json);
}

// ---------------------------------------------------------------------------
// with_supervision: purity against unrelated keys
// ---------------------------------------------------------------------------

/// Calls `with_supervision` and asserts that unrelated bytes are untouched: keys that *contain*
/// `supervision_user` as a substring, keys whose spelling differs only by an escape, and string values that
/// happen to mention `supervision_user`.
#[test]
fn with_supervision_does_not_touch_unrelated_keys_or_values() {
    let cases: &[(&str, &[&str])] = &[
        // A string value that mentions the key name must not be rewritten.
        (
            r#"{"id":"a#0","seq":1,"kind":"text","role":"user","text":"do not patch supervision_user here"}"#,
            &["do not patch supervision_user here"],
        ),
        // A key whose name merely contains `supervision_user` as a substring.
        (
            r#"{"id":"a#0","seq":1,"kind":"text","role":"user","x_supervision_user":false,"not_supervision_user_key":false}"#,
            &["\"x_supervision_user\":false", "\"not_supervision_user_key\":false"],
        ),
    ];
    for (json, must_keep) in cases {
        let item = Item::from_wire(raw(json).as_ref()).expect("wire accepts");
        let promoted = item.with_supervision();
        assert!(promoted.supervision_user, "promotion must set supervision_user: {json}");
        for needle in *must_keep {
            assert!(
                promoted.json().contains(needle),
                "promotion rewrote an unrelated byte: expected to find {needle:?} in {}",
                promoted.json()
            );
        }
    }
}

/// Escaped-prefix keys (`"a\"supervision_user"`) must never be the target of promotion (#9224).
#[test]
fn with_supervision_does_not_patch_escaped_prefix_keys() {
    let json =
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","a\"supervision_user":false,"supervision_user":false}"#;
    let item = Item::from_wire(raw(json).as_ref()).expect("wire accepts");
    let promoted = item.with_supervision();
    // The escaped-prefix key must still read as `false`; only the actual `supervision_user` flips.
    assert!(
        promoted.json().contains(r#""a\"supervision_user":false"#),
        "the escaped-prefix key was patched: {}",
        promoted.json()
    );
}

// ---------------------------------------------------------------------------
// Error positions: fold errors are reported inside the whole item
// ---------------------------------------------------------------------------

/// Fold parse errors are reported within the whole item, not at fold-relative positions (#9224).
#[test]
fn fold_errors_carry_whole_item_positions() {
    // A fold with a number that overflows a double. The error column must point past the `1e400` literal in the
    // original item, not at a column inside the re-parsed fold object.
    let json = r#"{"id":"a","fold":{"x":1e400}}"#;
    let error = Item::from_raw(raw(json)).expect_err("a fold that overflows must be refused");
    let text = error.to_string();
    // The 1e400 literal starts at column 23 of the item. The error position must be at or past it.
    // The #9224 regression reported column 10 (inside the fold) instead.
    let column = text
        .split("column ")
        .nth(1)
        .and_then(|s| s.split_whitespace().next())
        .and_then(|s| s.parse::<usize>().ok())
        .expect("error must carry a column");
    assert!(
        column >= 23,
        "fold error must be reported at a column inside the whole item, got: {text}"
    );
}

// ---------------------------------------------------------------------------
// A sanity check that the harness itself is not vacuous.
// ---------------------------------------------------------------------------

/// If this test passes against the *current* main but a future refactor regresses the snapshot reader, the
/// duplicate-key tests above must fail. This meta-test exists to catch a generator that accidentally produces
/// items the wire reader rejects — those would make the property vacuous.
#[test]
fn generators_produce_wire_accepted_items() {
    let samples = [
        r#"{"id":"a#0","id":"b#0","seq":1,"kind":"text","role":"user"}"#,
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","supervision_user":null}"#,
        r#"{"id":"a#0","seq":1,"kind":"text","role":"user","fold":null}"#,
    ];
    for json in samples {
        assert!(
            Item::from_wire(raw(json).as_ref()).is_ok(),
            "generator sample refused by from_wire: {json}"
        );
    }
}

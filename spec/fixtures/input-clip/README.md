# Bounded tool inputs

The hub (`server/internal/transcript/clip.go` `clipJSON`) and runtime SDK (`integrations/runtime-sdk/lib/items.js`
`clipInput`) read these cases. They cover the third pass over tool-input objects: after the two existing string passes,
arrays lose tail items longest-first, then objects lose trailing fields widest-first, until the encoded object is at most
24,576 UTF-8 bytes. All data is made up.

## Rules

| Rule | Cases |
| --- | --- |
| Thousands of short values cannot keep an object over budget. | `short_values` |
| Nested arrays are detached correctly, and surviving values retain their order and types. | `nested_arrays`, `object_array`, `todo_array`, `question_array` |
| Object fields are dropped only after no attached array can be shortened; the root marker itself is never removed. | `wide_object`, `root_wide_object`, `escaped_root_object`, `single_long_key` |
| The two string passes still run first. | `mixed_strings` |
| The marker, its root comma and its changing omission-count digits are included in the exact bound. | `one_byte_over`, `exact_budget`, `marker_growth_boundary`, `empty_nested_boundary` |
| Removal and container ties use UTF-8/code-point key order, not JavaScript's UTF-16 or integer-first order. | `unsorted_root`, `unsorted_two_arrays`, `integer_keys`, `unicode_keys` |
| An input at the budget is unchanged, and a user field is never overwritten by the marker. | `at_budget`, `marker_collision_base`, `marker_collision` |

The marker is one collision-safe top-level object field, normally `zz_persona_clipped`, with exactly one `note` string:

```json
"zz_persona_clipped": {
  "note": "Omitted X array items and Y object fields.\n…（已截断，共 N 字节）"
}
```

The note names direct array-item and object-field removals and ends with the existing cut note. No marker value is inserted
into an array. The object wrapper keeps old and new clients' "first string argument" subject and summary fallbacks from using
the note as the call's title. Specialized clients ignore this unknown root field; generic argument and Raw Input views retain
and show it. Go and the web detect the cut by recursively visiting nested strings, including this note.

Removal visits object children in UTF-8 byte order, which is code-point order for valid Unicode, and uses that preorder for
container ties. This is the hub's order. The SDK applies the same choice without reordering the surviving fields in its own
output. Shared cases therefore compare parsed values and exact retained sets, not a requirement that both encoders emit object
fields in the same order.

## Format

`{"version": 1, "cases": [...]}`. Each case has `input`, a compact generated JSON value, and `expected` assertions. The
test consumers expand these directives recursively:

- `{"$repeat": N, "value": V}`: an array of N copies of V.
- `{"$fields": N, "key": "prefix_", "pad": 4, "value": V}`: an object with zero-padded indexes in its keys. Optional
  `base` fields are inserted first in the SDK, deliberately making input order differ from removal order.
- `{"$keyed_fields": [K...], "suffix": "k", "count": N, "value": V}`: one field per listed key, each suffixed by N copies
  of `suffix`; long keys can exceed the budget without invoking either string pass.
- `{"$index": 0}`: the innermost repeat index as a string; a positive value zero-pads to that width.
- `{"$string": "x", "count": N}`: a string repeated N times.
- `{"$single_field": K, "value": V}`: an object whose one expanded key K maps to V.
- `{"$pad_object_to": N, "value": {...}}`: add an ASCII `pad` field so the compact object is exactly N bytes.

`expected.check` selects structural assertions in both consumers. `input_bytes` and `output_bytes`, when present, are exact.
`remaining`, `a_remaining`, `b_remaining`, `inner`, `last_key`, `present`, `absent`, `array_items`, `object_fields`,
`key_prefix`, and `marker_key` pin exact values where shown. `marker` records whether structural clipping must leave a marker.
For an encoder-specific case, `go_input_bytes` / `sdk_input_bytes`, `go_output_bytes` / `sdk_output_bytes`, `go_remaining` /
`sdk_remaining`, and `go_object_fields` / `sdk_object_fields` override the shared values in their respective consumer.

Go and JavaScript each measure their own emitted JSON. Most exact sizes are shared; `escaped_root_object` deliberately pins
different values because Go escapes U+2028/U+2029 and JavaScript leaves them raw. Other known encoder differences are not
unified: JavaScript serializes negative zero as `0`, while Go can retain `-0`; Go replaces a lone UTF-16 surrogate with U+FFFD,
while JavaScript preserves and escapes it; and U+2028/U+2029 have the size effect above. Such differences can change a removal
count at the margin, so they need platform-specific expectations rather than byte-for-byte cross-language output comparisons.

## Checks

- Go: `cd server && go test ./internal/transcript -run TestClipInputSharedCases`
- Runtime SDK: `cd integrations/runtime-sdk && node --test test/input-clip.test.js test/input-clip-naive.test.js`

The SDK's naive-reference test independently walks only attached values and re-encodes before every removal, like the Go
oracle. It covers boundary budgets and seeded unsorted, integer-like and Unicode keys.

To add a case, define its generated input and independent structural expectations here, then implement the same named check
in both consumers. Do not generate expected values from the implementation under test.

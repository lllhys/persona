# StreamChunks split fixtures

This narrow domain replays iOS `StreamChunks.split(_:)`, not the complete streaming pipeline. Its reference is the current Apple implementation and the direct Apple test assertions named by each case. This is a host-side pure-core slice; no Swift execution, production migration, timing or rendering acceptance is implied.

`split.json` has `schema_version: 1`, `operation: "stream_chunks_split"`, and `cases`. Every case contains:

- a unique snake-case `name`, a one-sentence `description`, and `source` paths with function or test names;
- `input.text`, the complete valid UTF-8 string passed to the projection;
- `expected`, exactly `{ "done": [String], "tail": String }`;
- either `from_test` for an assertion copied from an Apple test, or `derived: true` for a boundary derived directly from the cited Apple source. A derived case that extends a test also cites that test and does not claim that its full expected object was asserted there.

The Rust replay checks the exact file and case-name manifests and compares every complete `expected` object. Except for the explicitly whitespace-padded separator, it also checks that joining `done` followed by `tail` with two newlines reconstructs the input. Apple discards that separator's spaces and tabs rather than normalizing them to one LF. The reconstruction invariant is not an independent expectation oracle. The original `done` and `tail` are the authority.

## Rules

| Rule | Cases |
| --- | --- |
| A blank line outside a fence cuts only when a later component exists and its raw text is nonempty with neither a literal space nor tab prefix. | `cuts_after_blank_line_once_next_block_starts`, `blank_line_at_end_is_not_cut_yet`, `keeps_indented_continuation_with_block`, `tab_indented_continuation_stays_with_block`, `non_ascii_space_is_not_raw_indentation` |
| The chosen separator line is omitted. Other original lines, spaces, carriage returns and newlines are not normalized or trimmed. Consecutive blank lines can therefore leave a newline in a finished block. | `space_padded_separator_is_omitted`, `consecutive_blank_lines_keep_their_newline`, `leading_blank_line_can_finish_an_empty_block`, `carriage_return_is_not_a_line_separator`, `crlf_carriage_return_is_not_a_blank_line`, `crlf_fence_is_not_recognized` |
| Lines split on LF only. A carriage return is not trimmed by Foundation `.whitespaces`, so CRLF fence recognition and its resulting split are not generalized from ordinary LF inputs. | `carriage_return_is_not_a_line_separator`, `crlf_carriage_return_is_not_a_blank_line`, `crlf_fence_is_not_recognized` |
| An opening fence has at least three backticks or tildes. A later backtick rejects a backtick opener. A closer uses the same character, is at least as long, and has only spaces or tabs after its run. Candidate lines are trimmed before fence recognition. | `never_cuts_inside_code_fence`, `longer_fence_ignores_shorter_inner_closer`, `language_suffixed_line_does_not_close_fence`, `inline_backticks_do_not_open_a_fence`, `unclosed_fence_keeps_remainder_in_tail`, `indented_fence_preserves_original_spaces`, `tilde_fence_requires_tilde_closer` |
| The unfinished text is returned as the tail. The projection does not repair Markdown, calculate visual spacing, schedule reveals or decode UTF-8. | `prewarm_uses_the_tail`, `empty_input`, `unclosed_fence_keeps_remainder_in_tail` |

`whatIsCutStaysAsTheTextGrows` also checks every third extended-grapheme prefix in Swift. `reassembles_full_text` uses that test's full input but is derived from its reconstruction assertion and the split algorithm; it does not claim a source-derived Rust prefix loop reproduces Swift Character indexing. Unicode combining and ZWJ boundaries are not independently asserted by a new Apple replay here.

Run:

```sh
cargo +1.98.0 test --manifest-path native/app-core/Cargo.toml -p persona-core --test stream_chunks_projection
```

Expected values are manually sourced from Apple tests and source; they are never generated from the Rust implementation. The `ios_current` behavior is not extended to a proposed new streaming policy.

Out of scope: `StreamChunks.gap`, `StreamRepair`, rendering, prewarming, UI spacing and animation, coalescing or reveal timing, production adapters, C/JNI/NAPI bindings, device evidence and performance acceptance (#4044).

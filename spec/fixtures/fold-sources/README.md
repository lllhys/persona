# Sources of a run of work

The web pages a run of work used: the Sources row under a finished reply (web, #9132) and the `sources` a fold
carries (`docs/protocol/protocol.md` "Folded work"). The hub's node reads a folded run
(`server/internal/transcript/fold_sources.go` `foldSources`); the web reads the same calls when they are on the page
(`web/src/lib/turn-sources.ts` `turnSources`). Both read these cases, so the row says the same before a reload (the
calls came live) and after it (the page came folded). All data is made up.

## Rules

| Rule | Cases |
| --- | --- |
| A Claude Code WebSearch names its results on a `Links: [{"title","url"}, …]` line, in order. | `claude_links` |
| A result with several searches has a Links line for each, all read. | `several_links_lines` |
| A Codex web search names one result a line, `title  url` (two spaces); a line with only a domain names no page, a bare URL one without a title. | `codex_lines` |
| Pages the run opened (WebFetch, Codex's open page) come first, then the search results; each URL once, and a later mention gives an entry without a title its title. | `opened_pages_first`, `deduplicated_and_capped` |
| A result the hub cut (`full_size`, its note at the end) loses its last line, which may be cut; a Links list cut inside an entry keeps the entries before it. | `clipped_links`, `clipped_lines` |
| Only web calls whose result is there and is not an error count; other tools never do. | `only_successful_web_calls` |
| Only `http` / `https` URLs (either case) without white space count. | `only_web_urls` |
| At most 10 (`proto.FoldSourcesMax`). | `deduplicated_and_capped` |
| A title is trimmed and cut past 200 characters to 199 and an ellipsis. | `long_title` |
| A run without web calls has none: the hub leaves `sources` out, the web draws no row. | `no_web_calls` |

## Format

`{"version": 1, "cases": [...]}`. Each case has `input.items`, the run's items as the hub sends them (`tool_use` and
`tool_result` items, with `tool_name`, `tool_input`, `text`, `is_error`, `full_size`), and `expected.sources`, the list
in order, each `{"url", "title"?}`.

## Checks

- `server/internal/transcript/fold_sources_test.go` (`go test ./internal/transcript/ -run FoldSources`)
- `web/src/lib/turn-sources.test.ts` (`npx vitest run src/lib/turn-sources.test.ts`)

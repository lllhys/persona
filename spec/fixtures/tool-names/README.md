# Tool names, subjects and arguments

How a tool call reads in a chat when no table has words for it (`docs/design/DESIGN_SYSTEM.md` section 4.7): another
server's MCP tool as "<Server> · <Tool>", any other unknown name in words, the subject a row shows after the tool's
name, a shell tool's command line, and the arguments an opened unknown tool lists instead of its JSON. iOS is the
reference (`apple/Persona/Views/Chat/ToolNaming.swift`, `ToolPresentation.subject` in
`apple/Persona/Views/Chat/ToolViews.swift`, the cases of `apple/PersonaTests/ToolNamingTests.swift`). All data is
made up. Words here are English only: the verbs a table gives a known tool ("Read", "Run") are localized in each
client's resources and are not in these cases.

## Rules

| Rule | Cases |
| --- | --- |
| A name in words: cut at `_`, `-`, `.`, `/`, `:`, spaces and where camel case turns; words spelled their own way keep it ("GitHub", "PR", "iOS"), an acronym stays as written, the rest lowercase; the first word gets a capital. | `words_*` |
| A server's name: a plugin's (`plugin_<plugin>_<server>`) and a claude.ai connector's (`claude_ai_<name>`) read as their own name, once; every word capitalized or spelled. | `server_*` |
| Another server's MCP tool reads "<Server> · <Tool>", even when its bare name is a built-in tool's; Persona's own tools and built-in tools have no such label. | `mcp_*` |
| The tables match a tool on its own name, but another server's tool on its whole name, which no table has. | `table_name_*` |
| An unknown tool's subject is the first of `description`, `title`, `subject`, `name`, `file_path`, `filePath`, `notebook_path`, `path`, `url`, `uri`, `query`, `pattern`, `command`, `prompt` with words in it (a path by its last part, else its first line), else its first string by field name; numbers, flags and blank strings say nothing. | `subject_*` |
| A row's subject: another server's tool by that rule; Claude Code's tools by their own fields (the to-do, "#3", "to · summary", "select:" as names, a file and line). Two subjects are words of their own, in English here: several findings by their count ("2 findings"), and a wake-up that gives no reason by its delay as a work line writes a time ("5 min", "1m 20s", "1 hr 5 min"). | `row_*` |
| A shell tool's detail is its command line: Bash `$ `, PowerShell `PS> `, Monitor `$ ` or its WebSocket's address; no other tool has one, nor another server's tool of the same name. | `command_*` |
| An unknown tool's arguments: the subject fields first, then the rest by name; each labelled in words; text on one line, or up to 3 when it has lines of its own, blank lines closed up; a token (no spaces, ASCII) in the code face, a sentence in the text face; numbers, flags and `null` in the code face (numbers as a Double prints: `1e+20`); lists of plain values joined with ", "; anything nested as one line of JSON with sorted keys; empty strings, lists and objects left out; a value cut at 300 characters. | `arguments_*` |

## Format

`{"version": 1, "cases": [...]}`. Each case has a unique `name`, a `check` and an `expected`:

| `check` | Input | `expected` |
| --- | --- | --- |
| `humanize`, `server_name`, `table_name` | `input`: a string | a string |
| `mcp_label` | `input`: a tool's full name | a string, or `null` for no label |
| `subject` | `input`: a call's input (any JSON) | a string |
| `tool_subject` | `tool`: a tool's full name; `input`: its input | the subject its row shows |
| `command_line` | `tool`, `input` | a string, or `null` |
| `arguments` | `input`; optional `skipping`: field names left out | a list of `{"id", "label", "value", "code", "lines"}` in order |

## Checks

- Android: `cd android && ./gradlew :core:domain:test --tests '*ToolNamingFixtureTest*'` (`ToolNaming.kt`, the row subject
  `toolSubject` in `Rows.kt`).
- iOS reads these cases in a later change (`docs/plans/android-ios-alignment/phase-4-staying-aligned.md`, R2); until then
  `ToolNamingTests` holds the same cases. Where they disagree, iOS is the reference and these cases follow.

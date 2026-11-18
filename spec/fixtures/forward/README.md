# Forwarded messages

A message forwarded from one chat to another (#3807) carries the original in a `<persona-context kind="forward">` block
before the note the user typed. The format is in [protocol.md](../../../docs/protocol/protocol.md), "Forwarded messages".
The iOS app (`apple/Persona/Views/Chat/Forward.swift`, `ContextRef` in `apple/Persona/Views/Link/LinkSheet.swift`)
writes and reads these blocks and checks them against these cases; the web app and Android follow the same cases.

## Rules

- The opening tag is `kind`, `title`, `url`, `msg`, then the metadata in this order: `role`, `author`, `chat`,
  `project`, `machine`, `time`; any other attribute follows, by name. `write_agent_reply`
- The text sits on its own lines between the tags. `write_agent_reply`
- Attribute values escape `&`, `"`, `<` and `>`; a closing tag inside the text is taken out. `write_escapes`
- `url` is `persona://session/<machine id>/<session id>?at=<item id>`, the item id percent-encoded (`#` as `%23`).
  `write_agent_reply`, `read_with_note`
- Reading back gives the block's fields, unknown attributes included, and the note after it; the origin is the
  session and item `url` names. `read_with_note`

## Cases

`cases.json` holds `{"version": 1, "cases": [...]}`. A writing case has `block` (the fields) and `xml` (the block it
writes); a reading case has `message` and `expected`.

## Several at once

Forwarding several messages at once (#3838) sends one block per message; nothing new is written into a block.

- The blocks go in the chat's order, not the order they were picked, one per line; a blank line, then the note.
  `three_then_a_note`
- Each message is cut at the smaller of 20,000 characters and 60,000 divided by the number forwarded (rounded down),
  with the usual line saying so; at most 30 go at once. `budget`
- Reading: forward blocks next to each other whose `url` names the same session make one group, shown as one card;
  another session's forward or any other block ends it. `groups`

`several.json` holds `budget` (`each`, `total`, `most` and the limit for some counts), `write` (`blocks`, `note` and the
`message` they make) and `read` (`message`, each block's `msgs`, the `rest` after the blocks, and the `groups` as
indices of the blocks).

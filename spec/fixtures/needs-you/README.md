# Agents' Needs You fixtures

The Agents tab names one number "Needs you" in three places: its section's header, its Needs you filter and the tab's
badge. These cases pin what that number counts, so the places and the clients agree (`docs/design/PRINCIPLES.md`,
"Consistency first"): one row per Agent session that waits for the user, the definition `docs/assistant/managers.md`
("Counts") gives an assistant's waiting agents.

| Rule | Cases |
| --- | --- |
| A session waits while it has at least one pending card, whatever its state says, or while the hub says it needs the user and no card was read (a prompt only its terminal shows). It is one row; its cards sit under it, oldest first. | `cards_of_one_session_are_one_row`, `a_session_waiting_in_its_terminal_is_a_row`, `a_card_makes_its_session_wait_whatever_its_state` |
| A card that belongs to no session (`personad ask` outside herdr) is a row of its own, even beside another on the same computer. | `a_card_with_no_session_is_its_own_row` |
| Rows go oldest first: by their oldest card or, with none, by when the session started waiting (`state_since`). | `rows_go_oldest_first` |
| The assistant's own conversations are the Persona tab's: never a row. | `persona_conversations_are_not_agents` |
| A muted agent (itself, or its space unless it was turned back on) and a quiet task's agent stay rows: they still wait. The tab's badge leaves them out, as the assistant's count does. | `muted_and_quiet_agents_are_rows_but_not_on_the_badge` |
| Working, finished (Awaiting reply included), idle and offline sessions without a card are not rows. | `other_states_do_not_need_the_user` |

- Structure: `{"version": 1, "cases": [...]}`. `input` has the hub's wire objects, `sessions` and `interactions` (the
  pending cards), with `mutes` as the hub's pins give them (`sessions`, `spaces`, `allowed`, each `<machine>/<name>`)
  and `quiet`, the `<machine>/<session>` of quiet tasks' agents.
- `expected.rows` lists the section's rows in order, each with its `id` (`agent:<machine id>/<session id>`, or
  `card:<interaction id>` for a card with no session) and its `cards` (interaction ids, oldest first);
  `expected.badge` is the tab's badge. The header and the filter show the number of rows.
- All data is made up.
- Run: `AgentsNeedsYouFixtureTests` in `apple/PersonaTests` (iOS: the section, its filter and the tab's badge) and
  `cd android && ./gradlew :core:domain:test --tests '*AgentsNeedsYouFixtureTest*'` (Android: the tab's badge; its
  "Needs your decision" section counts decisions, one per card, under a name of its own). The web app's Agents badge
  counts sessions in Needs you without quiet tasks' agents by a rule of its own, which Persona Link's summary shares
  (spec/fixtures/attention): it counts muted agents and no card on no session, so it does not replay these cases.

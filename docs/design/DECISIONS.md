# Design decisions

The history of the rules in `docs/design/DESIGN_SYSTEM.md`: what was decided, when, what it replaced, and where it landed. Append only; correct an entry with a new one. This file holds no rules: where it and DESIGN_SYSTEM.md differ, DESIGN_SYSTEM.md wins. The principles and their cases are in `docs/design/PRINCIPLES.md`.

## 2026-11

- **11-10 Structure rules for primary screens** (#3186): sessions that need the user, the work-item row and list controls (3.4), pending requests (4.8), objects, titles and state words (5.6), a reading route, and who looks at a UI change (1.4). They hold on iOS, Android and web; visual values stay per platform. They apply `docs/design/PRINCIPLES.md` to Agent work rather than adding principles. Rejected: five more principles next to those six; rules for the Apple apps only.
- **11-10 Default views stay as they are** (#3186): the rules do not choose a default grouping. iOS and Android open on By Repository; the web opens on its paged list, on By to-do, or on the chat-list view (#2805). Rejected: attention sections as the default on every platform.
- **11-10 Rows may show the model's short name** (#2805, #3186): raw model IDs stay off primary screens. Rejected: no model in rows.
- **11-10 List controls follow #2805** (#3186): a grouping control, status chips and a filter bar may sit above a list, each kind looking like itself. Rejected: one row of filter chips, with grouping and display in the ⋯ menu.
- **11-10 Awaiting Reply stays its own state** (#3186). Rejected: folding it into Needs You.
- **11-10 No freeze on new appearance options** (#3186): revisit once the structure rules have been in use for a while. Rejected: freezing new styles now. Where: 1.2.
- **11-10 Row content, not a new row style** (#3186): every row style shows the work-item row's content, with the Avatar style's look as the reference. Rejected: a fourth row style. Where: 3.4.
- **11-10 Answers stay in rows** (#3186): choices, and low-risk tool approvals whose full command is shown. Plans, high-risk actions and Always Allow for This Session open the session. Rejected: opening the session for every answer. Where: 3.4, 4.8.
- **11-10 An Agent's run is called a session** (#3186). Rejected: "task", which names the work Persona hands out. Where: 5.5, 5.6.
- **11-10 Needs You counts sessions** (#3186): a row is a session, so the number equals the rows (principle 4). Rejected: counting requests. Where: 4.8.
- **11-10 No failed state** (#3186): a failed run says what failed in its row. Rejected: a failed state with its own priority. Where: 5.6.
- **11-10 Who looks at a UI change** (#3186): a person for a large change, the Agent itself for a small one. Rejected: always a person; always the Agent. Where: 1.4.
- **11-10 By Repository keeps its tree** (#3186): computer, repository, worktree, then session. Rejected: the work-item row in By Repository, which would leave the old view nowhere to be found. Where: 3.4.

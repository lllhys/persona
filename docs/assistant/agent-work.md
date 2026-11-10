# Agent work state: working / blocked on a named prerequisite / done (#1686)

The user's words (2026-11-04): "Persona isn't proactive enough right now. I see lots of unread sessions… Every session should either be working, explicitly waiting for a prerequisite,
or done… Minimize waiting time as much as possible."

## Why

The unified states in `docs/protocol/agent-states.md` describe the **host's perspective**: Needs you / Working / Unread / Seen. "Unread" means only that a turn ended and the user has not read it,
not how far the session has progressed **in its work**. The Agents page ends up with a row of green dots that must each be opened and read to find out:

- Which tasks are done and ready to wrap up;
- Which are waiting for a user decision ("Should I push to beta?", "Which of D2 / D4 / D10?"), yet show only a green dot in the list;
- Which are waiting for an external event (deployment completion before testing, CI, an OAuth link), with nothing to wake them when the event occurs;
- Which stopped only because of a transient error such as `API Error: 503`, and simply need a "continue" message;
- Authorization links that expire within minutes and are already invalid by the time the user sees them.

## Model

In work terms, each session is in exactly one of three states (`proto.Session.work.phase`):

| phase | Meaning | List preview |
| --- | --- | --- |
| `working` | The host is running (`state = working`) | Keep using `activity` / `last_text` |
| `blocked` | Stopped, waiting for a **named** prerequisite (`on`) | "What it is waiting for" |
| `done` | The task is complete and delivered | One-line conclusion + artifacts |

A `blocked` prerequisite has four types (`work.on`):

| on | Examples | Who advances it |
| --- | --- | --- |
| `user` | Choose a plan, decide whether to push to beta, provide information, click an authorization link, expired login | User: Persona turns the question and options into a "Needs you" card; one tap sends the answer back to the Agent. |
| `agent` | "Wait for the neighboring Agent to merge, then rebase" | Persona: wakes it when that Agent stops. |
| `external` | Wait for deployment / CI / review / a time; background commands and `/loop` wake themselves | Persona: wakes it on schedule to check for itself (no wake needed for background commands or `/loop`). |
| `transient` | `API Error: 503 / 529 overloaded / Connection error` | Persona: automatically sends "continue" after backoff, with a limit. |

Relationship to unified states: `state` values **do not change** (old Apps display them as before); the algorithm simply also considers work state:

| Work state (this turn) | `state` | Explanation |
| --- | --- | --- |
| `done` | `idle` | No longer occupies "Unread"; conclusion is in `work.line` and included in pushes. |
| `blocked` · `user`, with options | `needs_you` | Persona created a question card (`pending_interactions` increases by one). |
| `blocked` · `agent` / `external` / `transient`, with Persona waiting (`wake_at` or a target Agent) | `idle` | New Apps show "Waiting" + what it is waiting for; old Apps show Seen. |
| Not triaged, triage failed, or unclear (`unsure`) | Unchanged (`unread`) | Better to leave a green dot than hide something not understood. |

"Unread" then remains only while triage is pending (a dozen or so seconds after a turn ends) or when the model cannot tell.

## Responsibilities (in the manager since 2026-11-04)

Triage belongs to the **manager** (currently just one: the assistant process `personad assistant`, `server/internal/assistant/agents_*.go`). The hub is only a transport
(`hub/agents_link.go`; bus methods are in `docs/protocol/protocol.md`, "Bus" → "Agents"):

| Component | Responsibilities |
| --- | --- |
| Manager | Post-turn triage (rules + model `agent.triage`, through the hub's `app.call models.json`), tool-loop checks, automatic continuation, wakeups, cards and answers, triage switches (settings `agents.triage`), tables `session_work` / `session_work_events`, statistics; labels for new Agents (`agent.title`), and launchers / models for new and resumed Agents ("Starting" below). |
| hub | Reports what happened: turn starts / ends (`session.turn_started` / `session.turn_ended`, already sent), a user views a session in the App (`session.seen`). Displays the manager's snapshot: session-list `work` (snapshot `work`), its cards (snapshot `interactions`, displayed, pushed, and expired like assistant questions, with answers returned through `manager.action`). Messages to Agents forward to the node through `device.call session.send`. |

- **Session-list `work`**: the manager's snapshot reports each session's latest turn's work state (`work.turn_at` is when that turn ended). The hub displays it only if the session is still on that turn.
  Working (`{"phase":"working"}`), the Agent asking its own question (its cards precede the manager's), and a terminal dialog are reported by the hub from the current session state without waiting for the manager. No `work` when the main switch is off.
- **Starting**: before `POST /api/v1/sessions`, the hub asks the manager for `agents.start_params`. It fills launcher, model, thinking effort, and fingerprint from **Model Settings › Agents** (the launcher / model selected for this start override them; `docs/assistant/model-config.md`),
  then derives a tab label from the first message. Agents started in the background (`async`) or from Lark cards request their names later through `agents.tab_name`. Before sending to a stopped session with an unknown launcher (resuming it), ask `agents.resume_params`.
  `POST /api/v1/tabs/autoname` also belongs to the manager.

**When the manager is disconnected** (the hub just started and it has not handshaken; the second or two during its restart; it crashed and is being relaunched):

- Starting an Agent or resuming a session with an unknown launcher: if the manager has never connected since hub startup, wait for it (until 60 seconds after hub startup, no longer than the request's own deadline). Requests queued on the port and App replays arrive immediately after a hub restart,
  so they still get parameters from the manager. If it connected before and is now absent, do not wait.
- If it does not arrive, the Agent cannot start: since 2026-11-04, the manager connects to computers ([machines.md](../server/machines.md), "Who connects to computers"). Without it nothing can reach the machine, so starts return `503 assistant_offline`,
  as do messages to stopped sessions (resuming them). Previously, the hub applied **Model Settings › Agents** itself and could still start them.
- Triage pauses: turn-end events are not sent to it (the hub does not queue them). Due continuations / wakeups wait until it returns, with the usual rechecks before sending. On connection / reconnection, catch up: the next check (within 15 seconds) begins triaging turns that ended during the outage,
  remain unread, and are within 24 hours (up to 3 at a time, with further batches each minute; the same applies at hub startup, because its bus opens last and nodes connect first). Do not catch up turns already seen during the outage (in the App or herdr on the computer), as before after hub restarts.
  Cards for sessions that resumed working during the outage are removed at the next check (every 15 seconds).
- The session list retains its last reported `work` (still only for sessions remaining on that turn). If a wakeup is over 5 minutes late, it no longer counts as "Waiting" (`wake_at` is removed from the list, returning to Unread).
- Its cards remain visible and answerable: the hub records the answer first and delivers it after the manager returns and reports a snapshot. If the manager temporarily cannot accept it (for example, it has not restored its state), retain it and retry after 10 seconds until it accepts,
  says the card no longer exists, or the card's expiry is reached. If the hub also restarts, this answer is lost and the card reappears.
- Switch / statistics endpoints (`/api/v1/agents/triage`, `/api/v1/agents/work/stats`, `/api/v1/tabs/autoname`) return `503 assistant_offline`.
- Manager-only restart: cards still held by the hub remain, without another push. As when triage lived in the hub, remove them only on answer, resumed Agent work, or expiry.
- Hub restart (the manager is its child process and restarts with it): for the second or two before the manager's first snapshot, the list has no `work` (triaged sessions briefly return to Unread). Cards reappear after the computer reconnects (same id, another push, as with previous hub restarts).
  Remove a card only if the Agent has progressed (resumed work or a later turn). A session merely looking different just after reconnection (seen and now idle, waiting for its own question, or a restarted node not yet having read the transcript timestamp) does not count. Retain the card and restore it at the next check (`cardBack`).
  This card also stays pending in interaction history: at startup the hub marks pending interactions left by the previous process as cancelled, except those referenced by `session_work.interaction_id` (`db.ExpireStaleInteractions`). The manager marks as cancelled only those it does not restore.
  Previously (before 2026-11-06), every hub restart removed decision cards (including safety-critical deployment and merge cards): sessions looked "seen" or "on another turn" in the first seconds after reconnection, so the manager forgot the cards, and interaction history had already marked them cancelled.

If a card is answered but the message cannot reach the Agent (its computer just disconnected), recreate the card with a new id and push again so the user can answer again (previously the App's answer attempt returned an error).
If the manager's snapshot still reports a card whose answer was delivered to it (but not accepted), the hub restores the card after 30 seconds.

## Triage

### When

- A turn ends (working → done / idle in `setSessions`, at the same point as the "Completed" push). If it remains inactive for `triage_settle_seconds` (default 15 seconds), begin triage.
  If work resumes during that interval (a Stop hook continues it, or another message arrives immediately), do not triage that turn.
- After every manager connection and once per minute afterward, catch up stopped sessions whose turns ended within 24 hours, are still unread, and have not yet been triaged (including turns that ended after deployment or while the manager was disconnected; see "Responsibilities").
- Do not triage assistant chats (`assistant`, including AI twins and Lark /agent) or sessions outside the host (`offline`). Muted sessions are still triaged, but no cards are created.

### Rules first, then the model

Apply in order; on a match, do not call the model:

1. Pending interactions / host `blocked`: already Needs you, `blocked · user`, with the interaction's question as `line`.
2. This turn was interrupted by a model API error (`turn_error`):
   - Source: Claude Code writes the error as its last words (`API Error: …`); the hub recognizes the prefix of `last_text`. Codex does not write it as a reply. Its error is in this turn's
     `task_complete.error` (rollout) or `turn/completed` with `status: failed` + `turn.error` (app-server). The node reads and reports `turn_error` directly,
     plus `turn_error_kind` based on the Codex error type (`codex_error_info`) (#2185, "Codex error types" below).
   - Use `turn_error_kind` when present; otherwise use the text:
   - A subscription's usage limit (`usage_limit`) → an `external` wait for its reset, or a card when the reset is not known ("Usage limits" below); never `transient`;
   - Transient (408, 429, 500, 502, 503, 504, 529, `overloaded`, `Connection error`, `Connection lost mid-response`, `timed out`, `socket hang up`, `ECONNRESET`, `ECONNREFUSED`, `fetch failed`, `no eligible upstream`, `upstream`) → `transient`, schedule automatic continuation;
   - Other errors (`Failed to authenticate`, `OAuth token has expired`, `Invalid API key`, `Not logged in · Please run /login`—Claude Code's synthetic reply when not logged in—400 / 401 / 403 / 413, `invalid_request_error`, a prompt or context too long, selected model unavailable, `Request was aborted` after a user interrupt) → `blocked · user`, with a card "The model service needs your attention: <original text>" and options "Fixed, continue" / "Leave it for now". These words win when an error also has transient ones (`transientError`).

   **Codex error types** (`transcript/codex_error.go`; `server_overloaded` in rollout and `serverOverloaded` in app-server mean the same thing):

   | Codex reports | `turn_error_kind` |
   | --- | --- |
   | `server_overloaded`, `internal_server_error`, `rate_limit_exceeded` | `transient` |
   | `http_connection_failed`, `response_stream_connection_failed`, `response_stream_disconnected`, `response_too_many_failed_attempts`: no status code, or 408 / 429 / 5xx | `transient` |
   | Same types, other status codes (401, 403, 400…) | `user` |
   | `usage_limit_exceeded` | `usage_limit` ("Usage limits" below) |
   | `context_window_exceeded`, `unauthorized`, `model_not_found`, policy errors, and future new types | `user` (continuation cannot resolve them) |
   | `other`, no type | Not reported; use text |

   Stopping with Esc (`turn_aborted`, `status: interrupted`) is not an error. The node rereads a changing rollout at most every 3 seconds. If the file is written again during throttling,
   omit the cached `turn_error` (the continued turn may already have completed normally; an old error would continue it again). Better to be a few seconds late.
3. Background commands / sub-Agents still running (`background`, `subagents` contain running entries) → `external`, "N background tasks running"; do not register a wakeup (results wake it themselves).
4. `/loop` running (`loop`) → `external`, "Scheduled loop, next at HH:MM"; do not register a wakeup.
5. Otherwise: model.

### Model

- Purpose `agent.triage`, "Session Triage" (**Settings › Model Settings**, under "Information filtering"; defaults to inheriting the inexpensive Inbox triage model).
- Input: Agent name, directory / project, the user's request for this turn (`lastTurn` ask), final reply (first and last 1500 characters), other working Agents on the same computer (name + short id), current time, and up to 20 resolved user question/answer pairs from the session's latest 100 interactions, newest first. Each question and answer is capped at 200 runes. Non-question interactions, assistant-authored answers, cancellations and deferrals (including localized canned pause/wait/hold answers) are excluded. A history read failure leaves triage retryable. This history is decision context, not permission to send an automatic wake.
- JSON output (`server/internal/prompts/agents/triage.md`):

  ```json
  {"phase":"done|blocked|unsure", "on":"user|agent|external",
   "line":"At most 30 characters: conclusion or what it is waiting for",
   "questions":[{"question":"…","header":"…","options":[{"label":"…","description":"…"}],"multi_select":false}],
   "link":{"url":"https://…","minutes":10},
   "wait":{"what":"What it is waiting for","then":"What to do afterward","check_minutes":10,"agent":"<short id>","risky":false},
   "artifacts":[{"kind":"pr|commit|doc|build|url","ref":"…"}]}
  ```

- Decision principles in the prompt:
  - An optional offer at the end such as "Would you also like me to…?" does not mean blocked. If the task is complete, it is `done`; include the offer in the conclusion.
  - Only an unanswered request for a user decision, information, or action can be `blocked · user`. Arrangements, status reports, and acknowledgments are not questions. `questions[].question` must quote the final reply verbatim: a question ending in `?` / `？` after ignoring trailing Markdown, quotes, brackets, emoji or an answer annotation such as `(y/n)`, or an explicit request. Code rejects invented or paraphrased questions, and an authorization/login link is a separate `user` case even without a question.
  - **Pushing code, deploying / releasing, sending messages / comments to others, or merging / modifying someone else's MR / PR** still receives no automatic wake unless it is approved (see "Safety boundary"). Blocking automation does not invent a request for permission.
  - If uncertain whether the task is complete, use `unsure`, not `done`.

### Where the conclusion goes

- The `session_work` table ("Storage" below), sent with the session list as `work`;
- When done: add `完成：<结论>` ("Completed: <conclusion>") to the knowledge-base project timeline for this directory (`kbAgentDone` used to record the last words of every turn; now it records the conclusion when available);
- Replace the "Completed" push body with the conclusion; the main chat's live snapshot adds "Done" and "Waiting" sections ("What Persona knows" below).

## Closeout

A closeout is a small record an Agent ends a turn with, so that a stopped session answers "done or not, merged or not, deployed or not, what is missing" without anyone reading its transcript or asking it (#3612). It is the Agent's own claim; nothing in it has been checked by the hub.

```text
<closeout status="done|not_done|blocked" merged="yes|no|n/a" deployed="yes|no|n/a">
did: <what changed, one sentence; up to three such lines>
artifact: <pr|commit|branch|doc|build|url> <reference>
verified: <what was run or measured and its reading, or "untested">
missing: <what it still needs from someone, if anything>
waiting: <for status="blocked": who or what it waits for>
</closeout>
```

- **Reading it** (`server/internal/closeout`): the last block in the final reply counts. The attribute names, the line keys and the status values are fixed; the text after each colon is in the user's language. A block with no status, or one that is not `done`, `not_done` or `blocked`, is no closeout. Bounds: three `did` lines of 200 characters, five artifacts, 300 characters for `verified` and `missing`, 120 for `waiting`. `n/a` and an unsaid `merged` / `deployed` are left empty.
- **Asking for it** (the `closeout` switch, off by default): when an Agent's Stop hook fires, the node holds the stop once (`decision: block`, `server/internal/node/closeout_stop.go`) if all of these hold: the session has Persona's own hooks (sessions the user started in a terminal only report and are never held); it is not an assistant conversation, a subagent or a review's reviewer; the stop is not already a held one (`stop_hook_active`); the last reply is not empty, not a model API error and has no closeout; and the turn changed files or ran commands (a `tool_use` of `Edit`, `Write`, `MultiEdit`, `NotebookEdit` or `Bash` since the turn's prompt, read from the last 2 MiB of the transcript). The node then asks the manager (`agents.closeout.check`, 5 seconds), which answers with the reason only while the main switch and `closeout` are on (`server/internal/assistant/agents_closeout.go`, prompt `prompts/agents/closeout_required.md`). The Agent is told to repeat its conclusion with a closeout and to start nothing new. A turn that cannot be read, a manager that does not answer, and an Agent that ignores the request all end as before: at most one held stop per turn. The work event `closeout_asked` records each one.
- **Triage** reads the closeout from the final reply before it asks the model. `status="done"` is `done` by rule (`by: "rule"`, no model call), with the first `did` line as the list line. With `not_done` or `blocked` the model is still asked what the turn waits for, and the closeout is kept on the result; when the Agent says `not_done` and the model reads the reply as done, the Agent's word wins and the turn stays untriaged (Unread), as any turn nobody can call finished.
- **Where it goes**: `work.closeout` on the session ("Protocol" below). The session lists carry only where it stands (`status`, `merged`, `deployed`, `waiting`, `missing`); `did` and `verified` come with the session itself.
- **Supervision messages** carry the closeout of a task's Agent as its own lines, marked as the Agent's claim that nobody checked, with the block taken out of "what it said last" (`supervise_digest.go`, `prompts/assistant/supervise.md`); the supervisor is told to verify merged, deployed and verified as for any report.
- **Checked by the hub** (`checkCloseout`): when the closeout names a GitHub pull request by its link, triage looks at it with the deploy check that deliveries use (`deploy_check.go`: the pull request's merge, and whether the running hub contains the merge commit) and keeps the result beside the claim as `closeout.checked {merged, deployed}`, each `yes`, `no` or absent when it could not tell. `deployed` is about the running hub, so it is known only for the hub's own repository; for another repository only `merged` is. A closeout that says merged or deployed when the check says it is not is never `done`: the turn stays untriaged (Unread) with the closeout and the finding kept. Supervision messages show the finding under the closeout.
- **Delivery**: a reply with a closeout and no delivery line of its own (`docs/assistant/todos.md` "Delivery") states its delivery through the closeout: deployed `yes` is live, merged `yes` is merged and not live, merged `no` with a pull request is held at the PR, and a done closeout that says neither is "nothing to ship". A delivery line in the same reply wins.
- **Not done yet** (follow-ups of #3612): the snapshot still shows only the work line; no client draws the closeout or has the switch; a `not_done` closeout has no list state of its own; Codex has no held stop (a closeout it writes is read all the same); Agents learn the form only when a stop is held.

## Automatic actions

An Agent still linked to a completed to-do, with no open to-do linked to it, receives no automatic continuation,
wakeup or triage card. Triage can record its work state, but checks the current to-do links before acting; pending
wakes are checked again before sending. Released and dismissed links do not count. A later manual message is allowed;
to resume follow-ups while keeping the completed link, reopen the to-do or link the Agent to an open to-do (see
`docs/assistant/todos.md`, "Close Agents for completed tasks"). If the to-do state cannot be read, triage and wakes retry
later without sending.

### Transient errors: automatic continuation

- On by default and independent of the main switch (`transient_retry`, #3208): it is a rule, with no triage model call. With the main switch off, only
  turns that a transient error ended are followed (`workActive`), and the lists show only the continuation on its way (`transientPending`); everything else stays as without triage.
- Exponential backoff (`retryBackoff`): `retry_base_seconds` (default 15) doubled for each attempt, so 15 s, 30 s, 60 s, 120 s, 240 s; a rate limit (429, "rate limit",
  "too many requests") waits four times as long (60 s … 960 s); never more than 30 minutes. Up to 5 consecutive attempts (`agents.triage.max_retries`).
  A turn that ends otherwise (normally, or on an error that is not transient) starts the count again.
- If all fail: the session keeps its error and the apps' "Retry", as before. With the main switch and `decide` on, also a `blocked · user` card,
  "The model service keeps failing after 5 automatic retries: <original text>", with options "Try Again" / "Leave it for now".
- Send exactly `继续` ("Continue"), the same text sent by "Retry" on the App's error card (`continueText`).
- The manager logs each step: `work <machine>/<session>: auto-continue: transient model error, attempt <n>/<max> in <backoff>: <error>` when it schedules one,
  `work <machine>/<session>: auto-continue: sending "继续" after a transient model error (attempt <n>/<max>)` and `work <machine>/<session>: continued (<n>)` when it sends,
  and `work <machine>/<session>: auto-continue: transient model error persists after <n> automatic continues, left to the user: <error>` at the cap.

### Usage limits: continue at the reset

A subscription's usage limit gets no backoff "Continue" (each would be refused at once): Persona continues the work about a minute after the limit resets, unless the CLI goes on by itself ("Usage limits" below).

### Waiting: scheduled wakeups

- `external`: register `wake_at` using the model's `check_minutes` (2 minutes to 4 hours, default 10 minutes); double the interval for each further wait (up to 1 hour), with at most 6 wakeups (`max_wakes`).
  When the wait itself names CI and a GitHub PR is identifiable, Persona first polls its checks without another model turn. The active GitHub connector reads the PR head's check runs and commit statuses, including all pages, and checks the head again; the node's current-branch PR reader is the fallback when it can prove the result complete and the PR matches. Empty, incomplete, or still-running checks never count as complete; an empty set that persists until the original scheduled wake falls back to that wake. Rate limits defer polling, and source errors are retried with backoff; after three consecutive attempts where no source yields usable checks, the ordinary scheduled wake takes over. A valid pending check or explicit rate limit resets that error count and remains monitored. After all reported checks finish, Persona waits two minutes and verifies the same check set on the same head before waking the agent. This observation window reduces early wakes, but a provider can register checks later and no finite window proves it will not. Polling backs off to a capped interval and continues while checks are available, including after a long offline period; it falls back to the scheduled wake if neither source can read the PR; work machines and guest conversations use scheduled wakes to preserve their privacy boundary. The wake contains a localized result and bounded, escaped failing check names explicitly marked as untrusted data, without logs. The watch and its pending message are persisted, and the hub's once-only send guard avoids duplicate messages on retry; an uncertain delivery is not resent. A new turn, a user message or viewing that turn in an app cancels it, and the wake switch controls it.
  For other waits, or when no check source works, Persona sends a wakeup message (`prompts/agents/wake.md`): what the previous turn said it was waiting for, what to do afterward, and a request to check for itself. If ready, continue; if not, say how much longer.
  Triage the resulting turn as usual; register again if still waiting. After 6 wakeups without completion, show a card "Still waiting for 'X' after 6 wakeups", with options "Keep waiting" / "Stop waiting".
- `agent`: the model identifies the target among the working Agents shown to it (short id). Wake this Agent when the target's turn ends (or it goes offline), with `check_minutes` as a fallback.

A GitHub extension may opt into machine-readable polling with the read tool `github_ci_snapshot_v1`, called with `{"repo":"owner/repo","number":12}`. Its JSON response has `version: 1`, `host: "github.com"`, matching `repo` and `number`, `head` and `head_after` containing the same full commit hash, `complete: true`, and both `check_runs` and `statuses` arrays. Each check run has `id`, `name`, `status`, and `conclusion`; each commit status has `context` and `state`. The extension must read every page of both sources, then verify the head again; it must not include logs. Unknown versions or fields, missing arrays, duplicate items, mismatched targets or heads, and incomplete results are rejected. An extension offering only the existing human-readable `github_checks` does not provide this contract: Persona uses the node fallback or scheduled wake instead.

### User decisions: cards

- Persona creates a `question` interaction (`tool_name: persona_triage`, `summary` is `line`; the manager reports it in snapshot `interactions`, and the hub displays, pushes, and expires it accordingly), using the model's questions and options. The App, web, Android, Watch, Lark cards, Inbox's "Agents waiting for you", and main-chat task dock already render and answer them; no client changes needed.
  Push is time-sensitive, like other questions.
- Normal selections (or typing an "Other answer") reach the manager through `manager.action`, which formats them as a message (`prompts/agents/answer.md`: `关于你上面的问题：Q → A`, "Regarding your question above: Q → A"), and sends it to that Agent with `session.send`.
- Triage cards advertise `can_cancel:true`. Closing one commits its turn as deferred and clears its scheduled wakeup without sending an Agent message or interrupting the task. A model-generated single-choice option with `action:"defer"` has the same effect when selected; its label remains the answer stored in interaction history. Only options actually offered on the card carry this intent, not arbitrary free text. Legacy persisted pause labels have a narrow compatibility path.
- A deferred turn stays quiet after refresh, reconnect, and manager restart; a genuinely new turn can ask a new question. Repeated owner snapshots replay the original terminal outcome instead of registering the same question again. Persistence failures remain retryable rather than acknowledging a durable close.
- For authorization links (`link`), the auth relay goes first ([auth-relay.md](../operations/auth-relay.md)): when an extension's `auth_relay` contribution names the link and it was printed by the Agent's own command on one of your computers, the extension completes it with the account it is signed in with, the Agent is told to go on and no card goes up. Otherwise the card's first question contains the link and validity period (plus one line on why Persona didn't do it, when it tried), with options "Authorized, continue" / "Not now". Push as soon as triage finishes (15 seconds after the turn stops plus a second or two for the model), not after the 60-second "Completed" push delay.
- Remove a card (without sending anything) when the Agent resumes work (you replied directly on the computer or in the App), a new turn starts, the session disappears, or 24 hours pass without an answer.

## Repeated tool calls

The optional `tool_loop` triage switch is off by default and requires the main switch. It can be set as an initial value in `manager.json` under `agents.triage` and is exposed in the triage settings API. While an agent is working, its node reports a candidate containing only the tool name, repetition count and start time. The manager asks the Session Triage model at most three times per turn, with a five-second timeout; an error, timeout or unrecognized answer leaves the agent running. At 30 repetitions it asks the user without another model call. The node excludes recognized waits, terminal-output polls, log-tail reads, CI status checks and calls spaced 30 seconds or more apart; other polling forms may not be recognized. For shell commands, every component must be recognized as waiting or observation; commands with shell expansion or redirection remain eligible for triage. To bound memory, the node scans at most an 8 MiB transcript tail and skips lines larger than that limit; large calls or rapid writes can therefore be missed. The question offers **Interrupt** and **Let it run**; the latter silences the current candidate. The card, the silence and the model-check budget are saved separately from `session_work` and restored after a manager or hub restart. The first working session list after a manager restart, or a working list after an offline period, is an observation of the current turn and does not by itself clear its restored card or budget. A temporarily unavailable node or missing candidate does not by itself withdraw a restored card. Once this manager has seen the same candidate and its transcript revision, a larger revision with no candidate withdraws the old card. After reconnection, an unconfirmed missing candidate is retained; two subsequent revision increases with no candidate withdraw the card but keep the same-tool silence. The revision is the transcript's size, so unrelated transcript writes can also advance it; without an explicit node scan-completion signal this distinction remains an approximation. After reconnection or manager restart, a transcript tail rescan can change the candidate's start time. If a previously asked or silenced candidate comes back with the same tool and a different start time, Persona withdraws any old card and stays quiet until a new turn or different tool makes the change unambiguous. This can suppress a genuinely new same-tool loop in the same turn, because the summary does not contain enough information to distinguish it from a rescan. A new message, queued input or turn removes the card. A user's choice is saved before it is acknowledged; on a storage failure the card stays retryable. An interrupt is marked consumed before the existing `session.interrupt` call, so an uncertain delivery is not automatically repeated after a restart. If the request fails while the same candidate remains, the question reappears with a new id: a repeated delivery of the old answer cannot interrupt again, and the user can retry. The manager rechecks the candidate immediately before interrupting, but direct terminal input can still start another turn in the short interval before the runtime receives the interrupt. Older clients can update other switches without resetting this one.

## Usage limits

A subscription's 5-hour or weekly usage limit (#1710) is no failure to report and nothing for the user to fix: the turn waits for the limit to reset, then goes on.

- **Where the facts come from** (`proto.Session`: `turn_error_kind: "usage_limit"`, `turn_error_resets_at`, `turn_error_resumes`, `turn_error_after_work`): the agent's own record first (Claude Code's refused reply with `error: "rate_limit"` and `quotaLimits.resetsAt`; the reset of a Codex rollout's own rate-limit window), the words as the fallback for an older node or CLI (`transcript.UsageLimitText` / `UsageLimitReset`, completed on the session lists by `decorateLimit`): Claude Code's "You've hit your … limit · resets 4:20pm (Area/City)". A time without a time zone is never read (Codex's "try again at 3:05 PM" is some machine's local time), and a subagent's limit line (`isSidechain`) says nothing of the session.
- **Only a subscription's limit has a reset.** A gateway's or an API key's 429 (no `quotaLimits`, "Request rejected (429) · … quota reached") keeps its old handling: transient, "Continue" with backoff.
- **Triage** (a rule, before transient errors): with its reset known, `blocked · external` with `resets_at` and no `wait` (the error is the session's `turn_error`; an external wait with nothing named is one that ends by itself, as every client's list reads it); the line says when: "continues by itself at 16:20" when the CLI said so, else "resets at 16:20" (the user's time zone; with month and day, `01-02 16:20`, when the reset is on another day). Without a known reset: a card, "The subscription's usage limit is used up, and it didn't say when it resets: …", with options "Try again" / "Leave it". Never `transient`.
- **The CLI goes on by itself** (Claude Code's "Usage limit reached · continuing automatically at …"): no wake and no card, also when its reset is not known (the line then says it goes on after the reset). Typing into the session would cancel its own continuation. Claude Code writes the notice once, at the first refused turn; the node keeps its word while nothing in the transcript's tail took it back (a message typed, a reply, its "Automatic continue …" notices), however far the wait's request lines push the notice out of the tail it reads.
- **Otherwise Persona continues it** about a minute after the reset, spread over 30 seconds so that the agents of one account do not all start in the same second (`limitContinueAt`), with `prompts/agents/limit_continue.md`; event `limit_continued`. It is the `auto_continue` switch's, not `wake`'s: off (or turned off before the continuation went), the line stays and nothing is sent; the lists then name the line as the wait's `wait`, so they keep the session for the user instead of showing a wait that ends by itself. The apps show the continuation as one line ("Usage limit reset: Persona told it to go on", `injected` state `limit_continue`). None of a wait's other checks apply (risky next step, `check_minutes`): it goes on with what the limit stopped. Right before sending, the session is read again: when it now says it goes on by itself, nothing is sent; when its limit now resets later, the continuation moves to then.
- **At most 3 continuations without a productive turn** (`maxLimitContinues`): a reset read too early is refused again at once. Then a card, "It was continued automatically 3 times after the usage limit reset, and hit the limit again each time", with options "Try again" / "Leave it"; the card keeps `resets_at`, so a turn refused again while it is up (a background task's report woke it) is the user's too. A turn that did some work before the limit refused it again (`turn_error_after_work`), or a later reset (the next window: the earlier reset was read right), starts the count again; an ordinary external wait never takes over a usage limit's count, nor the other way round.
- **The user's actions cancel it** as any automatic action ("Avoid conflicting with manual user actions"): a message, looking at it in herdr, queued input, a pending card.
- **No misleading notices while it waits** (`limitTakenUp`: the reset is known and at most 10 minutes past, and the CLI said it goes on by itself, or Persona's continuation or its card is set): no "Completed" or "Interrupted" push for the turn. The turn stays followed, so the end after the reset is the one pushed, timed from the turn's start. A task dispatched from the main chat gets no receipt meanwhile; it is looked at again when the wait is over, so a turn that did not go on after all (its continuation cancelled) still gets its "Interrupted" receipt. A limit nothing takes up (the switches off and a CLI that does not go on by itself, or no reset known) is told as the turn ends, as a usage limit ("Usage limit reached; continue it once the limit resets: …"), never as "the model service failed". For the main assistant, a task waiting for the reset reads as `usage_limit` in its snapshot, not `interrupted`. A sign-in that is gone ("Please run /login · …", "Login expired · …") is no usage limit: it reads as interrupted (`turn_error_code` `auth_failed` / `not_logged_in`), never as "Completed".
- **Scheduled tasks** (`watchScheduledRun`): a run whose conversation stopped on a usage limit with a known reset is retried a minute or so after the reset instead of after 3 minutes; that wait is not cut by the watcher's 30 minutes, and `retryStillDue` decides at that time as before (a retry that fails again is not retried); a conversation that went on meanwhile (its turn is no longer the one the limit stopped) is not retried, so what the run does is not done twice. A conversation that goes on by itself is not retried. The failure push is unchanged.
- **Known limits**: the waits are kept in `session_work` like any wakeup and come back after a restart, but only for turns that ended within the last two days (a weekly limit can be longer); a scheduled run's retry waits in memory and is lost when the manager restarts.

## Safety boundary

Persona **sends to Agents on its own** only four kinds of message, none introducing new intent:

1. Continuation after a transient error: like "Retry" on the App's error card, it resumes the interrupted task; likewise the continuation once a subscription's usage limit reset (`prompts/agents/limit_continue.md`: go on with what you were doing, without redoing what is done);
2. A wakeup after a registered wait: asks only whether the awaited event is ready, and explicitly says `接下来要推代码、部署、给别人发消息、合并或改别人的 MR 时先停下来问用户，不要自己做` ("Before pushing code, deploying, messaging others, or merging / modifying someone else's MR, stop and ask the user; do not do it yourself");
3. The answer the user selected on a card;
4. After the auth relay completed the authorization link it stopped at ([auth-relay.md](../operations/auth-relay.md)): go on with what it was doing.

Any next step involving **pushing code, deploying / releasing, messaging others, or touching someone else's MR / PR** must not receive an unapproved automatic wake:

- The prompt marks such waits with `wait.risky`; without an approval the hub registers no wakeup and does not invent a question for the user. A card still requires an actual unanswered request in the Agent's reply; missing request evidence is left uncertain, not labeled done.
- Code fallback: if `wait.then` / `line` contains push, deploy, `部署` (deploy), `发布` (release), `上线` (go live), merge, `合并` (merge), `发消息` (send a message), `回复` (reply), `评论` (comment), testflight, release, `发送` (send), `删除` (delete), `购买` / `付费` / `续费` / `充值` (spend), or `生产` / production, do not register a wakeup unless the same wait and step are approved;
- An approval comes from the user's "Do it once it's ready" on a card the Agent asked, or from a task that authorizes the step (#3246). The wake then goes out with the approval, and no second card is opened;
- When the user chooses "Do it" on the card, the Agent receives the user's answer, which counts as user authorization.

**A step its task already asked for is not asked again** (#3246, `agents_work_authorized.go`): before the card goes up, the hub reads the title and notes of the Agent's open to-do (supervised or not). When the to-do authorizes the step, the hub treats it as the user's "Do it once it's ready": no card and no push, a normal wakeup once the wait is due, the line `等「X」，任务已授权，好了就去「Y」`, and the work event `auto_approved` (to-do id, whether supervised, wait, step) as the trace. The wake tells the Agent that its task authorized the step. A new wait (the next release node) is checked the same way, so it is not asked again either. The to-do authorizes the step only when every risky word in it passes:

- Never: sending or replying to people (`发消息`, `回复`, `评论`, `发送`, send, comment), deleting (`删除`, delete), spending (`购买`, `付费`, `续费`, `充值`), changing production (`生产`, production). These remain the user's whatever the to-do says.
- Pushing its own branch (push, `推送`, `推到`): allowed for any to-do.
- Merging, releasing, deploying (`合入`, `发布`, `上线`, `部署`, merge, release…): only when the to-do text contains the same word and no negation (`不要`, `别`, `禁止`, don't…) precedes it in the same clause.

With no open to-do, or with a step its to-do does not authorize, the hub neither wakes it nor opens a card of its own: a card only carries a question the Agent actually wrote (#3290). With the wake switch off nothing is woken either, and a card the Agent wrote still goes up. With more than one question or an authorization link, the task does not auto-approve the step.

## Avoid conflicting with manual user actions

**Immediately before** each automatic action (continue, wake, create a card), reread the session and proceed only if all conditions hold:

- It is still the triaged turn (`updated_at` unchanged), stopped (not working), with no pending interaction or queued input (`queued`);
- The host is `done`, not `idle`: if you focused this tab in herdr (watching it at the computer), leave it alone and only record work state. Hosts that keep reporting `idle` after stopping therefore get no automatic actions (better not to interfere);
- The computer is online;
- It is not linked to a completed to-do without an open to-do as well (released and dismissed links do not count).

Sending a message yourself or typing on the computer starts the session working: all retries, wakeups, and cards for that turn become invalid. Triage again after the new turn ends.
The same applies to "Retry" on an App error card (work begins, invalidating queued automatic retries). Persona records continuations and wakeups in `session_work`, sending each only once per turn.

## Switches (requested 2026-11-05: all off by default)

Triage calls a model after every stopped turn; continuation and wakeups also make the Agent run another turn on its own model. All consume the user's tokens. Therefore **everything defaults to off**,
except two rules that need no triage model: `reply_reminder` and, since #3208, `transient_retry` (continuing a turn a transient error cut off only finishes what the user already asked for).
Users enable them under **Settings › Session Triage**. The switches are stored in settings `agents.triage` (read and written only by the manager); tool-loop detection is configured on the web:

| Switch | Controls | Whose tokens it uses |
| --- | --- | --- |
| `enabled` Session Triage | Main switch: triage after a turn stops, show what it is waiting for, and stop counting completed work as Unread | Triage model (**Settings › Model Settings › Session Triage**, `agent.triage`, defaults to the Inbox triage model) |
| `decide` Create decision cards | Turn user decisions into "Needs you" cards | No extra tokens |
| `auto_continue` Continue automatically on errors | Automatically send continuation once a subscription's usage limit resets | Agent's model (resumes the interrupted turn) |
| `transient_retry` Continue after transient errors (#3208) | Automatically send continuation after transient model API errors ("Transient errors: automatic continuation") | Agent's model. **On by default**, independent of the main switch |
| `wake` Scheduled wakeups | Wake Agents waiting for deployment / CI / another Agent so they check for themselves | Agent's model (one turn per wakeup) |
| `tool_loop` Detect tool loops | Offer to interrupt an agent repeating tool calls, excluding polling | Triage model (at most three checks per turn) |
| `closeout` Ask for a closeout | Hold an Agent's stop once when a turn that did work ends without a closeout ("Closeout") | Agent's model (one more turn when the closeout is missing) |
| `reply_reminder` Remind when seen, unanswered (#5096) | One push when an Agent asking the user has waited `reply_reminder_minutes` (default 30) since they looked ([agent-states.md](../protocol/agent-states.md) "Awaiting your reply") | None: rules only. **On by default**, independent of the main switch |

- Subswitches work only when the main switch is on (but `reply_reminder` and `transient_retry`). Disabling a switch takes effect immediately: Persona-created cards are removed, and queued retries / wakeups are not sent.
- `GET / PUT /api/v1/agents/triage` (`{enabled, auto_continue, wake, decide, tool_loop, closeout, reply_reminder, reply_reminder_minutes, transient_retry}`, forwarded by the hub to the manager); changes broadcast `{"type":"agents.triage"}`. A PUT without `tool_loop`, `closeout`, `reply_reminder`, `reply_reminder_minutes` (or with minutes 0) or `transient_retry` keeps their values for older apps; negative minutes go back to the default 30. While the saved switches cannot be read, such a PUT saves their defaults.
- A `GET` whose first read of the database fails answers `503` with an `error` in the request's language (Chinese, English or Japanese) saying why, instead of returning the defaults as if they were the saved switches (#4553). Failures are not cached: the next request reads again. Switches once read or saved stay cached in memory; `manager.json`'s initial values apply only when the database really has no setting. Triage itself still falls back to the defaults when its own read fails, and `PUT` saves as before.
- `agents.triage` in the manager's `manager.json` (`docs/operations/config.md` ("manager.json"); in hub.json before 2026-11-06) now retains limits (`max_retries` default 5, `retry_base_seconds` default 15, `max_wakes` default 6, `settle_seconds` default 15);
  its triage switches are initial values only when settings have never been saved in the App (omitted means off). The manager reads this section itself and lays it over the hub's handshake reply (`assistantwire.Hello.Agents`); the hub no longer reads or sends it.

## Storage

A process can be redeployed and cleared at any time, so triage results, retry counts, wakeup times, and cards are persisted (`0331_session_work.sql`, read and written only by the manager):

- `session_work`: one row per session, with the latest turn's work state (`work` JSON), turn-end time `turn_at`, next action `wake_at`, retry / wakeup counts, and card id.
  On startup the manager restores recent turns and, beyond the usual 48-hour window, CI watches with a pending wake, including prepared messages, and recently retired turns. The 30-day cleanup retains active CI watches until they finish or are cancelled, and retains recent retirement markers even when their original turn is older. A todo that ends while its agent's turn is settling records its retirement and, when available, that stopped turn's node timestamp, preventing it from registering a new wake without comparing the node's clock to the manager's. Before scheduling automatic follow-ups or sending a wake, the manager also checks the current todo links, so a failed retirement-state write cannot revive follow-ups for a still-linked completed todo unless there is an open todo too. If todo state cannot be read during triage, the turn stays untriaged and the usual backfill retries it after ten minutes while it remains unread and within the 24-hour catch-up window; a later read failure for an existing watch postpones its next attempt. A later turn-start event or a newer node turn verified against the retired turn can clear the retirement marker, but automatic follow-ups still require that no completed todo blocks them. Reopening the todo or linking the Agent to an open todo allows new follow-ups, including after a restart. A persisted CI proof from an older manager never overrides a currently completed todo. If the retired turn's identity and a new turn-start event are both unavailable, the manager conservatively waits for another start. Continue due actions and restore unanswered cards only if still the same stopped turn, with the same id; wait for the computer to connect before restoring cards.
- `session_work_events`: one row per triage, automatic continuation, wakeup, card creation, card answer, and wait completion (30 days), for evaluation below.

## What Persona knows

The main chat's live snapshot (`prompts/assistant/snapshot*.md`):

- Each Agent under "Needs you" includes what it is waiting for;
- Adds "Waiting" (Persona is waiting for it: retrying, deployment, another Agent) and "Done (within 24 hours)": name + conclusion;
- "Unread" contains only sessions not yet successfully triaged.

For tasks dispatched from the main chat (`internal/assistant/tasks_follow.go`), if the Agent stops on a transient error and Persona will still continue automatically (`work.on = transient` with `wake_at`), do not yet write an "Interrupted" receipt. Wait until retries are exhausted. The same holds while an Agent waits for a usage limit to reset ("Usage limits"); the snapshot shows its task as waiting for the reset, not as interrupted.

## Protocol

`proto.Session` adds `work` (`docs/protocol/protocol.md`, "Data types"):

```json
"work": {"phase":"blocked","on":"external","line":"Wait for deployment, then run production tests",
         "wait":"Deployment completes","then":"Run production tests","wake_at":"…","attempts":1,"max":6,
         "agent":{"machine_id":"…","session_id":"…","label":"…"},
         "link":{"url":"…","expires_at":"…"},
         "artifacts":[{"kind":"pr","ref":"https://github.com/…/pull/1"}],
         "closeout":{"status":"done|not_done|blocked","waiting":"…","did":["…"],"merged":"yes|no","deployed":"yes|no",
                     "verified":"…","missing":"…","checked":{"merged":"yes|no","deployed":"yes|no"}},
         "turn_at":"…","at":"…","by":"rule|model"}
```

- While working, only `{"phase":"working"}`; pending interactions use `{"phase":"blocked","on":"user","line":<question>}`; sessions not triaged omit `work`.
- `closeout`: what the Agent said of its own work as the turn ended ("Closeout"); `merged` and `deployed` are absent when they do not apply or were not said. Session lists omit `did` and `verified`.
- `resets_at`: the turn stopped on a subscription's usage limit that resets then ("Usage limits"): on `external` the wait for it, on the card after the automatic continuations when it resets.
- `GET /api/v1/agents/work/stats?hours=24`: evaluation metrics (below).

## Evaluation

`GET /api/v1/agents/work/stats` (forwarded by the hub to the manager) and manager logs (prefixed `assistant:` in hub logs, `triage <machine>/<session>: done (model 1.2s): …`):

- Distribution of post-turn triage results (done / blocked types / unsure / failure), split by rule and model;
- "Unread" count: every 5 minutes the manager samples the number of non-assistant sessions with `state = unread` (`sample` in `session_work_events`), reporting average, maximum, and current count for the interval;
- Wait duration: median / p90 from turn end to user action (viewed, replied, answered a card), split by "Done", "Waiting for your decision", and "Not triaged";
- Automatic continuation: number sent and proportion followed by normal completion; wakeups: count and proportion completed after waking;
- Cards: created, answered, wait before answer, and removed (because you replied directly).

Pre-launch baseline (evening of 2026-11-04, non-assistant sessions on the hub): 5 unread (4 within 24 hours), with a median wait already around 10 minutes.

## Clients

- Web (#1837): the list preview (`sessionPreview`: overview, Machines page, knowledge-base details, sidebar hover tooltip) switches to work state when the session is stopped and the hub has triaged it:
  "✓ Conclusion" for completed work, "What it is waiting for (wake in N minutes / retrying now)" for waits. Sessions the hub will wait for go into "Waiting" (`hubWaits`, `web/src/lib/work.ts`).
  The switches are under **Settings › General › Session triage**, with a footer link to **Model settings › Session triage** to choose the model. Screenshots use a fake hub with `MOCK_WORK=1`.
  A subscription usage limit (`turn_error_kind` `usage_limit`) is not shown as Interrupted: task cards, agent rows and the chat say "Usage limit · resumes <time>" when the CLI or the hub will continue it, and "Usage limit · resets <time>" when nothing will (the user's time zone; the date as well when that is another day). An older hub that does not send the fields keeps the interrupted text. Screenshots use `MOCK_USAGE_LIMIT=1`.
- Android (#1837): the Agents list and knowledge-base "Agents working on this project" rows use the same preview (`Session.workLine`, `WorkLine.kt` in `core:domain`). Sessions the hub waits for go into "Waiting";
  **Settings › AI › Session triage** has the main switch, triage model (link to model settings), and three action switches.
- iOS / Mac (#2895): Agents-list and space-page card previews (`Session.listPreview`) switch to the same line when the session is stopped and the hub has triaged it (`Session.workLine`, `apple/Shared/Models.swift`).
  Host-provided reasons for needing the user (botmux) and failure notices still take precedence. Sessions the hub waits for go into "Waiting", not collapsed into idle (`WorkState.hubWaits`, `Session.isWaiting`). Unit tests: `SessionWorkTests`.
  **Settings › Session Triage** (below the assistant, `TriageSettingsView` / `TriageSettingsModel`) has the main switch, triage model (opens `agent.triage` in Model Settings), and three action switches (disabled when the main switch is off). Failed changes revert to the old value with an explanation; old hubs (404) report unsupported. Unit tests: `TriageSettingsTests`; DEBUG `-tab settings -openTriageSettings YES`.
- Watch: list rows do not show last words; work previews are not displayed yet.

## Steps

| Step | Scope |
| --- | --- |
| 1 | This document; hub (moved to the manager since 2026-11-04, see "Responsibilities"): `session_work` table, triage (rules + model), automatic continuation, wakeups, cards, `state` algorithm, `work` field, statistics endpoint; snapshot; task interruption receipts |
| 2 | Move switches into App settings, all off by default; web displays `work.line`, Waiting, and switches (#1837) |
| 3 | Android (#1837); iOS / Mac work previews (#2895) and settings page (#3078) |

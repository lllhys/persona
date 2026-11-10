# Meeting minutes (offline meetings)

An in-person meeting or hallway discussion becomes a recording, a transcript, and structured minutes. This is not a new recording product: it is the recordings already in the knowledge base (#3235, [recording.md](../apple/recording.md), [knowledge.md](./knowledge.md) "Recordings"), with a minutes document instead of a short summary when the recording is a meeting.

The iPhone and Apple Watch entry points, the Live Activity, and showing minutes inside the app are #2694. This document is the contract they follow. Mac recording is #2704 and uses the same fields.

## Why the recordings we already have don't produce minutes

The knowledge base currently holds no recordings at all, so nothing is being captured. In order:

1. **The entry is too deep to remember.** Recording starts at Knowledge › Recordings, the last kind capsule, then a language choice, then start. Nobody does that while walking into a room. A one-tap entry is the highest priority, and it belongs to #2694 (an in-app entry, an App Intent, a Control Center control, a Live Activity).
2. **The summary is not minutes.** `prompts/kb/recording.md` produces at most 600 Chinese characters under "what this is / key points / to-dos and open questions". It has no decisions, no "who does what by when", no place, and its points are not pinned to transcript timestamps. A transcript over 24k characters keeps only its head and tail, so the middle of a long meeting is dropped.
3. **The calendar is ignored.** A Lark meeting or event in the same time window already has a title (`meetings`, `events`), and a recording never reads it. (Those items carry only a title and a start time: no end, place or attendees.)
4. **A failed sync looks like a connection problem.** With Knowledge off or read-only, recording sync says it can't reach the hub and retries forever (#1915). The user thinks it was captured.

## End to end

1. The user starts a **meeting recording** (#2694: one tap, Siri, the Action button, or Control Center). A recording started the old way, from Knowledge › Recordings, stays an ordinary recording.
2. The phone (or the watch, relayed through the phone) records in segments and transcribes **on device**, exactly as today. Audio never leaves the device.
3. The phone uploads the transcript so far with `PUT /api/v1/recordings/{id}`, now also saying whether this is a meeting and, if it knows, which calendar event it is. `complete: false` until recording and transcription are both finished.
4. Once `complete: true` and the recording is a meeting, the assistant generates **minutes** with the model it already uses for knowledge-base compression. An ordinary recording still gets the short summary it gets today.
5. Minutes are stored inside the recording's directory. Deleting the recording deletes them. When they are ready, the user gets one notification.
6. Action items are **suggested to-dos**. Nothing is created until the user confirms.
7. The web Knowledge › Recordings detail shows the minutes; the iOS detail does the same (#2694). Asking the assistant "what were the minutes of the meeting just now" resolves through `kb_get` / `kb_search`.

## Who does what

| | iOS / watch (#2694) | Hub (the assistant) |
| --- | --- | --- |
| Capture and transcription | On device, segmented, as today | Never sees audio |
| Marking it a meeting | Sends `kind` and, when it has one, the calendar hint | Trusts the phone; also treats a recording as a meeting when it overlaps a calendar item in the knowledge base, or when the user asks for minutes |
| Calendar | Reads the on-device calendar for the current event's title, attendees and place | Reads `meetings` / `events` already in the knowledge base for the title (they have no attendees) |
| Minutes | Displays them; does not generate them | Generates, stores, regenerates on request |
| To-dos | Offers the suggested ones for confirmation | Creates a to-do only for the action items the user confirmed |

## Field contract

Added to [protocol.md](../protocol/protocol.md) ("Recording sync"). Everything else about an upload is unchanged, and a client that sends none of the new fields behaves exactly as before.

On `PUT /api/v1/recordings/{id}`:

```jsonc
{
  "kind": "meeting",            // optional: "meeting" or "note" (default "note")
  "calendar": {                 // optional, only when the device matched an event
    "title": "Design review",
    "attendees": ["Ada", "Bo"],
    "location": "Room 3"
  }
}
```

- `kind` is `meeting` when the user explicitly started a meeting recording, or when the device matched an event happening now. Anything else, including omitted, is `note`.
- `calendar` is whatever the device already knows. It is a hint, not the source of truth: the hub prefers a `meetings` or `events` item in the knowledge base whose time overlaps the recording. The device does not send a calendar event id, because the hub cannot see the device's calendar. Other keys in `calendar` are ignored; a title or location over 200 characters, or more than 100 attendees or one over 100 characters, is `400`.
- The phone keeps owning the transcript. These fields travel with the full-state upload, so a later upload replaces them like everything else.

Stored on the recording, and returned by `GET /api/v1/kb/recordings/{id}` inside `detail.meta` (alongside the fields already there):

| Field | Meaning |
| --- | --- |
| `kind` | `meeting` or `note` |
| `calendar` | The hint above, when one was sent |
| `meeting` | The matched knowledge-base item, when one was found: `{kind, id, name}` (`kind` is `meetings` or `events`) |
| `minutes_state` | `pending` (a meeting whose minutes for the current revision are not written yet: waiting for the transcript, the model, or regenerating), `ready`, `failed`, or absent when the recording is not a meeting (minutes it got while it was one stay in `detail.minutes`) |
| `minutes_asked` | `true` once the user asked for minutes (`POST …/minutes`), which makes the recording a meeting whatever its `kind` |

`GET /api/v1/recordings` (reconciliation) carries the same `minutes_state` on each recording when it is set, so the phone knows when to fetch a detail.

The minutes themselves are `detail.minutes`, present once minutes have been written. While newer ones are `pending` or `failed` (a re-transcription, a regenerate), the previous minutes stay; their `revision` is then below `meta.revision`:

```jsonc
{
  "version": 2,                      // increases each time the minutes are rewritten; sent back when confirming to-dos
  "revision": 7,                     // the recording revision they were made from
  "generated_at": "2026-11-10T07:12:03Z",
  "title": "Design review",
  "when": "2026-11-10 14:00–15:10",  // in the user's time zone
  "where": "Room 3",                 // omitted when nobody said and no calendar has it
  "attendees": ["Ada", "Bo"],        // omitted when not known; never invented
  "points": [
    {"text": "The export path drops entries over 4 MB.", "at": 312.0, "who": "Ada"}
  ],
  "decisions": [
    {"text": "Ship the cap as-is for this release.", "at": 1504.5}
  ],
  "actions": [
    {"id": "a1", "who": "Bo", "what": "Add the regression test.", "by": "Friday", "due_date": "2026-11-13", "at": 2100.0, "todo_id": ""}
  ],
  "open": [
    {"text": "Whether the cap should be per entry or per recording.", "at": 2408.0}
  ],
  "partial": false,                  // true when the transcript was too long and its end was not read; omitted when false
  "markdown": "# Design review\n…"    // the same minutes as Markdown with clock times: what Copy / Share send, and minutes.md on disk
}
```

- `at` is seconds from the start of the recording, the same clock as `segments[].start`, so a client can jump straight to that part of the transcript. Every point, decision, action and open question carries one.
- `who` is omitted unless the transcript or the calendar actually supports it. There is no speaker diarization, so a name appears only when someone was addressed by name or is the only attendee who could have said it. Otherwise the field is absent, not guessed.
- `by` is the deadline as spoken ("Friday", "before the 20th"), not a parsed date. Absent when none was said. `due_date` (`YYYY-MM-DD`) is added only when the spoken deadline resolves to one date unambiguously, counting from the meeting day.
- `id` names an action within one `version` (`a1`, `a2`, …). `todo_id` is set once the user confirmed it as a to-do.
- Sections with nothing in them are omitted, not filled with "none".
- `points`, `decisions`, `actions` and `open` keep the transcript's order.

Reading it back:

- `GET /api/v1/kb/recordings/{id}` returns `detail.minutes` and the meta fields above. `detail.summary` stays the short summary, for a meeting as well, so existing clients keep working.
- `kb_get` on a recording prints the minutes (`minutes.md`: the same items, each at its clock time) before the transcript, and `kb_search` matches the minutes text as well as the transcript, so "the minutes of the meeting just now" resolves without a new tool.
- Past meetings (#2981, phase 0) resolve through the same tools. "What did last week's design review decide", "what's still open from the X meeting" and "who owns the export to-do" each need three things, and the tools give them:
  - **Finding the meeting.** `kb_search` takes an optional `kind`; `kind=recordings` reads only the recordings' files (`meta.json` with the user's title, `calendar.title` and the matched `meeting.name`, `minutes.md`, `summary.md`, `transcript.md`), so a broad query can no longer fill its limit with chats first. Within a kind, entries whose name matches come first, then as `kb_list` ranks them (a recording's id is random, so the walk alone would not put last week's first). `kb_list` shows the start time of `meetings` and `events` items as it does for recordings, in the user's time zone, so "last week's" can be told apart.
  - **From the calendar item to its minutes.** `kb_get` on a `meetings` or `events` item lists the recordings matched to it, each with its start, length and minutes state.
  - **Where each action item stands.** After `minutes.md`, `kb_get` prints every action item with `who`, `what` and the deadline as said, and its state now: a suggestion that is not a to-do yet, or its to-do open (with the to-do's due date), done, cancelled or deleted. The to-do is the one `todo_id` names, or the one a confirmation made under the action's deterministic id when writing `todo_id` back failed. The order stays minutes, action items, then the transcript, which the 12k cut may end early.

Regenerating: `POST /api/v1/recordings/{id}/minutes` asks for a fresh generation. It also works on a recording that is not a meeting (an older app, the watch, a forgotten mark), which becomes one (`minutes_asked`). `202 {"state":"pending"}` when it is queued, `409` when the transcript is not `complete`, `404` when there is no such recording. It does not change the transcript or the revision.

Confirming suggested to-dos: `POST /api/v1/recordings/{id}/minutes/todos` with `{"version": 2, "actions": ["a1", "a3"]}` creates one Persona to-do per listed action and writes its `todo_id` back. The title is `what`, prefixed with `who: ` when someone was named; the due date is `due_date` when set; the notes name the meeting, its date and the item's clock time. It is an ordinary to-do (no task, no Agent).
`200 {"todos": [Todo], "minutes": Minutes}`. An action that already has a `todo_id` is not created again, and a retry after a lost response creates nothing new. `409 minutes_stale` when `version` is not the current one (refetch and ask again); `404` no such recording, no minutes, or an unknown action; `400` an empty `actions`; `403` from a device that cannot write to-dos.

## What the minutes must contain

The theme (the title), when and where, attendees and speakers only when known, the points discussed, the decisions, the action items (who, what, by when), and the open questions. Each of those carries the transcript timestamp it came from. Nothing the transcript and the calendar don't support is added: a misrecognized word is read in context, but an unspoken decision is not invented.

## How the hub generates them

- For a meeting: `kind: meeting`, a calendar item in the knowledge base overlapping a recording of at least 5 minutes, or `minutes_asked`. Only once `complete: true` and the current revision has no minutes yet. The same round that summarizes ordinary recordings does it, at most a few per pass, with the model configuration the assistant already uses (`kb.compress`). No new service. An upload that completes a recording wakes that round, so minutes start within seconds.
- A long transcript is handled in segments: consecutive chunks in time order, each chunk reduced to candidate points, decisions, actions and questions with their timestamps, then one merge pass that deduplicates and orders them. The middle of a meeting is no longer dropped on the floor. Chunks are sized to fit the model's context with room for the instructions.
- When a `meetings` or `events` item overlaps the recording's time window (it starts at most 15 minutes before the recording and before its end; the closest start wins, events before meetings), it becomes `meeting` and its name names the recording unless the user typed a title or the phone sent `calendar.title`. Attendees and place come only from the phone's `calendar` hint or from the transcript.
- Every item must cite a line the model actually read; items whose `at` matches no transcript line are dropped, and so is a `due_date` that is not a real date from the meeting day to a year after it.
- The minutes are written next to the recording (`minutes.json`, and `minutes.md` for search, in its directory) and removed by the same delete and the same retention sweep that remove the recording.
- On success the user gets a single push naming the meeting, with the number of decisions and suggested to-dos; it has a stable id per recording and `version`, so a restart does not repeat it. A tap opens Persona's main chat, as reminders do; the Lark bot and channels with pushes on get the same text with the recording's web link. Opening the recording straight from a push needs a new push field and is later work.
- No model configured, or the model paused: the minutes stay `pending` and the next round tries again. An answer that cannot be used (not JSON, refused, nothing citable) counts as a failure; after three for one revision they are `failed` until a new revision or a regenerate. Failures do not notify.
- A slow model is not a failure (#3170). A minutes answer is several times longer than a summary, so a call gets 6 minutes where a summary call gets 3. A try that runs out of its time (one call, or the 45 minutes of all the calls of one recording) is counted apart from unusable answers: the recording waits 2, then 4, then 8 minutes before its next try, the other recordings going first meanwhile, and each try gets 6 more minutes per call than the one before (6, 12, 18, 24). After four such tries for one revision the minutes are `failed`, so a recording no model ever finishes cannot hold the queue for ever.
- Each action item is offered as a **suggested to-do** the user confirms (`POST …/minutes/todos` above). An unconfirmed suggestion stays only a suggestion. None are created automatically.

## Privacy

Unchanged by default. Transcription stays on device, only text is uploaded, audio never leaves the phone, and recordings still expire after `recording_days` (default 14).

Attending a meeting (below, off by default) also puts the meeting's lines into the chosen session's conversation. Those copies stay in that conversation and do not expire with `recording_days`.

### Needs a decision: cloud transcription and speaker diarization

On-device transcription of a Chinese meeting is good enough for a single speaker and degrades with crosstalk, distance and accents, and it cannot say who spoke. Two options, **neither implemented**:

| | On device (today) | Cloud or hub transcription, with diarization |
| --- | --- | --- |
| Privacy | Audio never leaves the phone | Audio, or a high-quality recording of it, leaves the phone for a third party or the hub machine |
| Chinese meeting quality | Usable for one speaker nearby; poor across a table | Meaningfully better, and can label speakers |
| Cost and dependency | None | A new service, or a model the hub does not run today, plus the bandwidth of an hour of audio |
| Failure | Already handled: no text, no upload | A new failure mode for every meeting |

Until that is decided, minutes name a speaker only when the words themselves or the calendar make it plain.

## Attending a meeting (#2981)

Persona can attend a recorded meeting while it happens, not only write minutes after it. The owner decided four things in #2981:

1. **The role is chosen each time a recording starts**: `off`, `listener` or `participant`. A global default (`meeting_attendance_default`, below) pre-selects it.
2. **One session attends, chosen per recording**: the main chat, another Persona chat or an Agent. It is not fixed to the main chat.
3. **In front of others Persona says only meeting material**: earlier minutes, project progress and who owns which to-do. Never private chats, mail, health or other personal content. The hub enforces this by what it gives the answering model ("What the hub gives the answering model" below), not by the prompt.
4. **Tap to ask.** No wake phrase.

| Role | During the meeting | Answers |
| --- | --- | --- |
| `off` (default) | Nothing new: the recording is uploaded as before and gets minutes afterwards | None |
| `listener` | The phone streams finished sentences to the hub, which feeds them to the attending session; the session keeps running notes | On the phone's screen only |
| `participant` | The same as listener | On screen, and the phone says them aloud |

### Attending, step by step

1. The user starts a meeting recording. If the hub lists the `meeting_attendance` feature, the phone offers the role (pre-selected from `meeting_attendance_default`) and the attending session.
2. With a role other than `off`, the phone uploads the recording at once, before it has any text (`PUT /api/v1/recordings/{id}` with `attendance`, `attending_session`, `segments: []`, `complete: false`), so the hub knows the role. Afterwards it uploads exactly as before; every full-state upload carries the same two fields.
3. As sentences are recognized, the phone appends them with `POST /api/v1/recordings/{id}/live`. They are append-only and resume after a reconnect (`next`).
4. The hub feeds the new lines to the attending session in batches. The session keeps running notes in its own conversation, where the user can open them.
5. The user taps to ask (`POST /api/v1/recordings/{id}/ask`), with a typed question or, from the Live Activity, with none: then the question is what was just said in the room. The hub answers from meeting material only. The phone shows the answer and, for a participant, says it aloud.
6. The recording ends and completes as before, and gets minutes as before. A meeting that completes with no text at all gets no minutes (`minutes_state` absent).

### Attendance fields

All of it is optional and additive. A phone that sends none of it, and a hub without the `meeting_attendance` feature, behave exactly as before. Characters are counted as Unicode code points (Go runes, Swift `unicodeScalars.count`).

**The feature flag.** The hub lists `meeting_attendance` in WS `hello.features` and trusted `/health` only while the default manager (the one recording routes go to) is connected and names the capability `meeting_attendance` in its `manager.hello`, as with `agent_session_archives` (protocol.md "Health check and feature flags"); an extension's report cannot add it. It means that manager takes everything in this section: an older hub or manager accepts the upload fields silently and does nothing with them. The flag only decides whether the phone offers a role when a recording starts, from the last `hello` it saw. A recording that attends keeps its role whatever later `hello`s say; the phone stops attending it only on a `404` without `recording_not_found` from `…/live` or `…/ask`, or an upload `200` with no `attendance` (an older manager took it). A `503` is retried with backoff.

**On `PUT /api/v1/recordings/{id}`:**

```jsonc
{
  "kind": "meeting",
  "attendance": "participant",       // optional: "off" (default), "listener" or "participant"
  "attending_session": {"machine_id": "<machine id>", "session_id": "<session id>"}  // optional: absent = the main chat
}
```

- `attending_session` names one session the way the rest of the API does. Absent, `null`, or both ids empty means the main chat, as it is at each batch. Each id is at most 200 characters; other keys are ignored.
- **A problem with these two fields never fails the upload**, since the same request carries the transcript. The transcript is stored as usual and the attendance is stored as `off` when `attendance` is not one of the three values (`attendance_problem: "unknown_role"`), when it is not `off` on a recording whose `kind` is not `meeting` (`"not_meeting"`), or when `attending_session` has exactly one id or one over 200 characters (`"bad_session"`). The `200` response then adds `"attendance"` (the role in effect) and `"attendance_problem"`. On a hub with the feature, every `200` carries `attendance`.
- Like `kind` and `calendar`, both fields travel with the full-state upload: a later upload that omits `attendance` turns attending off, and one that names another session moves the meeting to it.
- `GET /api/v1/kb/recordings/{id}` returns them in `detail.meta`: `attendance` (`listener` or `participant`; absent when off) and `attending_session` (as stored; absent for the main chat).
- Watch recordings, also those the phone tags as meetings, and notes never carry a role.
- A meeting that completes with no text gets no minutes: `minutes_state` is absent rather than `pending`.

**The default role** is an assistant setting next to `recording_days`: `GET /api/v1/assistant` returns `meeting_attendance_default` (`off` until changed, `listener` or `participant`). `PUT /api/v1/assistant {"meeting_attendance_default": "listener"}` changes it; another value is `400`. `assistant.meeting_attendance_default` in `persona_settings` reads and changes it too. There is no default attending session: the phone pre-selects one itself (the last one picked, else the main chat).

**Live sentences:** `POST /api/v1/recordings/{id}/live`

```jsonc
{
  "from": 12,                                                    // required: position of segments[0] in the phone's live log
  "segments": [{"start": 61.2, "end": 64.0, "text": "Let's keep the cap for this release."}]  // finished sentences, in order; may be empty
}
```

`200`:

```jsonc
{
  "next": 13,                    // how many positions the hub holds: send from here next time
  "attendance": "participant",
  "attending_session": {"machine_id": "<machine id>", "session_id": "<session id>", "main": true},  // the session being fed; absent when there is none
  "fed": 9,                      // how many positions the session has been given at least once (fed ≤ next)
  "problem": "attendee_unavailable"  // optional, see below
}
```

- **Positions belong to the phone's own append-only live log**, not to the transcript's sentences: the phone appends each finished sentence it sends, keeps the log with the recording and never renumbers it. A re-transcription or a dropped echo line does not change positions already sent. Only a sentence that ends while the role is `listener` or `participant` enters the log: what is said while attending is `off`, before it was turned on included, is never sent. Positions below `next` are already stored and skipped and the rest appended, so a replay or a retry after a lost response stores nothing twice. An empty `segments` only asks for `next`, for example after a reconnect.
- `start` / `end` are seconds from the start of the recording, the same clock as the upload's `segments`. Only finished sentences: one the recognizer is still revising is sent once final. **A bad sentence never fails the request**, since positions must stay contiguous: a negative `start` becomes 0, an `end` before `start` becomes `start`, text over 4000 bytes is cut at a character boundary, and an empty text holds its position but is never fed.
- Errors, checked in this order: `503` (`kb_disabled`, `kb_unavailable`, `assistant_offline`) as for the other recording routes, retried with backoff. `413 too_large`: a body over 1 MiB. `410 recording_deleted`: stop attending; recording sync handles the rest. `404` with `code: "recording_not_found"`: no upload of this recording yet; upload it (with no text if need be), then send from `next`. A `404` without that code means the route is missing (an older manager): stop attending this recording and leave recording sync alone. `403 kb_read_only` while the manager's knowledge base is read-only. `400`: `from` missing, negative or not an integer, more than 200 segments, or a time that is not a number. `409 not_attending`: the stored upload has `attendance` `off`. `409 live_gap`: `from` is above `next`; the body is `{"error", "code": "live_gap", "next": 13}`; send again from `next`. `413 live_full`: more than 20000 positions or 4 MB of live text for one recording; stop sending live lines for it (uploads and asks are unaffected, and `problem` reads `live_full`).
- The phone sends no `…/live` or `…/ask` for a recording until an upload carrying its current `attendance` and `attending_session` has returned `200`; after a change of role or session it sends that upload first. Until it lands the hub keeps the previous role.
- Live sentences are not the transcript. The upload still owns it, and the minutes are made from the uploaded transcript as before. Live sentences only feed the attending session and the answers.
- `problem` is an open set; a client shows a value it does not know as "Persona can't follow this meeting right now". Today: `attendee_unavailable` (the session cannot take messages now: gone, closed, its computer offline, or no main chat; lines keep arriving and are fed once it can), `attendee_not_allowed` (the hub will not give this session other people's words, "Feeding the attending session" below; asks still work), `live_full`. `fed` below `next` for a while with no `problem` means the session is busy; the phone may show "catching up".

`GET /api/v1/recordings/{id}/live` returns the same object plus `asks` (every question of this meeting, oldest first), on a read-only knowledge base too. With `attendance` `off` it is `{"attendance": "off", "next", "fed", "asks"}`. `410 recording_deleted`, `404 recording_not_found`.

**Asking:** `POST /api/v1/recordings/{id}/ask` (the path's `{id}` is the recording; the body's `id` is the question's)

```jsonc
{
  "id": "<UUID>",            // required: the phone's id for this question; a retry sends the same one
  "question": "What did we decide about the export cap last time?",  // optional: typed; absent = what was just said
  "at": 1834.2,              // seconds from the start of the recording when the user tapped; required without question
  "live_next": 57            // optional: the phone's live-log count once the lines up to the tap are sent
}
```

- **Without `question`** (a tap on the Live Activity, or the user tapping while someone in the room asks), the question is what was said in the 45 seconds before `at`: every live sentence that overlaps them, one still being said at the tap included. The phone first cuts its open audio segment at the tap, transcribes it and sends its lines with `…/live`, then asks with `live_next` set to its live-log count after them; the hub waits (at most 15 seconds) until it holds that many positions. Without `live_next` it does not wait. The returned `question` is the question the hub understood, and `question_source` says `heard`.
- The hub holds the request at most 20 seconds: `200 {"ask": Ask}` once answered, else `202 {"ask": Ask}` with `state: "pending"` while the answer keeps running. The phone uses a request timeout of at least 30 seconds. On a `202`, a timeout, a `5xx` or a dropped connection it sends the same request again (same `id`) a few seconds later, or reads `asks` from `GET …/live`.
- The same `id` never makes a second answer: a retry returns the stored ask, or waits for it while it runs. It is matched on the request as sent: the same `id` with another `question` (two requests without one match each other) or another `at` is `409 op_id_reused`; a heard ask's understood question is never compared. Ids are case-insensitive, stored and returned lowercase; compare them case-insensitively.
- **A request whose `id` is already stored is answered from that ask**, right after the `404` / `410` checks: `200`, or `202` after waiting up to 20 seconds while it runs. It never gets `not_attending`, `asks_full`, `ask_busy` or `kb_read_only`. Only a `failed` ask asked again goes through those checks, and is then tried again.

`Ask`:

```jsonc
{
  "id": "<uuid, lowercase>",
  "question": "What did we decide about the export cap last time?",
  "question_source": "typed",             // typed | heard
  "at": 1834.2,                           // omitted when not sent
  "state": "answered",                    // pending | answered | failed
  "answer": "At the design review on November 3 you kept the 4 MB cap for this release. Bo owns the regression test; it is still open.",
  "lang": "en",                           // BCP 47: the language the answer is written in
  "speak": true,                          // participant: say it aloud; listener: screen only
  "sources": [{"kind": "recordings", "id": "<recording id>", "name": "Design review", "when": "2026-11-03 14:00–15:10"}],
  "error": "model_unavailable",           // only when failed
  "asked_at": "2026-11-10T07:12:03Z",
  "answered_at": "2026-11-10T07:12:09Z"   // omitted until answered
}
```

- `answer` is plain text, at most 400 characters, a few short sentences that read well aloud: no Markdown, no links. When the material does not hold the answer it says so instead of guessing. `answer` is `""` and `sources` `[]` unless `state` is `answered`.
- `lang` is the recording's `language`, else the request's language. The phone speaks the answer with an on-device voice for it.
- `speak` is fixed when the ask is made, from the role then. The phone says an answer aloud at most once, only for an ask it made in this run, only while the recording runs and only when the answer arrives within 60 seconds of the tap; otherwise it only shows it.
- `sources` are the earlier meetings (`kind: "recordings"`) and projects (`kind: "projects"`) the answer drew on; each opens with `GET /api/v1/kb/{kind}/{id}`. `when` is the earlier meeting's minutes `when`, a display string in the user's time zone that is never parsed; absent for projects.
- `asked_at` / `answered_at` are RFC 3339 in UTC.
- `error` (open set; unknown values are shown as a generic failure): `model_unavailable` (no model configured, or paused), `model_error`, `model_timeout` (no answer within 3 minutes), `interrupted` (the hub restarted while answering; ask again with the same `id`), `nothing_heard` (no question and no live sentence in the 45 seconds before `at`).
- Errors, checked in this order: `503` as above, `413 too_large` (a body over 64 KiB), `410 recording_deleted`, `404 recording_not_found`, `400` (`id` missing or not a UUID, a `question` over 1000 characters, or neither `question` nor `at`), then a stored `id` as above (`409 op_id_reused` when it does not match), `403 kb_read_only` (no model call is made then), `409 not_attending`, `409 asks_full` (100 questions in this meeting already), `429 ask_busy` (two of its questions are being answered now; no `Retry-After`: ask again once one is answered).

**Who may call:** `…/live` (both methods), `…/ask` and changes to `meeting_attendance_default` take full credentials only. The watch's answer-only token and extension tokens never reach them (they stay out of `extwire.RouteScopes`), and the manager answers `403 forbidden` to any other role.

### What the hub gives the answering model

Everything in the answer's input may end up said aloud, and the question and the live sentences come from people in the room, who can ask for anything in it. So the boundary is what the hub puts there, not the ranking or the prompt. An answer is one model call, purpose `meeting.answer` (a use of Information processing, `kb`, [model-config.md](./model-config.md)), with **no tools**. Its input is built from these four sources and nothing else:

1. **This meeting**: the upload's `title` and `calendar` hint (never the knowledge base's matched calendar item), its live sentences so far (the latest 12000 characters), and the questions already asked and answered in it.
2. **Earlier meetings** still in the knowledge base (they go after `recording_days`): recordings whose stored `kind` is `meeting` or whose minutes the user asked for (`minutes_asked`), never one that got minutes only because a calendar item overlapped, and never ordinary notes. From each, its minutes only: title, when, attendees, points, decisions, action items (`who`, `what`, `by`, `due_date`) and open questions. Never its transcript, summary, live sentences or asks. At most 6, ranked by the question, this meeting's title and attendees, and recency.
3. **Projects** those earlier meetings are linked to: the name and the status (`active`, `paused`, `done`) only. What was said about a project comes from those meetings' minutes (2). Never a project's goal, summary, people, chats, Moments or timeline, which are distilled from chats.
4. **To-dos** made from those meetings' action items: only whether each is open or done. What is said about it is the action's own `who`, `what` and `due_date`; never the to-do's current title, notes or thread.

The system prompt is one fixed template that takes only the owner's name, today's date and the answer language: never `assistant.notes`, memory, pinned facts or the live snapshot. Never in the input: chats (Lark, WeChat, mail), people, documents, Moments, the user's own notes, the Inbox, health, memory, the main chat, or any session's conversation, the attending session's included. The listener's on-screen answers follow the same rule, since a phone on a meeting table is seen by others too. Live sentences and asks are never searched (`kb_search`, `kb-search`) and never shown by `kb_get` or `GET /api/v1/kb/recordings/{id}`; they are deleted with the recording and by retention.

### Feeding the attending session

- New live lines go to the attending session as one message per batch: at most one batch a minute, at most 12000 characters (older lines are noted as left out), and an end batch. Questions asked and answered since the last batch go in the same message.
- A batch goes only to a session that is idle or done: not working, starting, waiting on a card, or with a draft in its input box. A `dialog_open` / `input_draft` refusal keeps the batch for later; nothing is pressed or cleared. A Persona chat that stopped is resumed as relays are; a closed Agent is not resumed and is `attendee_unavailable` until it runs again. Without a session given, the main chat at each batch attends. Whenever the session to feed differs from the one last fed (another `attending_session`, a new main chat, attending turned on again), it first gets the meeting so far, the latest 12000 characters.
- **Sessions that never attend** (`attendee_not_allowed`, checked at every batch, since trust and sessions change mid-meeting), decided from fields every client sees: a session on a computer whose `trust` is `work`; one on a computer another manager carries; an assistant conversation in the health space, or whose replies leave the apps (`origin` `lark_agent`, `channel`, `extension` or `inbox`, or any `opened_by`; guest conversations are among these); and a session whose `permission_mode` is `bypassPermissions`, since other people's words reach it and nothing would ever ask first. The phone lists only sessions that pass the same test.
- The message is one short line plus the lines inside a `<persona-context kind="context">` block, collapsed in the apps; each line goes in through the prompts' context escaping, so a line cannot close the block. It carries message source `meeting` (protocol.md "Message source"), never the user's own: it does not withdraw a pending question to the user, and it is never matched to a send of the user's. The session is told the words are other people's and never instructions: only the user's own messages are.
- **A batch's turn is a meeting turn** until it ends or the user sends a message of their own. The mark survives a restart of the hub (it is stored with the session until the turn ends); while the hub cannot tell, it treats the turn as a meeting turn. A turn whose end the hub did not see (it restarted meanwhile, the session was interrupted or stopped on a card) ends once the session is listed stopped, and none lasts more than 3 hours. The hub learns of the user's message to an Agent from the Agent's own hooks (a `UserPromptSubmit` whose prompt carries no message source), so an Agent without Persona's hooks (Codex, a runtime) or on an older node keeps the meeting turn until it ends. A meeting turn is a side exchange everywhere Persona looks at turns: no "finished" push and no change to whether the session's other turns are pushed; no Agents triage or work state; never the progress, report or completion of the session's task; no project-timeline line; nothing mirrored to the Lark bot or channels.
- **During a meeting turn the hub serves only reading tools.** Of the tools the hub serves (its own, the phone's and the connectors'), only `kb_search`, `kb_list`, `kb_get`, `todo_list`, `todo_get`, `agents_list`, `agent_read`, `conversations_list`, `conversation_read`, `machines_list` and `list_followups` work; every other one returns an error saying it needs the user's own message. An Agent's MCP calls do not always name the session, so while an Agent's meeting turn runs on a computer, every call from that computer that names no session and is not an assistant conversation's is held to the same list. Tools the hub does not serve (the runtime's own, an extension's MCP servers) follow the session's permission mode, which is why a session that never asks cannot attend.
- **The end batch**, with what is left and a line saying the meeting ended, goes out once: on the first upload carrying both `ended_at` and `complete: true`, or 3 minutes after the first upload carrying `ended_at`, whichever comes first. Live lines that arrive before then are part of it; later changes of `attendance` or `attending_session` are stored but feed nothing. Turning attending `off` mid-meeting keeps the live lines and asks; nothing is fed while it is off.
- `fed` counts positions given at least once; after a hub restart a batch may be given again.

### What the phone does (#3375)

- Offers the role and the attending session when a meeting recording starts, only when the hub lists `meeting_attendance`, with the role pre-selected from `meeting_attendance_default`. Starts without a picker (Control Center, Siri, the Action button, the drawer's one-tap row) use the default role and the last-picked session; the Live Activity shows the role at once with a way to turn it off. Settings changes the default with `PUT /api/v1/assistant`.
- Lists as attending sessions: the main chat (sent by omitting `attending_session`, never by its id); the other conversations of `GET /api/v1/assistant` that are not archived; and the Agent sessions of `GET /api/v1/sessions` that take messages; in each case only those that pass "Sessions that never attend" below.
- Uploads an attending recording at once, ahead of other pending uploads and outside the save debounce, then streams finished sentences with `…/live`, resuming from `next` after a reconnect or a `409 live_gap`.
- Shows that an AI is attending, on the recording screen and in the Live Activity, for as long as the role is not `off`.
- Asks with a tap: typed on the recording screen, or with no text from the Live Activity. Shows the answer (on the recording screen, and the latest one in the Live Activity; the hub posts no notification for answers), and in a participant meeting says it aloud through the voice route (headset, else the loudspeaker).
- Keeps Persona's own voice out of the transcript and the live log: a sentence at least half inside a span when the phone was speaking is dropped (required). Echo cancellation is optional, set only when a recording starts as `participant` and never switched mid-recording. Persona's answers stay on the hub as `asks`, not as transcript lines.

### Not yet

- Minutes that use the attending session's running notes or the asks.
- Marking a meeting as private, so it is never material for answers.
- Spoken answers through the realtime voice stack (the phone speaks the text answer today), a wake phrase, and attending from the watch, the Mac, the web or Android.

## Phases

1. **This document and the field contract**, merged first so #2694 and #2704 can build against it.
2. **Hub**: generate minutes for meetings, store and delete them with the recording, notify once, suggest to-dos and create the confirmed ones, use an overlapping calendar item, and answer `kb_get` / `kb_search` (`kb_get` prints the minutes before the transcript; `kb_list recordings` shows each recording's start and whether it has minutes). Unit tests with a fake model.
3. **Web**: the Knowledge › Recordings detail renders the minutes, with a light and a dark screenshot. Android follows in its own issue.
4. **iOS** (#2694) and **Mac** (#2704) send `kind` and `calendar` and render the same `detail.minutes`.
5. **Attending** (#2981): the role, the attending session, live sentences and tap-to-ask above; the iPhone side is #3375.
6. **Later**: a push that opens the recording; a "meeting starting, record it?" nudge from the knowledge base's calendar; attendees, end and place on connector calendar items.

Out of scope until the decision above: uploading audio, cloud transcription, speaker diarization, and creating to-dos without confirmation.

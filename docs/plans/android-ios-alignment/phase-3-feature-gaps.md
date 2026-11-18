# Phase 3: feature gaps (#4211)

Part of the [Android alignment plan](README.md) (#4194). Status: plan, 2026-11-11.

## Requirements

### Goal

What people do on an iPhone they can also do on Android, most-used first. Whatever isn't built has an issue with its reason. Alignment goes both ways: features only Android has get iOS issues.

### Items, by user impact

**F1. Permission requests that need fields filled in.** On iOS the card becomes a form: one field per declared field, secret fields masked, and two buttons, Deny and Fill In & Allow (no Always); Allow stays disabled until the required fields are filled. On Android, Allow and Always are blocked for these cards and the panel sends the user to the web or the iPhone. Requirement: the same form and buttons on Android.

**F2. New Agent.** iOS offers attachments, a new worktree, ultracode (#582), help when a launcher is blocked, and choosing an Orca workspace. Android has the computer, the folder browser, the launcher and model, mode, effort, recent setups and a herdr space preset, and none of these five. Requirement: all five, as on iOS.

**F3. Editing files and reviewing code in a session.** iOS uploads into a folder (replace or keep both), renames, moves and trashes with Undo, and has a review page: pick the scope (uncommitted, branch or commit), select lines, comment, then send the review or add it to the composer; comment blocks show as cards in messages. Android's files pages are read-only and it has no review page. Requirement: both, as on iOS.

**F4. Main chat cards.** iOS shows the questions the assistant answered for you, with the answer and a way to change it; HTML cards; and Undo on quick results (an event or reminder made on this phone). Android shows a "by assistant" line, every HTML card's fallback, and quick results with Undo on the calendar events this phone made (#9340; reminders keep none, there is no reminders store to delete from). Requirement: all three.

**F5. Lark replies.** iOS sends @ mentions, images and files, and shows topic groups as one card per topic (#576, #511). Android sends text and threaded replies. Requirement: as on iOS.

**F6. To-dos.** iOS edits the due date, the reminder and the priority in the detail, shows the stage bar, and dictates notes. Android edits the title and notes only. Requirement: as on iOS.

**F7. Agents list.** iOS groups By Repository, By Project, By To-do, By Status, By Machine, By App and Recently Used; Android has By repository, By project, By status, By to-do, By Machine (#9319), By App and Recently Used (#9199); its By computer and space has iOS's By Project shape, so the [Agents page](agents-page.md) (A5) renames it. iOS also has git and worktree actions (pull, push, fetch, new worktree, rename, cleanup when an Agent closes) and Orca workspaces. Requirement: the actions and workspaces (flat By Machine done in #9319). How the shared groupings look is the Agents page's.

**F8. Voice.** On iOS the composer turns into a voice bar tied to the current chat. Android opens a separate page whose work always goes to the main chat. Requirement: the voice bar; the page stays for captions.

**F9. Larger areas, each decided in its own issue.** Health; meeting recording (Android shows the minutes in Knowledge but can't record); and the Machines extras (live view, usage chart, clipboard, computer use). The archived-chats browser moved to [chats](chats.md) (C12), with the rest of the conversation history.

**F10. Settings Android lacks.** Meeting settings (with recording); Claude Code Live Replies; offering the clipboard when you come back to the app; the in-app tunnel page; Appearance (text size, which is open question 6, layout, glow while replying); workout reports; source subscriptions (#2041). Saved Places is complete (#9293) in Settings › Assistant › Saved Places. Knowledge Selection is complete (#2217) in Settings › Knowledge Selection and is no longer an outstanding gap. GitHub connector sign-in and Inbox settings is complete (#9307) on Settings › Connectors › GitHub and is no longer an outstanding gap. The Live Activity, Dynamic Island, Watch sync and background push are platform features, not gaps.

Related: #506 (marking up photos before sending, recording context in the composer), #1581 (password cards).

### Features only Android has

Each gets an iOS issue: answering questions and terminal choices from notifications (iOS question and terminal notifications have no buttons); sharing files to any running Agent (iOS shares text, links and images to the main chat only); search inside a chat; the pending request countdown (iOS reads `expires_at` but never shows it); public session sharing. The floating capsule, the Agents home-screen widget and the Quick Settings tile stand where iOS has Live Activities and Control Center controls: those are intentional differences, not gaps.

### Acceptance criteria

- Each of the ten items is done on Android with screenshots next to iOS, light and dark, or has its own issue with the reason it isn't done (principle 4).
- Items F1 to F5 land before the rest.
- Each new Android setting says where it is shown and how it is changed back (principle 3).
- The iOS issues for the Android-only features exist.

### Out of scope

- Interactions (phase 2) and tokens (phase 1).
- New hub endpoints, except where an item says so. The endpoints below exist.

## Technical design

### Summary

| Item | iOS | Hub | Android today | Where | Size |
| --- | --- | --- | --- | --- | --- |
| F1 | `apple/Persona/Views/Interactions/PermissionInputForm.swift` | resolve with `values` | model ready, Allow blocked | core/domain, feature/chat | S |
| F2 | `apple/Persona/Views/NewChat/NewChatView.swift` and its folder | sessions, launchers, worktree, workspaces, files | none of the five | feature/agents, core/contracts, core/hub | L, five PRs |
| F3 | `apple/Persona/Views/Session/FolderEdits.swift`, `apple/Persona/Views/Session/ReviewPage.swift` | files edits, git review | read-only files and git | feature/files, core/domain, feature/chat | M + L |
| F4 | `apple/Persona/Views/Interactions/DelegateViews.swift` and others | interaction history, reanswer, HTML actions | text line, HTML off, calendar Undo done | feature/assistant, feature/chat, feature/settings | M |
| F5 | `apple/Persona/Views/Inbox/Lark/LarkComposer.swift` and its folder | Lark members, images, files, topics | text only | feature/inbox | M to L |
| F6 | `apple/Persona/Views/Todos/TodoDetailView.swift` | `PATCH /api/v1/todos/{id}` | title and notes only | core/hub, feature/chat | M |
| F7 | `apple/Persona/Views/Chats/NodeActions.swift` and the grouping code | worktree, git, workspaces | four groupings | core/domain, feature/agents | M to L |
| F8 | `apple/Persona/Views/Voice/VoiceBar.swift` | voice protocol 2 | protocol 1, separate page | feature/voice, feature/chat, app | L |

### F1. Fields on permission requests (S)

- Wire: the card carries `fields: [{key, label, placeholder?, secret?, optional?}]`. Answering is `POST /api/v1/interactions/{id}/resolve` with `{"allow": true, "values": {...}}`. The hub refuses missing required values, drops undeclared keys and redacts secrets in history (`docs/protocol/protocol.md` ("Permission cards that need filling in")).
- Android already has `InteractionField` and `Resolution.values` in core/contracts. `android/core/domain/src/main/kotlin/persona/core/domain/answer/AnswerBuilder.kt` blocks Allow and Always for these cards; only the secret run's password fills `values` today.
- Change: add `values` to `AnswerDraft`. Allow sends the declared keys and requires the non-optional ones; Always is hidden, as on iOS. Turn the secret-run form (`SecretRunSummary.kt` in feature/chat) into a general fields form with `JSecretField`, keeping its clear-on-dispose. Notifications keep leaving Allow out for these cards.
- Tests: AnswerBuilder unit tests, and cases in `spec/fixtures/answer` if the rule belongs in the shared fixtures.

### F2. New Agent (L, five PRs)

1. Ultracode (#582): `ultracode` on `AgentStartRequest` (`POST /api/v1/sessions`), shown for Claude Code 2.1.284 and later as on iOS, reusing the `Ultracode` control from `android/feature/chat/src/main/kotlin/persona/feature/chat/ChatModes.kt`.
2. Blocked launchers: `health` on `AgentLauncher` from the launchers endpoint (`docs/server/launcher-health.md`), and a help sheet with the reason, Details, Add Provider (the existing provider screen) and Try Anyway.
3. New worktree: worktree status and add before the start, remove when the Agent closes, as methods on `AgentCreationService`.
4. Workspaces (Orca): `GET /api/v1/workspaces` and a picker; New Child Worktree through worktree prepare and create.
5. Attachments: upload through the files endpoint (`docs/protocol/protocol.md` ("Sending files")) and put the paths into the first message; Codex launchers take none. `ChatComposerService.upload` needs an overload that takes a computer instead of a session.

### F3. Files and review (M + L)

- File edits (M): write methods on `SessionFiles` and its hub implementation for upload into a folder with a conflict choice, rename, move, trash and restore (`docs/protocol/protocol.md` ("Changing files"); hub features `upload_dir` and `file_edit`). A long-press menu and Undo in `android/feature/files/src/main/kotlin/persona/feature/files/FilesScreen.kt`, through the phase 1 feedback layer.
- Review (L): a review page in feature/files reusing the diff rows of `GitPages.kt`, fed by the git review endpoint. A codec for comment blocks in core/domain, tested against `spec/fixtures/comments/cases.json`, and comment cards in feature/chat bubbles.

### F4. Main chat cards (M)

- Answered for you: a store for `GET /api/v1/interactions/history`, `POST /api/v1/interactions/{id}/reanswer` and the `interaction_reanswered` event, then the card and the change sheet, reusing `DecisionOption`.
- HTML cards (S): the sandbox and bridge exist in `android/feature/settings/src/main/kotlin/persona/feature/settings/AssistantHtml.kt` but stay switched off until the security check in `docs/assistant/assistant-views.md` passes on a real phone. Run the check, then switch them on.
- Undo on quick results (S): done for calendar events (#9340). iOS undoes on the phone only; there is no hub endpoint. `QuickResult` keeps the created item's numeric id, `CalendarTools.remove` deletes it through the same provider path without a second confirmation (the user is undoing the assistant's own write), `TasksStore.quickResultUndos` remembers the undone ids for the app's run, and the card reads Undone and dims as on iOS. Reminder cards keep no Undo: Android has no reminders store, so `phone_reminders_create` is never declared there. A tool result does not say which phone made the item, so Undo is offered for any numeric id: an iPhone's item has none and shows no button, while another Android phone's numeric id is only found out when the delete finds no such event (the chat's toast says "Couldn't undo"); a field naming the phone would be the hub's change if that ever matters.

### F5. Lark replies (M to L)

- Mentions (S): reuse the @ picker of `android/feature/inbox/src/main/kotlin/persona/feature/inbox/LarkDocComments.kt`; the members search and the `mentions` field are in `docs/protocol/protocol.md` ("Lark API").
- Images and files (M): through the reply composer's accessory slot; images go up base64 up to 7.5 MiB, files raw up to 100 MiB.
- Topic groups (M): a topic-card mode when the chat is a topic group, using the topic message list and thread endpoints.

### F6. To-dos (M)

- Extend `patchTodo` in `android/core/hub/src/main/kotlin/persona/core/hub/HttpTasksApi.kt` and the tasks store with the due date, the reminder and the priority (`null` clears; `docs/assistant/todos.md`), and add pickers to the detail.
- Add `stage` to the model and the stage bar (S).
- Move the dictation helper out of feature/chat, where it is internal, and add a mic to the notes (S).
- The `PATCH` row in `docs/protocol/protocol.md` is out of date; fix it in the same PR.

### F7. Agents list (M to L)

- Groupings (S each): By App and Recently Used done (#9199: `AgentAppGroups`, `AgentGrouping.App` / `Recent`); flat By Machine done (#9319: `AgentGrouping.Machine` in `selectable` / `fromPreference`, projection in `AgentList.kt`).
- Node actions (M): git pull, push and fetch, split pane, close tab, run an Agent in a pane, new and renamed worktrees, and cleanup when an Agent closes, as a service plus dialogs next to the existing Agent operation dialogs.
- Workspaces (M): the same catalog as F2.

### F8. Voice bar (L)

- Move the voice session to protocol 2: a `start` frame with `target`, `recent`, `history` and `resume`, and the `ready.target`, `run`, `ask`, `link` and `resend` frames (`docs/assistant/voice.md`).
- Keep the call in an app-level holder keyed by session. The chat screen swaps its composer for a voice bar the app supplies, since feature/chat doesn't depend on feature/voice. The page stays for captions.

### F9. Larger areas

| Area | Hub | Android today | Size |
| --- | --- | --- | --- |
| Health | health chats and memory, health reports (`docs/assistant/health.md`) | none | L |
| Meeting recording | recordings, minutes, live, ask | minutes in Knowledge; recording itself is an open question (long audio on Android) | L |
| Live view | VNC over a forwarded port | `android/core/hub/src/main/kotlin/persona/core/hub/PortForwarder.kt` already connects ports | L |
| Usage chart | per-computer usage | reuse the metric charts | M |
| Clipboard | clipboard and its settings | none | S to M |
| Computer use | computer, apps, clipboard | none | M |

### F10. Settings

| Setting | Size |
| --- | --- |
| Saved Places | S — done (#9293); Settings › Assistant › Saved Places |
| Knowledge Selection | M — done (#2217); Settings › Knowledge Selection |
| Meeting settings | with recording |
| Claude Code Live Replies | S |
| GitHub connector sign-in and Inbox settings | S to M — done (#9307); Settings › Connectors › GitHub |
| Clipboard offer on return | S |
| In-app tunnel page | M |
| Appearance: layout, glow while replying (text size is question 6) | S each |
| Workout reports | with Health |
| Source subscriptions (#2041) | M |

### Order of work

1. F1.
2. F2: ultracode and blocked launchers first, then worktree, attachments and workspaces.
3. F6 and the HTML card check (F4).
4. F5, file edits (F3) and the groupings (F7).
5. Review (F3), the voice bar (F8) and the node actions (F7).
6. The rest of F9 and F10, each after its own decision.

Each item gets its own issue (reusing #582, #576, #511 and #2041) and one PR, with screenshots next to iOS.

### Risks

- Older hubs: gate file edits and folder uploads on the hub's `file_edit` and `upload_dir` features.
- HTML cards run web content; they stay off until the device check passes.
- Voice protocol 2 needs a voice extension that speaks it; keep protocol 1 as the fallback.
- Recording long meetings on Android (background limits, battery) needs its own design before it starts.

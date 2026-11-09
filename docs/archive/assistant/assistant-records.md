# Assistant: records

Moved unchanged from the [assistant guide](../../assistant/assistant.md), which now describes only the current design.
These are what the main chat lacked before the 2026-10-26 capability upgrade, the tests added for it on 2026-10-30 and
2026-10-31, the 2026-10-30 capability measurements and retest on the production hub, and the 2026-10-26 end-to-end run.
Written 2026-10-26 to 2026-11-07, before the current issue tracker. `git blame` dates it to the move; blame the parent of the move commit to see the original lines.
The commit it names (`e50f972`) does not resolve in this history.

## Why it was weak before 2026-10-26 (from real chat records)

- **Broken confirmation**: CLAUDE.md told it to confirm through AskUserQuestion, but in `-p` that tool immediately reports
  `No such tool available: AskUserQuestion … nobody in this session can answer it`; PreToolUse hooks never get a chance to run.
- **Most time spent finding data**: 33 `cd` / `ls` calls in one chat; `docker inspect` to extract a Lark token; ssh and parsing `herdr agent list` to see Agents.
  The hub already has this data.
- **Slow**: 36–136 seconds per turn, repeated ssh timeouts (exit 124 / 137); cold-starting claude for each chat, and ToolSearch before Persona tools.
- **Reactive only**: even on starting, it didn't know the time, who was waiting, or how many unread items existed.

## Verification

Server tests in `docs/operations/testing.md` cover `nextRun` / `parseWhen`, expired-task skipping, ask_user round trips, agents_list, assistant-only tools.
Added 2026-10-30: scheduling across hub restart (`TestScheduleAcrossHubRestart`: no rerun, missed follow-ups run once with lateness, very late routines skipped, history retained),
`schedule_routine`, retaining processes with background tasks, explaining lost tasks after restart; App `AssistantScheduleTests`, UI `AssistantProfileTests`
(avatar → profile, scheduled list / details, Run now for tasks / follow-ups). After 2026-10-31 table unification: `assistant/schedule_test.go` (lossless / nonduplicating migration,
self-renewal becomes one `interval`, interval cadence, permission validation, old APIs), App `AssistantRoutinesModelTests`, UI `testRepeatingTaskAndItsPermissionMode`
(check appears in list, Allow Everything confirmation, hub save).

### Capability measurements (2026-10-30, production hub, early morning while user inactive; test chats start `【Persona 能力测试，可忽略】`—"Persona capability test, safe to ignore")

| Capability | Before fix | Evidence (UTC) |
| --- | --- | --- |
| Request "Remind me in 3 minutes" in chat | Partial: created and ran on time, but both `schedule_followup` and due `notify_user` needed phone approval; blocked without a tap | 18:45:15 schedule_followup → approval wait; due 18:50:05 notify_user → approval wait again |
| Follow-up punctuality | 21 seconds late (20-second polling + process resume) | Due 18:49:31.3, entered chat 18:49:52.6 |
| Request "Do this daily at a time" | No scheduled-task creation tool; one-shot follow-ups had to relay (as with the user's project checks) | `list_followups` said "the user edits these in the app's settings" |
| Scheduled tasks / follow-ups after hub restart | Yes: Postgres storage, 18:48:07 restart still triggered follow-up at 18:49; routines record ran before execution, no duplicates; over 30 minutes late skip (formerly invisible) | journalctl 18:48:07–10 restart; unit skip / no-duplicate tests |
| Monitor | Partial: live-process event woke assistant in 0.3 seconds; Monitor and notify_user needed approval, hub restart silently lost Monitor | 18:47:33.423 ALERT write → 18:47:33.719 assistant receives → 18:47:44.1 notify_user |
| Background (`run_in_background`) | Partial: 2-minute command immediately woke assistant on completion; over-20-minute commands killed with idle reclamation, also on restart, so assistant waited forever | 18:47:14 command ends → 18:47:14.79 assistant receives → 18:47:33 notify_user; 23-minute command killed by 18:48 restart, no further chat activity |
| phone_* while locked | Yes: silent push woke App, result in 2.8 seconds (02:54 local) | 18:54:42.9 approval → 18:54:45.7 result |
| Phone push | Yes, notify_user uses APNs even with App closed; hub formerly didn't record successful pushes, preventing latency measurement, now records "APNs acceptance time" | — |

Most latency is the model (10–19 seconds from event to notify_user); hub / node stays within 1 second.

### Retest after fixes (2026-10-30, after e50f972 deployment, production hub)

| Capability | Result | Event → assistant handles → push (APNs accepted) |
| --- | --- | --- |
| "Remind me in 3 minutes" | Works, no approval throughout | Due 19:39:40.29, ran 19:39:40.30 (0.01 seconds late) → 19:39:47.4 notify_user → APNs 175 ms |
| "Do this daily at HH:MM" | Works (`schedule_routine`) | Due 19:43:00, ran 19:43:00.01 → new chat 19:43:01.96 → 19:43:07.9 notify_user → APNs 313 ms; run recorded |
| Monitor | Works (Monitor itself still needs one permission-mode approval) | 19:37:23.855 write → 19:37:24.167 assistant receives (0.3 seconds) → 19:37:30.9 notify_user → APNs 313 ms |
| 2-minute background task | Works, no approval throughout | 19:38:43 command ends → 19:38:43.85 assistant receives → 19:38:55.5 notify_user → APNs 173 ms |
| 23-minute background task (beyond old 20-minute reclamation) | Works: process stayed unreclaimed through 23 idle minutes | 19:36:51 start → 19:59:51 command ends → 19:59:51.74 assistant receives → 20:00:09.9 notify_user → APNs 233 ms |
| Phone tools while locked | Works, no approval | Silent push APNs 259 ms, phone connects 900 ms later, tool result in 3.2 seconds |
| Explain lost background tasks after hub restart | Unit tested (`TestRecoverAssistantTasksAfterRestart`); production verification awaits a deployment with tasks running (log `background tasks lost in the restart`) | — |

Background / locked App: APNs alerts and silent wakes don't require foreground. Phone stayed locked during tests (after 03:00 local), phone tools returned in 3.2 seconds. hub can't measure when the notification actually appears on the phone.

End-to-end (2026-10-26): new chat answered "Who is waiting for me, what meetings remain today?" within 10 seconds using snapshot + `agents_list` + `lark_api calendar/week`;
ask_user displayed as a native App question card and returned the answer; `schedule_followup 2m` triggered on time and used `notify_user`.

Real-Lark tests of the extension-supplied skill (2026-10-30) are in the extension's own instructions.

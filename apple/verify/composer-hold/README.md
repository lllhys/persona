# The composer's mic: tap, hold and the permission wait (TLA+)

`ComposerHold.tla` checks the iPhone composer's mic (#2660) in every order of its steps. A tap toggles dictation.
A hold dictates while the finger is down. Sliding left cancels, sliding up locks, and letting go keeps the words.
Three things share one `Dictation`, and so one microphone-owner UUID, plus the app's `RecordingMicrophone`: the
gesture, the hold's start task and the tap's start task. `Dictation.start` suspends once, while it asks for
permission (`allowed()`), and the gesture keeps running during that wait. That wait is the only place where the
steps interleave.

## Properties and what breaking them costs

| Property | Breaking it means |
|---|---|
| `NoLostEngine`: every running audio engine is the one `Dictation.engine` holds | An engine nothing can stop: the microphone stays on (the orange dot) until the app quits |
| `SettledAsAsked`: once nothing is pending and the finger is up, dictation is on exactly when the person last asked for it | Words go on into the draft after the person let go, cancelled or sent, or it stays off when they asked for it |
| `NoWrongToast`: no "The microphone is in use" toast while nothing else owns the microphone | A wrong error toast, and the hold that raised it is ended |

## When to run

Run `./run.sh` after changing `ComposerHoldMic.swift`, `Dictation` (`PromptComposer.swift`), `Dictation.toggle`
(`ComposerButtons.swift`), the composer's send (`ShellComposerBar.swift`) or `RecordingMicrophone.swift`.
It prints `WARN DRIFT` when one of these has changed since the sync point. TLA+ tools 1.7.4: `./run.sh --fetch`.

## Results (TLC 2.19, `-workers 1`)

| Config | Variant, bounds | Checks | Result | States, depth |
|---|---|---|---|---|
| `r1_current_one_touch` | current; 1 touch, 1 start | all three | PASS (expected) | 21, 6 |
| `r2_no_postcheck_one_touch` | without the check after `start` in `ComposerHold.begin` (ComposerHoldMic.swift:43-44) | `SettledAsAsked` | FAIL (expected): the negative example | 21 |
| `r3_current_two_touches_toast` | current; 2 touches, 2 starts | `NoLostEngine`, `NoWrongToast` | **FAIL: a finding** | 191 |
| `r4_current_send_settled` | current; 1 touch, 1 send, 1 start | `NoLostEngine`, `SettledAsAsked` | **FAIL: a finding** | 38 |
| `r5_current_three_touches_mic` | current; 3 touches, 1 send, 3 starts | `NoLostEngine` | PASS | 7268, 17 |
| `r6_reclaim_two_touches_mic` | `RecordingMicrophone.claim` lets its own owner claim again | `NoLostEngine` | FAIL (expected): the negative example | 209 |

r1 matches what the tests cover (`DictationHoldTests` checks the hold's thresholds, and the UI tests tap the mic
once). The negative examples show that each property can fail: without the check after `start`, a hold released
during the permission wait leaves dictation on (r2). If the same owner could claim again, a second start would
replace the engine and leave the first one running (r6). The current code keeps `NoLostEngine` up to 3 touches
and 3 starts (r5), because the second claim fails.

### Finding 1 (r3): tap, then hold while the tap's start still waits for permission

1. A tap on the mic: `toggle` makes start task 1 (ComposerButtons.swift:56).
2. The finger goes down again before task 1 has permission. The mic isn't listening yet, so the hold's delay starts
   (ComposerHoldMic.swift:124). The hold begins and makes start task 2 (:26-35).
3. Task 1 gets permission, claims the microphone and listens (PromptComposer.swift:345-372).
4. Task 2 gets permission. `claim` fails for the same owner (RecordingMicrophone.swift:19), and `start` returns
   "The microphone is in use". `begin` shows the toast and ends the hold (ComposerHoldMic.swift:40-42).

Result: an error toast, the hold's strip goes away under the finger, and the tap's dictation goes on writing
into the draft.

### Finding 2 (r4): send while a tapped mic still waits for permission

1. A tap on the mic: start task 1 (ComposerButtons.swift:56).
2. Send. `endDictation` stops only a dictation that is already listening (ShellComposerBar.swift:492-495), so it
   does nothing here, and the message goes.
3. Task 1 gets permission, claims and listens: dictation starts writing into the emptied composer after the send.

The same happens when the composer goes away (`onDisappear`, ShellComposerBar.swift:282). Both findings need the
second action inside the permission wait. Once permission is granted, `allowed()` comes back after two callbacks on
a system queue, a few milliseconds. The first time, it waits for the system's prompts. **Only model evidence:**
`allowed()` is private and the DEBUG dictation sample (`-showDictationSample`) has no wait, so the real code can't
be held inside that window without changing the product. These findings are not fixed here; whether to fix them is
the maintainers' call.

## Model and code

| Action | Code (apple/) |
|---|---|
| `Down`, `Move` | `ComposerHoldMic.press.onChanged`, DesignSystem/ComposerHoldMic.swift:117-131 |
| `HoldFires` | the delay task (:125-129), then `ComposerHold.begin` (:26-35) |
| `Slide(o)` | `whileHeld` decides (:118-123), then `finish` (:49-63) |
| `Up(o)` | `onEnded` (:133-143): `finish` with `onRelease`'s outcome, or `toggle` for a tap; then `onChange(of: touching)` (:91-98), which then has nothing to do |
| `Taken` | `onChange(of: touching)` without an end (:91-98): `finish(.lock)` |
| `Send` | send (ShellComposerBar.swift:221) and `endDictation` (:492-495) |
| `TaskRuns(t)` | the task body runs into `Dictation.start` up to `await Self.allowed()` (Views/Chat/PromptComposer.swift:327-338) |
| `TaskResumes(t)` | the rest of `start` runs without suspending: `claim` (:345, Shared/Recording/RecordingMicrophone.swift:12-22), the engine (:356-361), `isListening` (:372). Then the caller: `begin`'s check (ComposerHoldMic.swift:36-46) or `toggle`'s (ComposerButtons.swift:57-59) |
| `StopS` | `Dictation.stop` (PromptComposer.swift:402-417): only the owner gets past `release` |

`who` in a trace: 1 is the finger, 2 is send, 10 + t is start task t.

## Bounds and assumptions

- Up to 3 finger-downs, 1 send and 3 start tasks (r5). One composer and one mic. The line's and the card's mic
  share `hold` and `dictation`, but the model has one gesture: a mic that disappears mid-hold (:100-104) isn't
  modelled.
- Permission is always granted. The other `start` failures (a recording in progress, no recogniser, the engine not
  starting) aren't modelled. The microphone has no other owner (no voice call, no `giveWay`).
- Each main-actor step is atomic: a gesture callback, the part of a task before its first suspension, and
  everything after `allowed()` returns, up to the caller's check. The words the recogniser sends and the text
  they write aren't modelled.
- `wantOn` (what the person last asked for) is a ghost variable. A tap that starts, a hold or a lock means on.
  Keep, cancel, a tap that stops, send, and a hold that failed with a toast mean off.

## Sync point

`COVERAGE.txt`: the sha256 of each of the five covered files as the model was written from them.

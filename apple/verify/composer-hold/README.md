# The composer's mic: tap, hold and the permission wait (TLA+)

`ComposerHold.tla` checks the iPhone composer's mic (#2660) in every order of its steps. A tap toggles dictation.
A hold dictates while the finger is down. Sliding left cancels, sliding up locks, and letting go keeps the words.
Three things share one `Dictation`, and so one microphone-owner UUID, plus the app's `RecordingMicrophone`: the
gesture, the hold's start task and the tap's start task. `Dictation.start` takes its turn at once and starts a task
that suspends once, while it asks for permission (`allowed()`), and the gesture keeps running during that wait. That
wait is the only place where the steps interleave.

## Properties and what breaking them costs

| Property | Breaking it means |
|---|---|
| `NoLostEngine`: every running audio engine is the one `Dictation.engine` holds | An engine nothing can stop: the microphone stays on (the orange dot) until the app quits |
| `SettledAsAsked`: once nothing is pending and the finger is up, dictation is on exactly when the person last asked for it | Words go on into the draft after the person let go, cancelled or sent, or it stays off when they asked for it |
| `NoWrongToast`: no "The microphone is in use" toast while nothing else owns the microphone | A wrong error toast, and the hold that raised it is ended |

## When to run

Run `./run.sh` after changing `ComposerHoldMic.swift`, `Dictation` (`PromptComposer.swift`), `Dictation.toggle`
(`ComposerButtons.swift`), the composer's send (`ShellComposerBar.swift`) or `RecordingMicrophone.swift`. TLC needs
Java 11 or later.
It prints `WARN DRIFT` when one of these has changed since the sync point. TLA+ tools 1.7.4: `./run.sh --fetch`.

## Results (TLC 2.19, `-workers 1`)

| Config | Variant, bounds | Checks | Result | States, depth |
|---|---|---|---|---|
| `r1_current_one_touch` | current; 1 touch, 1 start | all three | PASS (expected) | 23, 6 |
| `r2_stop_keeps_start_send` | `stop()` leaves a waiting start its turn (as before #3407); 1 touch, 1 send, 1 start | `SettledAsAsked` | FAIL (expected): the negative example | 37 |
| `r3_current_two_touches_toast` | current; 2 touches, 2 starts | all three | PASS (expected) | 287 |
| `r4_current_send_settled` | current; 1 touch, 1 send, 1 start | all three | PASS (expected) | 58 |
| `r5_current_three_touches_mic` | current; 3 touches, 1 send, 3 starts | all three | PASS (expected) | 13690 |
| `r6_no_takeover_two_touches_toast` | a start whose turn it is does not stop an older start already listening; 2 touches, 2 starts | `NoWrongToast` | FAIL (expected): the negative example | 222 |

r1 matches what the tests cover (`DictationHoldTests` checks the hold's thresholds, `DictationTurnTests` the orderings
below with the sample standing in for the microphone and a held permission question, and the UI tests tap the mic
once). The negative examples show that each half of the fix is needed: if `stop()` did not pass the turn, a hold let
go (or a send) while the microphone is still asked for would leave dictation on (r2). Without the takeover, a tap's
start that got in during the hold's delay keeps the microphone and the hold fails with a toast (r6).

### What #3407 fixed

Before #3407 the model found two orderings that end wrong; both need the second action inside the permission wait
(a few milliseconds once permission is granted, the system's prompts the first time):

1. A tap, then a hold while the tap's start still waits: both starts asked for the microphone, the second found it in
   use, toasted and ended the hold while the tap's dictation went on writing into the draft (`NoWrongToast`).
2. A send (or the composer going away) while a tapped mic still waits: `endDictation` stopped only a dictation already
   listening, so the start listened after the send and wrote into the emptied composer (`SettledAsAsked`).

Now `Dictation.start` takes a turn in the caller's step (the person's newest action decides, not the order the tasks
happen to run in). At the end of the wait a start whose turn has passed ends without listening or a toast. A start
whose turn it is stops an older start of the same `Dictation` that already listens (a tap's that got in during the
hold's delay) before it claims. `stop()` passes the turn, so a send, the composer going away, saving a to-do or a hold
let go end a waiting start too, and `ComposerHold.begin` needs no check of its own after the start.

## Model and code

| Action | Code (apple/) |
|---|---|
| `Down`, `Move` | `ComposerHoldMic.press.onChanged`, DesignSystem/ComposerHoldMic.swift:117-131 |
| `HoldFires` | the delay task (:125-129), then `ComposerHold.begin` (:27-45), which calls `Dictation.start` |
| `Slide(o)` | `whileHeld` decides (:118-123), then `finish` (:47-61) |
| `Up(o)` | `onEnded` (:133-143): `finish` with `onRelease`'s outcome, or `toggle` for a tap; then `onChange(of: touching)` (:91-98), which then has nothing to do |
| `Taken` | `onChange(of: touching)` without an end (:91-98): `finish(.lock)` |
| `Send` | send (ShellComposerBar.swift:224) and `endDictation` (:507-510), which calls `stop()` |
| `NewTask` | `Dictation.start` (Views/Chat/PromptComposer.swift:367-373): takes the turn, starts the task |
| `TaskRuns(t)` | the task runs `listen` up to the permission wait (:376-386) |
| `TaskResumes(t)` | the rest of `listen` runs without suspending: the turn check (:387), the takeover (:389), `claim` (:399, Shared/Recording/RecordingMicrophone.swift:12-22), the engine, `isListening` (:426). A failure goes to the caller's `failed`: `toggle`'s toast (ComposerButtons.swift:58-60) or `begin`'s, which also ends its hold (ComposerHoldMic.swift:41-44) |
| `StopS` | `Dictation.stop` (PromptComposer.swift:457-460): the turn passes, then `stopListening` (:462-478), where only the owner gets past `release` |

`who` in a trace: 1 is the finger, 2 is send, 10 + t is start task t.

## Bounds and assumptions

- Up to 3 finger-downs, 1 send and 3 start tasks (r5). One composer and one mic. The line's and the card's mic
  share `hold` and `dictation`, but the model has one gesture: a mic that disappears mid-hold (:100-104) isn't
  modelled.
- Permission is always granted. The other `start` failures (a recording in progress, no recogniser, the engine not
  starting) aren't modelled. The microphone has no other owner (no voice call, no `giveWay`), so `claim` fails only
  for this `Dictation`'s own older start, which the takeover stops first.
- Each main-actor step is atomic: a gesture callback, the part of a task before its first suspension, and
  everything after `allowed()` returns, up to the caller's check. The words the recogniser sends and the text
  they write aren't modelled.
- `wantOn` (what the person last asked for) is a ghost variable. A tap that starts, a hold or a lock means on.
  Keep, cancel, a tap that stops, send, and a hold that failed with a toast mean off.

## Sync point

`COVERAGE.txt`: the sha256 of each of the five covered files as the model was written from them.

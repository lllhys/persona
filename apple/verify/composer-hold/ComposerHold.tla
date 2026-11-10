---------------------------- MODULE ComposerHold ----------------------------
(* The iPhone composer's mic (#2660): a tap toggles dictation, a hold dictates while held. One `Dictation`
   (one microphone-owner UUID) and one `ComposerHold` are shared by the mic's gesture, the hold's start task
   and the tap's start task. `Dictation.start` suspends once, asking for permission (`allowed()`), and the
   gesture keeps running meanwhile: that wait is the only interleaving point between the main-actor steps.

   Every action is one main-actor step; the comment names the code it stands for (paths from apple/).
   Process model: the finger (gesture callbacks), the send button, and up to MaxTasks start tasks. *)
EXTENDS Naturals, FiniteSets, Sequences

CONSTANTS
    MaxTouches,  \* finger-downs in one behaviour
    MaxSends,    \* send taps in one behaviour
    MaxTasks,    \* start tasks (hold or tap) in one behaviour
    Variant      \* "current" | "no_postcheck": begin's task does not stop a start that resolved after the finger let go
                 \* | "reclaim": RecordingMicrophone.claim also lets its current owner claim again

VARIABLES
    owner,       \* RecordingMicrophone.owner: "none" or "D" (this Dictation's microphoneOwner)
    listening,   \* Dictation.isListening
    engine,      \* Dictation.engine: the engine it holds, 0 = nil
    running,     \* engines started and not stopped (a running engine keeps the microphone hot)
    nextEngine,
    holding,     \* ComposerHold.holding
    locked,      \* ComposerHold.locked
    run,         \* ComposerHold.run (a fresh UUID is a fresh number)
    down,        \* the finger is on the mic (ComposerHoldMic.touching)
    pending,     \* ComposerHoldMic.pending: the hold's delay task, not cancelled yet
    decided,     \* ComposerHoldMic.decided
    tasks,       \* start tasks: [kind |-> "hold"|"tap", mine |-> run at begin, pc |-> "queued"|"waiting"|"done"]
    wantOn,      \* ghost: what the person last asked for (on: a tap that starts, a hold, a lock; off: the rest)
    toasts,      \* ghost: "The microphone is in use" toasts; nothing else owns the microphone here, so each is wrong
    touches, sends,
    last

vars == <<owner, listening, engine, running, nextEngine, holding, locked, run, down, pending, decided, tasks,
          wantOn, toasts, touches, sends, last>>
view == <<owner, listening, engine, running, nextEngine, holding, locked, run, down, pending, decided, tasks,
          wantOn, toasts, touches, sends>>

Task == 1..MaxTasks

Init ==
    /\ owner = "none" /\ listening = FALSE /\ engine = 0 /\ running = {} /\ nextEngine = 1
    /\ holding = FALSE /\ locked = FALSE /\ run = 0
    /\ down = FALSE /\ pending = FALSE /\ decided = FALSE
    /\ tasks = <<>>
    /\ wantOn = FALSE /\ toasts = 0 /\ touches = 0 /\ sends = 0
    /\ last = [run |-> 0, at |-> "", did |-> "start"]

\* ---- Helpers: the state a synchronous call leaves (they are parts of one step, not steps) ----

\* Dictation.stop(), Views/Chat/PromptComposer.swift:402-417: only the owner gets past `release`.
StopS(s) ==
    IF s.owner = "D"
    THEN [s EXCEPT !.owner = "none", !.listening = FALSE, !.running = s.running \ {s.engine}, !.engine = 0]
    ELSE s

\* ComposerHold.finish(outcome), DesignSystem/ComposerHoldMic.swift:49-63, then end() (:65-70).
FinishS(s, o) ==
    IF ~s.holding THEN s
    ELSE LET a == IF o = "lock" THEN [s EXCEPT !.locked = TRUE, !.wantOn = TRUE]
                  ELSE [StopS(s) EXCEPT !.wantOn = FALSE]
             b == IF o = "lock" THEN a ELSE [a EXCEPT !.run = a.run + 1]
         IN [b EXCEPT !.holding = FALSE]

\* A new start task (Task { await dictation.start … }); it first runs later, when the main actor gets to it.
NewTask(s, kind) == [s EXCEPT !.tasks = Append(s.tasks, [kind |-> kind, mine |-> s.run, pc |-> "queued"])]

\* Dictation.toggle(into:), DesignSystem/ComposerButtons.swift:52-61.
ToggleS(s) ==
    IF s.listening THEN [StopS(s) EXCEPT !.wantOn = FALSE]
    ELSE [NewTask(s, "tap") EXCEPT !.wantOn = TRUE]

St == [owner |-> owner, listening |-> listening, engine |-> engine, running |-> running, nextEngine |-> nextEngine,
       holding |-> holding, locked |-> locked, run |-> run, down |-> down, pending |-> pending, decided |-> decided,
       tasks |-> tasks, wantOn |-> wantOn, toasts |-> toasts, touches |-> touches, sends |-> sends]

Put(s, who, at, did) ==
    /\ owner' = s.owner /\ listening' = s.listening /\ engine' = s.engine /\ running' = s.running
    /\ nextEngine' = s.nextEngine /\ holding' = s.holding /\ locked' = s.locked /\ run' = s.run
    /\ down' = s.down /\ pending' = s.pending /\ decided' = s.decided /\ tasks' = s.tasks
    /\ wantOn' = s.wantOn /\ toasts' = s.toasts /\ touches' = s.touches /\ sends' = s.sends
    /\ last' = [run |-> who, at |-> at, did |-> did]

CanTask == Len(tasks) < MaxTasks

\* ---- The finger ----

\* The press's first onChanged, DesignSystem/ComposerHoldMic.swift:117-131: not holding, nothing pending, not
\* decided, not listening: the hold's delay starts.
Down ==
    /\ ~down /\ touches < MaxTouches
    /\ LET s == [St EXCEPT !.down = TRUE, !.touches = touches + 1]
       IN Put(IF ~holding /\ ~pending /\ ~decided /\ ~listening THEN [s EXCEPT !.pending = TRUE] ELSE s,
              1, "ComposerHoldMic.swift:117-131", "down")

\* A later onChanged while not holding (:124): the delay starts once the mic is no longer listening.
Move ==
    /\ down /\ ~holding /\ ~pending /\ ~decided /\ ~listening
    /\ Put([St EXCEPT !.pending = TRUE], 1, "ComposerHoldMic.swift:124-131", "move: hold delay starts")

\* The delay ran out (:125-129): decided, then ComposerHold.begin (:26-47) makes the start task.
HoldFires ==
    /\ pending /\ down /\ CanTask
    /\ LET s == [St EXCEPT !.pending = FALSE, !.decided = TRUE, !.holding = TRUE, !.locked = FALSE,
                           !.run = run + 1, !.wantOn = TRUE]
       IN Put(NewTask(s, "hold"), 1, "ComposerHoldMic.swift:128-129 + :26-35", "hold begins")

\* Slid far enough while held (:118-123): cancel or lock at once.
Slide(o) ==
    /\ down /\ holding
    /\ Put([FinishS(St, o) EXCEPT !.decided = TRUE], 1, "ComposerHoldMic.swift:118-123", "slide: " \o o)

\* onEnded (:133-143), then onChange(of: touching) (:91-98), which finds nothing left to do.
Up(o) ==
    /\ down
    /\ holding \/ decided \/ CanTask \/ listening
    /\ LET s == [St EXCEPT !.pending = FALSE]
           t == IF holding THEN FinishS(s, o) ELSE IF ~decided THEN ToggleS(s) ELSE s
       IN Put([t EXCEPT !.decided = FALSE, !.down = FALSE], 1, "ComposerHoldMic.swift:133-143",
              IF holding THEN "up: " \o o ELSE IF ~decided THEN "up: a tap, toggle" ELSE "up")

\* The system takes the touch away without an end (:91-98): a hold goes on locked.
Taken ==
    /\ down
    /\ Put([FinishS([St EXCEPT !.pending = FALSE, !.decided = FALSE], "lock") EXCEPT !.down = FALSE],
           1, "ComposerHoldMic.swift:91-98", "touch taken away")

\* ---- Send (ShellComposerBar.swift:221, endDictation :492-495) ----
Send ==
    /\ sends < MaxSends /\ ~down
    /\ Put([(IF listening THEN StopS(St) ELSE St) EXCEPT !.wantOn = FALSE, !.sends = sends + 1],
           2, "ShellComposerBar.swift:221 + :492-495", "send: dictation ended")

\* ---- A start task ----

\* The task body runs into Dictation.start up to `await Self.allowed()` (PromptComposer.swift:327-338).
TaskRuns(t) ==
    /\ t \in DOMAIN tasks /\ tasks[t].pc = "queued"
    /\ Put([St EXCEPT !.tasks[t].pc = "waiting"], 10 + t, "PromptComposer.swift:338",
           "start: asks for permission")

\* Permission answered: the rest of start runs without a suspension (claim :345, engine :356-361,
\* isListening :372), and so does the caller after `await dictation.start` (ComposerHoldMic.swift:36-46 or
\* ComposerButtons.swift:57-59).
TaskResumes(t) ==
    /\ t \in DOMAIN tasks /\ tasks[t].pc = "waiting"
    /\ LET claimed == owner = "none" \/ (Variant = "reclaim" /\ owner = "D")  \* RecordingMicrophone.claim, Shared/Recording/RecordingMicrophone.swift:12-22
           e == nextEngine
           s0 == [St EXCEPT !.tasks[t].pc = "done"]
           s1 == IF claimed
                 THEN [s0 EXCEPT !.owner = "D", !.engine = e, !.running = running \cup {e}, !.nextEngine = e + 1,
                                 !.listening = TRUE]
                 ELSE [s0 EXCEPT !.toasts = toasts + 1]          \* "The microphone is in use…"
           mine == tasks[t].mine
           s2 == IF tasks[t].kind = "tap" THEN s1
                 ELSE IF ~claimed
                      THEN (IF s1.run = mine THEN [s1 EXCEPT !.holding = FALSE, !.wantOn = FALSE] ELSE s1)
                      ELSE IF Variant = "current" /\ (s1.run /= mine \/ (~s1.holding /\ ~s1.locked))
                           THEN StopS(s1)                        \* :43-44, the finger let go meanwhile
                           ELSE s1
       IN Put(s2, 10 + t, "PromptComposer.swift:345-372 + caller",
              IF claimed THEN "start resolves: claims, listens" ELSE "start resolves: claim fails, toast")

Done == ~down /\ ~pending /\ ~holding /\ \A t \in DOMAIN tasks : tasks[t].pc = "done"
Terminated == Done /\ UNCHANGED vars

Next ==
    \/ Down \/ Move \/ HoldFires \/ Taken \/ Send
    \/ \E o \in {"cancel", "lock"} : Slide(o)
    \/ \E o \in {"keep", "cancel", "lock"} : Up(o)
    \/ \E t \in Task : TaskRuns(t) \/ TaskResumes(t)
    \/ Terminated

Spec == Init /\ [][Next]_vars

\* ---- Properties ----

TypeOK ==
    /\ owner \in {"none", "D"} /\ listening \in BOOLEAN /\ holding \in BOOLEAN /\ locked \in BOOLEAN
    /\ running \subseteq 1..(MaxTasks + 1) /\ Len(tasks) <= MaxTasks

\* No engine keeps running unreferenced (a microphone left hot that nothing can stop).
NoLostEngine == running \subseteq {engine} /\ (engine /= 0 => engine \in running)

\* Once everything has settled, dictation is on exactly when the person last asked for it.
SettledAsAsked == Done => (listening = wantOn)

\* Nothing else owns the microphone in this model, so "the microphone is in use" is never true.
NoWrongToast == toasts = 0
=============================================================================

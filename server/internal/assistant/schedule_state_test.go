package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestTaskStateNoteEdit(t *testing.T) {
	now := time.Now().UTC()
	task := Task{Repeat: repeatDaily}
	if err := editTaskState(&task, Task{FreshEachRun: true}, map[string]json.RawMessage{
		"fresh_each_run": json.RawMessage("true"),
		"state_text":     json.RawMessage(`"last issue #12"`),
	}, now); err != nil {
		t.Fatal(err)
	}
	if !task.FreshEachRun || task.StateNote.Text != "last issue #12" || task.StateNote.Revision != 1 || !task.StateNote.UpdatedAt.Equal(now) {
		t.Fatalf("unexpected task state: %+v", task.StateNote)
	}
	if err := editTaskState(&task, Task{}, nil, now); err != nil || !task.FreshEachRun || task.StateNote.Revision != 1 {
		t.Fatal("an older client cleared a continuity setting")
	}
	oversize, _ := json.Marshal(strings.Repeat("界", 1366))
	if err := editTaskState(&task, Task{}, map[string]json.RawMessage{"state_text": oversize}, now); err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("oversize note: %v", err)
	}
	if task.StateNote.Revision != 1 {
		t.Fatal("an oversize note changed the saved revision")
	}
}

func TestTaskStateDraftCommitChecksSessionAndRevision(t *testing.T) {
	ctx := context.Background()
	s := newStorelessService(t, newFakeHub())
	task := Task{ID: "test", Repeat: repeatInterval, LastSessionID: "scheduled"}
	setTasks(s, task)

	stage := func(machine, session, text string) {
		task.stateRunKey = s.openTaskStateDraft()
		s.sched.mu.Lock()
		s.sched.stateDrafts[task.stateRunKey] = taskStateDraft{machine: machine, session: session, text: text}
		s.sched.mu.Unlock()
	}
	run := ScheduleRun{MachineID: "machine", SessionID: "scheduled"}
	stage("machine", "another", "wrong session")
	s.finishTaskState(ctx, task, run)
	current, _ := taskByID(s, task.ID)
	if current.StateNote != nil {
		t.Fatal("another conversation changed the task state")
	}
	stage("machine", "scheduled", "last issue #12")
	s.finishTaskState(ctx, task, run)
	current, _ = taskByID(s, task.ID)
	if current.StateNote == nil || current.StateNote.Text != "last issue #12" || current.StateNote.Revision != 1 {
		t.Fatalf("missing state: %+v", current.StateNote)
	}
	stage("machine", "scheduled", "stale update")
	s.finishTaskState(ctx, task, run)
	current, _ = taskByID(s, task.ID)
	if current.StateNote.Text != "last issue #12" || current.StateNote.Revision != 1 {
		t.Fatalf("stale run overwrote state: %+v", current.StateNote)
	}
}

func TestTaskStateFreshRunStartsNewConversationWithSavedNote(t *testing.T) {
	node := newFakeHub().withMachine()
	s := New(Options{Hub: node, Logf: testLogf(t)})
	setTasks(s, Task{
		ID: "patrol", Title: "Patrol", Prompt: "Check for changes", Repeat: repeatInterval,
		EveryMinutes: 30, Enabled: true, FreshEachRun: true,
		StateNote: &TaskStateNote{Text: "last issue #12", Revision: 1},
	})
	first, err := runNow(t, s, "patrol")
	if err != nil {
		t.Fatal(err)
	}
	second, err := runNow(t, s, "patrol")
	if err != nil {
		t.Fatal(err)
	}
	send := node.lastSend(t)
	if send["session_id"] != nil && send["session_id"] != "" || first.SessionID == second.SessionID {
		t.Fatalf("fresh run reused %q: %+v", first.SessionID, send)
	}
	if prompt := send["text"].(string); !strings.Contains(prompt, "last issue #12") || !strings.Contains(prompt, "schedule_state_set") {
		t.Fatalf("fresh run did not receive its note and write instructions: %q", prompt)
	}
}

func TestTaskStateSurvivesRestartAndSkippedRun(t *testing.T) {
	s, _, store := scheduleStorageFixture(t)
	body := `{"title":"Daily","prompt":"Example","repeat":"daily","time":"09:00","enabled":true,"fresh_each_run":true,"state_text":"last issue #12"}`
	if response := scheduleRequest(s, "PUT", "tasks/daily", body); response.Code != 200 {
		t.Fatalf("saving note: %d %s", response.Code, response.Body)
	}
	s.recordRun(ScheduleRun{RefID: "daily", Status: runSkipped})
	s.recordRun(ScheduleRun{RefID: "daily", Status: runFailed, SessionID: "failed"})
	assertScheduleStored(t, s, store)
	restarted := newServiceOn(t, newFakeHub(), store)
	task, ok := taskByID(restarted, "daily")
	if !ok || !task.FreshEachRun || task.StateNote == nil || task.StateNote.Text != "last issue #12" || task.StateNote.Revision != 1 {
		t.Fatalf("reloaded state: %+v", task)
	}
}

func TestTaskStateDiscardsDraftWhenRunCannotBeRegistered(t *testing.T) {
	ctx := t.Context()
	store := &flakyScheduleStore{data: map[string][]byte{}}
	task := Task{ID: "daily", Repeat: repeatDaily, StateNote: &TaskStateNote{Text: "previous cursor", Revision: 1}}
	if err := store.PutSetting(ctx, tasksKey, []Task{task}); err != nil {
		t.Fatal(err)
	}
	node := newFakeHub().withMachine()
	s := newServiceOn(t, node, store)
	task.stateRunKey = s.openTaskStateDraft()
	// The state instructions belong to the dispatched scheduled prompt (as the hub sends it, its source in front), not a
	// separate injected notice.
	now := time.Now().UTC()
	sent := prompts.MessageSourceText(taskPrompt(task, now, now, time.UTC, false, false, false), taskMessageSource(task))
	node.setItems(proto.Item{Role: "user", Kind: proto.KindText, Text: sent})
	args, _ := json.Marshal(map[string]string{"run_key": task.stateRunKey, "text": "new cursor"})
	result, err := s.toolScheduleStateSet(ctx, "server", "scheduled", args)
	if err != nil || !strings.Contains(result, "not yet saved") {
		t.Fatalf("staging an already dispatched turn: %q, %v", result, err)
	}
	// Fail registration after the dispatched turn has staged its state.
	store.failReads(true)
	s.startScheduledRun(ScheduleRun{RefID: task.ID, MachineID: "server", SessionID: "scheduled"}, task, &Device{ID: "server"})
	store.failReads(false)
	if _, err := s.toolScheduleStateSet(ctx, "server", "scheduled", args); err == nil || !strings.Contains(err.Error(), "no longer accepting state") {
		t.Fatalf("the unregistered run still accepts state: %v", err)
	}
	tasks, runs := scheduleState(s)
	if len(runs) != 0 || len(tasks) != 1 || tasks[0].StateNote == nil ||
		tasks[0].StateNote.Text != "previous cursor" || tasks[0].StateNote.Revision != 1 {
		t.Fatalf("reloaded tasks and runs after storage recovered: %+v, %+v", tasks, runs)
	}
}

func TestTaskStateDiscardsDraftWhenPreviousRunIsBusy(t *testing.T) {
	node := readyHub()
	node.setSessions("server", proto.Session{SessionID: "busy", Status: proto.StatusWorking, State: proto.StateWorking})
	s := newStorelessService(t, node)
	task := Task{ID: "patrol", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true, FreshEachRun: true,
		RunMachineID: "server", RunSessionID: "busy", RunLauncher: "claude"}
	sc := setTasks(s, task)
	now := time.Now()
	for _, manual := range []bool{false, true} {
		if _, err := s.runTask(task, now, now, manual, false); !errors.Is(err, errTaskBusy) {
			t.Fatalf("manual=%v: run did not skip the busy chat: %v", manual, err)
		}
		sc.mu.Lock()
		drafts := len(sc.stateDrafts)
		sc.mu.Unlock()
		if drafts != 0 {
			t.Fatalf("manual=%v: skipped run left %d state draft(s)", manual, drafts)
		}
	}
	_, runs := scheduleState(s)
	if len(runs) != 2 || runs[0].SessionID != "busy" || runs[1].SessionID != "busy" {
		t.Fatalf("skipped runs should still link to the busy chat: %+v", runs)
	}
}

func TestTaskStateFiveFreshRunsCarryOnlyNewFindings(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := newFakeHub().withMachine()
		s := New(Options{Hub: node, Logf: testLogf(t)})
		s.turnPoll = time.Second
		s.runRetry = time.Minute
		setTasks(s, Task{
			ID: "patrol", Title: "Patrol", Prompt: "Report only new issues", Repeat: repeatInterval,
			EveryMinutes: 30, Enabled: true, FreshEachRun: true,
		})
		seenSessions := map[string]bool{}
		for issue := 101; issue <= 105; issue++ {
			session, err := runNow(t, s, "patrol")
			if err != nil {
				t.Fatal(err)
			}
			if seenSessions[session.SessionID] {
				t.Fatalf("reused session %s", session.SessionID)
			}
			seenSessions[session.SessionID] = true
			prompt := node.lastSend(t)["text"].(string)
			if issue > 101 && !strings.Contains(prompt, fmt.Sprintf("last reported #%d", issue-1)) {
				t.Fatalf("run %d did not receive the prior cursor: %s", issue, prompt)
			}
			_, suffix, ok := strings.Cut(prompt, "call schedule_state_set with run_key ")
			if !ok {
				t.Fatalf("run %d lacks a state key", issue)
			}
			key, _, _ := strings.Cut(suffix, " ")
			user := proto.Item{ID: "scheduled", Role: "user", Kind: proto.KindText, Text: prompt}
			node.setItems(user)
			node.setSessions("server", proto.Session{SessionID: session.SessionID, Status: proto.StatusWorking, State: proto.StateWorking})
			synctest.Wait()
			<-time.After(2 * s.turnPoll)
			synctest.Wait()
			note := fmt.Sprintf("last reported #%d", issue)
			args, _ := json.Marshal(map[string]string{"run_key": key, "text": note})
			if _, err := s.runTool(t.Context(), "server", session.SessionID, "schedule_state_set", args); err != nil {
				t.Fatal(err)
			}
			staged, _ := taskByID(s, "patrol")
			if staged.StateNote.revision() != uint64(issue-101) {
				t.Fatal("a working turn committed its draft")
			}
			node.setItems(user, proto.Item{ID: "report", Role: "assistant", Kind: proto.KindText, Text: fmt.Sprintf("New issue #%d", issue)})
			node.setSessions("server", proto.Session{SessionID: session.SessionID, Status: proto.StatusDone, State: proto.StateIdle})
			<-time.After(3 * s.turnPoll)
			synctest.Wait()
			task, _ := taskByID(s, "patrol")
			if task.StateNote == nil || task.StateNote.Text != note || task.StateNote.Revision != uint64(issue-100) {
				t.Fatalf("run %d did not commit the cursor: %+v", issue, task.StateNote)
			}
		}
	})
}

func TestTaskStateRecurringFollowupLeavesMainChat(t *testing.T) {
	node := readyHub()
	node.settings = &Settings{Main: "main", Timezone: "UTC"}
	s := newStorelessService(t, node)
	before := len(s.tasksView(t.Context(), nil))
	if _, err := s.toolScheduleFollowup(t.Context(), "server", "main", json.RawMessage(
		`{"title":"Public issue patrol","prompt":"Report only changes","every":"30m"}`,
	)); err != nil {
		t.Fatal(err)
	}
	tasks := s.tasksView(t.Context(), nil)
	if len(tasks) != before+1 {
		t.Fatalf("expected one new task, got %d", len(tasks)-before)
	}
	for _, task := range tasks {
		if task.Title != "Public issue patrol" {
			continue
		}
		if task.SessionID != "" || task.FreshEachRun {
			t.Fatalf("expected the task's own reusable chat: %+v", task)
		}
		first, err := runNow(t, s, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		second, err := runNow(t, s, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if first.SessionID == "" || first.SessionID == "main" || second.SessionID != first.SessionID {
			t.Fatalf("recurring runs should share a task chat: first %q, second %q", first.SessionID, second.SessionID)
		}
		return
	}
	t.Fatal("the patrol was not saved")
}

// A scheduled task that keeps one conversation (not FreshEachRun) is a resumed session. Its prompts are injected, so
// the node's one-turn page does not stop at them and reaches back into the previous run's prompt, which carries the
// previous run's key. The key of the current turn must still be accepted, and the previous one still refused.
func TestTaskStateSetInResumedSessionReadsCurrentTurnKey(t *testing.T) {
	ctx := context.Background()
	node := newFakeHub().withMachine()
	s := newStorelessService(t, node)
	task := Task{ID: "patrol", Repeat: repeatInterval, EveryMinutes: 30}
	task.stateRunKey = s.openTaskStateDraft()
	previous := task
	previous.stateRunKey = s.openTaskStateDraft() // the previous run's draft is still open: only the prompt check can refuse it
	prompt := func(task Task) proto.Item {
		return proto.Item{Role: "user", Kind: proto.KindText, Text: hubRunPrompt(task.stateRunKey, "")}
	}
	node.pageTurns = true // the page starts at the last prompt the node counts, which a scheduled one is not
	node.setItems(prompt(previous), proto.Item{Role: "assistant", Kind: proto.KindText, Text: "done"}, prompt(task))

	stale, _ := json.Marshal(map[string]string{"run_key": previous.stateRunKey, "text": "stale"})
	if _, err := s.toolScheduleStateSet(ctx, "server", "scheduled", stale); err == nil || !strings.Contains(err.Error(), "current scheduled turn") {
		t.Fatalf("the previous turn's key was accepted: %v", err)
	}
	current, _ := json.Marshal(map[string]string{"run_key": task.stateRunKey, "text": "new cursor"})
	if result, err := s.toolScheduleStateSet(ctx, "server", "scheduled", current); err != nil || !strings.Contains(result, "not yet saved") {
		t.Fatalf("the current turn's key was refused: %q, %v", result, err)
	}
	other, _ := json.Marshal(map[string]string{"run_key": "0123456789abcdef0123456789abcdef", "text": "x"})
	if _, err := s.toolScheduleStateSet(ctx, "server", "scheduled", other); err == nil {
		t.Fatal("a key from no turn was accepted")
	}
}

func TestCurrentTaskStateRun(t *testing.T) {
	user := func(text string) proto.Item { return proto.Item{Role: "user", Kind: proto.KindText, Text: text} }
	run := func(key string) proto.Item { return user(hubRunPrompt(key, "")) }
	queued := user("another message")
	queued.Queued = true
	queuedRun := run("aaa")
	queuedRun.Queued = true
	peer := user("from another conversation")
	peer.From = "other"
	peerRun := user(hubRunPrompt("old", ""))
	peerRun.From = "other"
	app := user(hubRunPrompt("old", ""))
	app.ClientMsgID = "c1"
	woke := proto.Item{Role: "system", Kind: proto.KindNotice, Text: "background task finished", Wake: true}
	reply := proto.Item{Role: "assistant", Kind: proto.KindText, Text: "ok"}
	typed := []proto.Item{run("old"), reply, run("aaa"), reply, user(typedRunKey("old"))}
	both := user(runLineOf(hubRunPrompt("old", "")) + "\n" + runLineOf(hubRunPrompt("aaa", "")))
	legacy := user("⏰ Patrol（定时任务，每 30 分钟）" + taskStatePrompt(Task{Repeat: repeatInterval, stateRunKey: "aaa"}))
	for name, tc := range map[string]struct {
		items []proto.Item
		key   string
		want  bool
	}{
		"only turn":         {[]proto.Item{run("aaa")}, "aaa", true},
		"paged back a turn": {[]proto.Item{run("old"), run("aaa")}, "aaa", true},
		"previous key":      {[]proto.Item{run("old"), reply, run("aaa")}, "old", false},
		"queued is no turn": {[]proto.Item{run("old"), run("aaa"), queued}, "aaa", true},
		// #8913: what comes after the run's prompt goes on with the run.
		"later plain prompt":   {[]proto.Item{run("aaa"), reply, user("📋 督办：check the PR")}, "aaa", true},
		"woken by its notice":  {[]proto.Item{run("aaa"), reply, woke, reply}, "aaa", true},
		"another chat's words": {[]proto.Item{run("aaa"), reply, peer, reply}, "aaa", true},
		"queued run prompt":    {[]proto.Item{run("old"), reply, user("busy"), queuedRun}, "aaa", true},
		"next run's prompt":    {[]proto.Item{run("old"), reply, user("📋 督办"), reply, run("aaa")}, "old", false},
		"in no prompt here":    {[]proto.Item{run("aaa"), reply, user("x")}, "bbb", false},
		"no items":             {nil, "aaa", false},
		"prefix of the key":    {[]proto.Item{run("aaab")}, "aaa", false},
		// #9043: only a prompt the hub sent for a run hands out a key, whatever else carries its words.
		"the owner typed an earlier key":     {typed, "old", false},
		"the run's own key after that":       {typed, "aaa", true},
		"the owner pasted a run's line":      {[]proto.Item{run("aaa"), reply, user(runLineOf(hubRunPrompt("old", "")))}, "old", false},
		"both runs' lines in one message":    {[]proto.Item{run("old"), reply, run("aaa"), reply, both}, "old", false},
		"an app's message, whatever it says": {[]proto.Item{run("aaa"), reply, app}, "old", false},
		"the owner quoted a run's prompt": {[]proto.Item{run("aaa"), reply, user(prompts.ProtectUserMessage(hubRunPrompt("old", "")))},
			"old", false},
		"another chat sent a run's prompt": {[]proto.Item{run("aaa"), reply, peerRun}, "old", false},
		"an earlier key in the saved state": {[]proto.Item{run("old"), reply, user(hubRunPrompt("aaa", runLineOf(hubRunPrompt("old", ""))))},
			"old", false},
		"a run's line alone":                 {[]proto.Item{user(runLineOf(hubRunPrompt("aaa", "")))}, "aaa", false},
		"an older run's template, no source": {[]proto.Item{legacy}, "aaa", true},
	} {
		if got := currentTaskStateRun(tc.items, tc.key); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

// #8913: a run's turn ends while a subagent it launched async still runs. An assistant conversation then reads as done,
// not working, and its row lists no subagents; the subagent's notice wakes it later, and a supervision message and
// another conversation's message come in between. The run stages its state with its own key once all that is in; the
// draft stays open until the subagent has reported back and the conversation is idle, and then the state is saved. An
// earlier run's key (its draft still open: only the key check can refuse it), a key no prompt in this conversation
// carries, and the run's own key after the run is over are refused.
func TestTaskStateRunGoesOnAfterItsBackgroundWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := newFakeHub().withMachine()
		node.pageTurns = true
		s := New(Options{Hub: node, Logf: testLogf(t)})
		s.turnPoll = time.Second
		s.runRetry = time.Minute
		setTasks(s, Task{ID: "patrol", Title: "Patrol", Prompt: "Report only new issues", Repeat: repeatInterval, EveryMinutes: 120, Enabled: true})
		earlier := Task{ID: "patrol", Repeat: repeatInterval}
		earlier.stateRunKey = s.openTaskStateDraft()
		elsewhere := s.openTaskStateDraft() // a run in another conversation: its prompt is not here
		session, err := runNow(t, s, "patrol")
		if err != nil {
			t.Fatal(err)
		}
		sid := session.SessionID
		prompt := node.lastSend(t)["text"].(string)
		_, suffix, ok := strings.Cut(prompt, "call schedule_state_set with run_key ")
		if !ok {
			t.Fatal("the run's prompt lacks a state key")
		}
		key, _, _ := strings.Cut(suffix, " ")
		stage := func(key, text string) (string, error) {
			args, _ := json.Marshal(map[string]string{"run_key": key, "text": text})
			return s.runTool(t.Context(), "server", sid, "schedule_state_set", args)
		}
		idle := proto.Session{SessionID: sid, Assistant: true, Status: proto.StatusDone, State: proto.StateUnread}
		working := proto.Session{SessionID: sid, Assistant: true, Status: proto.StatusWorking, State: proto.StateWorking}
		items := []proto.Item{
			{ID: "earlier", Role: "user", Kind: proto.KindText, Text: hubRunPrompt(earlier.stateRunKey, "")},
			{ID: "earlier-report", Role: "assistant", Kind: proto.KindText, Text: "New issue #100"},
			{ID: "run", Role: "user", Kind: proto.KindText, Text: prompt},
			{ID: "launch", Role: "assistant", Kind: proto.KindToolUse, ToolName: "Agent", ToolUseID: "toolu_a1"},
			{ID: "launched", Role: "user", Kind: proto.KindToolResult, ToolUseID: "toolu_a1",
				Text: "Async agent launched successfully. (This tool result is internal metadata.)\nagentId: a1 (internal ID)"},
		}
		node.setItems(items...)
		node.setSessions("server", working)
		synctest.Wait()
		<-time.After(2 * s.turnPoll)
		synctest.Wait()

		// The turn ends; the subagent runs on.
		items = append(items, proto.Item{ID: "wait", Role: "assistant", Kind: proto.KindText, Text: "Waiting for the subagent."})
		node.setItems(items...)
		node.setSessions("server", idle)
		<-time.After(20 * s.turnPoll)
		synctest.Wait()
		s.sched.mu.Lock()
		_, open := s.sched.stateDrafts[key]
		s.sched.mu.Unlock()
		if !open {
			t.Fatal("the run was over while its background work still ran")
		}

		// A supervision message and another conversation's message come in, then the subagent's notice wakes it.
		items = append(items,
			proto.Item{ID: "supervision", Role: "user", Kind: proto.KindText, Text: "📋 督办：how far is the patrol?"},
			proto.Item{ID: "answer", Role: "assistant", Kind: proto.KindText, Text: "Still waiting."},
			proto.Item{ID: "peer", Role: "user", Kind: proto.KindText, Text: "the build is green", From: "other"},
			proto.Item{ID: "thanks", Role: "assistant", Kind: proto.KindText, Text: "Noted."},
		)
		node.setItems(items...)
		<-time.After(6 * s.turnPoll) // idle again in between: the subagent has not reported back, the run goes on
		synctest.Wait()
		items = append(items, proto.Item{ID: "notice", Role: "system", Kind: proto.KindNotice, Wake: true, Text: "subagent finished",
			Task: &proto.TaskNote{Kind: "agent", Status: "completed", TaskID: "a1", ToolUseID: "toolu_a1"}})
		node.setItems(items...)
		node.setSessions("server", working)
		synctest.Wait()

		if _, err := stage(earlier.stateRunKey, "stale"); err == nil || !strings.Contains(err.Error(), "current scheduled turn") {
			t.Fatalf("an earlier run's key was accepted: %v", err)
		}
		if _, err := stage(elsewhere, "elsewhere"); err == nil || !strings.Contains(err.Error(), "current scheduled turn") {
			t.Fatalf("a key from no prompt here was accepted: %v", err)
		}
		if result, err := stage(key, "last reported #101"); err != nil || !strings.Contains(result, "not yet saved") {
			t.Fatalf("the run's key was refused after its background work woke it: %q, %v", result, err)
		}
		if task, _ := taskByID(s, "patrol"); task.StateNote != nil {
			t.Fatal("a run still at work saved its state")
		}

		items = append(items, proto.Item{ID: "report", Role: "assistant", Kind: proto.KindText, Text: "New issue #101"})
		node.setItems(items...)
		node.setSessions("server", idle)
		<-time.After(4 * s.turnPoll)
		synctest.Wait()
		task, _ := taskByID(s, "patrol")
		if task.StateNote == nil || task.StateNote.Text != "last reported #101" {
			t.Fatalf("the run's state was not saved: %+v", task.StateNote)
		}
		if _, err := stage(key, "late"); err == nil || !strings.Contains(err.Error(), "no longer accepting") {
			t.Fatalf("a run that is over took state: %v", err)
		}
	})
}

// A run that staged its state before its turn ended is saved then, as before #8913, even with background work left
// running: it does not wait for that work.
func TestTaskStateStagedBeforeTheStopIsSavedAtTheStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := newFakeHub().withMachine()
		s := New(Options{Hub: node, Logf: testLogf(t)})
		s.turnPoll = time.Second
		s.runRetry = time.Minute
		setTasks(s, Task{ID: "patrol", Title: "Patrol", Prompt: "Report only new issues", Repeat: repeatInterval, EveryMinutes: 120, Enabled: true})
		session, err := runNow(t, s, "patrol")
		if err != nil {
			t.Fatal(err)
		}
		prompt := node.lastSend(t)["text"].(string)
		_, suffix, _ := strings.Cut(prompt, "call schedule_state_set with run_key ")
		key, _, _ := strings.Cut(suffix, " ")
		items := []proto.Item{
			{ID: "run", Role: "user", Kind: proto.KindText, Text: prompt},
			{ID: "launched", Role: "user", Kind: proto.KindToolResult, ToolUseID: "toolu_b1",
				Text: "Command running in background with ID: b1. Output is being written to: /tmp/b1.output"},
		}
		node.setItems(items...)
		node.setSessions("server", proto.Session{SessionID: session.SessionID, Assistant: true, Status: proto.StatusWorking, State: proto.StateWorking})
		synctest.Wait()
		<-time.After(2 * s.turnPoll)
		synctest.Wait()
		args, _ := json.Marshal(map[string]string{"run_key": key, "text": "cursor 7"})
		if _, err := s.runTool(t.Context(), "server", session.SessionID, "schedule_state_set", args); err != nil {
			t.Fatal(err)
		}
		node.setItems(append(items, proto.Item{ID: "done", Role: "assistant", Kind: proto.KindText, Text: "Done; the command runs on."})...)
		node.setSessions("server", proto.Session{SessionID: session.SessionID, Assistant: true, Status: proto.StatusDone, State: proto.StateUnread})
		<-time.After(4 * s.turnPoll)
		synctest.Wait()
		if task, _ := taskByID(s, "patrol"); task.StateNote == nil || task.StateNote.Text != "cursor 7" {
			t.Fatalf("a run that staged before its stop was not saved at the stop: %+v", task.StateNote)
		}
	})
}

func TestRunBackground(t *testing.T) {
	user := func(text string) proto.Item { return proto.Item{Role: "user", Kind: proto.KindText, Text: text} }
	prompt := func(key string) proto.Item { return user(hubRunPrompt(key, "")) }
	launched := func(id, text string) proto.Item {
		return proto.Item{Role: "user", Kind: proto.KindToolResult, ToolUseID: id, Text: text}
	}
	ended := func(id, status string) proto.Item {
		return proto.Item{Role: "system", Kind: proto.KindNotice, Task: &proto.TaskNote{Status: status, ToolUseID: id}}
	}
	agent := launched("a", "Async agent launched successfully.\nagentId: a1 (internal ID)")
	shell := launched("b", "Command running in background with ID: b1. Output is being written to: /tmp/b1")
	moved := launched("c", "Command was moved to the background (ID: c1) after 2m")
	monitor := launched("m", "Monitor started (task m1, expires in 30m unless stopped)")
	plain := launched("p", "ok")
	for name, tc := range map[string]struct {
		items []proto.Item
		want  int
	}{
		"none":                  {[]proto.Item{prompt("k"), plain}, 0},
		"an async subagent":     {[]proto.Item{prompt("k"), agent}, 1},
		"commands":              {[]proto.Item{prompt("k"), shell, moved}, 2},
		"reported back":         {[]proto.Item{prompt("k"), agent, shell, ended("a", "completed"), ended("b", "failed")}, 0},
		"still running":         {[]proto.Item{prompt("k"), agent, ended("a", "running")}, 1},
		"a monitor is no end":   {[]proto.Item{prompt("k"), monitor}, 0},
		"before the run":        {[]proto.Item{agent, prompt("k")}, 0},
		"another run's work":    {[]proto.Item{prompt("old"), agent}, 0},
		"no prompt":             {[]proto.Item{agent}, 0},
		"after a later message": {[]proto.Item{prompt("k"), agent, user("📋 督办"), shell}, 2},
		// #9043: the run starts at its prompt, not at a message that only carries a key's words.
		"after the run's key typed": {[]proto.Item{prompt("k"), agent, user(typedRunKey("k")), shell}, 2},
		"after another key typed":   {[]proto.Item{prompt("k"), agent, user(typedRunKey("old"))}, 1},
	} {
		if got := runBackground(tc.items, "k"); got != tc.want {
			t.Errorf("%s: got %d, want %d", name, got, tc.want)
		}
	}
}

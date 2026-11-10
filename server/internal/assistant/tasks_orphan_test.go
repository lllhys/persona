package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Open tasks nobody works on (tasks_orphan.go).

// closedAgent is s1 on box with its tab closed, last active at.
func closedAgent(at time.Time) proto.Session {
	return proto.Session{SessionID: "s1", MachineID: "box", Cwd: "/code/app", Agent: "claude", Status: proto.StatusOffline,
		State: proto.StateOffline, StartedByPersona: true, UpdatedAt: at, LastText: "API Error: 400"}
}

func TestAgentGone(t *testing.T) {
	now := time.Now()
	wake, reset := now.Add(time.Hour), now.Add(2*time.Hour)
	cases := []struct {
		name string
		edit func(*orphanAgent)
		want string
	}{
		{"closed", func(*orphanAgent) {}, orphanClosed},
		{"closed after a model error", func(a *orphanAgent) { a.c.TurnError = "API Error: 400 invalid request" }, orphanError},
		{"computer offline", func(a *orphanAgent) { a.reachable = false }, orphanOffline},
		{"no longer listed", func(a *orphanAgent) { a.listed = false }, orphanClosed},
		{"open in a tab, stopped", func(a *orphanAgent) { a.c.Status, a.c.PaneID = proto.StatusIdle, "w1:p1" }, ""},
		{"in a host without a pane", func(a *orphanAgent) { a.c.Status, a.c.State, a.c.Host = proto.StatusIdle, proto.StateIdle, "paseo" }, ""},
		{"state unknown in a pane", func(a *orphanAgent) {
			a.c.Status, a.c.State, a.c.PaneID = proto.StatusUnknown, proto.StateUnknown, "w1:p1"
		}, ""},
		// Closed is no host and no pane, whatever the status says (sessionClosed).
		{"hostless, interrupted / unknown", func(a *orphanAgent) { a.c.Status, a.c.State = proto.StatusInterrupted, proto.StateUnknown }, orphanClosed},
		{"hostless, idle / idle", func(a *orphanAgent) { a.c.Status, a.c.State = proto.StatusIdle, proto.StateIdle }, orphanClosed},
		{"hostless, unknown / unknown", func(a *orphanAgent) { a.c.Status, a.c.State = proto.StatusUnknown, proto.StateUnknown }, orphanClosed},
		{"hostless, interrupted, after a model error", func(a *orphanAgent) {
			a.c.Status, a.c.State, a.c.TurnError = proto.StatusInterrupted, proto.StateUnknown, "API Error: 400 invalid request"
		}, orphanError},
		{"hostless, interrupted, a wake is due", func(a *orphanAgent) {
			a.c.Status, a.c.State = proto.StatusInterrupted, proto.StateUnknown
			a.c.Work = &proto.WorkState{Phase: proto.WorkDone, WakeAt: &wake}
		}, ""},
		{"hostless, interrupted, a continue is pending", func(a *orphanAgent) {
			a.c.Status, a.c.State, a.c.TurnError = proto.StatusInterrupted, proto.StateUnknown, "overloaded"
			a.c.Work = &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitTransient, WakeAt: &wake}
		}, ""},
		{"working", func(a *orphanAgent) { a.c.Status, a.c.State = proto.StatusWorking, proto.StateWorking }, ""},
		{"starting", func(a *orphanAgent) { a.c.Starting = true }, ""},
		{"assistant conversation", func(a *orphanAgent) { a.c.Assistant = true }, ""},
		{"a card waits", func(a *orphanAgent) { a.c.PendingInteractions = 1 }, ""},
		{"needs you", func(a *orphanAgent) { a.c.State = proto.StateNeedsYou }, ""},
		{"queued input", func(a *orphanAgent) { a.c.Queued = []string{"go on"} }, ""},
		{"a wake is due", func(a *orphanAgent) { a.c.Work = &proto.WorkState{Phase: proto.WorkDone, WakeAt: &wake} }, ""},
		{"a continue is pending", func(a *orphanAgent) {
			a.c.TurnError = "overloaded"
			a.c.Work = &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitTransient, WakeAt: &wake}
		}, ""},
		{"waits for CI", func(a *orphanAgent) { a.c.Work = &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal} }, ""},
		{"waits out a usage limit", func(a *orphanAgent) {
			a.c.TurnError, a.c.TurnErrorKind, a.c.TurnErrorResetsAt = "usage limit", proto.TurnErrorUsageLimit, &reset
		}, ""},
		{"work done, nothing pending", func(a *orphanAgent) { a.c.Work = &proto.WorkState{Phase: proto.WorkDone} }, orphanClosed},
	}
	for _, c := range cases {
		a := orphanAgent{machine: "box", session: "s1", reachable: true, listed: true, c: closedAgent(now.Add(-time.Hour))}
		c.edit(&a)
		if got := agentGone(a, now); got != c.want {
			t.Errorf("%s: agentGone = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTaskOrphan(t *testing.T) {
	now := time.Now()
	done := now.Add(-time.Hour)
	gone := func(session string, edit func(*orphanAgent)) orphanAgent {
		a := orphanAgent{machine: "box", session: session, reachable: true, listed: true, c: closedAgent(now.Add(-time.Hour))}
		a.c.SessionID = session
		if edit != nil {
			edit(&a)
		}
		return a
	}
	failed := gone("s2", func(a *orphanAgent) { a.c.TurnError = "API Error: 400 invalid request" })
	away := gone("s3", func(a *orphanAgent) { a.reachable = false })
	open := gone("s4", func(a *orphanAgent) { a.c.Status, a.c.PaneID = proto.StatusIdle, "w1:p1" })
	cases := []struct {
		name   string
		todo   Todo
		agents []orphanAgent
		want   string // "": not orphaned
	}{
		{"its only agent is closed", Todo{}, []orphanAgent{gone("s1", nil)}, orphanClosed},
		{"its only agent ended on an error", Todo{}, []orphanAgent{failed}, orphanError},
		{"its computer is away", Todo{}, []orphanAgent{away}, orphanOffline},
		{"the error is said before the rest", Todo{}, []orphanAgent{gone("s1", nil), away, failed}, orphanError},
		{"away is said before closed", Todo{}, []orphanAgent{gone("s1", nil), away}, orphanOffline},
		{"one agent is still open", Todo{}, []orphanAgent{failed, open}, ""},
		{"no agent yet", Todo{}, nil, ""},
		{"done", Todo{DoneAt: &done}, []orphanAgent{failed}, ""},
		{"waits for its review", Todo{Supervision: &Supervision{State: reviewPending}}, []orphanAgent{failed}, ""},
		{"waits for the user's decision", Todo{Supervision: &Supervision{State: reviewNeedsUser}}, []orphanAgent{failed}, ""},
		{"in rework", Todo{Supervision: &Supervision{State: reviewRework}}, []orphanAgent{failed}, orphanError},
	}
	for _, c := range cases {
		o, ok := taskOrphan(c.todo, c.agents, now)
		if ok != (c.want != "") || o.Reason != c.want {
			t.Errorf("%s: taskOrphan = %q %v, want %q", c.name, o.Reason, ok, c.want)
		}
	}

	// The stretch is named by the agents and their last step, not by why they are gone: a closed agent whose
	// computer then goes away is the same stretch, a new step is another.
	a, _ := taskOrphan(Todo{}, []orphanAgent{gone("s1", nil)}, now)
	b, _ := taskOrphan(Todo{}, []orphanAgent{gone("s1", func(a *orphanAgent) { a.c.TurnError = "API Error: 400" })}, now)
	moved, _ := taskOrphan(Todo{}, []orphanAgent{gone("s1", func(a *orphanAgent) { a.c.UpdatedAt = now })}, now)
	if a.key == "" || a.key != b.key || a.key == moved.key {
		t.Fatalf("keys: %q %q %q", a.key, b.key, moved.key)
	}
	if b.detail == "" || b.session != "s1" {
		t.Fatalf("the error's agent: %+v", b)
	}
}

// orphanTask is a supervised task of conv1 with its agent s1 on box.
func orphanTask(t *testing.T, s *Service) Todo {
	t.Helper()
	ctx := context.Background()
	todo, err := s.createTodo(ctx, "Example task", "Collect task evidence.", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	s.superviseTodo(ctx, todo.ID, "conv1", []string{"Tests pass"}, 30)
	if err := s.linkTodoAgent(ctx, todo.ID, "box", "s1", "Example task", "/code/app", "claude"); err != nil {
		t.Fatal(err)
	}
	return todoNow(s, todo.ID)
}

// listedOrphan is what GET /api/v1/todos says of to-do id's `orphaned`.
func listedOrphan(t *testing.T, s *Service, id string) *TodoOrphan {
	t.Helper()
	w := serve(s, "GET", "/api/v1/todos", "")
	var body struct {
		Todos []struct {
			ID       string      `json:"id"`
			Orphaned *TodoOrphan `json:"orphaned"`
		} `json:"todos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("todos: %d %s: %v", w.Code, w.Body, err)
	}
	for _, td := range body.Todos {
		if td.ID == id {
			return td.Orphaned
		}
	}
	t.Fatalf("todo %s not listed: %s", id, w.Body)
	return nil
}

// An open task whose only agent ended on a model error and was closed reads as orphaned on the to-do and in the
// snapshot, and its supervisor is told once for that stretch; an agent back at work ends it.
func TestOrphanedTaskIsFlaggedAndToldOnce(t *testing.T) {
	ctx := context.Background()
	s, f := newQuietService(t)
	s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour} // no message goes out while the test looks
	todo := orphanTask(t, s)
	now := time.Now()

	working := closedAgent(now.Add(-time.Hour))
	working.Status, working.State, working.PaneID = proto.StatusWorking, proto.StateWorking, "w1:p1"
	f.setSessions("box", working)
	s.orphanWatch(ctx, now)
	if o := listedOrphan(t, s, todo.ID); o != nil {
		t.Fatalf("a task with a working agent is orphaned: %+v", o)
	}

	failed := closedAgent(now.Add(-time.Hour))
	failed.TurnError = "API Error: 400 invalid request"
	f.setSessions("box", failed)
	s.orphanWatch(ctx, now)
	o := listedOrphan(t, s, todo.ID)
	if o == nil || o.Reason != orphanError || o.Since.IsZero() {
		t.Fatalf("orphaned = %+v", o)
	}
	var line prompts.SnapshotTask
	for _, task := range s.snapshotTasks(ctx, "") {
		if task.ID == todo.ID {
			line = task
		}
	}
	if line.Orphan != orphanError {
		t.Fatalf("snapshot task = %+v", line)
	}

	id := todo.ID + "|" + sigOrphaned
	pending := func() (superviseSignal, bool) {
		s.supervise.mu.Lock()
		defer s.supervise.mu.Unlock()
		g, ok := s.supervise.pending[id]
		return g, ok
	}
	s.superviseWatch(ctx, now)
	g, ok := pending()
	if !ok || g.Orphan != orphanError || g.Session != "s1" || !strings.Contains(g.Detail, "400") {
		t.Fatalf("signal = %+v %v", g, ok)
	}
	e, ok := s.digestEntry(ctx, g, "")
	if !ok || e.Orphan != orphanError {
		t.Fatalf("digest entry = %+v %v", e, ok)
	}
	if text := prompts.RenderSupervise(prompts.Supervise{Entries: []prompts.SuperviseEntry{e}}); !strings.Contains(text, "没有 Agent 在做了") { // i18n-ignore: the supervisor's prompt
		t.Fatalf("the message does not say it:\n%s", text)
	}
	// Told: the same stretch is not told again, whatever the later passes find.
	s.supervise.mu.Lock()
	delete(s.supervise.pending, id)
	s.supervise.sent[id] = g.Key
	s.supervise.mu.Unlock()
	for range 3 {
		s.orphanWatch(ctx, now.Add(time.Minute))
		s.superviseWatch(ctx, now.Add(time.Minute))
	}
	if g, ok := pending(); ok {
		t.Fatalf("told again within the stretch: %+v", g)
	}

	// Resumed, it is no longer orphaned, and what waited no longer holds.
	f.setSessions("box", working)
	s.orphanWatch(ctx, now.Add(2*time.Minute))
	if o := listedOrphan(t, s, todo.ID); o != nil {
		t.Fatalf("still orphaned with its agent back: %+v", o)
	}
	if _, ok := s.digestEntry(ctx, g, ""); ok {
		t.Fatal("the signal still holds with its agent back")
	}
	// Closed again after new work: another stretch, told again.
	again := closedAgent(now.Add(3 * time.Minute))
	f.setSessions("box", again)
	s.orphanWatch(ctx, now.Add(4*time.Minute))
	s.superviseWatch(ctx, now.Add(4*time.Minute))
	if g2, ok := pending(); !ok || g2.Key == g.Key || g2.Orphan != orphanClosed {
		t.Fatalf("the next stretch: %+v %v", g2, ok)
	}
}

// A computer that drops for a moment orphans nothing; one away for long does. A task not supervised is flagged on the
// to-do and tells nobody.
func TestOrphanedByAnOfflineComputer(t *testing.T) {
	ctx := context.Background()
	s, f := newQuietService(t)
	todo, err := s.createTodo(ctx, "Example task", "", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.linkTodoAgent(ctx, todo.ID, "box", "s1", "Example task", "/code/app", "claude"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.devices = []Device{{ID: "box", Name: "box"}} // not reachable
	f.mu.Unlock()
	now := time.Now()
	s.orphanWatch(ctx, now)
	if o := listedOrphan(t, s, todo.ID); o != nil {
		t.Fatalf("orphaned the moment its computer dropped: %+v", o)
	}
	s.orphanWatch(ctx, now.Add(orphanOfflineAfter+time.Minute))
	if o := listedOrphan(t, s, todo.ID); o == nil || o.Reason != orphanOffline {
		t.Fatalf("orphaned = %+v", o)
	}
	s.superviseWatch(ctx, now.Add(orphanOfflineAfter+time.Minute))
	if n := pendingSignals(s); n != 0 {
		t.Fatalf("%d signals for a task nobody supervises", n)
	}
	// Done, it is not listed as orphaned even before the next pass.
	if ok, err := s.autoDoneTodo(ctx, todo.ID, "done", time.Time{}); err != nil || !ok {
		t.Fatalf("auto done: %v %v", ok, err)
	}
	if o := listedOrphan(t, s, todo.ID); o != nil {
		t.Fatalf("a done to-do is orphaned: %+v", o)
	}
}

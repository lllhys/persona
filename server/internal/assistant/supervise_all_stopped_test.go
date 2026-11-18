package assistant

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

func stoppedFixture(now time.Time) (Todo, []proto.Session) {
	at := now.Add(-superviseAllStoppedAfter)
	t := Todo{ID: "task", Title: "Example task", Supervision: &Supervision{Supervisor: "supervisor", ExpectMinutes: 1440}}
	var sessions []proto.Session
	for _, id := range []string{"first", "second"} {
		t.Agents = append(t.Agents, TodoAgent{MachineID: "box", SessionID: id})
		sessions = append(sessions, proto.Session{MachineID: "box", SessionID: id, State: proto.StateUnread,
			Status: proto.StatusDone, StateSince: at, UpdatedAt: at, LastText: "Waiting for the other agent",
			Work: &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitAgent, Wait: "the other agent's review", TurnAt: at}})
	}
	return t, sessions
}

func TestAllStoppedSignal(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	for _, tc := range []struct {
		name string
		edit func(*Todo, []proto.Session)
		want bool
	}{
		{"at thirty minutes", func(*Todo, []proto.Session) {}, true},
		{"twenty nine minutes", func(_ *Todo, a []proto.Session) { a[1].UpdatedAt = now.Add(-29 * time.Minute) }, false},
		{"new state", func(_ *Todo, a []proto.Session) { a[1].StateSince = now.Add(-time.Minute) }, false},
		{"new activity", func(_ *Todo, a []proto.Session) { at := now.Add(-time.Minute); a[1].ActivityAt = &at }, false},
		{"raw working", func(_ *Todo, a []proto.Session) { a[1].Status = proto.StatusWorking }, false},
		{"working", func(_ *Todo, a []proto.Session) { a[1].State = proto.StateWorking }, false},
		{"future wake", func(_ *Todo, a []proto.Session) { a[1].Work.WakeAt = &future }, false},
		{"wake just expired", func(_ *Todo, a []proto.Session) { a[1].Work.WakeAt = &now }, false},
		{"pending interaction", func(_ *Todo, a []proto.Session) { a[1].PendingInteractions = 1 }, false},
		{"needs you", func(_ *Todo, a []proto.Session) { a[1].State = proto.StateNeedsYou }, false},
		{"work card", func(_ *Todo, a []proto.Session) { a[1].Work.InteractionID = "question" }, false},
		{"awaits reply", func(_ *Todo, a []proto.Session) { a[1].AwaitsReply = true }, false},
		{"queued input", func(_ *Todo, a []proto.Session) { a[1].Queued = []string{"Continue"} }, false},
		{"loop", func(_ *Todo, a []proto.Session) { a[1].Loop = &proto.LoopInfo{NextAt: future} }, false},
		{"self resumes", func(_ *Todo, a []proto.Session) { a[1].TurnErrorResumes = true }, false},
		{"offline", func(_ *Todo, a []proto.Session) { a[1].Status = proto.StatusOffline; a[1].State = proto.StateOffline }, false},
		{"missing", func(_ *Todo, a []proto.Session) { a[1].SessionID = "other" }, false},
		{"parked", func(task *Todo, _ []proto.Session) {
			task.Supervision.State = reviewParked
			task.Supervision.Reviews = []TaskReview{{Verdict: reviewParked, Until: future}}
		}, false},
		{"done", func(task *Todo, _ []proto.Session) { task.DoneAt = &now }, false},
		{"unsupervised", func(task *Todo, _ []proto.Session) { task.Supervision = nil }, false},
		{"needs user without a card", func(task *Todo, _ []proto.Session) { task.Supervision.State = reviewNeedsUser }, true},
		{"needs user with a card", func(task *Todo, a []proto.Session) {
			task.Supervision.State = reviewNeedsUser
			a[0].PendingInteractions = 1
		}, false},
		{"assistant leader", func(_ *Todo, a []proto.Session) { a[0].Assistant = true }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, agents := stoppedFixture(now)
			tc.edit(&task, agents)
			g, ok := allStoppedSignal(task, nil, agents, now)
			if ok != tc.want {
				t.Fatalf("signal=%+v, holds=%v; want %v", g, ok, tc.want)
			}
			watch := watchSignals(task, agents, now)
			if got := slices.ContainsFunc(watch, func(g superviseSignal) bool { return g.Kind == sigAllStopped }); got != tc.want {
				t.Fatalf("watch signals=%+v, want all_stopped=%v", watch, tc.want)
			}
			if ok && (g.Minutes != 30 || !g.urgent() || !strings.Contains(g.Detail, "box/first: agent: the other agent's review") || !strings.Contains(g.Detail, "box/second:")) {
				t.Fatalf("incomplete signal: %+v", g)
			}
		})
	}
}

func TestAllStoppedBackgroundActivity(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	old, fresh, future := now.Add(-time.Hour), now.Add(-29*time.Minute), now.Add(time.Minute)
	for _, tc := range []struct {
		name string
		edit func(*proto.Session)
		want bool
	}{
		{"recent shell", func(c *proto.Session) { c.Background = []proto.BackgroundTask{{Kind: "shell", StartedAt: fresh}} }, false},
		{"shell at thirty minutes", func(c *proto.Session) {
			c.Background = []proto.BackgroundTask{{Kind: "shell", StartedAt: now.Add(-30 * time.Minute)}}
		}, true},
		{"active long subagent", func(c *proto.Session) {
			c.Subagents = []proto.Subagent{{Running: true, StartedAt: old, UpdatedAt: fresh}}
		}, false},
		{"silent subagent", func(c *proto.Session) {
			c.Subagents = []proto.Subagent{{Running: true, StartedAt: old, UpdatedAt: old}}
		}, true},
		{"active workflow agent", func(c *proto.Session) {
			c.Workflows = []proto.Workflow{{Running: true, StartedAt: old, Agents: []proto.WorkflowAgent{{Running: true, UpdatedAt: fresh}}}}
		}, false},
		{"silent workflow", func(c *proto.Session) {
			c.Workflows = []proto.Workflow{{Running: true, StartedAt: old, UpdatedAt: old}}
		}, true},
		{"monitor will wake", func(c *proto.Session) {
			c.Background = []proto.BackgroundTask{{Kind: "monitor", StartedAt: old, ExpiresAt: &future}}
		}, false},
		{"expired monitor", func(c *proto.Session) {
			c.Background = []proto.BackgroundTask{{Kind: "monitor", StartedAt: old, ExpiresAt: &now}}
		}, true},
		{"background inferred state", func(c *proto.Session) {
			c.State = proto.StateWorking
			c.BackgroundWorking = true
			c.Background = []proto.BackgroundTask{{Kind: "shell", StartedAt: old}}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, agents := stoppedFixture(now)
			tc.edit(&agents[0])
			g, ok := allStoppedSignal(task, nil, agents, now)
			if ok != tc.want {
				t.Fatalf("signal=%+v, holds=%v; want %v", g, ok, tc.want)
			}
			if ok {
				next, holds := allStoppedSignal(task, nil, agents, now.Add(time.Minute))
				if !holds || g.Key != next.Key {
					t.Fatalf("quiet background changed the key: %q -> %q", g.Key, next.Key)
				}
			}
		})
	}
}

func TestAllStoppedGroupAndRearm(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	root, agents := stoppedFixture(now)
	root.Agents = root.Agents[:1]
	root.Agents[0].Source = sourceLeader
	worker := Todo{ID: "worker", Supervision: &Supervision{Supervisor: "box/first"}, Agents: []TodoAgent{{MachineID: "box", SessionID: "second", Source: sourceWorker, Via: root.ID}}}
	tasks := []Todo{root, worker}
	g, ok := allStoppedSignal(root, tasks, agents, now)
	w, wok := allStoppedSignal(worker, tasks, agents, now)
	if !ok || !wok || g.Key != w.Key || g.Todo == w.Todo {
		t.Fatalf("group signal root=%+v/%v worker=%+v/%v", g, ok, w, wok)
	}
	agents[1].Status, agents[1].State = proto.StatusWorking, proto.StateWorking
	if _, ok := allStoppedSignal(root, tasks, agents, now); ok {
		t.Fatal("reported a stopped leader while its worker runs")
	}
	agents[1].Status, agents[1].State = proto.StatusDone, proto.StateIdle
	agents[1].UpdatedAt, agents[1].StateSince = now, now
	if _, ok := allStoppedSignal(root, tasks, agents, now.Add(29*time.Minute)); ok {
		t.Fatal("rearmed before the last participant stopped for thirty minutes")
	}
	resumed, ok := allStoppedSignal(root, tasks, agents, now.Add(30*time.Minute))
	if !ok || resumed.Key == g.Key {
		t.Fatalf("did not rearm: %+v/%v", resumed, ok)
	}
	reversed := []proto.Session{agents[1], agents[0]}
	order, _ := allStoppedSignal(root, tasks, reversed, now.Add(31*time.Minute))
	if resumed.Key != order.Key {
		t.Fatal("list order or elapsed minutes changed the key")
	}
	// Persisted semantic dedup also distinguishes a new stretch with identical words.
	entry := prompts.SuperviseEntry{Detail: g.Detail}
	_, firstHash := digestFingerprint(entry, g)
	_, nextHash := digestFingerprint(entry, resumed)
	if firstHash == nextHash {
		t.Fatal("a new stretch was folded into an earlier one with the same words")
	}
	child := proto.Session{MachineID: "box", SessionID: "runtime-child", ParentSessionID: "second", State: proto.StateWorking, UpdatedAt: now.Add(30 * time.Minute)}
	if _, ok := allStoppedSignal(root, tasks, append(reversed, child), now.Add(31*time.Minute)); ok {
		t.Fatal("ignored a running runtime child")
	}
}

func TestAllStoppedCurrentMembers(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	task, agents := stoppedFixture(now)
	task.Agents[0].LinkedAt = now.Add(-2 * time.Hour)
	task.Agents[1].LinkedAt = now.Add(-time.Hour)
	agents[0].State, agents[0].Status = proto.StateOffline, proto.StatusOffline
	g, ok := allStoppedSignal(task, nil, agents, now)
	if !ok || strings.Contains(g.Key, "first") {
		t.Fatalf("superseded agent remained in group: %+v/%v", g, ok)
	}
	task.Agents[1].ReleasedAt = &now
	if _, ok := allStoppedSignal(task, nil, agents, now); ok {
		t.Fatal("a released replacement made the lone offline participant appear stopped")
	}
	// An assistant leader is linked as an agent too, but its supervisor references
	// use its conversation ID, not machine/session or source=worker.
	task, agents = stoppedFixture(now)
	task.Agents = task.Agents[:1]
	agents[0].Assistant = true
	child := Todo{ID: "child", Supervision: &Supervision{Supervisor: "first"}, Agents: []TodoAgent{{MachineID: "box", SessionID: "second"}}}
	g, ok = allStoppedSignal(task, []Todo{task, child}, agents, now)
	if !ok || !strings.Contains(g.Key, "second") {
		t.Fatalf("assistant leader's worker omitted: %+v/%v", g, ok)
	}
	agents[1].State, agents[1].Status = proto.StateWorking, proto.StatusWorking
	if _, ok := allStoppedSignal(task, []Todo{task, child}, agents, now); ok {
		t.Fatal("assistant leader reported while its worker runs")
	}
}

func TestAllStoppedNeedsUserTaskCard(t *testing.T) {
	s, f, task := newSupervisedService(t)
	now := time.Now()
	_, agents := stoppedFixture(now.Add(-time.Minute))
	agents = agents[:1]
	agents[0].SessionID = task.Agents[0].SessionID
	f.setSessions("box", agents...)
	task.Supervision.State = reviewNeedsUser
	ctx := context.Background()
	view := s.allStoppedGroup(ctx, task, []Todo{task}, machineSessions{})
	if _, ok := allStoppedSignal(task, view.tasks, view.sessions, now); !ok {
		t.Fatal("needs_user without a card blocked supervision")
	}
	s.accept.mu.Lock()
	s.accept.loaded = true
	s.accept.asks = map[string]*acceptAsk{task.ID: {AskedAt: now, Conclusion: true, Outcome: outcomeDecide, Recommend: "Use the smaller change"}}
	s.accept.mu.Unlock()
	view = s.allStoppedGroup(ctx, task, []Todo{task}, machineSessions{})
	if _, ok := allStoppedSignal(task, view.tasks, view.sessions, now); ok {
		t.Fatal("an answerable task-decision card was ignored")
	}
}

func TestAllStoppedDeliveryAndDedup(t *testing.T) {
	s, f := newQuietService(t)
	presenceTimes(t, 0, 0)
	ownerTurnTimes(t, 0, 0)
	root := startTaskLeader(t, s, f)
	worker := planWorker(t, s, f, nil)
	now := time.Now()
	_, agents := stoppedFixture(now.Add(-time.Minute))
	agents[0].SessionID = root.Agents[0].SessionID
	agents[1].SessionID = worker.Agents[0].SessionID
	f.setSessions("box", agents...)
	ctx := context.Background()
	tasks := []Todo{root, worker}
	f.mu.Lock()
	f.onSend = func(f *fakeHub, p map[string]any) {
		if p["session_id"] == agents[0].SessionID {
			// The first recipient starts working immediately. The supervisor's
			// copy must still carry the inactivity observed before dispatch.
			for i := range f.sessions["box"] {
				if f.sessions["box"][i].SessionID == agents[0].SessionID {
					f.sessions["box"][i].Status = proto.StatusWorking
					f.sessions["box"][i].State = proto.StateWorking
				}
			}
		}
	}
	f.mu.Unlock()
	for _, task := range tasks {
		view := s.allStoppedGroup(ctx, task, tasks, machineSessions{})
		for _, g := range watchSignals(task, s.taskAgents(ctx, task), now, view) {
			s.signal(g)
		}
	}
	waitFor(t, "the leader agent's supervision message", func() bool { return len(sendsTo(f, "box", agents[0].SessionID)) > 0 })
	waitFor(t, "the supervisor above the leader", func() bool { return len(f.notices()) > 0 })
	for _, text := range []string{sendsTo(f, "box", agents[0].SessionID)[0], f.notices()[0]} {
		if !strings.Contains(text, "all participants") || !strings.Contains(text, "box/"+agents[0].SessionID) || !strings.Contains(text, "box/"+agents[1].SessionID) {
			t.Fatalf("recipient did not receive the whole group: %s", text)
		}
	}
	for _, task := range tasks {
		g, _ := allStoppedSignal(task, tasks, agents, now)
		s.signal(g)
	}
	s.supervise.mu.Lock()
	pending := len(s.supervise.pending)
	s.supervise.mu.Unlock()
	if pending != 0 {
		t.Fatalf("same stretch queued again: %d", pending)
	}
}

func TestGoldenAllStopped(t *testing.T) {
	entries := []prompts.SuperviseEntry{{ID: "example", Title: "Example task", Kind: sigAllStopped, Minutes: 30,
		Detail: "box/leader: agent: waiting for the worker\nbox/worker: agent: waiting for the leader"}}
	worker := prompts.RenderWorkerSupervise(entries)
	golden(t, "task_leader.all_stopped", worker)
	// Both recipients keep the same facts and action. Supervisors receive the
	// action in their stable system policy, not on every supervision event.
	_, clause, ok := strings.Cut(worker, ": all participants")
	if !ok {
		t.Fatal("missing diagnosis")
	}
	clause, _, _ = strings.Cut(clause, " (box/leader")
	facts, action, ok := strings.Cut(clause, " Check each participant")
	if !ok {
		t.Fatal("missing task-leader action")
	}
	action = "Check each participant" + action
	supervisor := prompts.RenderSupervise(prompts.Supervise{Entries: entries})
	if !strings.Contains(supervisor, "all participants"+facts) || !strings.Contains(supervisor, entries[0].Detail) {
		t.Fatalf("supervisor lost the shared diagnosis or member statements: %s", supervisor)
	}
	if strings.Contains(supervisor, action) {
		t.Fatal("supervision event repeated the handling policy")
	}
	if !strings.Contains(prompts.RenderSupervisionInstructions(), action) {
		t.Fatal("supervisor system policy lost the task-leader action")
	}
}

func TestAllStoppedDigestRechecksAndLedgerWake(t *testing.T) {
	s, f, task := newSupervisedService(t)
	now := time.Now()
	_, agents := stoppedFixture(now.Add(-time.Minute))
	agents = agents[:1]
	agents[0].SessionID = task.Agents[0].SessionID
	f.setSessions("box", agents...)
	ctx := context.Background()
	g, ok := allStoppedSignal(task, nil, agents, now)
	if !ok {
		t.Fatal("fixture not stopped")
	}
	if _, ok := s.digestEntry(ctx, g, ""); !ok {
		t.Fatal("current stretch dropped")
	}
	wake := now.Add(time.Hour)
	s.putWork(ctx, db.SessionWork{MachineID: "box", SessionID: agents[0].SessionID, TurnAt: agents[0].UpdatedAt,
		WakeAt: &wake, Work: proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal}})
	if _, ok := s.digestEntry(ctx, g, ""); ok {
		t.Fatal("sent despite a new manager-side wake absent from the node snapshot")
	}
	agents[0].Status = proto.StatusWorking
	f.setSessions("box", agents...)
	if _, ok := s.digestEntry(ctx, g, ""); ok {
		t.Fatal("sent after a participant resumed")
	}
}

package assistant

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Regression cases from the independent review: elapsed time and a process restart
// do not turn an unchanged group into a new event.
func TestAllStoppedRestartDedup(t *testing.T) {
	ctx, now := context.Background(), time.Now()
	store := &memStore{}
	first, _ := newQuietServiceOn(t, store)
	task, agents := stoppedFixture(now.Add(-31 * time.Minute))
	g, ok := allStoppedSignal(task, nil, agents, now)
	if !ok {
		t.Fatal("fixture must be stopped")
	}
	entry := prompts.SuperviseEntry{ID: task.ID, Kind: g.Kind, Detail: g.Detail}
	_, kept, _, err := first.reserveSupervision(ctx, []prompts.SuperviseEntry{entry}, []superviseSignal{g}, now.Add(-31*time.Minute))
	if err != nil || len(kept) != 1 {
		t.Fatalf("initial checkpoint: %d %v", len(kept), err)
	}
	first.turnsTold(ctx, kept)
	restarted, _ := newQuietServiceOn(t, store)
	entry.Detail = "The same wait, reworded by triage"
	for _, later := range []time.Time{now, now.Add(7 * 24 * time.Hour)} {
		_, kept, _, err = restarted.reserveSupervision(ctx, []prompts.SuperviseEntry{entry}, []superviseSignal{g}, later)
		if err != nil || len(kept) != 0 {
			t.Fatalf("same group reserved again after restart: %d %v", len(kept), err)
		}
	}
}

func TestAllStoppedFingerprintLifecycle(t *testing.T) {
	ctx, now := context.Background(), time.Now()
	store := &memStore{}
	s, _ := newQuietServiceOn(t, store)
	task, agents := stoppedFixture(now)
	if err := s.ensureTodosLoaded(ctx); err != nil {
		t.Fatal(err)
	}
	s.todos.mu.Lock()
	s.todos.todos[task.ID] = &task
	s.todos.mu.Unlock()
	g, _ := allStoppedSignal(task, nil, agents, now)
	entry := prompts.SuperviseEntry{ID: task.ID, Kind: g.Kind}
	for i := range 10 {
		g.Key = fmt.Sprintf("new-group-%d", i)
		_, kept, _, err := s.reserveSupervision(ctx, []prompts.SuperviseEntry{entry}, []superviseSignal{g}, now.Add(time.Duration(i)*time.Hour))
		if err != nil || len(kept) != 1 {
			t.Fatalf("new key did not rearm: %d %v", len(kept), err)
		}
		var history map[string]superviseFingerprint
		if _, err := store.Setting(ctx, superviseDedupKey, &history); err != nil || len(history) != 1 {
			t.Fatalf("history grew with keys: %d %v", len(history), err)
		}
	}
	g.To = "another-supervisor"
	for i := range 3 {
		g.Key = fmt.Sprintf("other-recipient-%d", i)
		_, kept, _, err := s.reserveSupervision(ctx, []prompts.SuperviseEntry{entry}, []superviseSignal{g}, now)
		if err != nil || len(kept) != 1 {
			t.Fatalf("other recipient did not get its own key: %d %v", len(kept), err)
		}
		var history map[string]superviseFingerprint
		if _, err := store.Setting(ctx, superviseDedupKey, &history); err != nil || len(history) != 2 {
			t.Fatalf("expected one entry per recipient: %d %v", len(history), err)
		}
	}
	// A failed delivery must not consume even an indefinitely retained key.
	g.Key = "failed-delivery"
	_, _, rollback, err := s.reserveSupervision(ctx, []prompts.SuperviseEntry{entry}, []superviseSignal{g}, now)
	if err != nil {
		t.Fatal(err)
	}
	rollback()
	_, kept, _, err := s.reserveSupervision(ctx, []prompts.SuperviseEntry{entry}, []superviseSignal{g}, now)
	if err != nil || len(kept) != 1 {
		t.Fatalf("failed delivery could not retry: %d %v", len(kept), err)
	}
	s.todos.mu.Lock()
	s.todos.todos[task.ID].DoneAt = &now
	s.todos.mu.Unlock()
	s.pruneAllStoppedDedup(ctx)
	var history map[string]superviseFingerprint
	if _, err := store.Setting(ctx, superviseDedupKey, &history); err != nil || len(history) != 0 {
		t.Fatalf("closed task retained fingerprints: %d %v", len(history), err)
	}
}

func TestAllStoppedRuntimeChildGuards(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name string
		edit func(*proto.Session)
	}{
		{"queued input", func(c *proto.Session) { c.Queued = []string{"Continue"} }},
		{"starting", func(c *proto.Session) { c.Starting = true }},
		{"future wake", func(c *proto.Session) { later := now.Add(time.Hour); c.Work = &proto.WorkState{WakeAt: &later} }},
		{"future monitor", func(c *proto.Session) {
			later := now.Add(time.Hour)
			c.Background = []proto.BackgroundTask{{Kind: "monitor", StartedAt: now.Add(-time.Hour), ExpiresAt: &later}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, agents := stoppedFixture(now.Add(-time.Hour))
			child := proto.Session{MachineID: "box", SessionID: "runtime-child", ParentSessionID: "first", State: proto.StateIdle, Status: proto.StatusDone, UpdatedAt: now.Add(-time.Hour)}
			tc.edit(&child)
			if g, ok := allStoppedSignal(task, nil, append(agents, child), now); ok {
				t.Fatalf("reported while child is not stopped: %+v", g)
			}
		})
	}
}

type allStoppedReadStore struct {
	workMemStore
	failWork, failCards atomic.Bool
}

func (s *allStoppedReadStore) SessionWorks(ctx context.Context, since time.Time) ([]db.SessionWork, error) {
	if s.failWork.Load() {
		return nil, errors.New("work ledger unavailable")
	}
	return s.workMemStore.SessionWorks(ctx, since)
}

func (s *allStoppedReadStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == acceptanceKey && s.failCards.Load() {
		return false, errors.New("card ledger unavailable")
	}
	return s.memStore.Setting(ctx, key, out)
}

func TestAllStoppedTransientReadMustRetry(t *testing.T) {
	for _, source := range []string{"sessions", "work", "cards"} {
		t.Run(source, func(t *testing.T) {
			store := &allStoppedReadStore{}
			s, f := newQuietServiceOn(t, store)
			presenceTimes(t, 0, 0)
			ownerTurnTimes(t, 0, 0)
			s.superviseTimes.urgent = time.Hour // drive retry explicitly, without a timer racing the assertions
			ctx, now := context.Background(), time.Now()
			task, agents := stoppedFixture(now.Add(-time.Minute))
			task.Origin, task.Supervision.Supervisor = "conv1", "conv1"
			if err := s.ensureTodosLoaded(ctx); err != nil {
				t.Fatal(err)
			}
			s.todos.mu.Lock()
			s.todos.todos[task.ID] = &task
			s.todos.mu.Unlock()
			f.setSessions("box", agents...)
			g, ok := allStoppedSignal(task, nil, agents, now)
			if !ok {
				t.Fatal("fixture must be stopped")
			}
			g.At = now.Add(-time.Hour)
			s.supervise.mu.Lock()
			s.supervise.init()
			s.supervise.pending[g.id()] = g
			s.supervise.last = time.Time{}
			s.supervise.mu.Unlock()
			switch source {
			case "sessions":
				f.mu.Lock()
				f.sessionsFail["box"] = 1000
				f.mu.Unlock()
			case "work":
				s.work.mu.Lock()
				s.work.loaded = false
				s.work.mu.Unlock()
				store.failWork.Store(true)
			case "cards":
				s.accept.mu.Lock()
				s.accept.loaded = false
				s.accept.mu.Unlock()
				store.failCards.Store(true)
			}
			s.sendDigest(ctx)
			s.supervise.mu.Lock()
			sent := s.supervise.sent[g.id()]
			pending := s.supervise.pending[g.id()]
			scheduled := s.supervise.timer != nil
			if s.supervise.timer != nil {
				s.supervise.timer.Stop()
				s.supervise.timer = nil
			}
			s.supervise.mu.Unlock()
			if sent == g.Key || pending.Key != g.Key || !scheduled {
				t.Fatalf("unknown read consumed the signal: sent=%q pending=%+v retry=%v", sent, pending, scheduled)
			}
			if len(f.notices()) != 0 {
				t.Fatal("delivered without a complete snapshot")
			}
			f.mu.Lock()
			f.sessionsFail["box"] = 0
			f.mu.Unlock()
			store.failWork.Store(false)
			store.failCards.Store(false)
			s.sendDigest(ctx)
			waitFor(t, "delivery after the read recovers", func() bool { return len(f.notices()) == 1 })
		})
	}
}

func TestAllStoppedReviewPending(t *testing.T) {
	now := time.Now()
	task, agents := stoppedFixture(now)
	task.Supervision.State = reviewPending
	if g, ok := allStoppedSignal(task, nil, agents, now); ok {
		t.Fatalf("unexpected signal %+v", g)
	}
}

// Background work started during the parent's turn, so its age is at least the
// time since that parent stopped even when no background timestamp was reported.
func TestAllStoppedUnknownBackgroundAge(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name string
		edit func(*proto.Session)
	}{
		{"shell", func(c *proto.Session) { c.Background = []proto.BackgroundTask{{Kind: "shell"}} }},
		{"subagent", func(c *proto.Session) { c.Subagents = []proto.Subagent{{Running: true}} }},
		{"workflow", func(c *proto.Session) { c.Workflows = []proto.Workflow{{Running: true}} }},
		{"summary working", func(c *proto.Session) { c.BackgroundWorking = true }},
		{"summary waiting", func(c *proto.Session) { c.BackgroundWaiting = true }},
		{"inferred working state", func(c *proto.Session) { c.State = proto.StateWorking; c.BackgroundWorking = true }},
	} {
		for _, child := range []bool{false, true} {
			for _, minutes := range []int{29, 30} {
				t.Run(fmt.Sprintf("%s/child=%v/minutes=%d", tc.name, child, minutes), func(t *testing.T) {
					task, agents := stoppedFixture(now.Add(-time.Hour))
					stop := now.Add(-time.Duration(minutes) * time.Minute)
					c := proto.Session{MachineID: "box", SessionID: agents[0].SessionID, Status: proto.StatusDone,
						State: proto.StateIdle, UpdatedAt: now.Add(-2 * time.Hour), StateSince: stop,
						Work: &proto.WorkState{TurnAt: stop}}
					tc.edit(&c)
					if child {
						c.SessionID, c.ParentSessionID = "runtime-child", agents[0].SessionID
						agents = append(agents, c)
					} else {
						agents[0] = c
					}
					g, ok := allStoppedSignal(task, nil, agents, now)
					if ok != (minutes >= 30) {
						t.Fatalf("stop-time age bound: signal=%+v holds=%v", g, ok)
					}
					if ok {
						next, holds := allStoppedSignal(task, nil, agents, now.Add(time.Minute))
						if !holds || next.Key != g.Key {
							t.Fatalf("unknown background changed key on the next tick: %q -> %q", g.Key, next.Key)
						}
					}
				})
			}
		}
	}
}

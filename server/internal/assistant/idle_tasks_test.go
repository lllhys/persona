package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// idleTaskHub holds the device outside fakeHub's mutex, so an idle check can still read the hub's settings.
type idleTaskHub struct {
	*fakeHub
	entered, release         chan struct{}
	recording, finishRecords chan struct{}
}

func (h *idleTaskHub) DeviceCall(ctx context.Context, p DeviceCall) (json.RawMessage, error) {
	if p.Method == DeviceSessionStart {
		h.entered <- struct{}{}
		select {
		case <-h.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return h.fakeHub.DeviceCall(ctx, p)
}

func (h *idleTaskHub) Broadcast(ctx context.Context, b Broadcast) {
	if h.recording != nil {
		var event struct {
			Type string    `json:"type"`
			Plan *TaskPlan `json:"plan"`
		}
		if json.Unmarshal(b.Message, &event) == nil && event.Type == "task_plan" && event.Plan != nil && event.Plan.Tasks[0].Error != "" {
			h.recording <- struct{}{}
			<-h.finishRecords
		}
	}
	h.fakeHub.Broadcast(ctx, b)
}

func idleTaskService(t *testing.T, n int) (*Service, *idleTaskHub, *TaskPlan) {
	t.Helper()
	f := newFakeHub()
	f.ownModels = true
	f.sessions["box"] = nil
	s := newStorelessService(t, f)
	h := &idleTaskHub{fakeHub: f, entered: make(chan struct{}, n), release: make(chan struct{})}
	s.hub = h
	p := &TaskPlan{ID: "plan_idle", Conversation: "conv1", State: planStarted, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	for i := range n {
		title := fmt.Sprintf("Check example %d", i)
		todo, err := s.createTaskTodo(context.Background(), title, "Check the example.", p.Conversation, p.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		p.Tasks = append(p.Tasks, PlanTask{Title: title, Prompt: todo.Notes, Machine: "box", Cwd: "/code/app", TodoID: todo.ID})
	}
	if err := s.savePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(h.release)
		if h.finishRecords != nil {
			close(h.finishRecords)
		}
		s.supervise.mu.Lock()
		if s.supervise.timer != nil {
			s.supervise.timer.Stop()
		}
		s.supervise.mu.Unlock()
	})
	return s, h, p
}

func wantTaskStartIdle(t *testing.T, s *Service, want bool) {
	t.Helper()
	idle, why := s.idle(context.Background())
	if idle != want || !want && why != "a task's agent is starting" {
		t.Fatalf("idle = %v (%q), want %v", idle, why, want)
	}
}

// Reservations are visible as soon as starts are queued. One completing start, including its caller cleanup,
// must not make an overlapping start disappear from the manager's idle check.
func TestTaskStartsKeepManagerBusyUntilAllFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, h, p := idleTaskService(t, 2)
		wantTaskStartIdle(t, s, true)
		cleaning, finishCleanup := make(chan struct{}, 2), make(chan struct{})
		defer close(finishCleanup)
		for i, task := range p.Tasks {
			s.startPlanTaskAsync(p.ID, p.Conversation, i, task, false, func() {
				cleaning <- struct{}{}
				<-finishCleanup
			})
		}
		wantTaskStartIdle(t, s, false) // no wait for either goroutine to reach the device
		if got := s.taskStarting.Load(); got != 2 {
			t.Fatalf("queued starts = %d, want 2", got)
		}
		<-h.entered
		<-h.entered
		wantTaskStartIdle(t, s, false)
		for remaining := int64(1); remaining >= 0; remaining-- {
			h.release <- struct{}{}
			<-cleaning
			wantTaskStartIdle(t, s, false) // cleanup is still part of the admitted start
			finishCleanup <- struct{}{}
			synctest.Wait()
			if got := s.taskStarting.Load(); got != remaining {
				t.Fatalf("remaining starts = %d, want %d", got, remaining)
			}
			wantTaskStartIdle(t, s, remaining == 0)
		}
	})
}

// Synchronous callers are counted too, and neither the ordinary failure path nor beginRemedy's early return
// releases the reservation before the plan's error has been published.
func TestTaskStartIdleIncludesFailureAndRemedyBookkeeping(t *testing.T) {
	for _, remedy := range []bool{false, true} {
		t.Run(fmt.Sprintf("remedy=%v", remedy), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, h, p := idleTaskService(t, 1)
				h.failStart = errors.New("launcher unavailable")
				if remedy {
					h.failStart = keptRefusal(proto.CodeShellWaiting)
					s.superviseTodo(context.Background(), p.Tasks[0].TodoID, "conv1", nil, 0)
				}
				h.recording, h.finishRecords = make(chan struct{}, 1), make(chan struct{})
				done := make(chan struct{})
				go func() {
					defer close(done)
					s.startPlanTask(p.ID, p.Conversation, 0, p.Tasks[0], false)
				}()
				<-h.entered
				wantTaskStartIdle(t, s, false)
				h.release <- struct{}{}
				<-h.recording
				wantTaskStartIdle(t, s, false)
				h.finishRecords <- struct{}{}
				<-done
				synctest.Wait()
				wantTaskStartIdle(t, s, true)
				todo := todoNow(s, p.Tasks[0].TodoID)
				if remedy {
					if start := remedyState(t, s, p.ID); start == nil || start.State != remedying || todo.DoneAt != nil {
						t.Fatalf("start did not enter remedying: start=%+v todo=%+v", start, todo)
					}
				} else if todo.DoneAt == nil || todo.StartError == "" {
					t.Fatalf("start did not finish its failure bookkeeping: %+v", todo)
				}
			})
		})
	}
}

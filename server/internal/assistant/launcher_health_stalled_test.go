package assistant

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestStalledStartKeepsTaskAndLauncherHealth(t *testing.T) {
	s, f := launcherHealthHubOn(t, newStorelessService)
	ctx := context.Background()
	original := "agent opened but its session was not identified"
	f.failStart = fmt.Errorf("session.start: %w", &rpc.Error{Code: proto.CodeAgentStartStalled, Message: original})
	todo, err := s.createTodo(ctx, "Inspect files", "Inspect files", "conv1", "plan_stalled")
	if err != nil {
		t.Fatal(err)
	}
	task := PlanTask{Title: "Inspect files", Prompt: "Inspect files", Machine: "l4", Cwd: "/home/me/project", Launcher: "work", MachinePolicy: machineFlexible, Permission: "bypassPermissions", TodoID: todo.ID}
	p := &TaskPlan{ID: "plan_stalled", Conversation: "conv1", State: planStarted, Tasks: []PlanTask{task}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.savePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	s.startPlanTask(p.ID, p.Conversation, 0, task, false)
	if starts := taskStartMachines(f); len(starts) != 1 || starts[0] != "l4" {
		t.Fatalf("stalled start retried: %v", starts)
	}
	if len(f.checked) != 0 {
		t.Fatalf("stalled start checked: %v", f.checked)
	}
	if r, ok := s.launcherHealth(ctx, "l4", "work"); ok {
		t.Fatalf("stalled start counted: %+v", r)
	}
	stored, err := s.loadPlan(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := stored.Tasks[0]
	if got.Machine != task.Machine || got.Cwd != task.Cwd || got.Error != f.failStart.Error() || got.ErrorCode != proto.CodeAgentStartStalled || got.Reroute != nil {
		t.Fatalf("failed task changed: %+v", got)
	}
	if failed := todoNow(s, todo.ID); failed.DoneAt == nil || failed.StartError != f.failStart.Error() {
		t.Fatalf("failure not recorded: %+v", failed)
	}
}

func TestStalledStartReportsDoNotChangeExistingHealth(t *testing.T) {
	s, f := launcherHealthHubOn(t, newStorelessService)
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		before := s.recordHealth(ctx, "l4", "work", healthResult{err: "existing failure", source: healthSourceCheck})
		s.StartEnded(ctx, StartEnded{MachineID: "l4", Launcher: "work", Code: proto.CodeAgentStartStalled, Error: "stalled"})
		s.followStarts(ctx, DeviceSessions{MachineID: "l4", Sessions: []proto.Session{{SessionID: "pending", Launcher: "work", StartErrorCode: proto.CodeAgentStartStalled, StartError: "stalled"}}})
		synctest.Wait()
		if got := healthState(s, "l4", "work"); !reflect.DeepEqual(got, before) {
			t.Fatalf("stalled start changed existing health: before %+v, after %+v", before, got)
		}
		if len(f.checked) != 0 {
			t.Fatalf("stalled start checked: %v", f.checked)
		}
	})
}

func TestStartFailureAndDetourShareOneProbe(t *testing.T) {
	for _, pass := range []bool{false, true} {
		for _, completed := range []bool{false, true} {
			t.Run(fmt.Sprintf("pass=%v/completed=%v", pass, completed), func(t *testing.T) {
				s, f := launcherHealthHubOn(t, newStorelessService)
				synctest.Test(t, func(t *testing.T) {
					ctx := context.Background()
					f.checks["l4/work"] = proto.LauncherCheck{OK: pass, Error: "probe failed"}
					release := make(chan struct{})
					f.onCheck = func(*fakeHub) { <-release }
					s.recordStartFailure(ctx, "l4", "work", errors.New("start failed"))
					if completed {
						close(release)
						synctest.Wait()
					}
					done := make(chan bool, 1)
					go func() { _, _, moved := s.healthDetour(ctx, Device{ID: "l4"}, "work"); done <- moved }()
					if !completed {
						synctest.Wait()
						s.health.mu.Lock()
						rc := s.health.recheck[healthKey("l4", "work")]
						joined := rc != nil && rc.waiters == 1
						s.health.mu.Unlock()
						if !joined {
							t.Fatal("detour did not join the start's reserved probe")
						}
						close(release)
					}
					moved := <-done
					synctest.Wait()
					if moved == pass {
						t.Fatalf("probe pass=%v, moved=%v", pass, moved)
					}
					if len(f.checked) != 1 {
						t.Fatalf("one failure ran %d probes: %v", len(f.checked), f.checked)
					}
					r := healthState(s, "l4", "work")
					if r.Source != healthSourceCheck || (r.State == healthOK) != pass {
						t.Fatalf("probe result not recorded before detour: %+v", r)
					}
				})
			})
		}
	}
}

// An older node sends only text. The new hub must retain its generic failure handling.
func TestUncodedStalledStartKeepsLegacyHealthHandling(t *testing.T) {
	s, f := launcherHealthHubOn(t, newStorelessService)
	synctest.Test(t, func(t *testing.T) {
		err := &rpc.Error{Message: "agent opened but its session was not identified"}
		if startStalled(err) {
			t.Fatal("uncoded error classified by its text")
		}
		s.recordStartFailure(context.Background(), "l4", "work", err)
		synctest.Wait()
		if len(f.checked) != 1 || !healthState(s, "l4", "work").unusable() {
			t.Fatal("legacy failure handling changed")
		}
	})
}

// A start that ended in the new tab's shell before the launcher ran (#2570) says nothing of the launcher.
func TestStartTabRefusalsLeaveLauncherHealth(t *testing.T) {
	for _, code := range []string{proto.CodeShellWaiting, proto.CodeStartLineRejected, proto.CodeAgentStartStalled} {
		if !startStalled(&rpc.Error{Code: code}) {
			t.Errorf("%s counted against the launcher", code)
		}
	}
	if startStalled(&rpc.Error{Code: proto.CodeLauncherMissing}) {
		t.Error("launcher_missing left out")
	}
}

// A start whose own wait ran out (the node's link stuck, taskStartTimeout spent) or was cancelled learned nothing of the
// launcher: no failing record, no follow-up probe, and a record already there stays as it was. A node's own timeout
// text still counts (TestLauncherHealthRoundAndDetour).
func TestStartWaitEndedSaysNothingOfTheLauncher(t *testing.T) {
	for name, err := range map[string]error{
		"deadline":         fmt.Errorf("session.start: %w", context.DeadlineExceeded),
		"canceled":         context.Canceled,
		"relayed deadline": &rpc.Error{Message: context.DeadlineExceeded.Error()},
		"relayed canceled": &rpc.Error{Message: context.Canceled.Error()},
	} {
		t.Run(name, func(t *testing.T) {
			s, f := launcherHealthHubOn(t, newStorelessService)
			synctest.Test(t, func(t *testing.T) {
				ctx := context.Background()
				s.recordStartFailure(ctx, "l4", "work", err)
				synctest.Wait()
				if r, ok := s.launcherHealth(ctx, "l4", "work"); ok {
					t.Fatalf("an ended wait counted: %+v", r)
				}
				before := s.recordHealth(ctx, "box", "work", healthResult{ok: true, source: healthSourceCheck})
				s.recordStartFailure(ctx, "box", "work", err)
				synctest.Wait()
				if got := healthState(s, "box", "work"); !reflect.DeepEqual(got, before) {
					t.Fatalf("an ended wait changed the record: before %+v, after %+v", before, got)
				}
				if len(f.checked) != 0 {
					t.Fatalf("an ended wait was probed: %v", f.checked)
				}
			})
		})
	}
	if startWaitEnded(&rpc.Error{Message: "session.start: context deadline exceeded"}) || startWaitEnded(errors.New("session.start: context deadline exceeded")) {
		t.Fatal("a node's own timeout text taken for the hub's ended wait")
	}
}

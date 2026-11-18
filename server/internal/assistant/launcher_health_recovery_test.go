package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestLauncherHealthUsageLimitRechecksBeforeReset(t *testing.T) {
	for _, restored := range []bool{false, true} {
		name := "exhausted"
		if restored {
			name = "restored"
		}
		t.Run(name, func(t *testing.T) {
			s, f := launcherHealthHubOn(t, newStorelessService)
			ctx := context.Background()
			line, reset := sessionLimitLine(3 * time.Hour)
			s.recordHealth(ctx, "l4", "work", healthResult{err: line, kind: errKindUsageLimit, resetsAt: &reset, source: healthSourceSession})
			editHealth(s, "l4", "work", func(r *LauncherHealth) { r.CheckedAt = time.Now().Add(-time.Minute) })
			before := healthState(s, "l4", "work")
			if restored {
				delete(f.checks, "l4/work")
			} else {
				f.checks["l4/work"] = proto.LauncherCheck{Agent: "claude", Error: line}
			}

			for _, retry := range []bool{false, true} {
				task := &PlanTask{Machine: "l4", Cwd: "/work", Launcher: "work", MachinePolicy: machineFixed}
				mid, _, err := s.startTaskAgent(ctx, task, retry)
				if restored {
					if err != nil || mid != "l4" {
						t.Fatalf("restored quota refused: machine=%q error=%v", mid, err)
					}
				} else {
					var msg *i18n.Message
					if !errors.As(err, &msg) || msg.Key != "assistant-tasks.fixed_launcher_unhealthy" {
						t.Fatalf("exhausted quota not refused: %v", err)
					}
				}
			}
			if n := checkedCount(f, "l4/work"); n != 1 {
				t.Fatalf("burst ran %d probes, want one", n)
			}
			r := healthState(s, "l4", "work")
			if restored {
				if r.State != healthOK || r.Kind != "" || r.ResetsAt != nil || r.Fails != 0 || r.Source != healthSourceCheck {
					t.Fatalf("passing probe did not clear quota record: %+v", r)
				}
				if starts := taskStartMachines(f); len(starts) != 2 || starts[0] != "l4" || starts[1] != "l4" {
					t.Fatalf("restored starts changed machine: %v", starts)
				}
			} else {
				if !r.unusable() || !r.CheckedAt.After(before.CheckedAt) || r.Kind != errKindUsageLimit || !r.ResetsAt.Equal(reset) || r.Fails != before.Fails+1 {
					t.Fatalf("failed probe did not retain fresh quota evidence: before %+v, after %+v", before, r)
				}
				if starts := taskStartMachines(f); len(starts) != 0 {
					t.Fatalf("exhausted quota started agents: %v", starts)
				}
			}
		})
	}
}

func TestAgentStartUsageLimitRecoversBeforeReset(t *testing.T) {
	s, f := launcherHealthHub(t)
	ctx := context.Background()
	line, reset := sessionLimitLine(3 * time.Hour)
	s.recordHealth(ctx, "l4", "work", healthResult{err: line, kind: errKindUsageLimit, resetsAt: &reset, source: healthSourceSession})
	editHealth(s, "l4", "work", func(r *LauncherHealth) { r.CheckedAt = time.Now().Add(-time.Minute) })
	delete(f.checks, "l4/work")
	res, err := s.runTool(ctx, "server", "conv9", "agent_start", json.RawMessage(`{"machine":"l4","cwd":"/work","launcher":"work","prompt":"Inspect files"}`))
	if err != nil {
		t.Fatal(err)
	}
	if starts := taskStartMachines(f); len(starts) != 1 || starts[0] != "l4" || checkedCount(f, "l4/work") != 1 {
		t.Fatalf("restored agent start: starts %v, probes %d, answer %s", starts, checkedCount(f, "l4/work"), res)
	}
	if r := healthState(s, "l4", "work"); r.State != healthOK || r.ResetsAt != nil {
		t.Fatalf("agent start retained quota record: %+v", r)
	}
}

func TestLauncherHealthUsageLimitDoesNotProbeFreshEvidence(t *testing.T) {
	s, f := launcherHealthHubOn(t, newStorelessService)
	ctx := context.Background()
	line, reset := sessionLimitLine(3 * time.Hour)
	s.recordHealth(ctx, "l4", "work", healthResult{err: line, kind: errKindUsageLimit, resetsAt: &reset, source: healthSourceSession})
	delete(f.checks, "l4/work")
	s.recordStartSuccess(ctx, "l4", "work")
	if r, bad := s.launcherUnusableNow(ctx, "l4", "work"); !bad || r.Kind != errKindUsageLimit {
		t.Fatalf("fresh limit or terminal start was treated as recovery: %+v, unusable=%v", r, bad)
	}
	if n := checkedCount(f, "l4/work"); n != 0 {
		t.Fatalf("fresh usage limit ran %d probes", n)
	}
}

func TestFixedTaskUsageLimitRecoversOnRetryBeforeReset(t *testing.T) {
	s, f := launcherHealthHubOn(t, newStorelessService)
	ctx := context.Background()
	line, reset := sessionLimitLine(3 * time.Hour)
	f.checks["l4/work"] = proto.LauncherCheck{Agent: "claude", Error: line}
	s.recordHealth(ctx, "l4", "work", healthResult{err: line, kind: errKindUsageLimit, resetsAt: &reset, source: healthSourceSession})
	editHealth(s, "l4", "work", func(r *LauncherHealth) { r.CheckedAt = time.Now().Add(-time.Minute) })
	res, err := s.toolTaskPlan(ctx, "conv9", json.RawMessage(`{"tasks":[{"title":"Inspect files","prompt":"Inspect local files","machine":"l4","cwd":"/work","launcher":"work","permission":"bypassPermissions"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	id := regexp.MustCompile(`plan_[0-9a-f]+`).FindString(res)
	p, err := s.startPlan(ctx, id, []PlanTask{{Title: "Inspect files"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	todoID := p.Tasks[0].TodoID
	waitFor(t, "quota refusal", func() bool { return !s.taskStartOnWay(todoID) && todoNow(s, todoID).StartError != "" })
	if starts := taskStartMachines(f); len(starts) != 0 {
		t.Fatalf("exhausted quota dispatched: %v", starts)
	}

	f.mu.Lock()
	delete(f.checks, "l4/work")
	f.mu.Unlock()
	editHealth(s, "l4", "work", func(r *LauncherHealth) { r.CheckedAt = time.Now().Add(-healthRecheckThrottle - time.Second) })
	s.health.mu.Lock()
	s.health.recheckAt[healthKey("l4", "work")] = time.Now().Add(-healthRecheckThrottle - time.Second)
	s.health.mu.Unlock()
	if _, err := s.retryTask(ctx, todoID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "quota recovery on retry", func() bool {
		todo := todoNow(s, todoID)
		return !s.taskStartOnWay(todoID) && (len(todo.Agents) == 1 || todo.StartError != "")
	})
	if todo := todoNow(s, todoID); len(todo.Agents) != 1 || todo.StartError != "" {
		t.Fatalf("restored quota did not recover on retry: %+v", todo)
	}
	if starts := taskStartMachines(f); len(starts) != 1 || starts[0] != "l4" || checkedCount(f, "l4/work") != 2 {
		t.Fatalf("retry: starts %v, probes %d", starts, checkedCount(f, "l4/work"))
	}
	if r := healthState(s, "l4", "work"); r.State != healthOK || r.ResetsAt != nil {
		t.Fatalf("retry retained quota record: %+v", r)
	}
}

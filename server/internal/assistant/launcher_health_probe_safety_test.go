package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestLauncherHealthOtherModelCannotClearOpusLimit(t *testing.T) {
	s, f := launcherHealthHubOn(t, newStorelessService)
	ctx := context.Background()
	reset := time.Now().Add(time.Hour)
	s.recordHealth(ctx, "l4", "work", healthResult{err: "You've hit your Opus limit", kind: errKindUsageLimit, resetsAt: &reset, source: healthSourceSession})
	editHealth(s, "l4", "work", func(r *LauncherHealth) { r.CheckedAt = time.Now().Add(-time.Minute) })
	probes := 0
	f.answerNode(assistantwire.DeviceLaunchersCheck, func(_ string, params json.RawMessage) (any, error) {
		probes++
		var request struct{ Model string }
		if err := json.Unmarshal(params, &request); err != nil || request.Model != "opus" {
			t.Errorf("probe did not request limited Opus: %s", params)
		}
		return proto.LauncherCheck{OK: true, Agent: "claude", Model: "claude-sonnet-4-5"}, nil
	})
	mid, _, err := s.startTaskAgent(ctx, &PlanTask{Machine: "l4", Cwd: "/work", Launcher: "work", Model: "opus", MachinePolicy: machineFixed}, false)
	if err == nil {
		t.Fatalf("Sonnet success admitted limited Opus: machine=%q starts=%v", mid, taskStartMachines(f))
	}
	if r := healthState(s, "l4", "work"); !r.unusable() || r.Kind != errKindUsageLimit || r.ResetsAt == nil || !r.ResetsAt.Equal(reset) {
		t.Fatalf("Sonnet success erased Opus limit: %+v", r)
	}
	if starts := taskStartMachines(f); len(starts) != 0 || probes != 1 {
		t.Fatalf("limited Opus: starts=%v probes=%d", starts, probes)
	}
}

func TestLauncherHealthMatchingModelClearsOpusLimit(t *testing.T) {
	s, f := launcherHealthHubOn(t, newStorelessService)
	ctx := context.Background()
	reset := time.Now().Add(time.Hour)
	s.recordHealth(ctx, "l4", "work", healthResult{err: "You've hit your Opus limit", kind: errKindUsageLimit, resetsAt: &reset, source: healthSourceSession})
	editHealth(s, "l4", "work", func(r *LauncherHealth) { r.CheckedAt = time.Now().Add(-time.Minute) })
	f.answerNode(assistantwire.DeviceLaunchersCheck, func(_ string, params json.RawMessage) (any, error) {
		var request struct{ Model string }
		if err := json.Unmarshal(params, &request); err != nil || request.Model != "opus" {
			t.Errorf("probe did not request Opus: %s", params)
		}
		return proto.LauncherCheck{OK: true, Agent: "claude", Model: "claude-opus-4-1"}, nil
	})
	mid, _, err := s.startTaskAgent(ctx, &PlanTask{Machine: "l4", Cwd: "/work", Launcher: "work", Model: "opus", MachinePolicy: machineFixed}, false)
	if err != nil || mid != "l4" {
		t.Fatalf("recovered Opus was refused: machine=%q error=%v", mid, err)
	}
	if r := healthState(s, "l4", "work"); r.State != healthOK || r.ResetsAt != nil {
		t.Fatalf("matching probe retained Opus limit: %+v", r)
	}
}

func TestLauncherHealthUsageLimitProbeModelScope(t *testing.T) {
	for _, tc := range []struct {
		limit, model string
		matches      bool
	}{
		{"You've hit your Opus limit", "claude-sonnet-4-5", false},
		{"You've hit your Opus limit", "", false},
		{"You've hit your Opus limit", "notopus", false},
		{"You've hit your Opus limit", "claude-opus-4-1", true},
		{"You've hit your SONNET limit", "sonnet", true},
		{"You've hit your Haiku limit", "opus", false},
		{"You've hit your weekly limit", "m", true},
	} {
		if got := len(limitFamiliesLeft(limitFamilies(tc.limit), tc.model)) == 0; got != tc.matches {
			t.Errorf("limit=%q model=%q matches=%v, want %v", tc.limit, tc.model, got, tc.matches)
		}
	}
}

func TestLauncherHealthOldProbeCannotEraseNewLimit(t *testing.T) {
	s, f := launcherHealthHubOn(t, newStorelessService)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	oldReset, newReset := time.Now().Add(time.Hour), time.Now().Add(7*24*time.Hour)
	s.recordHealth(ctx, "l4", "work", healthResult{err: "You've hit your session limit", kind: errKindUsageLimit, resetsAt: &oldReset, source: healthSourceSession})
	editHealth(s, "l4", "work", func(r *LauncherHealth) { r.CheckedAt = time.Now().Add(-time.Minute) })
	entered, release := make(chan struct{}), make(chan struct{})
	f.answerNode(assistantwire.DeviceLaunchersCheck, func(_ string, _ json.RawMessage) (any, error) {
		close(entered)
		select {
		case <-release:
			return proto.LauncherCheck{OK: true, Agent: "claude", Model: "m"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	type answer struct {
		machine string
		err     error
	}
	done := make(chan answer, 1)
	go func() {
		mid, _, err := s.startTaskAgent(ctx, &PlanTask{Machine: "l4", Cwd: "/work", Launcher: "work", MachinePolicy: machineFixed}, false)
		done <- answer{mid, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("probe did not start")
	}
	s.recordHealth(ctx, "l4", "work", healthResult{err: "You've hit your weekly limit", kind: errKindUsageLimit, resetsAt: &newReset, source: healthSourceSession})
	close(release)
	select {
	case got := <-done:
		if got.err == nil {
			t.Fatalf("old probe admitted new exhaustion: machine=%q starts=%v", got.machine, taskStartMachines(f))
		}
	case <-ctx.Done():
		t.Fatal("probe did not finish")
	}
	if r := healthState(s, "l4", "work"); !r.unusable() || r.Kind != errKindUsageLimit || r.ResetsAt == nil || !r.ResetsAt.Equal(newReset) {
		t.Fatalf("old success erased new weekly limit: %+v", r)
	}
	if starts := taskStartMachines(f); len(starts) != 0 {
		t.Fatalf("new exhaustion started agents: %v", starts)
	}
}

func TestLauncherHealthManualProbeCannotEraseNewLimit(t *testing.T) {
	s, f := launcherHealthHub(t)
	ctx := context.Background()
	oldReset, newReset := time.Now().Add(time.Hour), time.Now().Add(7*24*time.Hour)
	s.recordHealth(ctx, "l4", "work", healthResult{err: "You've hit your session limit", kind: errKindUsageLimit, resetsAt: &oldReset, source: healthSourceSession})
	f.answerNode(assistantwire.DeviceLaunchersCheck, func(_ string, _ json.RawMessage) (any, error) {
		s.recordHealth(ctx, "l4", "work", healthResult{err: "You've hit your weekly limit", kind: errKindUsageLimit, resetsAt: &newReset, source: healthSourceSession})
		return proto.LauncherCheck{OK: true, Model: "m"}, nil
	})
	postJSON(t, s, "/api/v1/launchers/work/check?machine=l4")
	if r := healthState(s, "l4", "work"); !r.unusable() || r.ResetsAt == nil || !r.ResetsAt.Equal(newReset) {
		t.Fatalf("manual probe erased new weekly exhaustion: %+v", r)
	}
}

func TestLauncherHealthFailedProbeKeepsLaterReset(t *testing.T) {
	s, f := launcherHealthHubOn(t, newStorelessService)
	ctx := context.Background()
	shortReset := time.Now().Add(300 * time.Millisecond)
	weeklyReset := time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Minute)
	s.recordHealth(ctx, "l4", "work", healthResult{err: "You've hit your session limit", kind: errKindUsageLimit, resetsAt: &shortReset, source: healthSourceSession})
	editHealth(s, "l4", "work", func(r *LauncherHealth) { r.CheckedAt = time.Now().Add(-time.Minute) })
	line := fmt.Sprintf("You've hit your weekly limit · resets %s (UTC)", weeklyReset.Format("Jan 2, 3:04pm"))
	if reset := limitResetOf(healthResult{err: line}, time.Now()); reset == nil || !reset.Equal(weeklyReset) {
		t.Fatalf("fixture reset is not parseable: %v", reset)
	}
	f.checks["l4/work"] = proto.LauncherCheck{Agent: "claude", Error: line}
	if _, bad := s.launcherUnusableNow(ctx, "l4", "work"); !bad {
		t.Fatal("weekly exhaustion was not refused")
	}
	timer := time.NewTimer(time.Until(shortReset.Add(50 * time.Millisecond)))
	defer timer.Stop()
	<-timer.C
	mid, _, err := s.startTaskAgent(ctx, &PlanTask{Machine: "l4", Cwd: "/work", Launcher: "work", MachinePolicy: machineFixed}, false)
	if err == nil {
		t.Fatalf("old short reset admitted weekly exhaustion: machine=%q starts=%v", mid, taskStartMachines(f))
	}
	if r := healthState(s, "l4", "work"); !r.unusable() || r.ResetsAt == nil || !r.ResetsAt.Equal(weeklyReset) {
		t.Fatalf("failed probe lost later weekly reset: %+v", r)
	}
	if starts := taskStartMachines(f); len(starts) != 0 || checkedCount(f, "l4/work") != 1 {
		t.Fatalf("weekly exhaustion: starts=%v probes=%d", starts, checkedCount(f, "l4/work"))
	}
}

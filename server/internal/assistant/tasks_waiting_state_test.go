package assistant

import (
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A task whose agent ended its turn but waits on its own background work or on something the hub wakes it for is
// "waiting", not "followup" (#3274): only an agent that stopped with nothing to bring it back is a follow-up.
func TestTaskStateWaitingApartFromRunningAndStopped(t *testing.T) {
	now := time.Date(2026, 11, 10, 16, 16, 0, 0, time.UTC)
	todo := Todo{ID: "todo_1", Title: "等 CI"}
	running := proto.Session{MachineID: "m", SessionID: "run", State: proto.StateWorking, UpdatedAt: now}
	background := proto.Session{MachineID: "m", SessionID: "bg", State: proto.StateWorking, BackgroundWorking: true,
		Work: &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal, Line: "后台 1 个任务在跑，跑完会自己接着做"}, UpdatedAt: now}
	woken := proto.Session{MachineID: "m", SessionID: "ci", State: proto.StateIdle,
		Work: &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal, Wait: "CI 跑完"}, UpdatedAt: now}
	onAgent := proto.Session{MachineID: "m", SessionID: "sub", State: proto.StateIdle,
		Work: &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitAgent}, UpdatedAt: now}
	stopped := proto.Session{MachineID: "m", SessionID: "stop", State: proto.StateIdle,
		Work: &proto.WorkState{Phase: proto.WorkDone}, UpdatedAt: now}
	onUser := proto.Session{MachineID: "m", SessionID: "user", State: proto.StateIdle,
		Work: &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser}, UpdatedAt: now}
	finished := proto.Session{MachineID: "m", SessionID: "fin", State: proto.StateUnread,
		Work: &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal}, UpdatedAt: now}

	for _, c := range []struct {
		name   string
		agents []proto.Session
		want   string
	}{
		{"running", []proto.Session{running}, "working"},
		{"running beats waiting", []proto.Session{background, running}, "working"},
		{"background commands", []proto.Session{background}, "waiting"},
		{"hub wakes it for CI", []proto.Session{woken}, "waiting"},
		{"hub wakes it for another agent", []proto.Session{onAgent, stopped}, "waiting"},
		{"stopped", []proto.Session{stopped}, "followup"},
		{"no work state", []proto.Session{{MachineID: "m", SessionID: "x", State: proto.StateIdle}}, "followup"},
		{"waits for the user, no card", []proto.Session{onUser}, "followup"},
		{"finished unseen", []proto.Session{finished}, "unread"},
	} {
		if got := taskState(todo, c.agents); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if lead := taskLead("waiting", []proto.Session{stopped, background}); lead.SessionID != "bg" {
		t.Fatalf("a waiting task is shown with the agent that waits, got %s", lead.SessionID)
	}
	if label := taskStateLabel("waiting"); label == "" || label == taskStateLabel("followup") {
		t.Fatalf("waiting needs its own words for the assistant: %q", label)
	}
}

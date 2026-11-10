package assistant

import (
	"context"
	"fmt"

	"github.com/nwverse/persona/server/internal/proto"
)

// Idle says whether the manager can stop now without cutting its own work off, and what holds it when not:
// manager.RunBuiltin asks before the built-in manager's service starts again on a new binary. Its outbox holds
// nothing back: what it keeps for the hub waits in its database and goes once it runs again.
type Idle func(ctx context.Context) (idle bool, why string)

// extServing is what idle needs of the extensions (extmgr.Manager): the requests relayed to them being served.
type extServing interface{ Serving() int64 }

// managerIdle is a running manager's Idle: no request being answered (the hub's: an app's call, an extension's; the
// computers': an agent's tool call, a write the gate forwarded), no hub event waiting to be handled, no request or
// stream of the extensions' going (a voice call), and its service idle. A computer's card or question waits on a
// person, and is taken up again after a restart: it holds nothing back.
func managerIdle(ctx context.Context, b *BusHub, s *Service, ext extServing) (bool, string) {
	if n := b.answering.Load(); n > 0 {
		return false, fmt.Sprintf("answering %d request(s) of the hub's (an app's call, an extension's)", n)
	}
	if n := s.nodeAnswering.Load(); n > 0 {
		return false, fmt.Sprintf("answering %d request(s) of the computers' (an agent's tool call)", n)
	}
	if n := b.pending.Load(); n > 0 {
		return false, fmt.Sprintf("%d event(s) of the hub's not handled yet", n)
	}
	if ext != nil {
		if n := ext.Serving(); n > 0 {
			return false, fmt.Sprintf("%d request(s) or stream(s) of the extensions' open (a voice call, a live view)", n)
		}
	}
	return s.idle(ctx)
}

// waitsOnPerson: a computer's request that waits on a person (a hook's card, an elicitation), however long: it is
// taken up again after a restart, so idle does not wait for it.
func waitsOnPerson(method string) bool {
	return method == "interaction.create" || method == "computer.elicit"
}

// idle: none of its own conversations on a turn (when their machine does not answer it cannot tell, so not idle),
// no turn of an agent's being looked at (agents_work.go), no scheduled task's message and no channel's answer going
// out, and no task agent being started (including its failure bookkeeping).
func (s *Service) idle(ctx context.Context) (bool, string) {
	if s.taskStarting.Load() > 0 {
		return false, "a task's agent is starting"
	}
	s.sched.mu.Lock()
	sending := len(s.sched.sending)
	s.sched.mu.Unlock()
	if sending > 0 {
		return false, "a scheduled task's message is going out"
	}
	s.chans.mu.Lock()
	answering := len(s.chans.answering)
	s.chans.mu.Unlock()
	if answering > 0 {
		return false, "a channel's answer is going out"
	}
	s.work.mu.Lock()
	triaging := len(s.work.triaging)
	s.work.mu.Unlock()
	if triaging > 0 {
		return false, "an agent's finished turn is being looked at"
	}
	machine := s.settings(ctx).MachineID
	if machine == "" {
		return true, "" // no machine: none of its conversations can run
	}
	all, err := s.sessions(ctx, machine)
	if err != nil {
		return false, "its conversations' machine does not answer: " + err.Error()
	}
	for _, c := range all {
		if c.Assistant && (working(c) || (c.Compaction != nil && c.Compaction.State == proto.CompactionRunning)) {
			return false, "its conversation " + c.SessionID + " is on a turn"
		}
	}
	return true, ""
}

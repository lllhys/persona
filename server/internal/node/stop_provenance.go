package node

import (
	"context"
	"fmt"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func (s *stateFile) stopped(id string, cause proto.StopCause) {
	s.update(id, func(v *sessionState) bool {
		v.StopCause = cause
		v.StopCauseAt = max(time.Now().UnixNano(), v.StopCauseAt+1)
		return true
	})
}

// A new running observation invalidates the previous stopped-session capability.
func (n *Node) observedRunning(id string) {
	release, ok := n.recovery.tryLock(id)
	if !ok {
		return // Never clear a user stop while its close/quit is still in progress.
	}
	defer release()
	n.recordRunning(id)
}

func (n *Node) recordRunning(id string) {
	n.state.update(id, func(v *sessionState) bool {
		if v.StopCause == proto.StopUnknown {
			return false
		}
		v.StopCause = proto.StopUnknown
		v.StopCauseAt = max(time.Now().UnixNano(), v.StopCauseAt+1)
		return true
	})
}

// state.put is one-way. Verify the cause before closing or consuming a stopped
// capability: a failed save must not revive old recovery permission after a restart.
func (n *Node) saveStopCause(ctx context.Context, id string, cause proto.StopCause) error {
	n.state.stopped(id, cause)
	want, known := n.state.get(id)
	if !known || n.peer == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var saved struct {
		Sessions map[string]sessionState `json:"sessions"`
	}
	err := n.peer.Call(cctx, "state.load", map[string]any{}, &saved)
	got, ok := saved.Sessions[id]
	if err != nil || !ok || got.StopCause != cause || got.StopCauseAt != want.StopCauseAt {
		return refusal("stop_not_saved", "The stop state could not be saved. Nothing was started, stopped or closed; try again.", nil)
	}
	return nil
}

func (n *Node) stopCause(id string) proto.StopCause {
	st, _ := n.state.get(id)
	return st.StopCause
}

// Called again under the session's recovery lock: a cached assistant row must not
// undo a user close that happened after that row was listed.
func (n *Node) supervisorResumeAllowed(id, mode, launcher string) error {
	cause := n.stopCause(id)
	if !validID(id) || !cause.AllowsSupervisorResume() {
		return refusal("resume_not_allowed", fmt.Sprintf("Supervisor resume is refused: stop cause is %s. Use the owner's Resume.", cause), nil)
	}
	if mode != "" || launcher != "" {
		return refusal("resume_not_allowed", "Supervisor resume must keep the session's original launcher and permission mode.", nil)
	}
	if _, live := n.agent(id); live {
		return refusal("resume_not_allowed", "The session is running again; refresh it before sending.", nil)
	}
	return nil
}

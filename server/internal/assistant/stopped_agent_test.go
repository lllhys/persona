package assistant

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A stopped agent is read-only (node stopped.go, #4754): agent_send and agent_interrupt refuse it before anything is
// asked or sent, and point the model at agent_start (or an eligible explicit resume, agent_resume.go); /exit to it is
// a no-op. An older node's offline row without provenance is not authorization to resume.
func TestStoppedAgentIsReadOnlyForTheAssistant(t *testing.T) {
	s, f := newQuietService(t)
	ctx := t.Context()
	stopped := boxAgent("stop0001")
	stopped.Status, stopped.State = proto.StatusOffline, ""
	stopped.Capabilities, stopped.ReadOnlyReason = &proto.SessionCapabilities{Read: true, Terminal: true}, "stopped"
	f.setSessions("box", stopped)

	_, err := s.runTool(ctx, "server", "conv1", "agent_send", sendArgs("stop0001", "go on", ""))
	if err == nil || !strings.Contains(err.Error(), "has stopped") || !strings.Contains(err.Error(), "agent_start") {
		t.Fatalf("agent_send to a stopped agent: %v", err)
	}
	if res, err := s.runTool(ctx, "server", "conv1", "agent_send", sendArgs("stop0001", "/exit", "")); err != nil ||
		!strings.Contains(res, "already stopped") {
		t.Fatalf("/exit to a stopped agent: %q %v", res, err)
	}
	args, _ := json.Marshal(map[string]any{"machine": "box", "session_id": "stop0001", "then": "go on"})
	if _, err := s.runTool(ctx, "server", "conv1", "agent_interrupt", args); err == nil || !strings.Contains(err.Error(), "already stopped") {
		t.Fatalf("agent_interrupt of a stopped agent: %v", err)
	}
	if sent := sentToAgent(f); len(sent) != 0 {
		t.Fatalf("sent %q", sent)
	}
}

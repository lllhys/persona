package devices

import (
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A node's "claude.plugin" notification (node/claudeplugin.go) is the machine's ClaudePlugin from then on; one that
// says nothing does not wipe what node.info said.
func TestClaudePluginNotificationIsTheMachines(t *testing.T) {
	d := newTestDevices(t)
	m := newMachine(d, MachineConfig{ID: "box", Name: "box"})
	m.info.ClaudePlugin = &proto.ClaudePlugin{State: proto.ClaudePluginNotInstalled, Auto: true}
	if got := m.snapshot().ClaudePlugin; got == nil || got.State != proto.ClaudePluginNotInstalled {
		t.Fatalf("from node.info: %+v", got)
	}
	st, _ := json.Marshal(proto.ClaudePlugin{State: proto.ClaudePluginInstalled, Auto: true, Version: "0.1.0", Bundled: "0.1.0"})
	m.onEvent("claude.plugin", st)
	if got := m.snapshot().ClaudePlugin; got == nil || got.State != proto.ClaudePluginInstalled || got.Version != "0.1.0" {
		t.Fatalf("after the notification: %+v", got)
	}
	m.onEvent("claude.plugin", json.RawMessage(`{}`))
	if got := m.snapshot().ClaudePlugin; got == nil || got.State != proto.ClaudePluginInstalled {
		t.Fatalf("an empty notification: %+v", got)
	}
}

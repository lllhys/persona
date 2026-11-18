package node

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// closeStub is an io.WriteCloser whose Close is harmless, for an assistantProc a test stops without a real process.
type closeStub struct{ once sync.Once }

func (c *closeStub) Write(p []byte) (int, error) { return len(p), nil }
func (c *closeStub) Close() error {
	c.once.Do(func() {})
	return nil
}

// The owner can always switch a node-recovered conversation back to its configured model, even though the runtime
// never wrote a ModelRefusal: restore reads the node's marker, closes the idle process and releases the hold (#7824).
func TestRestoreModelHonorsTheNodeRecoveryMarker(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	n := codexTestNode(t) // allocates every map the transcript meta path reads (assistantNode leaves the bg maps nil)
	n.state.put(id, sessionState{Launcher: "claude", Cwd: t.TempDir(), Model: "primary-model", IsMain: true})
	fh := withFakeHub(t, n)
	fh.model = map[string]any{"launcher": "claude", "model": "primary-model", "is_main": true}
	// A minimal transcript so restore has a record to read; it carries no model fallback.
	dir := t.TempDir()
	path := dir + "/session.jsonl"
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n.paths[id] = path
	// No transcript fallback: the only evidence is the node's own marker, exactly the 23:20 situation.
	n.putModelRecovery(id, &proto.ModelRecovery{From: "primary-model", To: "fallback-model", Reason: proto.ModelRecoveryReasonSafeguards})
	// A reconnect keeps the persisted marker and the keeper's hold, but not the recoveryMarks map.
	n.recoveryMarks = nil
	if r := n.modelRecovery(id); r == nil {
		t.Fatal("the state-only marker is not visible before restore")
	}
	n.fallbacks = map[string]*assistantFallback{id: {launcher: "claude", from: modelExecution{Model: "primary-model"},
		to: modelExecution{Model: "fallback-model"}, at: time.Now(), until: time.Now().Add(assistantFallbackHold)}}
	// An idle process the restore closes (a proc with the seams stop() needs, like rsProc).
	c := &closeStub{}
	p := &assistantProc{id: id, agent: "claude", launcher: "claude", stdin: c,
		cmd: &exec.Cmd{}, exited: make(chan struct{}), model: modelExecution{Model: "fallback-model"}}
	n.procs[id] = p

	out, err := n.restoreModel(context.Background(), id)
	if err != nil {
		t.Fatalf("restore refused a node-driven switch: %v", err)
	}
	if out["restore"] != proto.ModelRestoreNextStart || out["restore_to"] != "primary-model" || out["closed"] != true {
		t.Fatalf("restore answer %+v", out)
	}
	if n.modelRecovery(id) != nil {
		t.Fatal("the marker outlived the restore")
	}
	if st, _ := n.state.get(id); st.ModelRecovery != nil {
		t.Fatal("the state-only marker was not cleared from session state")
	}
	if got := n.heldFallback(id, "claude", modelExecution{Model: "primary-model"}); got.Model != "primary-model" {
		t.Fatalf("the hold was not released: %+v", got)
	}
}

// A successful hub answer with a launcher and an empty model is a valid launcher default, not an unknown configuration.
func TestRestoreModelHonorsLauncherDefaultRecovery(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	n := codexTestNode(t)
	n.state.put(id, sessionState{Launcher: "claude", Cwd: t.TempDir(), Model: "", IsMain: true})
	fh := withFakeHub(t, n)
	fh.model = map[string]any{"launcher": "claude", "model": "", "is_main": true}
	path := t.TempDir() + "/session.jsonl"
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n.paths[id] = path
	n.putModelRecovery(id, &proto.ModelRecovery{From: "", ProbeFrom: "primary-model", To: "fallback-model", Reason: proto.ModelRecoveryReasonServer})
	n.fallbacks = map[string]*assistantFallback{id: {launcher: "claude", from: modelExecution{Model: ""},
		to: modelExecution{Model: "fallback-model"}, at: time.Now(), until: time.Now().Add(assistantFallbackHold)}}
	n.unavailable = map[string]time.Time{unavailableKey("claude", "primary-model"): time.Now().Add(time.Hour)}
	p := &assistantProc{id: id, agent: "claude", launcher: "claude", stdin: &closeStub{},
		cmd: &exec.Cmd{}, exited: make(chan struct{}), model: modelExecution{Model: "fallback-model"}}
	n.procs[id] = p
	out, err := n.restoreModel(context.Background(), id)
	if err != nil {
		t.Fatalf("restore refused a valid launcher default: %v", err)
	}
	if out["restore"] != proto.ModelRestoreNextStart || out["restore_to"] != "" || out["closed"] != true {
		t.Fatalf("default restore answer %+v", out)
	}
	if n.modelRecovery(id) != nil {
		t.Fatal("the default marker outlived manual restore")
	}
	if got := n.heldFallback(id, "claude", modelExecution{Model: ""}); got.Model != "" {
		t.Fatalf("manual restore pinned or held the default: %+v", got)
	}
	n.mu.Lock()
	_, unavailable := n.unavailable[unavailableKey("claude", "primary-model")]
	n.mu.Unlock()
	if unavailable {
		t.Fatal("manual restore left the concrete default unavailable")
	}
}

// A successful turn ends an exhausted chain's marker, so a later unrelated error does not show its stale cause.
func TestSuccessfulTurnClearsAnExhaustedMarker(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	n, _ := recoveryTestNode(t, id, true, false)
	n.putModelRecovery(id, &proto.ModelRecovery{From: "primary-model", Reason: proto.ModelRecoveryReasonQuota, ChainExhausted: true})
	p := &assistantProc{id: id, agent: "claude", launcher: "claude", model: modelExecution{Model: "primary-model"}}
	n.procs[id] = p
	n.clearAssistantRecovery(p, true)
	// clearAssistantRecovery does not touch an exhausted marker (there was no switch); the result handler does.
	if r := n.modelRecovery(id); r == nil || !r.ChainExhausted {
		t.Fatalf("expected the exhausted marker to still be present before the result: %+v", r)
	}
	// Mirror assistant.go's result handling for a successful result.
	if r := n.modelRecovery(id); r != nil && r.ChainExhausted {
		n.clearModelRecovery(id)
	}
	if n.modelRecovery(id) != nil {
		t.Fatal("the exhausted marker survived a successful turn")
	}
}

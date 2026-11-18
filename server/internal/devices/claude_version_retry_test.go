package devices

import (
	"context"
	"encoding/json"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// A node over an in-memory pipe that answers node.info without a Claude version the first `empty` times.
func versionNode(t *testing.T, empty int32, version string) (*rpc.Peer, *atomic.Int32) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	toNode, fromHub := io.Pipe()
	toHub, fromNode := io.Pipe()
	var asked atomic.Int32
	node := rpc.New("n", fromNode)
	node.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method != "node.info" {
			return map[string]any{}, nil
		}
		if asked.Add(1) <= empty {
			return proto.NodeInfo{Hostname: "box"}, nil
		}
		return proto.NodeInfo{Hostname: "box", ClaudeVersion: version}, nil
	}
	go func() { _ = node.Serve(ctx, toNode) }()
	hub := rpc.New("h", fromHub)
	go func() { _ = hub.Serve(ctx, toHub) }()
	return hub, &asked
}

func shortClaudeVersionRetry(t *testing.T) {
	saved := claudeVersionRetry.Load()
	short := []time.Duration{5 * time.Millisecond, 10 * time.Millisecond}
	claudeVersionRetry.Store(&short)
	t.Cleanup(func() { claudeVersionRetry.Store(saved) })
}

func TestAMissingClaudeVersionIsAskedForAgain(t *testing.T) {
	shortClaudeVersionRetry(t)
	peer, asked := versionNode(t, 2, "2.1.316")
	m := newMachine(newTestDevices(t), MachineConfig{ID: "box", Kind: "ssh", SSHHost: "box"})
	m.mu.Lock()
	m.peer, m.info, m.online = peer, proto.NodeInfo{Hostname: "box"}, true
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.retryClaudeVersion(ctx, peer) // returns once it has the version
	m.mu.Lock()
	got, host := m.info.ClaudeVersion, m.info.Hostname
	m.mu.Unlock()
	if got != "2.1.316" || host != "box" {
		t.Fatalf("info = %q on %q, want 2.1.316 on box", got, host)
	}
	if n := asked.Load(); n != 3 {
		t.Errorf("node.info asked %d times, want 3", n)
	}
}

func TestAClaudeVersionFromAnOldConnectionIsNotShown(t *testing.T) {
	shortClaudeVersionRetry(t)
	old, _ := versionNode(t, 0, "2.1.300")
	current, _ := versionNode(t, 0, "2.1.316")
	m := newMachine(newTestDevices(t), MachineConfig{ID: "box", Kind: "ssh", SSHHost: "box"})
	m.mu.Lock()
	m.peer, m.info = current, proto.NodeInfo{Hostname: "box", ClaudeVersion: "2.1.316"}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.retryClaudeVersion(ctx, old)
	m.mu.Lock()
	got := m.info.ClaudeVersion
	m.mu.Unlock()
	if got != "2.1.316" {
		t.Fatalf("version = %q: the replaced connection's answer overwrote the current one", got)
	}
}

func TestAskingForTheClaudeVersionStopsWithTheConnection(t *testing.T) {
	shortClaudeVersionRetry(t)
	peer, asked := versionNode(t, 1<<30, "")
	m := newMachine(newTestDevices(t), MachineConfig{ID: "box", Kind: "ssh", SSHHost: "box"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.retryClaudeVersion(ctx, peer) }()
	deadline := time.Now().Add(5 * time.Second)
	for asked.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if asked.Load() < 3 {
		t.Fatalf("asked %d times, want it to keep asking while connected", asked.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("still asking after the connection ended")
	}
}

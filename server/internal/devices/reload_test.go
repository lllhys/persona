package devices

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/nodeproto"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestInitialHandshakeRetriesResidentExec(t *testing.T) {
	d := newTestDevices(t)
	n := &testNode{}
	m := newMachine(d, MachineConfig{ID: "example-machine", Kind: "ssh"})
	var dials atomic.Int32
	m.dialNode = func(ctx context.Context) (net.Conn, error) {
		if dials.Add(1) != 1 {
			return n.dial(ctx)
		}
		a, b := net.Pipe()
		peer := rpc.New("n", b)
		peer.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
			_ = b.Close() // exec interrupted only the handshake; no application operation was admitted
			return nil, rpc.ErrClosed
		}
		go func() { _ = peer.Serve(ctx, b); _ = b.Close() }()
		return a, nil
	}
	m.warmUntil, m.warmed = time.Now().Add(5*time.Second), make(chan struct{})
	d.addEntry(m)
	runMachine(t, m)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := d.Call(ctx, devicewire.NodeCall{MachineID: m.cfg.ID, Method: "session.send", Params: json.RawMessage(`{"session_id":"example-session"}`)}); err != nil {
		t.Fatalf("first request failed instead of waiting for the retried handshake: %v", err)
	}
	if dials.Load() != 2 || len(n.seen("session.send")) != 1 {
		t.Fatalf("handshake dials=%d application calls=%d", dials.Load(), len(n.seen("session.send")))
	}
}

func TestHandshakeDoesNotRetryARealRefusal(t *testing.T) {
	d := newTestDevices(t)
	m := newMachine(d, MachineConfig{ID: "example-machine", Kind: "ssh"})
	var dials atomic.Int32
	m.dialNode = func(ctx context.Context) (net.Conn, error) {
		dials.Add(1)
		a, b := net.Pipe()
		peer := rpc.New("n", b)
		peer.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
			return nil, &rpc.Error{Code: "refused", Message: "refused"}
		}
		go func() { _ = peer.Serve(ctx, b); _ = b.Close() }()
		return a, nil
	}
	d.addEntry(m)
	path := &nodePath{src: m}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := m.establishVia(ctx, path, "ssh")
	var coded *rpc.Error
	if !errors.As(err, &coded) || coded.Code != "refused" || dials.Load() != 1 {
		t.Fatalf("refusal was retried or lost: error=%v dials=%d", err, dials.Load())
	}
}

func TestResidentReloadRetriesOnlyNotStartedRequests(t *testing.T) {
	for _, dropped := range []bool{false, true} {
		t.Run(map[bool]string{false: "not-started", true: "ambiguous-drop"}[dropped], func(t *testing.T) {
			d := newTestDevices(t)
			m := newMachine(d, MachineConfig{ID: "example-machine", Kind: "ssh"})
			var attempts, effects atomic.Int32
			m.dialNode = func(ctx context.Context) (net.Conn, error) {
				a, b := net.Pipe()
				peer := rpc.New("n", b)
				peer.OnRequest = func(_ context.Context, stringMethod string, params json.RawMessage) (any, error) {
					switch stringMethod {
					case "node.info":
						return proto.NodeInfo{Version: "test"}, nil
					case "sessions.list":
						return map[string]any{"sessions": []proto.Session{}}, nil
					case "session.send":
						if attempts.Add(1) == 1 {
							if !dropped {
								return nil, &rpc.Error{Code: nodeproto.CodeRestarting, Message: nodeproto.CodeRestarting}
							}
							effects.Add(1)
							_ = b.Close()
							return nil, rpc.ErrClosed
						}
						effects.Add(1)
					}
					return map[string]any{"ok": true}, nil
				}
				go func() { _ = peer.Serve(ctx, b); _ = b.Close() }()
				return a, nil
			}
			d.addEntry(m)
			runMachine(t, m)
			eventually(t, time.Second, "node ready", func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.online })
			m.mu.Lock()
			m.reloadUntil = time.Now().Add(5 * time.Second)
			m.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := d.Call(ctx, devicewire.NodeCall{MachineID: m.cfg.ID, Method: "session.send", Params: json.RawMessage(`{"session_id":"example-session"}`)})
			if dropped {
				var coded *rpc.Error
				if !errors.As(err, &coded) || coded.Code != devicewire.CodeNodeClosed || attempts.Load() != 1 {
					t.Fatalf("ambiguous operation replayed: %v attempts=%d", err, attempts.Load())
				}
			} else if err != nil || attempts.Load() != 2 {
				t.Fatalf("not-started refusal did not retry: %v attempts=%d", err, attempts.Load())
			}
			if effects.Load() != 1 {
				t.Fatalf("side effect count=%d", effects.Load())
			}
		})
	}
}

// A call that never went out is said to be one (rpc.ErrNotSent), for a caller that must tell it from one that may have
// reached the node: the node refused it as restarting until the retry budget or the caller's wait ran out between
// tries, or it waited for the resident handover with no connection until the caller's wait ran out. A send that may
// have been taken stays unmarked.
func TestACallThatNeverWentOutSaysSo(t *testing.T) {
	// restarting is a machine whose node refuses every session.send as restarting, counting the tries.
	restarting := func(t *testing.T) (*Devices, *Machine, *atomic.Int32) {
		d := newTestDevices(t)
		m := newMachine(d, MachineConfig{ID: "example-machine", Kind: "ssh"})
		var attempts atomic.Int32
		m.dialNode = func(ctx context.Context) (net.Conn, error) {
			a, b := net.Pipe()
			peer := rpc.New("n", b)
			peer.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
				switch method {
				case "node.info":
					return proto.NodeInfo{Version: "test"}, nil
				case "sessions.list":
					return map[string]any{"sessions": []proto.Session{}}, nil
				case "session.send":
					attempts.Add(1)
					return nil, &rpc.Error{Code: nodeproto.CodeRestarting, Message: nodeproto.CodeRestarting}
				}
				return map[string]any{"ok": true}, nil
			}
			go func() { _ = peer.Serve(ctx, b); _ = b.Close() }()
			return a, nil
		}
		d.addEntry(m)
		runMachine(t, m)
		eventually(t, time.Second, "node ready", func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.online })
		return d, m, &attempts
	}
	t.Run("refused as restarting until the retries ran out", func(t *testing.T) {
		old := reloadRetryBudget
		reloadRetryBudget = 250 * time.Millisecond
		t.Cleanup(func() { reloadRetryBudget = old })
		d, m, attempts := restarting(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := d.CallNode(ctx, m.cfg.ID, "session.send", json.RawMessage(`{"session_id":"example-session"}`))
		if !errors.Is(err, rpc.ErrNotSent) || !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || attempts.Load() < 2 {
			t.Fatalf("a send the node refused as restarting until the retries ran out: %v (attempts %d, caller %v), want a deadline marked not sent",
				err, attempts.Load(), ctx.Err())
		}
	})
	t.Run("refused as restarting until the caller's wait ran out", func(t *testing.T) {
		// The next try is a minute away: the caller's wait runs out while the manager waits for it, never while a try
		// is out, however slowly the first refusal comes back within that wait.
		old := reloadRetryEvery
		reloadRetryEvery = time.Minute
		t.Cleanup(func() { reloadRetryEvery = old })
		d, m, attempts := restarting(t)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := d.CallNode(ctx, m.cfg.ID, "session.send", json.RawMessage(`{"session_id":"example-session"}`))
		if !errors.Is(err, rpc.ErrNotSent) || !errors.Is(err, context.DeadlineExceeded) || attempts.Load() != 1 {
			t.Fatalf("a send the node refused as restarting until the wait ran out: %v (attempts %d), want a deadline marked not sent after one refusal",
				err, attempts.Load())
		}
	})
	t.Run("waited for the resident handover", func(t *testing.T) {
		d := newTestDevices(t)
		m := newMachine(d, MachineConfig{ID: "example-machine"})
		d.addEntry(m)
		m.mu.Lock()
		m.warmUntil, m.warmed = time.Now().Add(5*time.Second), make(chan struct{})
		m.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := d.CallNode(ctx, m.cfg.ID, "session.send", json.RawMessage(`{"session_id":"example-session"}`))
		if !errors.Is(err, rpc.ErrNotSent) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("a send that waited for the handover until its wait ran out: %v, want a deadline marked not sent", err)
		}
	})
	t.Run("dropped once it went out", func(t *testing.T) {
		d := newTestDevices(t)
		m := newMachine(d, MachineConfig{ID: "example-machine", Kind: "ssh"})
		m.dialNode = func(ctx context.Context) (net.Conn, error) {
			a, b := net.Pipe()
			peer := rpc.New("n", b)
			peer.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
				switch method {
				case "node.info":
					return proto.NodeInfo{Version: "test"}, nil
				case "sessions.list":
					return map[string]any{"sessions": []proto.Session{}}, nil
				case "session.send":
					_ = b.Close() // it may have been typed: the connection goes before the answer
					return nil, rpc.ErrClosed
				}
				return map[string]any{"ok": true}, nil
			}
			go func() { _ = peer.Serve(ctx, b); _ = b.Close() }()
			return a, nil
		}
		d.addEntry(m)
		runMachine(t, m)
		eventually(t, time.Second, "node ready", func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.online })
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := d.CallNode(ctx, m.cfg.ID, "session.send", json.RawMessage(`{"session_id":"example-session"}`))
		if err == nil || errors.Is(err, rpc.ErrNotSent) {
			t.Fatalf("a send dropped once it went out: %v, want an error not marked not sent", err)
		}
	})
}

func TestSchedulingAndAppsAgreeDuringResidentReconnect(t *testing.T) {
	d := newTestDevices(t)
	m := newMachine(d, MachineConfig{ID: "example-machine"})
	d.addEntry(m)
	m.mu.Lock()
	m.warmUntil, m.warmed = time.Now().Add(time.Second), make(chan struct{})
	m.mu.Unlock()
	app, summary := m.snapshot(), d.summaryOf(m)
	if !app.Online || !summary.Online || !summary.Reachable {
		t.Fatal("scheduling took a waiting machine offline while the apps kept it online")
	}
	m.mu.Lock()
	m.endWarmLocked()
	m.mu.Unlock()
	if m.snapshot().Online || d.summaryOf(m).Reachable {
		t.Fatal("a real offline machine remained available after the existing grace ended")
	}
}

func TestResidentBarrierAbortClearsPauseAndReloadWindow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	peer, remote := rpc.New("m", a), rpc.New("n", b)
	admitted, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	remote.OnRequest = func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		if method == "slow" {
			close(admitted)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return map[string]bool{"ok": true}, nil
	}
	go func() { _ = peer.Serve(ctx, a) }()
	go func() { _ = remote.Serve(ctx, b) }()
	go func() { _ = peer.Call(ctx, "slow", nil, nil) }()
	select {
	case <-admitted:
	case <-time.After(time.Second):
		t.Fatal("old request was not admitted")
	}
	m := &Machine{peer: peer, reconnectGrace: 5 * time.Second}
	barrier, end := context.WithTimeout(ctx, 20*time.Millisecond)
	defer end()
	if _, err := m.residentRestartBarrier(barrier, peer, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("barrier did not wait for the old reply: %v", err)
	}
	m.mu.Lock()
	until := m.reloadUntil
	m.mu.Unlock()
	if !until.IsZero() {
		t.Fatal("failed barrier retained its reload window")
	}
	call, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err := peer.Call(call, "fast", nil, nil); err != nil {
		t.Fatalf("failed barrier left new requests suspended: %v", err)
	}
}

func TestResidentBarrierFromOldPeerCannotResumeTheNewPeer(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	old, current := rpc.New("old", a), rpc.New("current", b)
	current.SuspendCalls()
	until := time.Now().Add(time.Second)
	m := &Machine{peer: current, reloadUntil: until, reconnectGrace: 5 * time.Second}
	if _, err := m.residentRestartBarrier(context.Background(), old, false); !errors.Is(err, rpc.ErrClosed) {
		t.Fatalf("stale barrier was accepted: %v", err)
	}
	if !m.reloadUntil.Equal(until) {
		t.Fatal("stale barrier changed the new connection's reload window")
	}
	if err := current.Call(context.Background(), "unsent", nil, nil); !errors.Is(err, rpc.ErrSuspended) {
		t.Fatalf("stale barrier resumed the new peer: %v", err)
	}
}

package node

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

const runtimeRetrySession = "watch-retry"

func runtimeRetryList(id string) map[string]any {
	return map[string]any{"sessions": []map[string]any{{"session_id": id, "status": "working"}}}
}

// pushRuntimeRetryList waits until the runtime has taken the list in. The watch RPC itself is asynchronous.
func pushRuntimeRetryList(t *testing.T, host *fakeRuntime, list map[string]any) {
	t.Helper()
	_ = host.peer.Notify("runtime.sessions", list)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = host.peer.Call(ctx, "runtime.package", map[string]any{"name": "-"}, &struct{}{})
}

func runtimeRetryConn(n *Node) *runtimeConn {
	n.runtimes.mu.Lock()
	defer n.runtimes.mu.Unlock()
	return n.runtimes.byRuntime("dsh")
}

// automaticRetryWatch establishes a hub watch on one real runtime connection, then replaces that connection. Its next
// list makes setSessions start an automatic watch on the new connection.
func automaticRetryWatch(t *testing.T) (*Node, *runtimeHub, *fakeRuntime) {
	t.Helper()
	n, hub := runtimeNode(t)
	first := dialRuntime(t, n, dshHello())
	pushRuntimeRetryList(t, first, runtimeRetryList(runtimeRetrySession))
	runtimeSessionCall(t, hub, "session.watch", map[string]any{"session_id": runtimeRetrySession, "since_seq": 7})
	if got := len(first.got("session.watch")); got != 1 {
		t.Fatalf("initial watch calls = %d, want 1", got)
	}
	_ = first.conn.Close()
	waitFor(t, "the first runtime to leave", func() bool { return runtimeGone(n, "dsh") })
	second := dialRuntime(t, n, dshHello())
	return n, hub, second
}

// waitForAutomaticWatchFailure waits until the asynchronous caller has cleared the failed attempt. The old implementation
// never clears it, so the next list exposes the bug.
func waitForAutomaticWatchFailure(t *testing.T, n *Node, h *runtimeConn, id string) {
	waitFor(t, "the automatic watch failure to clear its registration", func() bool { return !n.runtimes.watchedOn(h, id) })
}

// runtimeRetryWatchFailed observes the completion of watchFailed itself rather than assuming another RPC is a barrier.
func runtimeRetryWatchFailed(h *runtimeConn) <-chan struct{} {
	ch := make(chan struct{})
	var once sync.Once
	h.mu.Lock()
	h.onWatchFailed = func() { once.Do(func() { close(ch) }) }
	h.mu.Unlock()
	return ch
}

func TestRuntimeWatchRetriesAfterTransientError(t *testing.T) {
	n, hub, host := automaticRetryWatch(t)
	var calls atomic.Int32
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.watch" && calls.Add(1) == 1 {
			return nil, errors.New("transient watch failure")
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()
	h := runtimeRetryConn(n)

	pushRuntimeRetryList(t, host, runtimeRetryList(runtimeRetrySession))
	waitFor(t, "the failed automatic watch", func() bool { return len(host.got("session.watch")) == 1 })
	waitForAutomaticWatchFailure(t, n, h, runtimeRetrySession)
	pushRuntimeRetryList(t, host, runtimeRetryList(runtimeRetrySession))
	waitFor(t, "exactly one retry after the next list", func() bool { return len(host.got("session.watch")) == 2 })

	// A successful retry changes no registration state, so an unchanged list does not start a third call.
	pushRuntimeRetryList(t, host, runtimeRetryList(runtimeRetrySession))
	if got := len(host.got("session.watch")); got != 2 {
		t.Fatalf("watch calls after the successful retry and an unchanged list = %d, want 2", got)
	}
	for _, raw := range host.got("session.watch") {
		var p struct {
			SinceSeq int `json:"since_seq"`
		}
		_ = json.Unmarshal(raw, &p)
		if p.SinceSeq != 7 {
			t.Fatalf("watch resumed from %d, want 7", p.SinceSeq)
		}
	}

	_ = host.peer.Notify("session.items", map[string]any{"session_id": runtimeRetrySession, "to_seq": 8,
		"items": []proto.Item{{ID: "retry-live", Seq: 8, Role: "assistant", Kind: proto.KindText, Text: "live after retry"}}})
	waitFor(t, "a live item after the retry", func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		for _, raw := range hub.items {
			if strings.Contains(string(raw), "live after retry") {
				return true
			}
		}
		return false
	})
}

func TestRuntimeWatchPendingAttemptFencesOlderFailure(t *testing.T) {
	n, hub, host := automaticRetryWatch(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method != "session.watch" {
			return map[string]any{"ok": true}, nil
		}
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return nil, errors.New("older attempt failed")
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()
	h := runtimeRetryConn(n)

	pushRuntimeRetryList(t, host, runtimeRetryList(runtimeRetrySession))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out holding the first automatic watch")
	}
	for range 3 {
		pushRuntimeRetryList(t, host, runtimeRetryList(runtimeRetrySession))
	}
	if got := len(host.got("session.watch")); got != 1 {
		t.Fatalf("lists while attempt A was pending produced %d calls, want 1", got)
	}

	// B is an explicit, newer watch. It completes before A fails, so A must not clear B's registration.
	runtimeSessionCall(t, hub, "session.watch", map[string]any{"session_id": runtimeRetrySession, "since_seq": 11})
	if got := len(host.got("session.watch")); got != 2 {
		t.Fatalf("newer explicit watch calls = %d, want 2", got)
	}
	failed := runtimeRetryWatchFailed(h)
	close(release)
	<-failed
	if !n.runtimes.watchedOn(h, runtimeRetrySession) {
		t.Fatal("stale attempt A failure cleared successful attempt B")
	}
	pushRuntimeRetryList(t, host, runtimeRetryList(runtimeRetrySession))
	if got := len(host.got("session.watch")); got != 2 {
		t.Fatalf("stale attempt A failure scheduled %d total calls, want 2", got)
	}
}

func TestRuntimeWatchStaleFailureCannotClearRewatch(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "unwatch"
		if reset {
			name = "reset"
		}
		t.Run(name, func(t *testing.T) {
			n, hub, host := automaticRetryWatch(t)
			started := make(chan struct{})
			release := make(chan struct{})
			var calls atomic.Int32
			host.mu.Lock()
			host.answer = func(method string, params json.RawMessage) (any, error) {
				if method != "session.watch" {
					return map[string]any{"ok": true}, nil
				}
				if calls.Add(1) == 1 {
					close(started)
					<-release
					return nil, errors.New("stale watch failure")
				}
				return map[string]any{"ok": true}, nil
			}
			host.mu.Unlock()
			h := runtimeRetryConn(n)

			pushRuntimeRetryList(t, host, runtimeRetryList(runtimeRetrySession))
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("timed out holding the stale automatic watch")
			}
			if reset {
				n.runtimes.resetWatches()
			} else {
				runtimeSessionCall(t, hub, "session.unwatch", map[string]any{"session_id": runtimeRetrySession})
			}
			runtimeSessionCall(t, hub, "session.watch", map[string]any{"session_id": runtimeRetrySession, "since_seq": 12})
			failed := runtimeRetryWatchFailed(h)
			close(release)
			<-failed
			if !n.runtimes.watchedOn(h, runtimeRetrySession) {
				t.Fatal("stale failure cleared the new watch generation")
			}
			pushRuntimeRetryList(t, host, runtimeRetryList(runtimeRetrySession))
			if got := len(host.got("session.watch")); got != 2 {
				t.Fatalf("watch calls after the stale failure = %d, want 2", got)
			}
		})
	}
}

func TestRuntimeWatchStaleFailureCannotClearNewConnection(t *testing.T) {
	n, _, oldHost := automaticRetryWatch(t)
	started := make(chan struct{})
	release := make(chan struct{})
	oldHost.mu.Lock()
	oldHost.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.watch" {
			close(started)
			<-release
			return nil, errors.New("old connection watch failure")
		}
		return map[string]any{"ok": true}, nil
	}
	oldHost.mu.Unlock()
	oldConn := runtimeRetryConn(n)
	pushRuntimeRetryList(t, oldHost, runtimeRetryList(runtimeRetrySession))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out holding the old connection's watch")
	}

	newHost := dialRuntime(t, n, dshHello())
	pushRuntimeRetryList(t, newHost, runtimeRetryList(runtimeRetrySession))
	waitFor(t, "the new connection's watch", func() bool { return len(newHost.got("session.watch")) == 1 })
	newConn := runtimeRetryConn(n)
	if newConn == oldConn {
		t.Fatal("the replacement connection did not become the owner")
	}
	failed := runtimeRetryWatchFailed(oldConn)
	close(release)
	<-failed
	if !n.runtimes.watchedOn(newConn, runtimeRetrySession) {
		t.Fatal("an error on the old connection serial cleared the new connection")
	}
	pushRuntimeRetryList(t, newHost, runtimeRetryList(runtimeRetrySession))
	if got := len(newHost.got("session.watch")); got != 1 {
		t.Fatalf("new connection watch calls = %d, want 1", got)
	}
}

func TestRuntimeWatchDirectFailureRetriesOnNextList(t *testing.T) {
	for _, withTranscript := range []bool{false, true} {
		name := "ordinary"
		if withTranscript {
			name = "transcript"
		}
		t.Run(name, func(t *testing.T) {
			n, hub := runtimeNode(t)
			host := dialRuntime(t, n, paseoHello())
			row := map[string]any{"session_id": "direct-watch-retry", "status": "working"}
			if withTranscript {
				sdkTranscript(t, n, sdkSession, "before the watch")
				row["transcript"] = sdkSession
				row["agent"] = "claude"
			}
			pushRuntimeRetryList(t, host, map[string]any{"sessions": []map[string]any{row}})
			var fail atomic.Bool
			fail.Store(true)
			host.mu.Lock()
			host.answer = func(method string, params json.RawMessage) (any, error) {
				if method == "session.watch" && fail.CompareAndSwap(true, false) {
					return nil, errors.New("direct watch failure")
				}
				return map[string]any{"ok": true}, nil
			}
			host.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := hub.nodePeer.Call(ctx, "session.watch", map[string]any{"session_id": "direct-watch-retry", "since_seq": 3}, nil)
			cancel()
			if withTranscript && err != nil {
				t.Fatalf("a transcript watch keeps its transcript subscription after a runtime error: %v", err)
			}
			if !withTranscript && err == nil {
				t.Fatal("an ordinary direct watch did not report the runtime error")
			}
			if got := len(host.got("session.watch")); got != 1 {
				t.Fatalf("direct watch calls = %d, want 1", got)
			}
			pushRuntimeRetryList(t, host, map[string]any{"sessions": []map[string]any{row}})
			waitFor(t, "the direct watch retry", func() bool { return len(host.got("session.watch")) == 2 })
		})
	}
}

func TestRuntimeWatchTakenOverFailureRetriesOnNextList(t *testing.T) {
	n, hub := runtimeNode(t)
	sdkTranscript(t, n, sdkSession, "where we left off")
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	host := dialRuntime(t, n, paseoEngineHello())
	row := map[string]any{"session_id": sdkSession, "transcript": sdkSession, "agent": "claude", "status": "working", "cwd": "/w/app"}
	var fail atomic.Bool
	fail.Store(true)
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "session.start":
			return map[string]any{"session": row}, nil
		case "session.watch":
			if fail.CompareAndSwap(true, false) {
				return nil, errors.New("takeover watch failure")
			}
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()
	waitFor(t, "listed offline", func() bool { freshOffline(); return listed(t, n)[sdkSession].Status == proto.StatusOffline })
	runtimeSessionCall(t, hub, "session.watch", map[string]any{"session_id": sdkSession, "since_seq": 0})
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": sdkSession, "text": "carry on"}); err != nil {
		t.Fatal(err)
	}
	if got := len(host.got("session.watch")); got != 1 {
		t.Fatalf("takeover watch calls = %d, want 1", got)
	}
	pushRuntimeRetryList(t, host, map[string]any{"sessions": []map[string]any{row}})
	waitFor(t, "the takeover watch retry", func() bool { return len(host.got("session.watch")) == 2 })
}

func TestRuntimeWatchTranscriptMovedFailureRetriesOnNextList(t *testing.T) {
	n, hub := runtimeNode(t)
	host := dialRuntime(t, n, paseoHello())
	row := map[string]any{"session_id": "moved-watch-retry", "transcript": sdkSession, "agent": "claude", "status": "working"}
	list := map[string]any{"sessions": []map[string]any{row}}
	pushRuntimeRetryList(t, host, list)
	runtimeSessionCall(t, hub, "session.watch", map[string]any{"session_id": "moved-watch-retry", "since_seq": 0})
	if got := len(host.got("session.watch")); got != 1 {
		t.Fatalf("initial watch calls = %d, want 1", got)
	}
	n.runtimes.mu.Lock()
	h := n.runtimes.byRuntime("paseo")
	n.runtimes.mu.Unlock()

	sdkTranscript(t, n, sdkSession, "the transcript exists now")
	var fail atomic.Bool
	fail.Store(true)
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.watch" && fail.CompareAndSwap(true, false) {
			return nil, errors.New("moved transcript watch failure")
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()
	pushRuntimeRetryList(t, host, list)
	waitFor(t, "the moved transcript watch", func() bool { return len(host.got("session.watch")) == 2 })
	waitForAutomaticWatchFailure(t, n, h, "moved-watch-retry")
	pushRuntimeRetryList(t, host, list)
	waitFor(t, "the moved transcript watch retry", func() bool { return len(host.got("session.watch")) == 3 })
	calls := host.got("session.watch")
	var p map[string]any
	_ = json.Unmarshal(calls[len(calls)-1], &p)
	if p["transcript"] != true {
		t.Fatalf("retry did not preserve transcript mode: %v", p)
	}
}

func TestReviewRuntimeWatchFailureDuringOwnershipGap(t *testing.T) {
	n, _, host := automaticRetryWatch(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.watch" && calls.Add(1) == 1 {
			close(started)
			<-release
			return nil, errors.New("watch failed while unlisted")
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()

	pushRuntimeRetryList(t, host, runtimeRetryList(runtimeRetrySession))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out holding automatic watch")
	}
	// The session can briefly disappear while its automatic watch is still pending. The failed RPC belongs to this
	// same live connection, so listing the session again must retry it even though ownership was empty at the error.
	pushRuntimeRetryList(t, host, map[string]any{"sessions": []map[string]any{}})
	h := runtimeRetryConn(n)
	failed := runtimeRetryWatchFailed(h)
	close(release)
	<-failed
	pushRuntimeRetryList(t, host, runtimeRetryList(runtimeRetrySession))
	waitFor(t, "retry when the same connection lists the session again", func() bool {
		return len(host.got("session.watch")) == 2
	})
}

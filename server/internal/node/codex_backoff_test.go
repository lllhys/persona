package node

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A failing Unix endpoint waits 5, 10, 20, 40, then 60 s between attempts while its socket looks the same; a changed
// socket, another spec or a handshake that went through ends the wait, and WebSocket endpoints never wait.
func TestCodexRetryBackoff(t *testing.T) {
	unix := proto.CodexServer{ID: "s", Home: "/h", Endpoint: "unix:///h/app-server-control/app-server-control.sock"}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// fail makes n attempts at spec fail, each as soon as it is due, and returns the waits between them.
	fail := func(r *codexRegistry, spec proto.CodexServer, print string, at time.Time, n int) ([]time.Duration, time.Time) {
		var waits []time.Duration
		for range n {
			r.attempted(spec, at, print)
			r.attemptEnded(spec, false)
			next := at
			for !r.retryDue(spec, next, print) {
				next = next.Add(100 * time.Millisecond)
			}
			waits = append(waits, next.Sub(at)+codexRetrySlack)
			at = next
		}
		return waits, at
	}
	sec := func(waits []time.Duration) (out []int) {
		for _, w := range waits {
			out = append(out, int(w/time.Second))
		}
		return out
	}

	r := &codexRegistry{}
	waits, at := fail(r, unix, "p", t0, 7)
	if got, want := sec(waits), []int{5, 10, 20, 40, 60, 60, 60}; !slices.Equal(got, want) {
		t.Fatalf("waits %v, want %v", got, want)
	}
	r.attempted(unix, at, "p") // at: when the next was due
	r.attemptEnded(unix, false)
	if r.retryDue(unix, at.Add(time.Second), "p") {
		t.Fatal("due a second after a failed attempt")
	}
	if !r.retryDue(unix, at.Add(time.Second), "p2") {
		t.Fatal("a changed socket waited")
	}
	moved := unix
	moved.Endpoint = "unix:///h/other.sock"
	if !r.retryDue(moved, at.Add(time.Second), "p") {
		t.Fatal("another spec waited")
	}

	// A handshake that went through: the next failure waits 5 s again.
	r.attempted(unix, at, "p")
	r.attemptEnded(unix, true)
	if !r.retryDue(unix, at, "p") {
		t.Fatal("waited after a connection")
	}
	if waits, _ := fail(r, unix, "p", at, 2); !slices.Equal(sec(waits), []int{5, 10}) {
		t.Fatalf("after a connection: waits %v", sec(waits))
	}

	// WebSocket endpoints keep the 5 s of runCodex's ticks.
	tcp := proto.CodexServer{ID: "w", Home: "/h", Endpoint: "ws://127.0.0.1:1"}
	if waits, _ := fail(&codexRegistry{}, tcp, codexEndpointPrint(tcp.Endpoint), t0, 6); !slices.Equal(sec(waits), []int{2, 2, 2, 2, 2, 2}) {
		t.Fatalf("a WebSocket endpoint waited %v (0 s: due at once, shown plus the slack)", sec(waits))
	}
}

// A Unix endpoint whose socket is missing is not dialled again on the next call; once its app-server listens there,
// the next call joins it.
func TestCodexRemoteBackoffJoinsANewSocketAtOnce(t *testing.T) {
	n := remoteTestNode(t)
	home := t.TempDir()
	dir, err := os.MkdirTemp("/tmp", "cxb") // sun_path is short on macOS
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "app-server-control.sock")
	spec := proto.CodexServer{ID: "unix", Home: canonicalCodexHome(home, n.home), Endpoint: "unix://" + socket}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	retry := func() (codexRetry, bool) {
		n.codices.mu.Lock()
		defer n.codices.mu.Unlock()
		_, live := n.codices.remotes[spec.ID]
		return n.codices.retries[spec.ID], live
	}

	n.ensureCodexRemote(ctx, spec)
	waitFor(t, "the failed attempt", func() bool { b, live := retry(); return !live && b.fails == 1 })
	first, _ := retry()
	n.ensureCodexRemote(ctx, spec)
	if b, live := retry(); live || !b.at.Equal(first.at) {
		t.Fatal("dialled again at once, the socket still missing")
	}

	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(socket, 0o600)
	f := &remoteFixture{home: home, threads: []string{remoteRaw}}
	server := &http.Server{Handler: http.HandlerFunc(f.serve)}
	go server.Serve(ln)
	defer server.Close()
	n.ensureCodexRemote(ctx, spec)
	waitFor(t, "joined the new socket", func() bool {
		_, _, err := n.codexOwner(n.registerCodex(home, remoteRaw, ""))
		return err == nil
	})
	n.codices.mu.Lock()
	c := n.codices.remotes[spec.ID]
	n.codices.mu.Unlock()
	c.close()
	waitFor(t, "the backoff cleared", func() bool { b, live := retry(); return !live && b.fails == 0 })
}

package link

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/osproc"
)

func serveDirectHandoffLink(t *testing.T, hub *sshTestHub, w http.ResponseWriter, r *http.Request, onAccept func(*websocket.Conn), onEnd func()) {
	t.Helper()
	if r.Header.Get("Authorization") != "Bearer jd_ssh" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	if onAccept != nil {
		onAccept(c)
	}
	if onEnd != nil {
		defer onEnd()
	}
	var hello Message
	if _, b, err := c.Read(r.Context()); err != nil || json.Unmarshal(b, &hello) != nil || hello.Type != "hello" {
		return
	}
	sendJSON(t, c, `{"type":"endpoints","hub_name":"home"}`)
	sendJSON(t, c, `{"type":"node","online":true}`)
	hc := sshHubConn{hello: hello, conn: c, ended: make(chan struct{})}
	hub.links <- hc
	for {
		if _, _, err := c.Read(context.Background()); err != nil {
			close(hc.ended)
			return
		}
	}
}

func writeDirectHandoffConfig(t *testing.T, hub *sshTestHub, direct *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "link.json")
	if err := Update(path, func(c *Config) error {
		*c = Config{
			Hub: "h1", Machine: "laptop", FP: hub.pin, Token: "jd_ssh",
			Endpoints: []Endpoint{{URL: direct.URL, Kind: "lan"}},
			SSH:       &SSH{Server: "user@example.com", Target: hub.target, Always: false},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return path
}

func stopDirectHandoffRun(t *testing.T, cancel context.CancelFunc, done <-chan struct{}) {
	t.Helper()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("link Run did not stop")
	}
}

func TestDirectRecoveryKeepsSSHWhenWebSocketFails(t *testing.T) {
	ssh := newSSHHarness(t)
	hub := newSSHTestHub(t, "")
	var health, refused atomic.Int32
	refusedRequest := make(chan struct{}, 16)
	direct := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			health.Add(1)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case Path:
			refused.Add(1)
			refusedRequest <- struct{}{}
			http.Error(w, "websocket route unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	direct.TLS = hub.srv.TLS.Clone()
	direct.StartTLS()
	t.Cleanup(direct.Close)
	if pinOf(direct) != hub.pin {
		t.Fatal("direct endpoint does not have the pinned hub key")
	}
	path := writeDirectHandoffConfig(t, hub, direct)
	status := make(chan Status, 128)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Run(ctx, Options{
			Path: path, SSHBin: ssh.bin, PairCheck: 30 * time.Millisecond, DirectRetryEvery: 40 * time.Millisecond,
			Status: func(s Status) {
				select {
				case status <- s:
				default:
				}
			},
		})
	}()
	t.Cleanup(func() { stopDirectHandoffRun(t, cancel, done) })
	waitFor(t, status, func(s Status) bool { return s.State == "connected" && s.Kind == KindSSH })
	sshLink := <-hub.links

	// The initial direct failure happened before SSH started. Require two more refused WebSocket requests from the
	// periodic recovery probes; neither may displace the one working SSH process and link.
	wantRefused := refused.Load() + 2
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for refused.Load() < wantRefused {
		select {
		case <-refusedRequest:
		case <-sshLink.ended:
			t.Fatalf("healthy endpoint with a broken WebSocket route displaced SSH (health=%d, refused=%d)", health.Load(), refused.Load())
		case <-timer.C:
			t.Fatalf("direct recovery did not make repeated WebSocket attempts (health=%d, refused=%d)", health.Load(), refused.Load())
		}
	}
	select {
	case <-sshLink.ended:
		t.Fatal("working SSH link ended after a direct WebSocket failure")
	default:
	}
	if got := len(ssh.runs()); got != 1 {
		t.Fatalf("SSH process count = %d, want 1", got)
	}
}

func TestDirectRecoveryAdoptsAcceptedSocket(t *testing.T) {
	ssh := newSSHHarness(t)
	hub := newSSHTestHub(t, "")
	var allow atomic.Bool
	var dialed, accepted atomic.Int32
	var oldEndedBeforeAccept atomic.Bool
	dialStarted := make(chan struct{}, 1)
	releaseAccept := make(chan struct{})
	acceptedConn := make(chan *websocket.Conn, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseAccept) }) }
	var sshLink sshHubConn
	direct := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == Path {
			if !allow.Load() {
				http.Error(w, "websocket route unavailable", http.StatusServiceUnavailable)
				return
			}
			dialed.Add(1)
			select {
			case dialStarted <- struct{}{}:
			default:
			}
			<-releaseAccept
			serveDirectHandoffLink(t, hub, w, r, func(c *websocket.Conn) {
				accepted.Add(1)
				acceptedConn <- c
				select {
				case <-sshLink.ended:
					oldEndedBeforeAccept.Store(true)
				default:
				}
			}, nil)
			return
		}
		hub.srv.Config.Handler.ServeHTTP(w, r)
	}))
	direct.TLS = hub.srv.TLS.Clone()
	direct.StartTLS()
	t.Cleanup(direct.Close)
	path := writeDirectHandoffConfig(t, hub, direct)
	status := make(chan Status, 128)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Run(ctx, Options{
			Path: path, SSHBin: ssh.bin, PairCheck: 30 * time.Millisecond, DirectRetryEvery: 40 * time.Millisecond,
			Status: func(s Status) {
				select {
				case status <- s:
				default:
				}
			},
		})
	}()
	t.Cleanup(func() {
		release()
		stopDirectHandoffRun(t, cancel, done)
	})
	waitFor(t, status, func(s Status) bool { return s.State == "connected" && s.Kind == KindSSH })
	sshLink = <-hub.links
	allow.Store(true)
	select {
	case <-dialStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("direct recovery did not dial the WebSocket")
	}
	select {
	case <-sshLink.ended:
		t.Fatal("working SSH link ended before the direct WebSocket was accepted")
	default:
	}
	release()
	waitFor(t, status, func(s Status) bool {
		return s.State == "connected" && s.Kind == "lan" && s.Address == direct.URL
	})
	if got := dialed.Load(); got != 1 {
		t.Fatalf("direct recovery WebSocket dials = %d, want the one adopted candidate", got)
	}
	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted direct WebSockets = %d, want 1", got)
	}
	if oldEndedBeforeAccept.Load() {
		t.Fatal("working SSH link ended before the direct WebSocket was accepted")
	}
	var directLink sshHubConn
	select {
	case directLink = <-hub.links:
	case <-time.After(5 * time.Second):
		t.Fatal("adopted direct socket did not serve its hello")
	}
	select {
	case c := <-acceptedConn:
		if directLink.conn != c {
			t.Fatal("hello arrived on a different socket than the accepted direct candidate")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("direct candidate was not accepted")
	}
	if directLink.hello.Address != direct.URL {
		t.Fatalf("adopted direct hello address = %q, want %q", directLink.hello.Address, direct.URL)
	}
	select {
	case <-sshLink.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("SSH link stayed open after direct recovery")
	}
	if got := len(ssh.runs()); got != 1 {
		t.Fatalf("SSH process count = %d, want 1", got)
	}
}

func TestDirectRecoveryCancellationClosesUnadoptedCandidate(t *testing.T) {
	ssh := newSSHHarness(t)
	hub := newSSHTestHub(t, "")
	var allow atomic.Bool
	var accepted atomic.Int32
	acceptedCh := make(chan struct{}, 1)
	candidateEnded := make(chan struct{}, 1)
	direct := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == Path {
			if !allow.Load() {
				http.Error(w, "websocket route unavailable", http.StatusServiceUnavailable)
				return
			}
			serveDirectHandoffLink(t, hub, w, r, func(*websocket.Conn) {
				accepted.Add(1)
				acceptedCh <- struct{}{}
			}, func() { candidateEnded <- struct{}{} })
			return
		}
		hub.srv.Config.Handler.ServeHTTP(w, r)
	}))
	direct.TLS = hub.srv.TLS.Clone()
	direct.StartTLS()
	t.Cleanup(direct.Close)
	path := writeDirectHandoffConfig(t, hub, direct)
	status := make(chan Status, 128)
	handoffStarted := make(chan struct{})
	releaseHandoff := make(chan struct{})
	var handoffOnce, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseHandoff) }) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Run(ctx, Options{
			Path: path, SSHBin: ssh.bin, PairCheck: 30 * time.Millisecond, DirectRetryEvery: 40 * time.Millisecond,
			Status: func(s Status) {
				select {
				case status <- s:
				default:
				}
			},
			Logf: func(format string, args ...any) {
				if strings.Contains(fmt.Sprintf(format, args...), "direct hub reachable again") {
					handoffOnce.Do(func() {
						close(handoffStarted)
						<-releaseHandoff
					})
				}
			},
		})
	}()
	t.Cleanup(func() {
		release()
		stopDirectHandoffRun(t, cancel, done)
	})
	waitFor(t, status, func(s Status) bool { return s.State == "connected" && s.Kind == KindSSH })
	sshLink := <-hub.links
	allow.Store(true)
	select {
	case <-handoffStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("direct candidate did not reach the handoff")
	}
	select {
	case <-acceptedCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("direct candidate was not accepted before the handoff (accepted=%d)", accepted.Load())
	}
	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted direct candidates at handoff = %d, want 1", got)
	}
	select {
	case <-sshLink.ended:
		t.Fatal("SSH link ended before the winning direct candidate was handed off")
	default:
	}
	cancel()
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancellation during direct handoff")
	}
	select {
	case <-candidateEnded:
	case <-time.After(5 * time.Second):
		t.Fatal("unadopted winning direct candidate leaked after cancellation")
	}
	select {
	case <-sshLink.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not close the SSH link")
	}
	runs := ssh.runs()
	if len(runs) != 1 {
		t.Fatalf("SSH process count = %d, want 1", len(runs))
	}
	waitCondition(t, 5*time.Second, "SSH process cleanup after cancelled handoff", func() bool {
		return !osproc.AliveMine(runs[0].PID)
	})
}
